package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

var errNotebookSelection = errors.New(i18n.M("The notebooks or destination changed. Review the selection again; nothing was queued."))
var errNotebookTitle = errors.New(i18n.M("Enter a title of 1–1024 bytes without control characters."))

type notebookChoice struct {
	ID, Revision, Title, Parent, ParentRevision string
	AssetsVersion                               string
}
type notebookIntent struct {
	Token, Action, Return string
	Choices               []notebookChoice
	Numbered              bool
}
type notebookManagementItem struct {
	notebookChoice
	Choice, State, Detail   string
	ParentTitle             string
	Deleted, RootFallback   bool
	Assets, Revisions, Jobs int
	Bytes                   int64
	PurgeFiles              int
	PurgeBytes              int64
}

// Serializes source mutation, publication and GC within one library. Provider
// calls and rendering never hold it. Keep it before row locks in these paths.
func notebookWriteLock(ctx context.Context, tx *sql.Tx) error {
	_, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('boox-native-writes',0))`)
	return e
}
func notebookReturn(value string) string {
	u, e := url.Parse(value)
	if e != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || (u.Path != "/boox" && u.Path != "/boox/notebook") {
		return "/boox"
	}
	return u.RequestURI()
}
func validNotebookTitle(title string) bool {
	return utf8.ValidString(title) && strings.TrimSpace(title) != "" && len(title) <= 1024 && strings.IndexFunc(title, unicode.IsControl) < 0
}
func folderMetadata(b map[string]any, uid string) bool {
	id, _ := b["_id"].(string)
	return id != "" && b["uniqueId"] == id && b["user"] == uid && b["dbId"] == uid+"-NOTE_TREE" && fmt.Sprint(b["type"]) == "0" && fmt.Sprint(b["status"]) == "1" && b["commitType"] == nil
}

// managementItems describes the chosen notebooks as they stand now. The
// bytes only they hold (what permanent removal frees) are slow to count and
// are left at zero unless withPurge.
func (s Service) managementItems(ctx context.Context, choices []notebookChoice, withPurge bool) ([]notebookManagementItem, error) {
	if len(choices) == 0 || len(choices) > cleanupLimit {
		return nil, errNotebookSelection
	}
	var out []notebookManagementItem
	for _, c := range choices {
		var raw []byte
		v := notebookManagementItem{notebookChoice: c}
		e := s.DB.QueryRowContext(ctx, `SELECT revision,body FROM boox_projection WHERE document_id=$1 AND NOT EXISTS(SELECT 1 FROM boox_notebook_purged WHERE notebook_id=$1)`, c.ID).Scan(&v.Revision, &raw)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, errNotebookSelection
		}
		if e != nil {
			return nil, e
		}
		var b map[string]any
		if nativeJSON(raw, &b) != nil || !noteMetadata(b, s.uid()) || c.Revision != "" && c.Revision != v.Revision {
			return nil, errNotebookSelection
		}
		v.Title, _ = b["title"].(string)
		v.Parent, _ = b["parentUniqueId"].(string)
		v.Deleted = fmt.Sprint(b["status"]) == "0"
		if v.Deleted && v.Parent != "" {
			var fraw []byte
			e = s.DB.QueryRowContext(ctx, `SELECT revision,body FROM boox_projection WHERE document_id=$1`, v.Parent).Scan(&v.ParentRevision, &fraw)
			var f map[string]any
			if errors.Is(e, sql.ErrNoRows) || e == nil && (nativeJSON(fraw, &f) != nil || !folderMetadata(f, s.uid())) {
				v.Parent = ""
				v.ParentRevision = ""
				v.RootFallback = true
			} else if e != nil {
				return nil, e
			} else {
				v.ParentTitle, _ = f["title"].(string)
			}
		}
		prefix := s.uid() + "/note/" + c.ID + "/"
		e = s.DB.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(v.bytes),0) FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE (starts_with(l.native_key,$1) OR l.native_key=rtrim($1,'/')||'.png')`, prefix).Scan(&v.Assets, &v.Bytes)
		if e != nil {
			return nil, e
		}
		e = s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM boox_revision WHERE document_id=$1),(SELECT count(*) FROM alexandria_recognition_job WHERE source='boox' AND notebook_id=$1)`, c.ID).Scan(&v.Revisions, &v.Jobs)
		if e != nil {
			return nil, e
		}
		v.AssetsVersion, e = notebookAssetVersion(ctx, s.DB, s.uid(), c.ID)
		if e != nil {
			return nil, e
		}
		if withPurge {
			e = s.DB.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(bytes),0) FROM (SELECT v.sha256,max(v.bytes) bytes FROM boox_blob_version v WHERE (starts_with(v.native_key,$1) OR v.native_key=rtrim($1,'/')||'.png') AND v.sha256 IS NOT NULL AND NOT EXISTS(SELECT 1 FROM boox_blob_version other WHERE other.sha256=v.sha256 AND NOT (starts_with(other.native_key,$1) OR other.native_key=rtrim($1,'/')||'.png')) GROUP BY v.sha256) exclusive`, prefix).Scan(&v.PurgeFiles, &v.PurgeBytes)
			if e != nil {
				return nil, e
			}
		}
		raw, _ = json.Marshal(v.notebookChoice)
		v.Choice = string(raw)
		out = append(out, v)
	}
	return out, nil
}

func (s Service) queueNotebookManagement(ctx context.Context, in notebookIntent) error {
	if _, e := uuid.Parse(in.Token); e != nil {
		return errNotebookSelection
	}
	if len(in.Choices) == 0 || len(in.Choices) > cleanupLimit {
		return errNotebookSelection
	}
	switch in.Action {
	case "rename", "move", "trash", "restore", "purge":
	default:
		return errNotebookSelection
	}
	in.Return = notebookReturn(in.Return)
	sort.Slice(in.Choices, func(i, j int) bool { return in.Choices[i].ID < in.Choices[j].ID })
	for i, c := range in.Choices {
		if c.ID == "" || c.Revision == "" || i > 0 && in.Choices[i-1].ID == c.ID {
			return errNotebookSelection
		}
		if in.Action == "rename" && !validNotebookTitle(c.Title) {
			return errNotebookTitle
		}
	}
	raw, _ := json.Marshal(in)
	fingerprint := hash(raw)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = notebookWriteLock(ctx, tx); e != nil {
		return e
	}
	var previous string
	e = tx.QueryRowContext(ctx, `SELECT fingerprint FROM boox_notebook_batch WHERE id=$1`, in.Token).Scan(&previous)
	if e == nil {
		if previous != fingerprint {
			return errNotebookSelection
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	var generation string
	if e = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); e != nil {
		return e
	}
	for _, c := range in.Choices {
		var revision string
		var raw []byte
		e = tx.QueryRowContext(ctx, `SELECT revision,body FROM boox_projection WHERE document_id=$1 AND NOT EXISTS(SELECT 1 FROM boox_notebook_purged WHERE notebook_id=$1) FOR SHARE`, c.ID).Scan(&revision, &raw)
		if errors.Is(e, sql.ErrNoRows) {
			return errNotebookSelection
		}
		if e != nil {
			return e
		}
		var b map[string]any
		if nativeJSON(raw, &b) != nil || !noteMetadata(b, s.uid()) || revision != c.Revision {
			return errNotebookSelection
		}
		status := fmt.Sprint(b["status"])
		if (in.Action == "restore" || in.Action == "purge") && status != "0" || in.Action != "restore" && in.Action != "purge" && status != "1" {
			return errNotebookSelection
		}
		if in.Action == "purge" {
			version, ve := notebookAssetVersion(ctx, tx, s.uid(), c.ID)
			if ve != nil {
				return ve
			}
			if c.AssetsVersion == "" || version != c.AssetsVersion {
				return errNotebookSelection
			}
		}
		if in.Numbered && !cleanupName.MatchString(fmt.Sprint(b["title"])) {
			return errNotebookSelection
		}
		if (in.Action == "move" || in.Action == "restore") && c.Parent != "" {
			var fr string
			var fb []byte
			e = tx.QueryRowContext(ctx, `SELECT revision,body FROM boox_projection WHERE document_id=$1 FOR SHARE`, c.Parent).Scan(&fr, &fb)
			if errors.Is(e, sql.ErrNoRows) {
				return errNotebookSelection
			}
			if e != nil {
				return e
			}
			var f map[string]any
			if nativeJSON(fb, &f) != nil || !folderMetadata(f, s.uid()) || fr != c.ParentRevision {
				return errNotebookSelection
			}
		}
		payload := map[string]any{"managementBatch": in.Token, "managementAction": in.Action, "managementTitle": b["title"], "generation": generation, "updatedAt": time.Now().UnixMilli()}
		kind := "set_note_status"
		switch in.Action {
		case "rename":
			kind = "rename_note"
			payload["title"] = c.Title
		case "move":
			kind = "move_note"
			payload["parent"] = c.Parent
			payload["parentRevision"] = c.ParentRevision
		case "trash":
			payload["status"] = 0
		case "restore":
			payload["status"] = 1
			payload["parent"] = c.Parent
			payload["parentRevision"] = c.ParentRevision
		case "purge":
			kind = "purge_note"
			payload["assetsVersion"] = c.AssetsVersion
		}
		if in.Numbered {
			payload["cleanupBatch"] = in.Token
			payload["cleanupTitle"] = b["title"]
		}
		p, _ := json.Marshal(payload)
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("boox-management:"+in.Token+":"+c.ID)).String()
		_, e = tx.ExecContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload,state) VALUES($1,$2,$3,$4,$5,$6,'queued')`, id, "notebook:"+id, kind, c.ID, c.Revision, p)
		if e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO boox_notebook_batch(id,fingerprint,return_to) VALUES($1,$2,$3)`, in.Token, fingerprint, in.Return); e != nil {
		return e
	}
	return tx.Commit()
}

// Version identity covers historical as well as live assets, so permanent-removal
// confirmation cannot silently absorb a newly uploaded attachment.
type notebookQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func notebookAssetVersion(ctx context.Context, db notebookQuery, uid, id string) (string, error) {
	var version string
	e := db.QueryRowContext(ctx, `SELECT md5(coalesce(string_agg(id::text||':'||coalesce(sha256,''),',' ORDER BY id),'')) FROM boox_blob_version WHERE (starts_with(native_key,$1) OR native_key=rtrim($1,'/')||'.png')`, uid+"/note/"+id+"/").Scan(&version)
	return version, e
}

// BOOX thumbnails are siblings of the notebook directory, not inside it.
func nativeNotebookKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) < 3 || parts[1] != "note" {
		return ""
	}
	if len(parts) >= 4 {
		return parts[2]
	}
	if strings.HasSuffix(parts[2], ".png") {
		return strings.TrimSuffix(parts[2], ".png")
	}
	return ""
}
