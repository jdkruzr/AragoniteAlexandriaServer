package web

import (
	"fmt"
	"html/template"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

// formatFuncs are the template functions that print numbers and relative
// times, in one language. Dates stay in the neutral 2006-01-02 15:04 form.
func formatFuncs(l i18n.Localizer) template.FuncMap {
	return template.FuncMap{
		"clockTime": func(t time.Time) string { return t.Format("15:04") },
		"calDate": func(t time.Time, kind string) string {
			weekdays := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
			months := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
			if kind == "weekday-short" {
				day := []rune(l.T(weekdays[t.Weekday()]))
				return string(day[:min(3, len(day))])
			}
			if kind == "weekday" {
				return l.T(weekdays[t.Weekday()])
			}
			if kind == "heading" {
				return fmt.Sprintf("%s %d", l.T(months[t.Month()-1]), t.Year())
			}
			return l.T(weekdays[t.Weekday()]) + " " + t.Format("2006-01-02")
		},
		"num": func(n int) string { return l.Number(n, 0) },
		"count": func(n int) string {
			if n < 0 {
				return "—"
			}
			return l.Number(n, 0)
		},
		// decimal prints a float with the given number of decimals.
		"decimal": func(v float64, places int) string { return l.Number(v, places) },
		"bytes": func(n int64) string {
			switch {
			case n >= 1<<20:
				return l.Number(float64(n)/(1<<20), 1) + " MB"
			case n >= 1<<10:
				return l.Number(float64(n)/(1<<10), 0) + " KB"
			}
			return l.Number(n, 0) + " B"
		},
		"ago": func(t time.Time) string {
			if t.IsZero() {
				return l.T("never")
			}
			switch d := time.Since(t); {
			case d < time.Minute:
				return l.T("just now")
			case d < time.Hour:
				return l.T("{{.Count}} minutes ago", int(d/time.Minute))
			case d < 48*time.Hour:
				return l.T("{{.Count}} hours ago", int(d/time.Hour))
			default:
				return l.T("{{.Count}} days ago", int(d/(24*time.Hour)))
			}
		},
		"until": func(t time.Time) string {
			m := int(time.Until(t).Round(time.Minute) / time.Minute)
			switch {
			case m < 1:
				return l.T("under a minute")
			case m < 60:
				return l.T("{{.Minutes}} min", "Minutes", m)
			}
			return l.T("{{.Hours}} h {{.Minutes}} min", "Hours", m/60, "Minutes", m%60)
		},
	}
}
