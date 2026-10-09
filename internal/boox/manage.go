package boox

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Configure is an explicit management operation. Runtime never changes Gateway
// database configuration. An existing database for another library is refused.
func (c Config) Configure(ctx context.Context, library string) error {
	if e := c.Validate(); e != nil {
		return e
	}
	marker := "alexandria-boox-library:" + library
	var existing map[string]any
	e := c.gateway(ctx, "GET", "/_config", nil, &existing)
	create := false
	if e != nil {
		u, ok := e.(UpstreamError)
		if !ok || (u.Status != 404 && u.Status != 403) {
			return e
		}
		// Gateway 3.2 returns 403 for an absent database before its
		// database-scoped permission check. Verify absence using the
		// authenticated server-level list; never treat denial as ownership.
		var databases []string
		if err := c.gatewayRequest(ctx, "GET", "/_all_dbs", nil, &databases); err != nil {
			return err
		}
		for _, name := range databases {
			if name == c.Database {
				return e
			}
		}
		create = true
	} else {
		sync, _ := existing["sync"].(string)
		if !strings.Contains(sync, marker) {
			return errors.New("Gateway database is not owned by this library; use a dedicated native database")
		}
	}
	// An authenticated device only receives its server-bound native UID as a
	// channel. This permits first-enrollment identity adoption without runtime
	// database provisioning or accepting arbitrary ownership claims.
	sync := nativeSyncFunction(marker)

	body := map[string]any{"bucket": c.Database, "use_views": true, "num_index_replicas": 0, "enable_shared_bucket_access": true, "guest": map[string]any{"disabled": true}, "session_cookie_secure": true, "session_cookie_http_only": true, "sync": sync}
	p := "/_config"
	if create {
		p = "/"
	}
	return c.gateway(ctx, "PUT", p, body, nil)
}

func nativeSyncFunction(marker string) string {
	return fmt.Sprintf(`function(doc,oldDoc){ // %s
var user=doc.user||(oldDoc&&oldDoc.user);var scope=doc.dbId||(oldDoc&&oldDoc.dbId);
if(typeof user!=="string"||!/^[A-Za-z0-9_-]{1,128}$/.test(user)||(oldDoc&&oldDoc.user&&oldDoc.user!==user)||typeof scope!=="string"||scope.indexOf(user+"-")!==0){throw({forbidden:"outside native library"});}
requireAccess(user);channel([user,scope]);}`, marker)
}
