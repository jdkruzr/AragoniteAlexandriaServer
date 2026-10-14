package recognition

import (
	"context"
	"errors"
)

// NotebookSkipped reports whether the owner keeps a notebook out of recognition.
func (s Store) NotebookSkipped(ctx context.Context, source, id string) (bool, error) {
	var skipped bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alexandria_recognition_skip WHERE source=$1 AND notebook_id=$2)`, source, id).Scan(&skipped)
	return skipped, err
}

// SkipNotebook keeps a notebook out of recognition, or lets it back in. Skipping
// leaves recognized pages as they are (their text stays searchable) and skips
// every other queue entry; triggers skip pages added or changed later.
// Un-skipping releases only the pages the notebook skip covered: they become
// cancelled, ready to be recognized on request, and pages the owner skipped
// one by one stay skipped. It returns the number of pages changed.
func (s Store) SkipNotebook(ctx context.Context, source, id string, skip bool) (int64, error) {
	if source != "boox" && source != "client" {
		return 0, errors.New("Unknown source")
	}
	if id == "" || len(id) > 200 {
		return 0, errors.New("Invalid notebook")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	boox := `UPDATE boox_page_index SET state='skipped',detail=$2,request_version=request_version+1,lease_token='',lease_until=NULL,updated_at=now() WHERE notebook_id=$1 AND state NOT IN ('ready','skipped')`
	client := `UPDATE alexandria_page_dirty SET state='skipped',detail=$2,lease_token=NULL,lease_until=NULL WHERE page_id IN (SELECT id FROM fn_page WHERE notebook_id=$1) AND state NOT IN ('ready','skipped')`
	if skip {
		_, err = tx.ExecContext(ctx, `INSERT INTO alexandria_recognition_skip(source,notebook_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, source, id)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM alexandria_recognition_skip WHERE source=$1 AND notebook_id=$2`, source, id)
		boox = `UPDATE boox_page_index SET state='cancelled',detail='Notebook no longer skipped; recognize it when you like.',updated_at=now() WHERE notebook_id=$1 AND state='skipped' AND detail=$2`
		client = `UPDATE alexandria_page_dirty SET state='cancelled',detail='Notebook no longer skipped; recognize it when you like.' WHERE page_id IN (SELECT id FROM fn_page WHERE notebook_id=$1) AND state='skipped' AND detail=$2`
	}
	if err != nil {
		return 0, err
	}
	query := boox
	if source == "client" {
		query = client
	}
	res, err := tx.ExecContext(ctx, query, id, NotebookSkippedDetail)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}
