package web

import (
	"errors"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/noteactions"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (d Deps) providerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /settings/semantic", func(w http.ResponseWriter, r *http.Request) {
		if d.Providers == nil {
			http.Error(w, "Provider settings are unavailable.", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid settings form.", 400)
			return
		}
		rev, err := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
		if err != nil {
			http.Error(w, "Reload the Settings page.", 409)
			return
		}
		snap, err := d.Providers.Load(r.Context())
		if err != nil {
			d.fail(w, err)
			return
		}
		if snap.Revision != rev {
			back(w, r, "/settings/search", providers.ErrStale.Error())
			return
		}
		action := r.PostForm.Get("action")
		if action == "enable_build" || action == "enable" || action == "build" || action == "test" {
			// Explicit actions may run a synthetic inference; viewing Settings never does.
			if err = snap.TestConnection(r.Context(), "embedding"); err != nil {
				back(w, r, "/settings/search", err.Error())
				return
			}
		}
		var notice string
		switch action {
		case "enable_build":
			err = d.Providers.EnableAndBuildIndex(r.Context(), rev)
			notice = "Semantic search enabled. Existing searchable text queued for indexing; results become available when the index is ready."
		case "enable":
			err = d.Providers.SetEmbeddingEnabled(r.Context(), rev, true)
			notice = "Semantic search enabled. Any ready index is available and unfinished work can resume."
		case "disable":
			err = d.Providers.SetEmbeddingEnabled(r.Context(), rev, false)
			notice = "Semantic search is off. New embedding work is paused; in-flight requests may finish. The index is retained and keyword search remains available."
		case "build":
			err = d.Providers.BuildIndex(r.Context(), rev)
			notice = "Index build queued. Any previous index stays available until the replacement is ready."
		case "retry":
			err = d.Providers.RetryIndex(r.Context())
			notice = "Failed index work queued for retry."
		case "test":
			notice = "Embedding connection test passed using a synthetic sentence. No notes were sent and settings are unchanged."
		default:
			http.Error(w, "Unknown semantic search action.", 400)
			return
		}
		if errors.Is(err, providers.ErrStale) {
			notice = providers.ErrStale.Error()
		} else if err != nil {
			notice = "Semantic search settings were not changed. Reload and check the saved provider and deployment settings."
		}
		back(w, r, "/settings/search", notice)
	})
	mux.HandleFunc("POST /settings/automatic", func(w http.ResponseWriter, r *http.Request) {
		if d.Providers == nil {
			http.Error(w, "Provider settings are unavailable in this deployment.", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid settings form.", 400)
			return
		}
		rev, e := strconv.ParseInt(r.Form.Get("revision"), 10, 64)
		if e != nil {
			http.Error(w, "Reload the Settings page.", 409)
			return
		}
		saved, e := d.Providers.Load(r.Context())
		candidate := saved
		if e == nil {
			candidate, e = d.Providers.AutomaticCandidate(r.Context(), rev, r.Form.Get("a_recognize") == "on", r.Form.Get("a_summarize") == "on")
		}
		if e == nil {
			e = d.Providers.Save(r.Context(), candidate)
		}
		if e != nil {
			back(w, r, "/settings/recognition", e.Error())
			return
		}
		// Turning it off, or back on from off, restarts the clock, so notebooks
		// changed while it was off aren't swept up.
		was, now := saved.Config.Automatic.Chosen(), candidate.Config.Automatic.Chosen()
		if was != now {
			if _, e := d.DB.ExecContext(r.Context(), `DELETE FROM alexandria_settings WHERE key=$1`, settings.AutomaticSince); e != nil {
				d.fail(w, e)
				return
			}
		}
		notice := "Automatic processing is off."
		switch {
		case now && !was:
			notice = "Automatic processing saved. It applies to notebooks that change from now on."
		case now:
			notice = "Automatic processing saved."
		}
		back(w, r, "/settings/recognition", notice)
	})
	mux.HandleFunc("POST /settings/actions", func(w http.ResponseWriter, r *http.Request) {
		if d.Providers == nil {
			http.Error(w, "Provider settings are unavailable in this deployment.", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid settings form.", 400)
			return
		}
		rev, e := strconv.ParseInt(r.Form.Get("revision"), 10, 64)
		if e != nil {
			http.Error(w, "Reload the Settings page.", 409)
			return
		}
		f := r.Form.Get
		a := providers.ActionsConfig{Off: f("x_on") != "on", Red: f("x_red"), Blue: f("x_blue"), TaskInstructions: f("x_task_instructions"), EventInstructions: f("x_event_instructions")}
		saved, e := d.Providers.Load(r.Context())
		candidate := saved
		if e == nil {
			candidate, e = d.Providers.ActionsCandidate(r.Context(), rev, a, [2]string{noteactions.DefaultTaskInstructions, noteactions.DefaultEventInstructions})
		}
		if e == nil {
			e = d.Providers.Save(r.Context(), candidate)
		}
		if e != nil {
			back(w, r, "/settings/recognition#note-actions", e.Error())
			return
		}
		// Turning them back on starts the clock again, so ink written while
		// they were off isn't swept up.
		if saved.Config.Actions.Off && !a.Off {
			if _, e := d.DB.ExecContext(r.Context(), `DELETE FROM alexandria_settings WHERE key=$1`, settings.NoteActionsSince); e != nil {
				d.fail(w, e)
				return
			}
		}
		notice := "Note actions are off."
		switch {
		case !a.Off && saved.Config.Actions.Off:
			notice = "Note actions are on. They apply to ink written from now on."
		case !a.Off:
			notice = "Note actions saved."
		}
		back(w, r, "/settings/recognition#note-actions", notice)
	})
	mux.HandleFunc("POST /settings/minutes", func(w http.ResponseWriter, r *http.Request) {
		if d.Providers == nil {
			http.Error(w, "Provider settings are unavailable in this deployment.", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid settings form.", 400)
			return
		}
		rev, e := strconv.ParseInt(r.Form.Get("revision"), 10, 64)
		if e != nil {
			http.Error(w, "Reload the Settings page.", 409)
			return
		}
		f := r.Form.Get
		t := providers.TranscriptionConfig{Enabled: f("t_enabled") == "on", URL: f("t_url"), Model: f("t_model"), Language: f("t_language")}
		m := providers.SummaryConfig{Enabled: f("s_enabled") == "on", URL: f("s_url"), Model: f("s_model"), Format: f("s_format"), AnthropicWorkspace: f("s_workspace"), VLLMDisableThinking: f("s_vllm") == "on", SendToBOOX: f("s_boox") == "on", Instructions: f("s_instructions"), NoteLanguage: noteLanguage(f("s_note_language"))}
		candidate, e := d.Providers.MinutesCandidate(r.Context(), rev, t, m, providers.KeyChange{Key: f("t_key"), Action: f("t_key_action")}, providers.KeyChange{Key: f("s_key"), Action: f("s_key_action")})
		if e == nil {
			switch f("action") {
			case "save":
				if e = d.Providers.Save(r.Context(), candidate); e == nil {
					back(w, r, "/settings/recognition", "Audio Note Minutes settings saved. Nothing was transcribed or summarized.")
					return
				}
			case "test_transcription":
				e = candidate.TestConnection(r.Context(), "transcription")
			case "test_summary":
				e = candidate.TestConnection(r.Context(), "summary")
			default:
				http.Error(w, "Unknown settings action.", 400)
				return
			}
		}
		view, loadErr := d.Providers.View(r.Context())
		if loadErr != nil {
			d.fail(w, loadErr)
			return
		}
		view.Config.Transcription, view.Config.Summary = t, m
		view.Revision = rev
		notice := "Connection test passed. Settings have not been saved. Re-enter a replacement key before saving."
		if e != nil {
			notice = e.Error()
		}
		r.URL.RawQuery = url.Values{"notice": {notice}}.Encode()
		d.settingsPage(w, r, "recognition", &view)
	})
	mux.HandleFunc("POST /settings/providers", func(w http.ResponseWriter, r *http.Request) {
		if d.Providers == nil {
			http.Error(w, "Provider settings are unavailable in this deployment.", 503)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32768)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid settings form.", 400)
			return
		}
		rev, e := strconv.ParseInt(r.Form.Get("revision"), 10, 64)
		if e != nil {
			http.Error(w, "Reload the Settings page.", 409)
			return
		}
		action := r.Form.Get("action")
		// Every provider form is on the Recognition tab; the embedding card
		// returns to its own section.
		tab, to := "recognition", "/settings/recognition"
		if page := r.Form.Get("page"); page == "embedding" || page == "search" || action == "test_embedding" {
			to += "#embedding"
		}
		if action == "build" || action == "retry" || action == "reprocess" {
			if action == "reprocess" {
				if r.Form.Get("confirm") != "on" {
					back(w, r, to, "Confirm recognition reprocessing before starting.")
					return
				}
				e = d.Providers.Reprocess(r.Context(), rev)
			} else if action == "build" {
				e = d.Providers.BuildIndex(r.Context(), rev)
			} else {
				e = d.Providers.RetryIndex(r.Context())
			}
			if e != nil {
				back(w, r, to, "Index action could not be started. Reload and check your saved settings.")
				return
			}
			if action == "reprocess" {
				back(w, r, to, "Previously requested recognition pages queued. Changed OCR inputs may incur provider charges.")
				return
			}
			back(w, r, to, "Index work queued. The previous index remains available while the replacement builds.")
			return
		}
		c := providers.Config{OCR: providers.OCRConfig{Enabled: r.Form.Get("ocr_enabled") == "on", URL: strings.TrimSpace(r.Form.Get("ocr_url")), Model: strings.TrimSpace(r.Form.Get("ocr_model")), Format: r.Form.Get("ocr_format"), Prompt: r.Form.Get("ocr_prompt"), AnthropicWorkspace: strings.TrimSpace(r.Form.Get("anthropic_workspace")), VLLMDisableThinking: r.Form.Get("vllm") == "on"}, Embedding: providers.EmbedConfig{Enabled: r.Form.Get("embed_enabled") == "on", URL: strings.TrimSpace(r.Form.Get("embed_url")), Model: strings.TrimSpace(r.Form.Get("embed_model"))}}
		candidate, e := d.Providers.Candidate(r.Context(), rev, c, r.Form.Get("api_key"), r.Form.Get("key_action"))
		if e == nil {
			switch action {
			case "save":
				e = d.Providers.Save(r.Context(), candidate)
				if e == nil {
					back(w, r, to, "Provider settings saved. New requests and jobs use them immediately. Existing pages were not queued for recognition.")
					return
				}
			case "test_ocr":
				e = candidate.TestConnection(r.Context(), "ocr")
			case "test_embedding":
				e = candidate.TestConnection(r.Context(), "embedding")
			default:
				http.Error(w, "Unknown settings action.", 400)
				return
			}
		}
		view, loadErr := d.Providers.View(r.Context())
		if loadErr != nil {
			d.fail(w, loadErr)
			return
		}
		view.Config = c
		view.Revision = rev
		notice := "Connection test passed. Settings have not been saved. Re-enter a replacement key before saving."
		if e != nil {
			notice = e.Error()
		}
		r.URL.RawQuery = url.Values{"notice": {notice}}.Encode()
		d.settingsPage(w, r, tab, &view)
	})
}

// noteLanguage accepts "auto" or an available language; anything else is English.
func noteLanguage(v string) string {
	if v == "auto" || (v != "en" && i18n.Supported(v)) {
		return v
	}
	return ""
}
