package boox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// CheckReady verifies the configured native database, including its view-backed
// document query. A running HTTP listener alone does not establish replication
// readiness after physical restoration. The caller supplies a bounded context.
func (c Config) CheckReady(ctx context.Context) error {
	var info struct {
		Name  string `json:"db_name"`
		State string `json:"state"`
	}
	if err := c.gateway(ctx, http.MethodGet, "/", nil, &info); err != nil {
		return errors.New("native sync unavailable")
	}
	if info.Name != c.Database || info.State != "Online" {
		return errors.New("native sync unavailable")
	}
	var docs struct {
		Rows []json.RawMessage `json:"rows"`
	}
	if err := c.gateway(ctx, http.MethodGet, "/_all_docs?limit=1", nil, &docs); err != nil || docs.Rows == nil || len(docs.Rows) > 1 {
		return errors.New("native sync unavailable")
	}
	return nil
}
