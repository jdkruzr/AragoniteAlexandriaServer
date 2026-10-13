package web

import (
	"errors"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
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
		tab := "recognition"
		if r.Form.Get("page") == "search" || action == "test_embedding" {
			tab = "search"
		}
		if action == "build" || action == "retry" || action == "reprocess" {
			if action == "reprocess" {
				if r.Form.Get("confirm") != "on" {
					back(w, r, "/settings/"+tab, "Confirm recognition reprocessing before starting.")
					return
				}
				e = d.Providers.Reprocess(r.Context(), rev)
			} else if action == "build" {
				e = d.Providers.BuildIndex(r.Context(), rev)
			} else {
				e = d.Providers.RetryIndex(r.Context())
			}
			if e != nil {
				back(w, r, "/settings/"+tab, "Index action could not be started. Reload and check your saved settings.")
				return
			}
			if action == "reprocess" {
				back(w, r, "/settings/"+tab, "Previously requested recognition pages queued. Changed OCR inputs may incur provider charges.")
				return
			}
			back(w, r, "/settings/"+tab, "Index work queued. The previous index remains available while the replacement builds.")
			return
		}
		c := providers.Config{OCR: providers.OCRConfig{Enabled: r.Form.Get("ocr_enabled") == "on", URL: strings.TrimSpace(r.Form.Get("ocr_url")), Model: strings.TrimSpace(r.Form.Get("ocr_model")), Format: r.Form.Get("ocr_format"), Prompt: r.Form.Get("ocr_prompt"), AnthropicWorkspace: strings.TrimSpace(r.Form.Get("anthropic_workspace")), VLLMDisableThinking: r.Form.Get("vllm") == "on"}, Embedding: providers.EmbedConfig{Enabled: r.Form.Get("embed_enabled") == "on", URL: strings.TrimSpace(r.Form.Get("embed_url")), Model: strings.TrimSpace(r.Form.Get("embed_model"))}}
		candidate, e := d.Providers.Candidate(r.Context(), rev, c, r.Form.Get("api_key"), r.Form.Get("key_action"))
		if e == nil {
			switch action {
			case "save":
				e = d.Providers.Save(r.Context(), candidate)
				if e == nil {
					back(w, r, "/settings/"+tab, "Provider settings saved. New requests and jobs use them immediately. Existing pages were not queued for recognition.")
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
