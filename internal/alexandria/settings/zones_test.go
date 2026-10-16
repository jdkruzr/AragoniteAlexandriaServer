package settings

import (
	"strings"
	"testing"
	"time"
)

func TestZones(t *testing.T) {
	groups := Zones(time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC))
	if groups[0].Region != "UTC" || groups[0].Zones[0].Name != "UTC" {
		t.Fatalf("UTC first: %+v", groups[0])
	}
	n, found := 0, ""
	for _, g := range groups {
		n += len(g.Zones)
		for _, z := range g.Zones {
			if z.Name == "America/Chicago" {
				found = g.Region + " " + z.Label
			}
			if strings.Contains(z.Label, "_") {
				t.Errorf("label %q", z.Label)
			}
		}
	}
	if n < 300 || found != "America Chicago (UTC−06:00)" {
		t.Fatalf("%d zones, chicago=%q", n, found)
	}
	if !KnownZone("America/Argentina/Buenos_Aires") || KnownZone("Local") || KnownZone("US/Central") {
		t.Fatal("KnownZone")
	}
}
