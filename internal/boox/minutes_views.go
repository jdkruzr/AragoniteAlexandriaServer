package boox

import (
	"bytes"
	"database/sql"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

type minutesView struct {
	ID, Title, Progress string
	Has, Stale, Enabled bool
	Recordings          int
	Stored              minutes.Stored
	// Sending to BOOX; Sendable is false for a deleted notebook.
	Sendable                       bool
	NoteTitle, Folder, SendRequest string
	Sent                           *sentNote
}

var minutesTemplate = template.Must(template.New("minutes.html").Funcs(web.Funcs()).ParseFS(templateFS, "templates/minutes.html"))

type minutesRequestView struct {
	ID, Title, Version, Request           string
	Transcriber, Summarizer               string
	Enabled, RecognitionEnabled, AutoSend bool
	Recordings                            []minutes.Source
	Pages, WithText, Missing, InProgress  int
}

var minutesRequestTemplate = template.Must(template.New("minutes-new.html").Funcs(web.Funcs()).ParseFS(templateFS, "templates/minutes-new.html"))

func (s Service) minutesPage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	n, e := s.notebookSnapshot(r.Context(), id)
	if errors.Is(e, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		failure(w, e)
		return
	}
	stored, has, e := minutes.Load(r.Context(), s.DB, "boox", id)
	if e != nil {
		failure(w, e)
		return
	}
	d := minutesView{ID: id, Title: n.Meta.Title, Has: has, Stored: stored, Enabled: s.Minutes.Enabled, Recordings: len(audioKeys(n))}
	if has && stored.Topic != "" && n.Meta.Status == 1 {
		d.Sendable, d.NoteTitle, d.Folder, d.SendRequest = true, n.Meta.Title+"_Minutes", s.folderTitle(r.Context(), n.Meta.ParentUniqueID), uuid.NewString()
		if d.Sent, e = s.sentMinutesNote(r.Context(), id); e != nil {
			failure(w, e)
			return
		}
	}
	if has && stored.State == "ready" {
		pages, e := s.pageTexts(r.Context(), n)
		if e != nil {
			failure(w, e)
			return
		}
		d.Stale = minutesInputHash(recordingIDs(n), pages, s.Minutes.Instructions) != stored.InputHash
	}
	if has && (stored.State == "queued" || stored.State == "processing") {
		d.Progress = s.minutesProgress(r, n, stored)
	}
	s.renderPage(w, r, minutesTemplate, "Audio Note Minutes · "+n.Meta.Title, d)
}

func (s Service) minutesProgress(r *http.Request, n notebookSnapshot, m minutes.Stored) string {
	done := 0
	for _, t := range m.Recordings {
		if t.State == "ready" {
			done++
		}
	}
	if done < len(m.Recordings) {
		return "Transcribing recordings: " + strconv.Itoa(done) + " of " + strconv.Itoa(len(m.Recordings)) + " done."
	}
	pages, e := s.pageTexts(r.Context(), n)
	if e == nil {
		waiting := 0
		for _, p := range pages {
			if pending(p) {
				waiting++
			}
		}
		if waiting > 0 {
			if waiting == 1 {
				return "Waiting for handwriting recognition on 1 page."
			}
			return "Waiting for handwriting recognition on " + strconv.Itoa(waiting) + " pages."
		}
	}
	return "Writing the minutes."
}

func (s Service) minutesRequestPage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	n, e := s.notebookSnapshot(r.Context(), id)
	if errors.Is(e, sql.ErrNoRows) || e == nil && n.Meta.Status != 1 {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		failure(w, e)
		return
	}
	pages, e := s.pageTexts(r.Context(), n)
	if e != nil {
		failure(w, e)
		return
	}
	d := minutesRequestView{ID: id, Title: n.Meta.Title, Version: n.Version, Request: uuid.NewString(), Transcriber: s.Minutes.Transcriber, Summarizer: s.Minutes.Summarizer, Enabled: s.Minutes.Enabled, AutoSend: s.Minutes.AutoSend, RecognitionEnabled: s.RecognitionEnabled, Recordings: s.recordings(r.Context(), n), Pages: len(pages)}
	for _, p := range pages {
		switch {
		case p.State == "ready":
			d.WithText++
		case pending(p):
			d.InProgress++
		default:
			d.Missing++
		}
	}
	s.renderPage(w, r, minutesRequestTemplate, "Summarize · "+n.Meta.Title, d)
}

func (s Service) minutesRequestHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	id := r.Form.Get("id")
	back := "/boox/minutes/new?id=" + url.QueryEscape(id)
	if r.Form.Get("confirm") != "on" {
		http.Redirect(w, r, back+"&notice="+url.QueryEscape("Confirm sending the recordings and text before summarizing."), http.StatusSeeOther)
		return
	}
	e := s.RequestMinutes(r.Context(), id, r.Form.Get("request"), r.Form.Get("version"), r.Form.Get("recognize") == "on", s.Minutes)
	if errors.Is(e, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		http.Redirect(w, r, back+"&notice="+url.QueryEscape(e.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/boox/minutes?id="+url.QueryEscape(id)+"&notice="+url.QueryEscape("Audio Note Minutes requested. Recordings are transcribed first; this page shows progress."), http.StatusSeeOther)
}

func (s Service) minutesControlHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	id := r.Form.Get("id")
	if e := s.ControlMinutes(r.Context(), id, r.Form.Get("action")); e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	http.Redirect(w, r, "/boox/minutes?id="+url.QueryEscape(id), http.StatusSeeOther)
}

// minutesAudio streams a decrypted recording to the owner, with Range support.
func (s Service) minutesAudio(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	n, e := s.notebookSnapshot(r.Context(), q.Get("id"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	key := q.Get("key")
	if a, ok := n.Assets[key]; !ok || a.SHA != q.Get("sha") {
		http.Error(w, "This recording changed or was removed; reload the minutes.", http.StatusNotFound)
		return
	}
	audio, name, e := s.recordingAudio(r.Context(), n, key)
	if e != nil {
		http.Error(w, "The recording could not be read.", http.StatusUnprocessableEntity)
		return
	}
	kind := "audio/mpeg"
	if path.Ext(name) == ".wav" {
		kind = "audio/wav"
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", `"`+q.Get("sha")+`"`)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(audio))
}
