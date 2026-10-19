-- Notebook management intents and minimal permanent-deletion identities.
-- Payloads are scrubbed after purge; these identities intentionally outlive data.
CREATE TABLE boox_notebook_batch (
 id uuid PRIMARY KEY, fingerprint text NOT NULL, return_to text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE boox_operation ADD COLUMN started_at timestamptz;
ALTER TABLE boox_operation ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX boox_operation_management_batch ON boox_operation((payload->>'managementBatch'));
CREATE TABLE boox_notebook_purged (
 notebook_id text COLLATE "C" PRIMARY KEY, operation_id uuid NOT NULL,
 generation text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE boox_purged_document (
 document_id text COLLATE "C" PRIMARY KEY,
 notebook_id text COLLATE "C" NOT NULL REFERENCES boox_notebook_purged(notebook_id),
 pending boolean NOT NULL DEFAULT true
);
CREATE TABLE boox_purge_object (
 operation_id uuid NOT NULL, object_key text COLLATE "C" NOT NULL,
 PRIMARY KEY(operation_id,object_key)
);

CREATE OR REPLACE FUNCTION boox_recognition_notebook_deleted(nid text) RETURNS boolean
LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM boox_notebook_purged WHERE notebook_id=nid) OR
 EXISTS(SELECT 1 FROM boox_projection WHERE document_id=nid
  AND domain='notebook' AND body->>'type'='1' AND body->>'uniqueId'=document_id
  AND NOT body ? 'commitType' AND body->>'status'='0');
$$;

-- Derived writers may finish after their source was purged. Never recreate
-- their rows. A shared identity lock pairs with the purge admission lock.
CREATE FUNCTION boox_guard_purged_derived() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME<>'boox_page_index' THEN
  IF NEW.source<>'boox' THEN RETURN NEW; END IF;
 END IF;
 PERFORM pg_advisory_xact_lock_shared(hashtextextended('boox-notebook:'||NEW.notebook_id,0));
 IF EXISTS(SELECT 1 FROM boox_notebook_purged WHERE notebook_id=NEW.notebook_id) THEN RETURN NULL; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON boox_page_index FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_derived();
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON alexandria_recognition_job FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_derived();
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON alexandria_recognition_batch FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_derived();
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON alexandria_recognition_skip FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_derived();
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON alexandria_note_minutes FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_derived();
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON alexandria_transcription FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_derived();
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON alexandria_note_action FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_derived();
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON alexandria_note_action_item FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_derived();

CREATE FUNCTION boox_guard_purged_run() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM alexandria_recognition_run WHERE id=NEW.run_id AND source='boox') THEN
  PERFORM pg_advisory_xact_lock_shared(hashtextextended('boox-notebook:'||NEW.notebook_id,0));
  IF EXISTS(SELECT 1 FROM boox_notebook_purged WHERE notebook_id=NEW.notebook_id) THEN RETURN NULL; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON alexandria_recognition_run_notebook FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_run();

-- Bounded cleanup and previews must not repeatedly scan the whole asset history.
CREATE INDEX boox_blob_version_native_key ON boox_blob_version(native_key);
CREATE INDEX boox_blob_version_digest ON boox_blob_version(sha256,native_key) WHERE sha256 IS NOT NULL;
CREATE INDEX boox_revision_digest ON boox_revision(body_sha256);
