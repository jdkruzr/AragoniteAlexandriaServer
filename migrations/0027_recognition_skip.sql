-- The owner can keep pages and notebooks out of recognition for good.
-- A skipped page is finished: nothing re-queues it (library runs, retries,
-- changes on the device) until the owner un-skips it.
ALTER TABLE boox_page_index DROP CONSTRAINT boox_page_index_state_check;
ALTER TABLE boox_page_index ADD CONSTRAINT boox_page_index_state_check
 CHECK(state IN ('queued','processing','ready','blocked','failed','paused','cancelled','skipped'));
ALTER TABLE alexandria_page_dirty DROP CONSTRAINT alexandria_page_dirty_state_check;
ALTER TABLE alexandria_page_dirty ADD CONSTRAINT alexandria_page_dirty_state_check
 CHECK(state IN ('queued','processing','ready','blocked','failed','paused','cancelled','skipped'));
ALTER TABLE alexandria_recognition_job DROP CONSTRAINT alexandria_recognition_job_state_check;
ALTER TABLE alexandria_recognition_job ADD CONSTRAINT alexandria_recognition_job_state_check
 CHECK(state IN ('queued','processing','ready','blocked','failed','paused','cancelled','skipped'));

-- Skipped notebooks: their pages are never queued, now or as they're added.
CREATE TABLE alexandria_recognition_skip (
 source text NOT NULL CHECK(source IN ('boox','client')),
 notebook_id text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(source,notebook_id)
);

CREATE OR REPLACE FUNCTION alexandria_recognition_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='alexandria_page_dirty' AND NEW.dirtied_at IS DISTINCT FROM OLD.dirtied_at THEN
  NEW.lease_token:=NULL; NEW.lease_until:=NULL;
  -- Edits and "reprocess" never undo a skip; only the owner's un-skip does.
  IF OLD.state='skipped' THEN NEW.state:='skipped'; NEW.detail:=OLD.detail;
  ELSIF NEW.state NOT IN ('paused','cancelled') THEN
   IF EXISTS(SELECT 1 FROM alexandria_recognition_skip s JOIN fn_page p ON p.notebook_id=s.notebook_id WHERE s.source='client' AND p.id=NEW.page_id) THEN
    NEW.state:='skipped'; NEW.detail:='Notebook is skipped in recognition.';
   ELSE
    NEW.state:='queued'; NEW.detail:='Page changed or recognition requested.';
   END IF;
  END IF;
 END IF;
 IF NEW.state='queued' AND OLD.state IN ('ready','blocked','failed','cancelled','skipped') THEN NEW.job_id:=gen_random_uuid(); END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION boox_recognition_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state='queued' AND OLD.state IN ('ready','blocked','failed','cancelled','skipped') THEN NEW.job_id:=gen_random_uuid(); END IF;
 RETURN NEW;
END $$;

-- New queue entries for a skipped notebook start skipped, whichever path
-- creates them (sync for Client pages, explicit requests for either source).
-- One function per table: a shared one can't name a column only one has.
CREATE FUNCTION boox_recognition_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state='queued' AND EXISTS(SELECT 1 FROM alexandria_recognition_skip WHERE source='boox' AND notebook_id=NEW.notebook_id) THEN
  NEW.state:='skipped'; NEW.detail:='Notebook is skipped in recognition.';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER boox_recognition_insert BEFORE INSERT ON boox_page_index FOR EACH ROW EXECUTE FUNCTION boox_recognition_insert();
CREATE FUNCTION alexandria_recognition_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state='queued' AND EXISTS(SELECT 1 FROM alexandria_recognition_skip s JOIN fn_page p ON p.notebook_id=s.notebook_id WHERE s.source='client' AND p.id=NEW.page_id) THEN
  NEW.state:='skipped'; NEW.detail:='Notebook is skipped in recognition.';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER alexandria_recognition_insert BEFORE INSERT ON alexandria_page_dirty FOR EACH ROW EXECUTE FUNCTION alexandria_recognition_insert();

CREATE OR REPLACE FUNCTION alexandria_track_recognition() RETURNS trigger LANGUAGE plpgsql AS $$
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
 CASE WHEN NEW.state='processing' THEN now() END,CASE WHEN NEW.state IN ('ready','blocked','failed','cancelled','skipped') THEN now() END)
 ON CONFLICT(id) DO UPDATE SET notebook_id=EXCLUDED.notebook_id,page_number=CASE WHEN EXCLUDED.page_number>0 THEN EXCLUDED.page_number ELSE alexandria_recognition_job.page_number END,state=EXCLUDED.state,
 attempts=EXCLUDED.attempts,detail=EXCLUDED.detail,model=CASE WHEN EXCLUDED.model<>'' THEN EXCLUDED.model ELSE alexandria_recognition_job.model END,
 next_at=EXCLUDED.next_at,updated_at=now(),started_at=coalesce(alexandria_recognition_job.started_at,EXCLUDED.started_at),finished_at=EXCLUDED.finished_at;
 RETURN NULL;
END $$;

-- A change on the device re-queues a notebook's pages, except paused,
-- cancelled and skipped ones; in a skipped notebook, changed pages are skipped.
CREATE OR REPLACE FUNCTION boox_invalidate_page_index() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE nid text; skip boolean;
BEGIN
 IF TG_TABLE_NAME='boox_projection' THEN
  IF TG_OP='DELETE' THEN nid:=OLD.document_id; ELSE nid:=NEW.document_id; END IF;
 ELSE
  IF TG_OP='DELETE' THEN nid:=split_part(OLD.native_key,'/',3); ELSE nid:=split_part(NEW.native_key,'/',3); END IF;
 END IF;
 skip:=EXISTS(SELECT 1 FROM alexandria_recognition_skip WHERE source='boox' AND notebook_id=nid);
 UPDATE boox_page_index SET state=CASE WHEN state IN ('paused','cancelled','skipped') THEN state WHEN skip THEN 'skipped' ELSE 'queued' END,
 request_version=request_version+1,lease_token='',lease_until=NULL,next_at=now()+interval '10 seconds',attempts=0,
 text='',detail=CASE WHEN state='skipped' THEN detail WHEN skip THEN 'Notebook is skipped in recognition.' ELSE 'Source changed; recognition result invalidated.' END,updated_at=now() WHERE notebook_id=nid;
 RETURN NULL;
END $$;
