// Package status holds source and work summaries shared by adapters.
package status

import "time"

// WorkSummary is the BOOX work beside recognition jobs: note actions and
// Audio Note Minutes. Counts are by state; Rows are the ones worth a look
// (still running, failed, or finished with something to say).
type WorkSummary struct {
	Actions, Minutes WorkGroup
}

type WorkGroup struct {
	Counts map[string]int
	Rows   []WorkRow
}

type WorkRow struct {
	Title, Link, State, Detail string
	Color, Action              string // note actions only
	Updated                    time.Time
}

// Active is work still queued or running; the Jobs page refreshes while any is.
func (g WorkGroup) Active() int { return g.Counts["queued"] + g.Counts["processing"] }

// Empty is a group with nothing to show.
func (g WorkGroup) Empty() bool { return len(g.Counts) == 0 }

// SourceSummary is a source's at-a-glance state for the library overview.
// Counts are -1 when unknown so a failed query never shows a misleading zero.
type SourceSummary struct {
	Notebooks, Books, Devices int
	LastActivity              time.Time // zero when nothing has been received
	ActivityLabel             string    // what LastActivity measures, e.g. "Last sync"
}
