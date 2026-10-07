-- OAuth 2.1 for MCP clients such as Claude Web: dynamically registered
-- public clients and single-use PKCE authorization codes. Issued access
-- tokens are ordinary API tokens (alexandria_api_tokens), revocable in
-- Settings. Ported from UltraBridge (internal/mcpauth, web OAuth handlers),
-- with codes moved from process memory into the database.

CREATE TABLE alexandria_oauth_clients (
    client_id     text COLLATE "C" PRIMARY KEY,
    client_name   text NOT NULL DEFAULT '',
    redirect_uris text NOT NULL, -- JSON array of exact redirect URIs
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE alexandria_oauth_codes (
    code_hash      text COLLATE "C" PRIMARY KEY CHECK (code_hash ~ '^[0-9a-f]{64}$'),
    client_id      text COLLATE "C" NOT NULL REFERENCES alexandria_oauth_clients(client_id) ON DELETE CASCADE,
    redirect_uri   text NOT NULL,
    code_challenge text NOT NULL,
    expires_at     timestamptz NOT NULL
);
CREATE INDEX alexandria_oauth_codes_expiry ON alexandria_oauth_codes(expires_at);
