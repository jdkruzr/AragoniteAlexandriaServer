CREATE TABLE IF NOT EXISTS loom_users (
    username text PRIMARY KEY,
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS loom_api_tokens (
    token_hash text PRIMARY KEY,
    label text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    revoked_at timestamptz
);
CREATE INDEX IF NOT EXISTS loom_api_tokens_active_idx ON loom_api_tokens(created_at) WHERE revoked_at IS NULL;
