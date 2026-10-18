package boox

import (
	"encoding/json"
	"errors"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxvisibility"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
	"html/template"
	"net/http"
	"strconv"
)

var booxSettings = template.Must(template.New("settings.html").Funcs(viewFuncs()).ParseFS(templateFS, "templates/settings.html"))

type readingItem struct {
	ID, Title, Quote, Note, Progress, Page, Kind string
	CreatedAt, ModifiedAt                        int64
}
type readingView struct {
	Listing        listview.State
	Items          []readingItem
	Next, Previous string
}

var readingTemplate = template.Must(template.New("reading.html").Funcs(listFuncs()).ParseFS(templateFS, "templates/reading.html"))

func (s Service) reading(w http.ResponseWriter, r *http.Request) {
	state := listview.Parse(r.URL.Query())
	offset := state.Offset
	rows, e := s.DB.QueryContext(r.Context(), `SELECT document_id,body,count(*) OVER() FROM boox_projection p WHERE `+booxvisibility.Reading("p")+` AND native_uid=$1 AND ($2='' OR document_id=$2) ORDER BY `+state.NativeOrder()+` LIMIT 60 OFFSET $3`, s.uid(), r.URL.Query().Get("record"), offset)
	if e != nil {
		s.failure(w, "reading", e)
		return
	}
	defer rows.Close()
	d := readingView{Listing: state}
	for rows.Next() {
		var v readingItem
		var b []byte
		if rows.Scan(&v.ID, &b, &d.Listing.Total) != nil {
			s.failure(w, "reading", errors.New("reading inventory failed"))
			return
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		str := func(k string) string {
			if s, ok := m[k].(string); ok {
				return s
			}
			if n, ok := m[k].(float64); ok {
				return strconv.FormatFloat(n, 'f', -1, 64)
			}
			return ""
		}
		v.CreatedAt, _ = strconv.ParseInt(str("createdAt"), 10, 64)
		v.ModifiedAt, _ = strconv.ParseInt(str("updatedAt"), 10, 64)
		v.Title = str("title")
		if v.Title == "" {
			v.Title = str("name")
		}
		if v.Title == "" {
			v.Title = "Untitled reading record"
		}
		v.Quote = str("quote")
		v.Note = str("note")
		v.Progress = str("progress")
		v.Page = str("pageNumber")
		v.Kind = map[string]string{"1": "Annotation", "2": "Bookmark", "4": "Book"}[str("modeType")]
		d.Items = append(d.Items, v)
	}
	if rows.Err() != nil {
		s.failure(w, "reading", rows.Err())
		return
	}
	if len(d.Items) == 0 && offset > 0 {
		q := r.URL.Query()
		q.Del("offset")
		http.Redirect(w, r, "/boox/reading?"+q.Encode(), http.StatusSeeOther)
		return
	}
	d.Listing.Finish(r.URL.Query(), "/boox/reading", d.Listing.Total)
	s.renderPage(w, r, readingTemplate, "BOOX Reading", d)
}
