-- Owner-managed application configuration. Credentials are AES-GCM ciphertext;
-- the encryption key belongs to the deployment, never this database.
CREATE TABLE alexandria_provider_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 revision bigint NOT NULL DEFAULT 1,
 config jsonb NOT NULL,
 credential bytea NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE alexandria_provider_audit (
 revision bigint PRIMARY KEY,
 changed_at timestamptz NOT NULL DEFAULT now(),
 action text NOT NULL
);
CREATE TABLE alexandria_embedding_generation (
 id text PRIMARY KEY,
 endpoint text NOT NULL,
 model text NOT NULL,
 state text NOT NULL CHECK(state IN ('building','active','retired')),
 dimensions integer,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX alexandria_embedding_one_active ON alexandria_embedding_generation(state) WHERE state='active';
CREATE TABLE alexandria_embedding_work (
 generation text NOT NULL REFERENCES alexandria_embedding_generation(id),
 note_key text NOT NULL,
 page integer NOT NULL,
 version bigint NOT NULL DEFAULT 1,
 lease text NOT NULL DEFAULT '',
 lease_until timestamptz,
 next_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 failed boolean NOT NULL DEFAULT false,
 PRIMARY KEY(generation,note_key,page)
);
CREATE TABLE alexandria_embedding_vectors (
 generation text NOT NULL REFERENCES alexandria_embedding_generation(id),
 note_key text NOT NULL,
 page integer NOT NULL,
 chunk integer NOT NULL,
 body_hash text NOT NULL,
 dimensions integer NOT NULL,
 embedding vector NOT NULL,
 PRIMARY KEY(generation,note_key,page,chunk)
);
-- Queue text changes for active and building generations, without running OCR.
CREATE FUNCTION alexandria_queue_embedding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE g record; k text; p integer;
BEGIN
 PERFORM pg_advisory_xact_lock(716204821001);
 IF TG_OP='UPDATE' AND NEW.body_text IS NOT DISTINCT FROM OLD.body_text THEN RETURN NULL; END IF;
 IF TG_OP='DELETE' THEN k:=OLD.note_key; p:=OLD.page; ELSE k:=NEW.note_key; p:=NEW.page; END IF;
 FOR g IN SELECT id FROM alexandria_embedding_generation WHERE state IN ('active','building') ORDER BY id FOR UPDATE LOOP
  INSERT INTO alexandria_embedding_work(generation,note_key,page) VALUES(g.id,k,p)
  ON CONFLICT(generation,note_key,page) DO UPDATE SET version=alexandria_embedding_work.version+1,lease='',lease_until=NULL,next_at=now(),attempts=0,failed=false;
 END LOOP;
 RETURN NULL;
END $$;
CREATE TRIGGER alexandria_content_embedding AFTER INSERT OR UPDATE OR DELETE ON alexandria_note_content FOR EACH ROW EXECUTE FUNCTION alexandria_queue_embedding();
ALTER TABLE alexandria_page_ocr ADD COLUMN config_revision bigint NOT NULL DEFAULT 0;
ALTER TABLE boox_page_index ADD COLUMN config_revision bigint NOT NULL DEFAULT 0;
