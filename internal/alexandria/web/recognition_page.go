package web

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
)

// recognitionView is the Recognition page: a tab of current work for one
// source or all of them.
type recognitionView struct {
	Source, Tab string
	// Sources shows the source switch; without BOOX there is only the Client.
	Sources, WorkTab bool
	Tally            recognition.Tally
	// Mode is "auto", "manual" or "off"; Model names the recognition model.
	Mode, Model    string
	Runs           []recognition.Run
	Groups         []recognition.Group
	Pages          []recognition.Job
	Work           WorkSummary
	Total, Offset  int
	Previous, Next string
	// Return brings an action back to this tab.
	Return string
}

// ActiveCount is the Active tab's count: groups plus unfinished library runs.
func (v recognitionView) ActiveCount() int {
	n := v.Tally.Active
	for _, r := range v.Runs {
		if runActive(r) {
			n++
		}
	}
	return n
}

// runActive reports whether a library run still has work or a decision
// ahead: preparing, waiting for review, running or paused.
func runActive(r recognition.Run) bool {
	switch r.Label() {
	case "Preparing review", "Ready for review", "Running", "Paused":
		return true
	}
	return false
}

const recognitionPageSize = 50

func (d Deps) recognitionPage(w http.ResponseWriter, r *http.Request, store recognition.Store) {
	q := r.URL.Query()
	ctx := r.Context()
	v := recognitionView{Source: q.Get("source"), Sources: d.Status.NativeBOOX, WorkTab: d.BOOXWork != nil, Model: d.Status.OCR}
	if v.Source != "boox" && v.Source != "client" || !d.Status.NativeBOOX {
		v.Source = ""
	}
	v.Mode = "off"
	if v.Model != "" {
		v.Mode = "manual"
		if d.Providers != nil {
			if c, e := d.Providers.LoadConfig(ctx); e == nil && c.AutoRecognize() {
				v.Mode = "auto"
			}
		}
	}
	var e error
	if v.Tally, e = store.Tally(ctx, v.Source); e != nil {
		d.fail(w, e)
		return
	}
	runs, e := store.RecentRuns(ctx)
	if e != nil {
		d.fail(w, e)
		return
	}
	for _, run := range runs {
		if v.Source == "" || run.Source == v.Source {
			v.Runs = append(v.Runs, run)
		}
	}
	v.Tab = q.Get("tab")
	switch v.Tab {
	case "active", "attention", "skipped", "finished":
	case "work":
		if !v.WorkTab {
			v.Tab = ""
		}
	default:
		v.Tab = ""
	}
	if v.Tab == "" {
		v.Tab = "active"
		if v.Tally.Attention > 0 {
			v.Tab = "attention"
		}
	}
	v.Offset, _ = strconv.Atoi(q.Get("offset"))
	if v.Offset < 0 || v.Offset > 1000000 {
		v.Offset = 0
	}
	link := func(extra url.Values) string {
		u := url.Values{"tab": {v.Tab}}
		if v.Source != "" {
			u.Set("source", v.Source)
		}
		for k, values := range extra {
			u[k] = values
		}
		return "/jobs?" + u.Encode()
	}
	v.Return = link(nil)
	if v.Offset > 0 {
		v.Return = link(url.Values{"offset": {strconv.Itoa(v.Offset)}})
	}
	switch v.Tab {
	case "active":
		v.Groups, v.Total, e = store.Groups(ctx, v.Source, true, 0, 500)
	case "finished":
		v.Groups, v.Total, e = store.Groups(ctx, v.Source, false, v.Offset, recognitionPageSize)
	case "attention":
		v.Pages, v.Total, e = store.Pages(ctx, v.Source, []string{"failed", "blocked"}, v.Offset, recognitionPageSize)
	case "skipped":
		v.Pages, v.Total, e = store.Pages(ctx, v.Source, []string{"skipped"}, v.Offset, recognitionPageSize)
	}
	if e != nil {
		d.fail(w, e)
		return
	}
	if v.Tab != "active" {
		if v.Offset > 0 {
			v.Previous = link(url.Values{"offset": {strconv.Itoa(max(0, v.Offset-recognitionPageSize))}})
		}
		if v.Offset+recognitionPageSize < v.Total {
			v.Next = link(url.Values{"offset": {strconv.Itoa(v.Offset + recognitionPageSize)}})
		}
	}
	running := v.Tally.Active > 0
	for _, run := range v.Runs {
		running = running || run.Label() == "Running" || run.Label() == "Preparing review"
	}
	if d.BOOXWork != nil {
		// The BOOX work sections are extra: a failure leaves them out.
		if s, e := d.BOOXWork(ctx); e == nil {
			v.Work = s
			running = running || s.Actions.Active()+s.Minutes.Active() > 0
		}
	}
	// The page updates itself while work is running (static/live.js).
	if running {
		w.Header().Set("Refresh", "5")
	}
	d.render(w, r, "jobs", "Recognition", "jobs", v)
}

// jobsReturn keeps an action's way back on the Recognition pages.
func jobsReturn(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.IsAbs() || u.Host != "" || u.Path != "/jobs" {
		return ""
	}
	q := u.Query()
	q.Del("notice")
	q.Del("notice_n")
	if len(q) == 0 {
		return "/jobs"
	}
	return "/jobs?" + q.Encode()
}
