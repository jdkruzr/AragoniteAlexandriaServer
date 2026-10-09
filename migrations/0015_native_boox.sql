-- Native BOOX is an independent source; no ForestNote/reader row mirroring.
CREATE TABLE boox_enrollment (
 code_hash text PRIMARY KEY, expires_at timestamptz NOT NULL, used_at timestamptz
);
CREATE TABLE boox_device (
 id uuid PRIMARY KEY, installation_id uuid NOT NULL UNIQUE, model text NOT NULL,
 identity_hint text NOT NULL, native_fingerprint text, account text NOT NULL UNIQUE,
 login_hash text NOT NULL, companion_hash text NOT NULL UNIQUE,
 bearer_hash text UNIQUE, bearer_expires timestamptz,
 generation text NOT NULL, revoked boolean NOT NULL DEFAULT false,
 requested_channels jsonb NOT NULL DEFAULT '[]', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE boox_grant (
 id text PRIMARY KEY, device_id uuid NOT NULL REFERENCES boox_device(id),
 kind text NOT NULL CHECK(kind IN ('session','oss')), secret text NOT NULL,
 security_token text NOT NULL DEFAULT '', expires_at timestamptz NOT NULL
);
CREATE INDEX boox_grant_device ON boox_grant(device_id);
CREATE TABLE boox_blob_version (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 native_key text COLLATE "C" NOT NULL, sha256 text, md5 text, bytes bigint,
 content_type text, device_id uuid REFERENCES boox_device(id),
 deleted boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(),
 CHECK(deleted OR (length(sha256)=64 AND length(md5)=32 AND bytes>=0))
);
CREATE TABLE boox_blob_live (
 native_key text COLLATE "C" PRIMARY KEY,
 version_id bigint NOT NULL REFERENCES boox_blob_version(id)
);
CREATE TABLE boox_cursor (id smallint PRIMARY KEY CHECK(id=1), cursor jsonb NOT NULL DEFAULT '0');
INSERT INTO boox_cursor(id) VALUES(1);
CREATE TABLE boox_revision (
 document_id text COLLATE "C" NOT NULL, revision text NOT NULL,
 body_sha256 text NOT NULL, body jsonb NOT NULL, deleted boolean NOT NULL,
 observed_at timestamptz NOT NULL DEFAULT now(), provenance text NOT NULL DEFAULT 'gateway_changes',
 PRIMARY KEY(document_id,revision)
);
CREATE TABLE boox_projection (
 document_id text COLLATE "C" PRIMARY KEY, revision text NOT NULL,
 domain text NOT NULL, native_type text NOT NULL, native_uid text,
 body jsonb NOT NULL, supported boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE boox_operation (
 id uuid PRIMARY KEY, idempotency_key text NOT NULL UNIQUE,
 kind text NOT NULL, document_id text NOT NULL, expected_revision text NOT NULL,
 selected_revision text, payload jsonb NOT NULL, state text NOT NULL DEFAULT 'preview',
 published_revision text, error_code text, created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE boox_receipt (
 operation_id uuid NOT NULL REFERENCES boox_operation(id), device_id uuid NOT NULL REFERENCES boox_device(id),
 delivery text NOT NULL DEFAULT 'unknown', application text NOT NULL DEFAULT 'unverified',
 updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(operation_id,device_id)
);
