-- A native deletion is an authoritative status=0 notebook, not a temporarily
-- absent projection or an incomplete sync record. Keep history, but do not
-- manufacture another recognition attempt for content that no longer exists.
CREATE FUNCTION boox_recognition_notebook_deleted(nid text) RETURNS boolean
LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM boox_projection WHERE document_id=nid
  AND domain='notebook' AND body->>'type'='1' AND body->>'uniqueId'=document_id
  AND NOT body ? 'commitType' AND body->>'status'='0');
$$;

CREATE FUNCTION boox_cancel_deleted_recognition(nid text) RETURNS void
LANGUAGE sql AS $$
 UPDATE boox_page_index SET
  state=CASE WHEN state IN ('paused','cancelled','skipped') THEN state ELSE 'cancelled' END,
  detail=CASE WHEN state IN ('paused','cancelled','skipped') THEN detail
   ELSE 'Notebook was deleted; recognition cancelled.' END,
  request_version=request_version+1,lease_token='',lease_until=NULL,text='',updated_at=now()
 WHERE notebook_id=nid AND boox_recognition_notebook_deleted(nid)
  AND (state NOT IN ('paused','cancelled','skipped') OR text<>'' OR lease_token<>'' OR lease_until IS NOT NULL);
$$;

CREATE OR REPLACE FUNCTION boox_invalidate_page_index() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE nid text; skip boolean; restored boolean;
BEGIN
 IF TG_TABLE_NAME='boox_projection' THEN
  IF TG_OP='UPDATE' AND OLD.domain=NEW.domain AND OLD.document_id=NEW.document_id
   AND (OLD.body - '_rev' - '_revisions' - 'updatedAt') = (NEW.body - '_rev' - '_revisions' - 'updatedAt') THEN
   RETURN NULL;
  END IF;
  IF TG_OP='DELETE' THEN nid:=OLD.document_id; ELSE nid:=NEW.document_id; END IF;
 ELSE
  IF TG_OP='UPDATE' AND OLD.native_key=NEW.native_key AND (OLD.version_id=NEW.version_id OR
   (SELECT o.sha256=n.sha256 AND NOT n.deleted AND NOT o.deleted FROM boox_blob_version o, boox_blob_version n WHERE o.id=OLD.version_id AND n.id=NEW.version_id)) THEN
   RETURN NULL;
  END IF;
  IF TG_OP='DELETE' THEN nid:=split_part(OLD.native_key,'/',3); ELSE nid:=split_part(NEW.native_key,'/',3); END IF;
 END IF;
 IF boox_recognition_notebook_deleted(nid) THEN
  PERFORM boox_cancel_deleted_recognition(nid);
  RETURN NULL;
 END IF;
 restored:=EXISTS(SELECT 1 FROM boox_projection WHERE document_id=nid
  AND domain='notebook' AND body->>'type'='1' AND body->>'uniqueId'=document_id
  AND NOT body ? 'commitType' AND body->>'status'='1');
 skip:=EXISTS(SELECT 1 FROM alexandria_recognition_skip WHERE source='boox' AND notebook_id=nid);
 UPDATE boox_page_index SET state=CASE
  WHEN state IN ('paused','skipped') THEN state
  WHEN state='cancelled' AND (detail<>'Notebook was deleted; recognition cancelled.' OR NOT restored) THEN state
  WHEN skip THEN 'skipped' ELSE 'queued' END,
 request_version=request_version+1,lease_token='',lease_until=NULL,next_at=now()+interval '10 seconds',attempts=0,
 text='',detail=CASE
  WHEN state IN ('paused','skipped') THEN detail
  WHEN state='cancelled' AND (detail<>'Notebook was deleted; recognition cancelled.' OR NOT restored) THEN detail
  WHEN skip THEN 'Notebook is skipped in recognition.' ELSE 'Source changed; recognition result invalidated.' END,
 updated_at=now() WHERE notebook_id=nid;
 RETURN NULL;
END $$;

-- Defense in depth for stale snapshots and retry/resume requests. Application
-- enqueue transactions lock the projection before the queue; these triggers
-- intentionally do not acquire a projection lock in the opposite order.
CREATE OR REPLACE FUNCTION boox_recognition_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state='queued' AND boox_recognition_notebook_deleted(NEW.notebook_id) THEN RETURN NULL; END IF;
 IF NEW.state='queued' AND EXISTS(SELECT 1 FROM alexandria_recognition_skip WHERE source='boox' AND notebook_id=NEW.notebook_id) THEN
  NEW.state:='skipped'; NEW.detail:='Notebook is skipped in recognition.';
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION boox_recognition_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state='queued' AND boox_recognition_notebook_deleted(NEW.notebook_id) THEN RETURN NULL; END IF;
 IF NEW.state='queued' AND OLD.state IN ('ready','blocked','failed','cancelled','skipped') THEN NEW.job_id:=gen_random_uuid(); END IF;
 RETURN NEW;
END $$;

-- Retire obsolete current work left by earlier versions. No historical jobs,
-- source records, skip policies, or cleanup markers are deleted.
SELECT boox_cancel_deleted_recognition(notebook_id) FROM
 (SELECT DISTINCT notebook_id FROM boox_page_index WHERE boox_recognition_notebook_deleted(notebook_id)) deleted;
