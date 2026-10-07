package contract

import "testing"

// The Alexandria client advertises this combined writer+reader hash
// (AragoniteAlexandria docs/test-plans/forestread-stage-2/README.md). A change here
// is a wire-compatibility change that needs a client release and a grace window.
const ClientCombinedSchemaHash = "55c37f7f1d386ce37ab57c976bccae8d4efed385f852db6d807dff549ad77a54"

func TestCombinedSchemaHashMatchesClient(t *testing.T) {
	if got := CandidateCombined().SchemaHash(); got != ClientCombinedSchemaHash {
		t.Fatalf("combined schema hash %s, client advertises %s", got, ClientCombinedSchemaHash)
	}
}
