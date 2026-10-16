-- 2026-10-16: BOOX devices sometimes re-save every notebook record with
-- nothing changed but Couchbase's revision bookkeeping (_rev, _revisions) and
-- updatedAt. Five such waves in eight days each cleared the recognized text of
-- every page and queued the whole library again. Page content arrives as page
-- files (boox_blob_live), so a record change that touches only those fields
-- leaves recognition alone. A page file rebound to identical bytes doesn't
-- count either.
CREATE OR REPLACE FUNCTION boox_invalidate_page_index() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE nid text; skip boolean;
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
 skip:=EXISTS(SELECT 1 FROM alexandria_recognition_skip WHERE source='boox' AND notebook_id=nid);
 UPDATE boox_page_index SET state=CASE WHEN state IN ('paused','cancelled','skipped') THEN state WHEN skip THEN 'skipped' ELSE 'queued' END,
 request_version=request_version+1,lease_token='',lease_until=NULL,next_at=now()+interval '10 seconds',attempts=0,
 text='',detail=CASE WHEN state='skipped' THEN detail WHEN skip THEN 'Notebook is skipped in recognition.' ELSE 'Source changed; recognition result invalidated.' END,updated_at=now() WHERE notebook_id=nid;
 RETURN NULL;
END $$;
