package settings

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // zone rules in the binary, so every listed zone loads anywhere
)

// zone1970.tab is the IANA time zone database's list of distinct zones
// (public domain, tzdata 2026c). Refresh it from /usr/share/zoneinfo when
// tzdata changes.
//
//go:embed zone1970.tab
var zoneTab string

// ZoneChoice is one selectable time zone.
type ZoneChoice struct {
	Name  string // IANA name, stored as the setting
	Label string // "Chicago (UTC−05:00)"
}

// ZoneGroup is the zones of one region ("America", "Europe", …).
type ZoneGroup struct {
	Region string
	Zones  []ZoneChoice
}

// zoneNames are the IANA names in zone1970.tab, plus UTC.
var zoneNames = func() map[string]bool {
	names := map[string]bool{"UTC": true}
	for _, line := range strings.Split(zoneTab, "\n") {
		if f := strings.Split(line, "\t"); len(f) >= 3 && !strings.HasPrefix(line, "#") {
			names[f[2]] = true
		}
	}
	return names
}()

// KnownZone reports whether name is a selectable zone.
func KnownZone(name string) bool { return zoneNames[name] }

// Zones lists the selectable zones by region, each labeled with its offset
// from UTC at now (offsets change with daylight saving time).
func Zones(now time.Time) []ZoneGroup {
	byRegion := map[string][]ZoneChoice{}
	for name := range zoneNames {
		loc, err := time.LoadLocation(name)
		if err != nil {
			continue
		}
		region, city, ok := strings.Cut(name, "/")
		if !ok {
			region, city = "UTC", name
		}
		_, off := now.In(loc).Zone()
		sign := "+"
		if off < 0 {
			sign, off = "−", -off
		}
		label := fmt.Sprintf("%s (UTC%s%02d:%02d)", strings.ReplaceAll(strings.ReplaceAll(city, "_", " "), "/", " / "), sign, off/3600, off%3600/60)
		if name == "UTC" {
			label = "UTC"
		}
		byRegion[region] = append(byRegion[region], ZoneChoice{Name: name, Label: label})
	}
	var out []ZoneGroup
	for region, zones := range byRegion {
		sort.Slice(zones, func(i, j int) bool { return zones[i].Label < zones[j].Label })
		out = append(out, ZoneGroup{Region: region, Zones: zones})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Region == "UTC") != (out[j].Region == "UTC") {
			return out[i].Region == "UTC"
		}
		return out[i].Region < out[j].Region
	})
	return out
}
