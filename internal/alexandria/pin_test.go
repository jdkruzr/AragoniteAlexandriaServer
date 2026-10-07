package alexandria

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The server's Rhizome requirement must be the client's pinned commit. A sibling
// Alexandria checkout, when present, is checked too; CI without it checks go.mod alone.
func TestRhizomeRequirementMatchesClientPin(t *testing.T) {
	mod, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`github.com/jdkruzr/rhizome/server-go v\S+-([0-9a-f]{12})`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("go.mod does not require a Rhizome pseudo-version")
	}
	if !strings.HasPrefix(RhizomeRevision, string(m[1])) {
		t.Fatalf("go.mod pins Rhizome %s, client pin is %s", m[1], RhizomeRevision)
	}
	if client, err := os.ReadFile("../../../AragoniteAlexandria/gradle/rhizome-integration-revision.txt"); err == nil {
		if got := strings.TrimSpace(string(client)); got != RhizomeRevision {
			t.Fatalf("Alexandria client pins %s, server expects %s", got, RhizomeRevision)
		}
	}
}
