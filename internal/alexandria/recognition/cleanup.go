package recognition

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// Only an authoritative native deletion or a valid, nonempty page list proves
// removal. Missing/malformed metadata can be a sync gap and stays visible.
const obsoleteJobs = ` FROM alexandria_recognition_job j
 JOIN boox_projection p ON p.document_id=j.notebook_id
 WHERE j.source='boox' AND j.state='blocked' AND j.detail='Page was removed.'
 AND j.superseded_by IS NULL AND j.cleaned_at IS NULL
 AND p.domain='notebook' AND p.body->>'type'='1'
 AND p.body->>'uniqueId'=p.document_id AND NOT p.body ? 'commitType'
 AND (p.body->>'status'='0' OR (p.body->>'status'='1'
 AND jsonb_array_length(boox_search_page_ids(p.body->'pageNameList'))>0
 AND NOT boox_search_page_ids(p.body->'pageNameList') ? j.page_id))`

const cleanupOrder = ` ORDER BY j.notebook_id,j.page_id,j.id LIMIT 200`

type CleanupItem struct {
	ID, Notebook, Title, Page, Revision, Updated, Reason string
	Number                                               int
}

type CleanupPreview struct {
	Items []CleanupItem
	Token string
}

func cleanupPreview(ctx context.Context, query func(context.Context, string, ...any) (*sql.Rows, error), lock bool) (CleanupPreview, error) {
	var out CleanupPreview
	q := `SELECT j.id::text,j.notebook_id,coalesce(p.body->>'title',''),j.page_id,j.page_number,p.revision,j.updated_at::text,
 CASE WHEN p.body->>'status'='0' THEN 'Notebook deleted' ELSE 'Page removed' END` + obsoleteJobs + cleanupOrder
	if lock {
		q += ` FOR UPDATE OF j`
	}
	rows, err := query(ctx, q)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	h := sha256.New()
	for rows.Next() {
		var item CleanupItem
		if err = rows.Scan(&item.ID, &item.Notebook, &item.Title, &item.Page, &item.Number, &item.Revision, &item.Updated, &item.Reason); err != nil {
			return out, err
		}
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", item.ID, item.Revision, item.Updated)
		out.Items = append(out.Items, item)
	}
	out.Token = hex.EncodeToString(h.Sum(nil))
	return out, rows.Err()
}

func (s Store) PreviewCleanup(ctx context.Context) (CleanupPreview, error) {
	return cleanupPreview(ctx, s.DB.QueryContext, false)
}

// CleanObsolete hides exactly the reviewed jobs. Lock source metadata before
// the job ledger, matching the native projection -> queue -> ledger triggers.
func (s Store) CleanObsolete(ctx context.Context, token string) (int, error) {
	if len(token) != 64 {
		return 0, ErrSelectionChanged
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT p.document_id`+obsoleteJobs+cleanupOrder+` FOR SHARE OF p`)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
	}
	readErr := rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if readErr != nil {
		return 0, readErr
	}
	preview, err := cleanupPreview(ctx, tx.QueryContext, true)
	if err != nil {
		return 0, err
	}
	if preview.Token != token {
		return 0, ErrSelectionChanged
	}
	for _, item := range preview.Items {
		if _, err = tx.ExecContext(ctx, `UPDATE alexandria_recognition_job SET cleaned_at=clock_timestamp() WHERE id=$1`, item.ID); err != nil {
			return 0, err
		}
	}
	return len(preview.Items), tx.Commit()
}
