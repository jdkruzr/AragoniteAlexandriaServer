package blob

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestS3ContractIntegration(t *testing.T) {
	endpoint := os.Getenv("LOOM_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("LOOM_TEST_S3_ENDPOINT not set")
	}
	ctx := context.Background()
	store, err := NewS3(ctx, S3Config{
		Endpoint: endpoint, Region: "us-east-1", Bucket: "aragonite-loom",
		AccessKey: "loom-local", SecretKey: "loom-local-secret", PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	key := "contract/test-object.txt"
	t.Cleanup(func() { _ = store.Delete(context.Background(), key) })
	if _, err := store.Put(ctx, key, "text/plain", strings.NewReader("woven"), 5); err != nil {
		t.Fatal(err)
	}
	info, err := store.Stat(ctx, key)
	if err != nil || info.Size != 5 {
		t.Fatalf("Stat() = %+v, %v", info, err)
	}
	body, gotInfo, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil || string(got) != "woven" || gotInfo.Size != 5 {
		t.Fatalf("Get() = %q, %+v, %v", got, gotInfo, err)
	}
	url, err := store.SignedGetURL(ctx, key, time.Minute)
	if err != nil || url == "" {
		t.Fatalf("SignedGetURL() = %q, %v", url, err)
	}
}
