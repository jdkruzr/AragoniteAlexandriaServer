// boox-verify independently reads every currently referenced native BOOX body
// from S3 and checks its recorded SHA-256 and length. It never writes library data.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/config"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/database"
)

type object struct {
	Hash  string
	Bytes int64
}
type report struct {
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Objects  int       `json:"uniqueObjectsAtSnapshot"`
	Verified int       `json:"verifiedObjects"`
	Bytes    int64     `json:"verifiedBytes"`
	Failed   []string  `json:"failedHashes,omitempty"`
	Scope    string    `json:"scope"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "BOOX resource verification failed; configuration and object details are not logged")
		os.Exit(1)
	}
}
func run() error {
	fetchHash := flag.String("fetch-hash", "", "Write one verified current resource to stdout instead of an audit report")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	rootStore, err := blob.NewS3(ctx, blob.S3Config{Endpoint: cfg.ObjectEndpoint, Region: cfg.ObjectRegion, Bucket: cfg.ObjectBucket, AccessKey: cfg.ObjectAccessKey, SecretKey: cfg.ObjectSecretKey, PathStyle: cfg.ObjectPathStyle, DisableTLS: cfg.ObjectDisableTLS})
	if err != nil {
		return err
	}
	var libraryID string
	if err = db.QueryRowContext(ctx, `SELECT library_id::text FROM alexandria_library_runtime WHERE singleton`).Scan(&libraryID); err != nil {
		return err
	}
	store, err := blob.ForLibrary(rootStore, libraryID)
	if err != nil {
		return err
	}
	if *fetchHash != "" {
		hashBytes, e := hex.DecodeString(*fetchHash)
		if e != nil || len(hashBytes) != 32 {
			return fmt.Errorf("invalid resource hash")
		}
		var size int64
		if e = db.QueryRowContext(ctx, `SELECT v.bytes FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE NOT v.deleted AND v.sha256=$1 LIMIT 1`, *fetchHash).Scan(&size); e != nil {
			return e
		}
		if size < 0 || size > 64<<20 {
			return fmt.Errorf("resource size exceeds bound")
		}
		stream, _, e := store.Get(ctx, "boox/bodies/"+*fetchHash)
		if e != nil {
			return e
		}
		raw, e := io.ReadAll(io.LimitReader(stream, size+1))
		closeErr := stream.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		hash := sha256.Sum256(raw)
		if int64(len(raw)) != size || hex.EncodeToString(hash[:]) != *fetchHash {
			return fmt.Errorf("resource integrity mismatch")
		}
		_, e = os.Stdout.Write(raw)
		return e
	}
	result := report{Started: time.Now().UTC(), Scope: "Independent S3 bytes against current native ingestion hashes; excludes unreferenced history and does not establish native reference closure or restore."}
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT v.sha256,v.bytes FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE NOT v.deleted`)
	if err != nil {
		return err
	}
	var objects []object
	for rows.Next() {
		var o object
		if err = rows.Scan(&o.Hash, &o.Bytes); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	result.Objects = len(objects)
	jobs := make(chan object)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for o := range jobs {
				operation, stop := context.WithTimeout(ctx, 30*time.Second)
				count, e := verifyObject(operation, store, o)
				stop()
				mu.Lock()
				if e != nil {
					result.Failed = append(result.Failed, o.Hash)
				} else {
					result.Verified++
					result.Bytes += count
				}
				mu.Unlock()
			}
		}()
	}
	for _, o := range objects {
		select {
		case jobs <- o:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	result.Finished = time.Now().UTC()
	if err = json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return err
	}
	if len(result.Failed) != 0 {
		return fmt.Errorf("%d objects failed", len(result.Failed))
	}
	return nil
}

// verifyObject rejects missing, truncated, overlong and changed bodies. Read at
// most the expected length plus one so an incorrect object cannot run unbounded.
func verifyObject(ctx context.Context, store blob.Store, o object) (int64, error) {
	if o.Bytes < 0 || o.Bytes == int64(^uint64(0)>>1) {
		return 0, fmt.Errorf("invalid length")
	}
	body, _, err := store.Get(ctx, "boox/bodies/"+o.Hash)
	if err != nil {
		return 0, err
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(body, o.Bytes+1))
	closeErr := body.Close()
	if err != nil {
		return count, err
	}
	if closeErr != nil {
		return count, closeErr
	}
	if count != o.Bytes || hex.EncodeToString(hash.Sum(nil)) != o.Hash {
		return count, fmt.Errorf("resource integrity mismatch")
	}
	return count, nil
}
