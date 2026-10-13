package web

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
)

func (d Deps) changePassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _, basic := r.BasicAuth()
	if !d.LocalPasswords || !basic || user == "" || auth.Identity(r.Context()) != user || auth.Identity(r.Context()) == "token" {
		http.Error(w, "Sign in with your local account to change its password.", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid password form.", 400)
		return
	}
	next := r.PostForm.Get("new_password")
	if next != r.PostForm.Get("confirm_password") {
		http.Error(w, "The new passwords do not match.", 400)
		return
	}
	err := auth.NewStore(d.DB).ChangePassword(r.Context(), user, r.PostForm.Get("current_password"), next)
	if errors.Is(err, auth.ErrPasswordPolicy) || errors.Is(err, auth.ErrCurrentPassword) {
		http.Error(w, err.Error(), 400)
		return
	}
	if err != nil {
		d.fail(w, err)
		return
	}
	// Render success before the next request challenges cached Basic credentials.
	r.URL.RawQuery = url.Values{"notice": {"Password changed. Use your new password when your browser next asks you to sign in."}}.Encode()
	d.settingsPage(w, r, "account", nil)
}
