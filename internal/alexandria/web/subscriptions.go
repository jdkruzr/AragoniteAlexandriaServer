package web

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
)

const subscriptionSettings = "/settings/integrations?section=subscriptions#subscriptions"
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
	mux.HandleFunc("GET /calendar/subscriptions/{id}/events/{key}", func(w http.ResponseWriter, r *http.Request) {
		sub, err := store.Get(r.Context(), r.PathValue("id"))
		if errors.Is(err, sql.ErrNoRows) || err == nil && !sub.Enabled {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		loc := settings.Zone(r.Context(), d.DB)
		var blob string
		err = d.DB.QueryRowContext(r.Context(), `SELECT ical_blob FROM alexandria_calendar_import WHERE subscription_id=$1 AND series_key=$2`, sub.ID, r.PathValue("key")).Scan(&blob)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		e, parseErr := events.FromSeries(subscriptions.EventID(sub.ID, r.PathValue("key")), blob, loc)
		day, err := time.ParseInLocation("2006-01-02", r.URL.Query().Get("on"), loc)
		if err != nil {
			day = midnight(time.Now(), loc)
		}
		result := events.Expansion{}
		if parseErr == nil {
			result = events.Expand(r.Context(), []events.Event{e}, []events.Window{{From: day, To: day.AddDate(0, 0, 1)}}, loc)
		} else {
			result.Incomplete = []events.Incomplete{{ID: e.ID, Title: e.Title, Reason: "unsupported"}}
		}
		var row *calendarRow
		for _, o := range result.Occurrences {
			if !o.Recurring || o.Identity == r.URL.Query().Get("occurrence") {
				v := subscriptionRow(makeCalendarRow(o, calendarReturn(r), loc), subscriptions.Imported{SubscriptionID: sub.ID, Key: r.PathValue("key"), Name: sub.Name, Color: sub.Color}, loc)
				row = &v
				break
			}
		}
		d.render(w, r, "subscription-event", "Event details", "calendar", map[string]any{"Subscription": sub, "Row": row, "Event": e, "Return": calendarReturn(r), "Incomplete": result.Incomplete})
	})
}
func subscriptionRow(row calendarRow, v subscriptions.Imported, loc *time.Location) calendarRow {
	if u, err := url.Parse(row.Event.URL); err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		row.Event.URL = ""
	}
	row.CalendarName, row.Color, row.ReadOnly = v.Name, v.Color, true
	on := row.Event.Start.In(loc).Format("2006-01-02")
	if row.Event.AllDay {
		on = row.Event.Start.Format("2006-01-02")
	}
	q := url.Values{"return": {row.Return}, "on": {on}, "occurrence": {row.Identity}}
	row.Link = "/calendar/subscriptions/" + v.SubscriptionID + "/events/" + v.Key + "?" + q.Encode()
	return row
}
