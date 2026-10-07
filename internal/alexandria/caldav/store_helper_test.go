package caldav

import (
	"testing"

	"github.com/google/uuid"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskattach"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

// testStore replaces UltraBridge's temp-dir blob store with a disposable
// object-storage prefix.
func testStore(t *testing.T) *taskattach.BlobStore {
	t.Helper()
	return &taskattach.BlobStore{Objects: testenv.Objects(t, uuid.NewString())}
}
