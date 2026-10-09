-- One management record per recognition request; source queues remain authoritative.
CREATE TABLE alexandria_recognition_batch (
 id uuid PRIMARY KEY, source text NOT NULL CHECK(source IN ('boox','client')),
 notebook_id text NOT NULL, title text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE alexandria_recognition_job (
 id uuid PRIMARY KEY, batch_id uuid REFERENCES alexandria_recognition_batch(id),
 source text NOT NULL CHECK(source IN ('boox','client')), notebook_id text NOT NULL DEFAULT '',
 page_id text NOT NULL, page_number integer NOT NULL DEFAULT 0,
 state text NOT NULL CHECK(state IN ('queued','processing','ready','blocked','failed','paused','cancelled')),
 attempts integer NOT NULL DEFAULT 0, detail text NOT NULL DEFAULT '', model text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 started_at timestamptz, finished_at timestamptz, next_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alexandria_recognition_job_list ON alexandria_recognition_job(created_at DESC,id);
CREATE INDEX alexandria_recognition_job_batch ON alexandria_recognition_job(batch_id);
ALTER TABLE boox_page_index DROP CONSTRAINT boox_page_index_state_check;
ALTER TABLE boox_page_index ADD CHECK(state IN ('queued','processing','ready','blocked','failed','paused','cancelled'));
ALTER TABLE boox_page_index ADD COLUMN job_id uuid NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE alexandria_page_dirty ADD COLUMN job_id uuid NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE alexandria_page_dirty ADD COLUMN state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','processing','ready','blocked','failed','paused','cancelled'));
ALTER TABLE alexandria_page_dirty ADD COLUMN detail text NOT NULL DEFAULT '';
ALTER TABLE alexandria_page_dirty ADD COLUMN lease_token uuid;

CREATE FUNCTION alexandria_recognition_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='alexandria_page_dirty' AND NEW.dirtied_at IS DISTINCT FROM OLD.dirtied_at THEN
  NEW.lease_token:=NULL; NEW.lease_until:=NULL;
  IF NEW.state NOT IN ('paused','cancelled') THEN NEW.state:='queued'; NEW.detail:='Page changed or recognition requested.'; END IF;
 END IF;
 IF NEW.state='queued' AND OLD.state IN ('ready','blocked','failed','cancelled') THEN NEW.job_id:=gen_random_uuid(); END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER alexandria_recognition_request BEFORE UPDATE ON alexandria_page_dirty FOR EACH ROW EXECUTE FUNCTION alexandria_recognition_request();
-- BOOX has no dirtied_at field: a separate function avoids record field lookup.
CREATE FUNCTION boox_recognition_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state='queued' AND OLD.state IN ('ready','blocked','failed','cancelled') THEN NEW.job_id:=gen_random_uuid(); END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER boox_recognition_request BEFORE UPDATE ON boox_page_index FOR EACH ROW EXECUTE FUNCTION boox_recognition_request();

CREATE FUNCTION alexandria_track_recognition() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE src text; nid text; num integer; msg text; mdl text;
BEGIN
 src:=CASE WHEN TG_TABLE_NAME='boox_page_index' THEN 'boox' ELSE 'client' END;
 IF TG_OP='DELETE' THEN
  UPDATE alexandria_recognition_job SET state=CASE WHEN OLD.state='ready' THEN 'ready' ELSE 'cancelled' END,
   finished_at=now(),updated_at=now(),detail=CASE WHEN OLD.state='ready' THEN OLD.detail ELSE 'Queue entry removed.' END WHERE id=OLD.job_id;
  RETURN NULL;
 END IF;
 IF src='boox' THEN nid:=NEW.notebook_id; num:=NEW.page_number; mdl:=NEW.model;
 ELSE SELECT notebook_id INTO nid FROM fn_page WHERE id=NEW.page_id; num:=0; mdl:=''; END IF;
 IF TG_OP='UPDATE' AND OLD.job_id<>NEW.job_id THEN
  UPDATE alexandria_recognition_job SET state='cancelled',finished_at=now(),updated_at=now(),detail='Superseded by a new request.'
   WHERE id=OLD.job_id AND state IN ('queued','processing','paused');
 END IF;
 INSERT INTO alexandria_recognition_job(id,source,notebook_id,page_id,page_number,state,attempts,detail,model,next_at,started_at,finished_at)
 VALUES(NEW.job_id,src,coalesce(nid,''),NEW.page_id,num,NEW.state,NEW.attempts,NEW.detail,mdl,NEW.next_at,
 CASE WHEN NEW.state='processing' THEN now() END,CASE WHEN NEW.state IN ('ready','blocked','failed','cancelled') THEN now() END)
 ON CONFLICT(id) DO UPDATE SET notebook_id=EXCLUDED.notebook_id,page_number=CASE WHEN EXCLUDED.page_number>0 THEN EXCLUDED.page_number ELSE alexandria_recognition_job.page_number END,state=EXCLUDED.state,
 attempts=EXCLUDED.attempts,detail=EXCLUDED.detail,model=CASE WHEN EXCLUDED.model<>'' THEN EXCLUDED.model ELSE alexandria_recognition_job.model END,
 next_at=EXCLUDED.next_at,updated_at=now(),started_at=coalesce(alexandria_recognition_job.started_at,EXCLUDED.started_at),finished_at=EXCLUDED.finished_at;
 RETURN NULL;
END $$;
CREATE TRIGGER boox_track_recognition AFTER INSERT OR UPDATE OR DELETE ON boox_page_index FOR EACH ROW EXECUTE FUNCTION alexandria_track_recognition();
CREATE TRIGGER alexandria_track_recognition AFTER INSERT OR UPDATE OR DELETE ON alexandria_page_dirty FOR EACH ROW EXECUTE FUNCTION alexandria_track_recognition();
UPDATE boox_page_index SET state=state;
UPDATE alexandria_page_dirty SET state=state;

CREATE OR REPLACE FUNCTION boox_invalidate_page_index() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE nid text;
BEGIN
 IF TG_TABLE_NAME='boox_projection' THEN
  IF TG_OP='DELETE' THEN nid:=OLD.document_id; ELSE nid:=NEW.document_id; END IF;
 ELSE
  IF TG_OP='DELETE' THEN nid:=split_part(OLD.native_key,'/',3); ELSE nid:=split_part(NEW.native_key,'/',3); END IF;
 END IF;
 UPDATE boox_page_index SET state=CASE WHEN state IN ('paused','cancelled') THEN state ELSE 'queued' END,
 request_version=request_version+1,lease_token='',lease_until=NULL,next_at=now()+interval '10 seconds',attempts=0,
 text='',detail='Source changed; recognition result invalidated.',updated_at=now() WHERE notebook_id=nid;
 RETURN NULL;
END $$;
