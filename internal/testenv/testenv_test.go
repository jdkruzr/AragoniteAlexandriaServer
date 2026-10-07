package testenv

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestDisposableLibraryAndObjects(t *testing.T) {
	lib := Database(t)
	var id string
	if err := lib.DB.QueryRow(`SELECT library_id::text FROM alexandria_library_runtime`).Scan(&id); err != nil || id != lib.ID {
		t.Fatalf("library row %q %v", id, err)
	}
	objects := Objects(t, lib.ID)
	ctx := context.Background()
	if _, err := objects.Put(ctx, "testenv/probe", "text/plain", strings.NewReader("ok"), 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = objects.Delete(context.Background(), "testenv/probe") })
	body, _, err := objects.Get(ctx, "testenv/probe")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if got, _ := io.ReadAll(body); string(got) != "ok" {
		t.Fatalf("read %q", got)
	}
}
