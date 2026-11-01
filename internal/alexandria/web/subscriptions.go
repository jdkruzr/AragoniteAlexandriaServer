package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
)

const subscriptionSettings = "/settings/calendar?section=subscriptions#subscriptions"
const sourceCookie = "alexandria_calendar_sources"

type calendarSource struct {
	Subscription subscriptions.Subscription
	Visible      bool
}

func (d Deps) subscriptionRoutes(mux *http.ServeMux) {
	store := subscriptions.Store{DB: d.DB}
	mux.HandleFunc("POST /calendar/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		_, err := store.Subscribe(r.Context(), r.FormValue("name"), r.FormValue("color"), r.FormValue("feed_url"), d.calendarHTTPClient)
		if err != nil {
			var inputError *subscriptions.InputError
			message := "Choose a name and a calendar color."
			switch {
			case errors.Is(err, subscriptions.ErrLimit):
				message = "You can subscribe to at most 20 calendars."
			case errors.As(err, &inputError):
				message = subscriptions.Problem(inputError.Code)
			case !errors.Is(err, subscriptions.ErrInvalid):
				d.fail(w, err)
				return
			}
			back(w, r, subscriptionSettings, message)
			return
		}
		back(w, r, subscriptionSettings, "Calendar subscribed. Its first refresh is queued.")
	})
	mux.HandleFunc("POST /calendar/subscriptions/{id}/refresh", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Refresh(r.Context(), r.PathValue("id")); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, subscriptionSettings, "Calendar refresh requested.")
	})
	mux.HandleFunc("POST /calendar/subscriptions/{id}/update", func(w http.ResponseWriter, r *http.Request) {
		rev, _ := strconv.ParseInt(r.FormValue("revision"), 10, 64)
		err := store.Update(r.Context(), r.PathValue("id"), rev, r.FormValue("name"), r.FormValue("color"), r.FormValue("enabled") == "yes")
		if errors.Is(err, subscriptions.ErrInvalid) {
			back(w, r, subscriptionSettings, "Choose a name and a calendar color.")
			return
		}
		if errors.Is(err, subscriptions.ErrChanged) {
			back(w, r, subscriptionSettings, "This subscription changed. Reload and try again.")
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, subscriptionSettings, "Calendar subscription saved.")
	})
	mux.HandleFunc("POST /calendar/subscriptions/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("confirm") != "yes" {
			back(w, r, subscriptionSettings, "Confirm that you want to unsubscribe.")
			return
		}
		rev, _ := strconv.ParseInt(r.FormValue("revision"), 10, 64)
		err := store.Remove(r.Context(), r.PathValue("id"), rev)
		if errors.Is(err, subscriptions.ErrChanged) {
			back(w, r, subscriptionSettings, "This subscription changed. Reload and try again.")
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, subscriptionSettings, "Calendar unsubscribed. Local events were kept.")
	})
	mux.HandleFunc("POST /calendar/subscriptions/visibility", d.updateCalendarFilters)
	// Subscribed events open read-only in the Calendar's editor column.
	mux.HandleFunc("GET /calendar/subscriptions/{id}/events/{key}", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		http.Redirect(w, r, calendarWith(r, "sub", r.PathValue("id"), "key", r.PathValue("key"), "occurrence", q.Get("occurrence"), "on", q.Get("on"), "date", q.Get("on")), http.StatusSeeOther)
	})
}
