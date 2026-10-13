-- Library reviews/dispatch survive browser disconnects and worker restarts.
CREATE TABLE alexandria_recognition_run (
 id uuid PRIMARY KEY,
 state text NOT NULL DEFAULT 'preparing' CHECK(state IN ('preparing','review','queued','paused','cancelled')),
 force boolean NOT NULL DEFAULT false,
 lease_token text NOT NULL DEFAULT '', lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE alexandria_recognition_run_notebook (
 run_id uuid NOT NULL REFERENCES alexandria_recognition_run(id), notebook_id text NOT NULL, title text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','ready','unavailable','excluded','queued')),
 pages jsonb NOT NULL DEFAULT '[]', version text NOT NULL DEFAULT '', detail text NOT NULL DEFAULT '',
 batch_id uuid NOT NULL DEFAULT gen_random_uuid(), queued integer NOT NULL DEFAULT 0, cursor integer NOT NULL DEFAULT 0, skipped integer NOT NULL DEFAULT 0,
 PRIMARY KEY(run_id,notebook_id)
);
CREATE INDEX alexandria_recognition_run_work ON alexandria_recognition_run_notebook(run_id,state);
ALTER TABLE alexandria_recognition_batch ADD COLUMN run_id uuid REFERENCES alexandria_recognition_run(id);
CREATE INDEX alexandria_recognition_batch_run ON alexandria_recognition_batch(run_id);
ALTER TABLE alexandria_recognition_job ADD COLUMN image_width integer NOT NULL DEFAULT 0;
ALTER TABLE alexandria_recognition_job ADD COLUMN image_height integer NOT NULL DEFAULT 0;
ALTER TABLE alexandria_recognition_job ADD COLUMN image_bytes integer NOT NULL DEFAULT 0;

-- Keep retries visible as history without counting their old failures as current work.
ALTER TABLE alexandria_recognition_job ADD COLUMN superseded_by uuid;
UPDATE alexandria_recognition_job j SET superseded_by=p.job_id FROM boox_page_index p
 WHERE j.source='boox' AND j.notebook_id=p.notebook_id AND j.page_id=p.page_id AND j.id<>p.job_id;
UPDATE alexandria_recognition_job j SET superseded_by=p.job_id FROM alexandria_page_dirty p
 WHERE j.source='client' AND j.page_id=p.page_id AND j.id<>p.job_id;
CREATE FUNCTION alexandria_link_recognition_attempts() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.job_id<>NEW.job_id THEN
  UPDATE alexandria_recognition_job SET superseded_by=NEW.job_id WHERE id=OLD.job_id;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER boox_link_recognition_attempts AFTER UPDATE ON boox_page_index FOR EACH ROW EXECUTE FUNCTION alexandria_link_recognition_attempts();
CREATE TRIGGER alexandria_link_recognition_attempts AFTER UPDATE ON alexandria_page_dirty FOR EACH ROW EXECUTE FUNCTION alexandria_link_recognition_attempts();
