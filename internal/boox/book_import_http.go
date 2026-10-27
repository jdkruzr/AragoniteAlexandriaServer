package boox

import (
	"encoding/json"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	"io"
	"net/http"
)

func (s Service) bookImportHTTP(w http.ResponseWriter, r *http.Request) {
	localizer := web.Deps{}.Localizer(r)
	failure := func(e error) { reply(w, 400, map[string]string{"error": localizer.T(importPublicError(e))}) }
	if r.Method == http.MethodGet && r.URL.Path == "/boox/books/imports" {
		v, e := s.bookImportStatus(r.Context(), r.URL.Query().Get("id"))
		if e != nil {
			failure(e)
			return
		}
		for n := range v.Items {
			v.Items[n].Error = localizer.T(v.Items[n].Error)
		}
		reply(w, 200, v)
		return
	}
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/boox/books/imports/file" {
		r.Body = http.MaxBytesReader(w, r.Body, maxBook+(1<<20))
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
			failure(importProblem("Choose a file up to 128 MiB."))
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, _, e := r.FormFile("book")
		if e != nil {
			failure(importProblem("Choose a book file."))
			return
		}
		defer f.Close()
		raw, e := io.ReadAll(io.LimitReader(f, maxBook+1))
		if e != nil {
			failure(e)
			return
		}
		op, e := s.acceptImportFile(r.Context(), r.FormValue("id"), r.FormValue("path"), raw)
		if e != nil {
			_, _ = s.DB.ExecContext(r.Context(), `UPDATE boox_book_import_item i SET error_code=$3,updated_at=now() FROM boox_book_import b WHERE b.id=i.import_id AND b.native_uid=$4 AND b.id::text=$1 AND i.path=$2 AND i.state='waiting'`, r.FormValue("id"), r.FormValue("path"), importPublicError(e), s.uid())
			failure(e)
			return
		}
		reply(w, 200, map[string]string{"operation": op})
		return
	}
	if r.URL.Path == "/boox/books/imports/retry" {
		var req struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		}
		if e := decode(r, &req); e != nil {
			failure(importProblem("Invalid import request."))
			return
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			failure(e)
			return
		}
		defer tx.Rollback()
		generation, e := s.publicationOwner(r.Context(), tx)
		if e != nil {
			failure(e)
			return
		}
		_, e = tx.ExecContext(r.Context(), `UPDATE boox_book_import_item i SET state='queued',error_code='',next_attempt_at=now() FROM boox_book_import b WHERE i.import_id=b.id AND b.id::text=$1 AND b.native_uid=$2 AND b.generation=$3 AND i.path=$4 AND i.kind='file' AND i.state IN ('blocked','queued') AND i.operation_id IS NOT NULL`, req.ID, s.uid(), generation, req.Path)
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			failure(e)
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.URL.Path != "/boox/books/imports" && r.URL.Path != "/boox/books/imports/preview" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var req importRequest
	if e := d.Decode(&req); e != nil {
		failure(importProblem("Invalid import request."))
		return
	}
	if d.Decode(new(any)) != io.EOF {
		failure(importProblem("Invalid import request."))
		return
	}
	var v importView
	var e error
	if r.URL.Path == "/boox/books/imports/preview" {
		var generation string
		e = s.DB.QueryRowContext(r.Context(), `SELECT generation FROM sync_library_generation WHERE id=1`).Scan(&generation)
		if e == nil {
			v, e = s.planBookImport(r.Context(), req, generation)
		}
	} else {
		v, e = s.createBookImport(r.Context(), req)
	}
	if e != nil {
		failure(e)
		return
	}
	for n := range v.Items {
		if v.Items[n].Error != "" {
			v.Items[n].Error = localizer.T(v.Items[n].Error)
		}
	}
	reply(w, 200, v)
}
