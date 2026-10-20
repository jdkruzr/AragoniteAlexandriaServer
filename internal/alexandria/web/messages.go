package web

import "github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"

// dynamicMessages are values code produces and templates print through t
// ({{t .State}}, {{t .Label}}, {{t .Kind}}). They are listed here so the
// catalogs carry them and the catalog test can check every language has them.
var dynamicMessages = []string{
	// Calendar subscription status and validated color labels.
	"Calendar subscriptions",
	"Show public calendars alongside your events and manage automatic refreshes.",
	"Subscribed calendars are read-only and refresh every 30 minutes. You can subscribe to at most 20 calendars.",
	"Calendar color",
	"Subscribe to calendar",
	"Last successful refresh",
	"Showing the last successful calendar refresh.",
	"No calendar snapshot is available yet.",
	"Enable calendar and automatic refresh",
	"Refresh now",
	"Remove this subscription and its cached events",
	"Unsubscribe",
	"Visible calendars",
	"Local calendar",
	"Apply",
	"Manage calendar subscriptions",
	"Expansion limit reached. Narrow the date range or view the source calendar.",
	"This series has malformed or unsupported calendar data. View it in its source calendar.",
	"This event belongs to a subscribed calendar and is read-only.",
	"Open source event",
	"Choose a name and a calendar color.",
	"You can subscribe to at most 20 calendars.",
	"Calendar subscribed. Its first refresh is queued.",
	"Calendar refresh requested.",
	"This subscription changed. Reload and try again.",
	"Calendar subscription saved.",
	"Confirm that you want to unsubscribe.",
	"Calendar unsubscribed. Local events were kept.",
	"Calendar visibility saved for this browser.",
	"Refreshing calendar…",
	"Waiting for the first calendar refresh.",
	"Calendar is up to date.",
	"Enter a public HTTPS iCalendar feed URL (port 443).",
	"This calendar exceeds the subscription import limits.",
	"The source did not return a complete, valid iCalendar feed.",
	"No calendar feed was found on this page. Paste a Google Calendar share link or a direct public ICS URL.",
	"This page contains several calendars. Paste the share link or ICS URL for the calendar you want.",
	"This address returned a webpage instead of calendar data. Check that the calendar is public and use its public ICS address.",
	"The calendar source returned an HTTP error. Check that the feed is public.",
	"The calendar could not be fetched. Check its public URL and try again.",
	"blue",
	"green",
	"purple",
	"orange",
	"gray",

	"Sunday",
	"Monday",
	"Tuesday",
	"Wednesday",
	"Thursday",
	"Friday",
	"Saturday",
	"January",
	"February",
	"March",
	"April",
	"May",
	"June",
	"July",
	"August",
	"September",
	"October",
	"November",
	"December",

	// Recognition job states (listJobStates).
	i18n.M("queued"), i18n.M("processing"), i18n.M("ready"), i18n.M("blocked"), i18n.M("failed"), i18n.M("paused"), i18n.M("cancelled"), i18n.M("skipped"),
	// Library batch labels (recognition.Run.Label).
	i18n.M("Preparing review"), i18n.M("No available pages"), i18n.M("Ready for review"), i18n.M("Paused"), i18n.M("Cancelled"), i18n.M("Running"), i18n.M("Needs attention"), i18n.M("Complete"),
	// Search result kinds (source_search.go) and BOOX reading kinds (boox/source_pages.go).
	i18n.M("Notebook"), i18n.M("Reading"), i18n.M("Annotation"), i18n.M("Bookmark"), i18n.M("Book"),
	// BOOX library kinds (boox/library_browser.go).
	i18n.M("Handwritten note"), i18n.M("Folder"), i18n.M("Reading note"), i18n.M("Associated note"), i18n.M("Text note"), i18n.M("Meeting note"), i18n.M("Draft note"), i18n.M("Other note"),
	// Provider availability (providers/availability.go).
	i18n.M("Automatic processing starts once a recognition provider, or transcription and a summary model, is set up."), i18n.M("Automatic processing saved."),
	i18n.M("Not configured"), i18n.M("Set an embedding endpoint under Search by Meaning in Settings → AI Tools and Recognition. The optional local Ollama service must be installed before it can be used."),
	i18n.M("Unavailable"), i18n.M("Alexandria could not check the saved provider. Check its address and service, then refresh."),
	i18n.M("Ready"), i18n.M("The saved Ollama provider is reachable and this model is installed. Availability was checked without running inference."),
	i18n.M("Model missing"), i18n.M("Ollama is reachable, but the saved model is not installed. Install it on that service or choose an installed embedding model."),
	// Source summaries and search coverage (overview.go, boox/summary.go, providers/store.go).
	i18n.M("Last sync"), i18n.M("Last upload"), i18n.M("Alexandria Client pages"), i18n.M("BOOX notebook pages"), i18n.M("BOOX reading records"), i18n.M("BOOX Audio Note Minutes"),
	// Composite page titles built in handlers.
	i18n.M("Audio Note Minutes"), i18n.M("Summarize"),
}
