package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// The Library's preview of one notebook: its first pages, what the server
// holds, recognition coverage, tags, minutes and queued changes, with the
// actions that apply to it. It loads only for the selected notebook.

// previewThumbs is how many pages the preview shows.
const previewThumbs = 8

type thumbLink struct {
	Number      int
	Image, Href string
}
type pendingOp struct{ Action, State string }

type notebookPreview struct {
	notebookManagementItem
	Entry       notebookEntry
	FolderTitle string
	Count       int
	Thumbs      []thumbLink
	More        int
	TypedText   bool
	Recognized  int
	Tags        []string
	Recordings  int
	Minutes     string
	Pending     []pendingOp
	Open        string
}

// notebookPreview returns nil when id isn't a notebook of this library.
func (s Service) notebookPreview(ctx context.Context, id string) (*notebookPreview, error) {
	var revision string
	var raw []byte
	e := s.DB.QueryRowContext(ctx, `SELECT revision,body FROM boox_projection WHERE document_id=$1 AND native_uid=$2 AND `+notebookPredicate+` AND body->>'type'='1'`, id, s.uid()).Scan(&revision, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	entry, e := parseNotebookEntry(id, revision, raw)
	if e != nil {
		return nil, e
	}
	var b map[string]any
	if e = nativeJSON(raw, &b); e != nil {
		return nil, e
	}
	deleted := fmt.Sprint(b["status"]) == "0"
	items, e := s.managementItems(ctx, []notebookChoice{{ID: id, Revision: revision}}, deleted)
	if errors.Is(e, errNotebookSelection) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	p := &notebookPreview{notebookManagementItem: items[0], Entry: entry, Open: "/boox/notebook?id=" + url.QueryEscape(id)}
	p.FolderTitle = p.ParentTitle
	if !p.Deleted && p.Parent != "" {
		if e = s.DB.QueryRowContext(ctx, `SELECT coalesce(body->>'title','') FROM boox_projection WHERE document_id=$1 AND native_uid=$2`, p.Parent, s.uid()).Scan(&p.FolderTitle); e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
	}
	n, e := s.notebookSnapshot(ctx, id)
	if e != nil {
		return nil, e
	}
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	p.Count = len(ids)
	p.TypedText = n.Meta.ActiveScene == 1
	if !p.TypedText {
		for i := range min(previewThumbs, len(ids)) {
			num := strconv.Itoa(i + 1)
			p.Thumbs = append(p.Thumbs, thumbLink{Number: i + 1,
				Image: "/boox/thumb.png?id=" + url.QueryEscape(id) + "&page=" + num + "&version=" + n.Version,
				Href:  p.Open + "&page=" + num})
		}
		p.More = len(ids) - len(p.Thumbs)
	}
	pages, _ := json.Marshal(ids)
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_page_index WHERE notebook_id=$1 AND state='ready' AND page_id IN (SELECT jsonb_array_elements_text($2::jsonb))`, id, string(pages)).Scan(&p.Recognized); e != nil {
		return nil, e
	}
	if _, p.Tags, e = s.notebookTags(ctx, id); e != nil {
		return nil, e
	}
	p.Recordings = len(audioKeys(n))
	if e = s.DB.QueryRowContext(ctx, `SELECT state FROM alexandria_note_minutes WHERE source='boox' AND notebook_id=$1`, id).Scan(&p.Minutes); e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT coalesce(payload->>'managementAction',''),state,started_at IS NOT NULL FROM boox_operation WHERE document_id=$1 AND payload ? 'managementBatch' AND state IN ('queued','purging_native','purging_server','purging_objects') ORDER BY created_at`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var op pendingOp
		var started bool
		if e = rows.Scan(&op.Action, &op.State, &started); e != nil {
			return nil, e
		}
		if op.State == "queued" && started {
			op.State = "publishing"
		}
		p.Pending = append(p.Pending, op)
	}
	return p, rows.Err()
}

// moveFolders are the folders a notebook can be moved to, root first, each
// titled with its path ("Work / 2026").
func (s Service) moveFolders(ctx context.Context) ([]notebookFolder, error) {
	out := []notebookFolder{{Title: "Notebook root", Choice: `{"ID":"","Revision":""}`}}
	rows, e := s.DB.QueryContext(ctx, `SELECT document_id,revision,body FROM boox_projection WHERE domain='notebook' AND body->>'type'='0' AND body->>'status'='1' ORDER BY lower(body->>'title'),document_id`)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var f notebookFolder
		var raw []byte
		if e = rows.Scan(&f.ID, &f.Revision, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		var b map[string]any
		if nativeJSON(raw, &b) != nil || !folderMetadata(b, s.uid()) {
			continue
		}
		f.Title, _ = b["title"].(string)
		f.Parent, _ = b["parentUniqueId"].(string)
		raw, _ = json.Marshal(f)
		f.Choice = string(raw)
		out = append(out, f)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	folders := map[string]notebookFolder{}
	for _, f := range out {
		folders[f.ID] = f
	}
	for i := range out {
		f := &out[i]
		parent := f.Parent
		seen := map[string]bool{f.ID: true}
		for parent != "" && !seen[parent] && len(seen) < 64 {
			p, ok := folders[parent]
			if !ok {
				break
			}
			seen[parent] = true
			f.Title = p.Title + " / " + f.Title
			parent = p.Parent
		}
	}
	return out, nil
}
