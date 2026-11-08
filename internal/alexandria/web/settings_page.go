package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/noteactions"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/oauth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/diagnostics"
)

// settingsEntry is one entry in the Settings nav; each is its own page.
type settingsEntry struct{ ID, Label, Path string }

var settingsEntries = []settingsEntry{
	{"account", i18n.M("Account"), "/settings"},
	{"devices", i18n.M("Devices"), "/settings/devices"},
	{"models", i18n.M("AI models"), "/settings/models"},
	{"automatic", i18n.M("Automatic processing"), "/settings/automatic"},
	{"actions", i18n.M("Note actions"), "/settings/actions"},
	{"search", i18n.M("Search by meaning"), "/settings/search"},
	{"calendar", i18n.M("Calendar & tasks"), "/settings/calendar"},
	{"api", i18n.M("API & MCP"), "/settings/api"},
}

func settingsSections() []settingsEntry { return settingsEntries }

// BOOXDevice is a BOOX tablet as Settings → Devices lists it.
type BOOXDevice struct {
	ID, Model string
	Revoked   bool
	// New marks a device that enrolled in the last ten minutes.
	New        bool
	LastUpload time.Time
}

// BOOXDevices is the BOOX column of Settings → Devices.
type BOOXDevices struct {
	Summary SourceSummary
	Devices []BOOXDevice
	// Waiting reports an unused pairing code that hasn't expired.
	Waiting bool
}

// Live lists the authorized devices; Revoked the others.
func (b BOOXDevices) Live() []BOOXDevice    { return b.filter(false) }
func (b BOOXDevices) Revoked() []BOOXDevice { return b.filter(true) }

func (b BOOXDevices) filter(revoked bool) []BOOXDevice {
	var out []BOOXDevice
	for _, d := range b.Devices {
		if d.Revoked == revoked {
			out = append(out, d)
		}
	}
	return out
}

// BOOXOperator holds the BOOX facts Operator tools shows.
type BOOXOperator struct{ Numbered, Deleted int }

type devicesView struct {
	BOOX *BOOXDevices
	// BOOXUnavailable is set when BOOX is configured but its list failed.
	BOOXUnavailable bool
	Client          SourceSummary
	ClientDevices   []relay.Device
}

// serverFacts is Operator tools' "This server" line.
type serverFacts struct {
	Database, Files int64
	Postgres        string
}

type operatorView struct {
	SyncDevices []relay.Device
	BOOX        *BOOXOperator
	Obsolete    int
	Server      serverFacts
}

func (d Deps) settingsPageRoutes(mux *http.ServeMux) {
	for _, s := range append(settingsEntries, settingsEntry{"operator", "Operator tools", "/settings/operator"}) {
		id := s.ID
		mux.HandleFunc("GET "+s.Path, func(w http.ResponseWriter, r *http.Request) { d.settingsPage(w, r, id, nil, nil) })
	}
	// Old Settings addresses (help, guides, bookmarks) go to their new home.
	moved := func(path string, sections map[string]string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			to := path
			if section := r.URL.Query().Get("section"); section != "" {
				if p, ok := sections[section]; ok {
					to = p
				}
			}
			http.Redirect(w, r, to, http.StatusMovedPermanently)
		}
	}
	mux.HandleFunc("GET /settings/recognition", moved("/settings/models", map[string]string{
		"provider":     "/settings/models?section=provider#provider",
		"minutes":      "/settings/models?section=minutes#minutes",
		"embedding":    "/settings/models?section=embedding#embedding",
		"automatic":    "/settings/automatic",
		"note-actions": "/settings/actions",
		"page-limit":   "/jobs/size",
		"reprocess":    "/settings/operator?section=recognition#recognition",
	}))
	mux.HandleFunc("GET /settings/integrations", moved("/settings/calendar", map[string]string{
		"subscriptions": "/settings/calendar?section=subscriptions#subscriptions",
		"api":           "/settings/api",
	}))
	for _, path := range []string{"/sources", "/sources/client"} {
		mux.HandleFunc("GET "+path, moved("/settings/devices#add", nil))
	}
	mux.HandleFunc("GET /files/forestnote/devices", moved("/settings/devices#client", nil))
}

// settingsPage renders one Settings section. Each loads only what it shows;
// the provider sections share one View so a failed connection test can
// re-render the submitted (unsaved) values. extra adds one-time data, such
// as a newly created token.
func (d Deps) settingsPage(w http.ResponseWriter, r *http.Request, tab string, override *providers.View, extra map[string]any) {
	ctx := r.Context()
	data := map[string]any{"LocalPasswords": d.LocalPasswords, "Status": d.Status, "Languages": i18n.Choices()}
	if c, err := r.Cookie(LanguageCookie); err == nil && i18n.Supported(c.Value) {
		data["Language"] = c.Value
	}
	for k, v := range extra {
		data[k] = v
	}
	var err error
	switch tab {
	case "devices":
		var v devicesView
		if v, err = d.devices(ctx); err == nil {
			data["Devices"] = v
			if v.BOOX != nil && v.BOOX.Waiting {
				w.Header().Set("Refresh", "5")
			}
		}
	case "calendar":
		err = d.calendarSettings(ctx, data)
	case "api":
		err = d.tokens(ctx, data)
	case "models", "automatic", "actions", "search", "operator":
		err = d.providerSettings(ctx, tab, override, data)
		if err == nil && tab == "operator" {
			var v operatorView
			if v, err = d.operator(ctx); err == nil {
				data["Operator"] = v
			}
		}
	}
	if err != nil {
		d.fail(w, err)
		return
	}
	title, name := i18n.M("Operator tools"), "settings-"+tab
	for _, s := range settingsEntries {
		if s.ID == tab {
			title = s.Label
		}
	}
	if tab == "account" {
		name = "settings"
	}
	w.Header().Set("Cache-Control", "no-store")
	if pv, ok := data["Providers"].(*providers.View); ok && tab == "search" && pv != nil && pv.IndexState != "" && pv.SavedEmbedding.Enabled {
		w.Header().Set("Refresh", "15")
	}
	d.render(w, r, name, title, "settings", data)
}

func (d Deps) calendarSettings(ctx context.Context, data map[string]any) error {
	values := map[string][2]string{
		"Collection":       {settings.CalDAVCollectionName, "Tasks"},
		"EventsCollection": {settings.CalDAVEventsName, "Calendar"},
		"DueMode":          {settings.DueTimeMode, "preserve"},
		"TimeZone":         {settings.TimeZone, ""},
	}
	for key, v := range values {
		got, err := settings.Get(ctx, d.DB, v[0], v[1])
		if err != nil {
			return err
		}
		data[key] = got
	}
	zone := data["TimeZone"].(string)
	data["ServerZone"], data["ZoneGroups"] = settings.ZoneName(time.Local), settings.Zones(time.Now())
	if zone != "" && !settings.KnownZone(zone) {
		data["LegacyZone"] = zone
	}
	all, err := (subscriptions.Store{DB: d.DB}).List(ctx)
	data["Subscriptions"], data["SubscriptionColors"] = all, subscriptions.Colors
	return err
}

func (d Deps) tokens(ctx context.Context, data map[string]any) error {
	rows, err := d.DB.QueryContext(ctx, `SELECT token_hash, label, created_at, last_used_at, scope='read', oauth_client_id IS NOT NULL OR label=$1
		FROM alexandria_api_tokens WHERE revoked_at IS NULL ORDER BY created_at DESC`, oauth.TokenLabel)
	if err != nil {
		return err
	}
	defer rows.Close()
	var tokens []apiToken
	for rows.Next() {
		var t apiToken
		if err := rows.Scan(&t.Hash, &t.Label, &t.Created, &t.LastUsed, &t.ReadOnly, &t.OAuth); err != nil {
			return err
		}
		tokens = append(tokens, t)
	}
	data["Tokens"], data["OAuthLabel"] = tokens, oauth.TokenLabel
	return rows.Err()
}

func (d Deps) providerSettings(ctx context.Context, tab string, override *providers.View, data map[string]any) error {
	var providerView *providers.View
	if d.Providers != nil {
		if override != nil {
			providerView = override
		} else {
			v, e := d.Providers.View(ctx)
			if e != nil {
				return e
			}
			providerView = &v
		}
		if tab == "search" {
			// A bounded model-list request; never inference.
			providerView.Availability = providers.EmbeddingAvailability(ctx, providerView.SavedEmbedding)
		}
	}
	data["Providers"], data["DefaultOCRPrompt"] = providerView, notes.DefaultPrompt
	data["DefaultMinutesInstructions"], data["MinutesFormat"] = minutes.DefaultInstructions, minutes.ResponseFormat
	data["DefaultTaskInstructions"], data["DefaultEventInstructions"] = noteactions.DefaultTaskInstructions, noteactions.DefaultEventInstructions
	data["TaskFormat"], data["EventFormat"] = noteactions.Format("task"), noteactions.Format("event")
	if providerView == nil {
		return nil
	}
	a := providerView.Config.Actions
	data["ActionColors"] = []struct{ Name, Action string }{{"red", a.For("red")}, {"blue", a.For("blue")}}
	if _, why := providerView.Config.ActionsReady(); why != "" {
		data["ActionsWhy"] = why
	} else if since, err := settings.Get(ctx, d.DB, settings.NoteActionsSince, ""); err == nil && since != "" {
		data["ActionsSince"] = strings.Replace(strings.TrimSuffix(since, "Z"), "T", " ", 1) + " UTC"
	} else {
		data["ActionsSince"] = "the next recognized page"
	}
	c := providerView.Config
	switch {
	case c.AutoRecognize() || c.AutoSummarize():
		if since, err := settings.Get(ctx, d.DB, settings.AutomaticSince, ""); err == nil && since != "" {
			data["AutoSince"] = strings.Replace(strings.TrimSuffix(since, "Z"), "T", " ", 1) + " UTC"
		} else {
			data["AutoNew"] = true
		}
	case c.Automatic.Chosen():
		data["AutoWhy"] = "Automatic processing starts once a recognition provider, or transcription and a summary model, is set up."
	}
	return nil
}

func (d Deps) devices(ctx context.Context) (devicesView, error) {
	var v devicesView
	var restore int
	v.Client = d.clientSummary(ctx, &restore)
	var err error
	if v.ClientDevices, err = (relay.Store{DB: d.DB}).ListDevices(ctx); err != nil {
		return v, err
	}
	if d.BOOXDevices != nil {
		// The BOOX list is extra: a failure leaves a notice, not an error page.
		if b, e := d.BOOXDevices(ctx); e == nil {
			v.BOOX = &b
		} else {
			v.BOOXUnavailable = true
			if d.Logger != nil {
				d.Logger.Warn("BOOX devices unavailable", "code", diagnostics.Code(e))
			}
		}
	}
	return v, nil
}

func (d Deps) operator(ctx context.Context) (operatorView, error) {
	var v operatorView
	devices, err := (relay.Store{DB: d.DB}).ListDevices(ctx)
	if err != nil {
		return v, err
	}
	for _, dev := range devices {
		if dev.LastSeenMs > 0 && !dev.Revoked {
			v.SyncDevices = append(v.SyncDevices, dev)
		}
	}
	if v.Obsolete, err = (recognition.Store{DB: d.DB}).CountObsolete(ctx); err != nil {
		return v, err
	}
	// Distinct BOOX file bodies plus Client assets; the database holds the rest.
	err = d.DB.QueryRowContext(ctx, `SELECT pg_database_size(current_database()),
 coalesce((SELECT sum(bytes) FROM (SELECT DISTINCT ON (sha256) bytes FROM boox_blob_version WHERE NOT deleted) b),0)::bigint+coalesce((SELECT sum(byte_length) FROM rhizome_asset),0)::bigint,
 split_part(current_setting('server_version'),' ',1)`).Scan(&v.Server.Database, &v.Server.Files, &v.Server.Postgres)
	if err != nil {
		return v, err
	}
	if d.BOOXOperator != nil {
		if b, e := d.BOOXOperator(ctx); e == nil {
			v.BOOX = &b
		} else if d.Logger != nil {
			d.Logger.Warn("BOOX operator facts unavailable", "code", diagnostics.Code(e))
		}
	}
	return v, nil
}

// settingsBack is a Settings group's address after a successful save.
func settingsBack(path, group string) string {
	q := url.Values{"section": {group}, "saved": {"1"}}
	return path + "?" + q.Encode() + "#" + group
}

// settingsTo is a Settings group's address for a notice that isn't a save.
func settingsTo(path, group string) string {
	return path + "?section=" + url.QueryEscape(group) + "#" + group
}
