package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxvisibility"
)

// A tablet recomputes readingStatus whenever its reader saves: Finished only
// while the last page is showing, In Progress otherwise. A mark is what the
// reader says instead, and it holds until removed.
const (
	markFinished   = "finished"
	markNotReading = "not_reading"
)

// readingMarkJoin adds a book's mark (alias k) to a query over projection p.
const readingMarkJoin = ` LEFT JOIN boox_reading_mark k ON k.native_uid=p.native_uid AND k.book_id=p.body->>'uniqueId' `

// readingStatusSQL is a book's status: its mark ("2" finished, "3" not
// reading) or else what the tablets last reported.
const readingStatusSQL = `coalesce(CASE k.mark WHEN 'finished' THEN '2' WHEN 'not_reading' THEN '3' END,p.body->>'readingStatus')`

// readingMarkColumns are the mark and when it was set (Unix ms, 0 if none).
const readingMarkColumns = `coalesce(k.mark,''),coalesce((extract(epoch FROM k.marked_at)*1000)::bigint,0)`

// setReadingMarks marks visible books (by native ID) finished or not reading;
// an empty mark removes theirs. Nothing is sent to tablets.
func (s Service) setReadingMarks(ctx context.Context, books []string, mark string) error {
	ids, _ := json.Marshal(books)
	if mark == "" {
		_, e := s.DB.ExecContext(ctx, `DELETE FROM boox_reading_mark WHERE native_uid=$1 AND book_id IN (SELECT jsonb_array_elements_text($2::jsonb))`, s.uid(), string(ids))
		return e
	}
	// Marking again keeps the date of the first time.
	_, e := s.DB.ExecContext(ctx, `INSERT INTO boox_reading_mark(native_uid,book_id,mark) SELECT DISTINCT p.native_uid,p.body->>'uniqueId',$3 FROM boox_projection p WHERE p.native_uid=$1 AND p.body->>'modeType'='4' AND `+booxvisibility.Reading("p")+` AND p.body->>'uniqueId' IN (SELECT jsonb_array_elements_text($2::jsonb))
 ON CONFLICT(native_uid,book_id) DO UPDATE SET mark=excluded.mark,marked_at=now() WHERE boox_reading_mark.mark<>excluded.mark`, s.uid(), string(ids), mark)
	return e
}

// carryReadingMark gives a new book the latest mark of the records it replaces.
func (s Service) carryReadingMark(ctx context.Context, tx *sql.Tx, book string, replaced []adoptCandidate) error {
	var old []string
	for _, c := range replaced {
		old = append(old, nativeString(c.Body, "uniqueId"))
	}
	ids, _ := json.Marshal(old)
	_, e := tx.ExecContext(ctx, `INSERT INTO boox_reading_mark(native_uid,book_id,mark,marked_at) SELECT native_uid,$2,mark,marked_at FROM boox_reading_mark WHERE native_uid=$1 AND book_id IN (SELECT jsonb_array_elements_text($3::jsonb)) ORDER BY marked_at DESC LIMIT 1 ON CONFLICT DO NOTHING`, s.uid(), book, string(ids))
	return e
}

func (s Service) readingMarkHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if e := r.ParseForm(); e != nil {
		http.Error(w, "Invalid form", 400)
		return
	}
	back := bookReturn(r.PostForm.Get("return"))
	mark, ids := r.PostForm.Get("mark"), r.PostForm["id"]
	if mark == "none" {
		mark = ""
	} else if mark != markFinished && mark != markNotReading {
		http.Error(w, "Invalid form", 400)
		return
	}
	if len(ids) == 0 || len(ids) > cleanupLimit {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if e := s.ResolveIdentity(r.Context()); e != nil {
		s.failure(w, "readingMarkHTTP", e)
		return
	}
	if e := s.setReadingMarks(r.Context(), ids, mark); e != nil {
		s.failure(w, "readingMarkHTTP", e)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
