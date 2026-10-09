-- Derived server-only BOOX recognition. Native winners and device OCR are never
-- overwritten. Work is explicitly requested; changed inputs invalidate results.
CREATE TABLE boox_page_index (
 notebook_id text COLLATE "C" NOT NULL,
 page_id text COLLATE "C" NOT NULL,
 page_number integer NOT NULL CHECK (page_number > 0),
 state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','processing','ready','blocked','failed')),
 request_version bigint NOT NULL DEFAULT 1,
 lease_token text NOT NULL DEFAULT '',
 lease_until timestamptz,
 next_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 input_version text NOT NULL DEFAULT '',
 text text NOT NULL DEFAULT '',
 ocr_hash text NOT NULL DEFAULT '',
 ocr_text text NOT NULL DEFAULT '',
 model text NOT NULL DEFAULT '',
 detail text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(notebook_id,page_id)
);
CREATE INDEX boox_page_index_due ON boox_page_index(next_at) WHERE state IN ('queued','processing');
CREATE INDEX boox_page_index_words ON boox_page_index USING gin(to_tsvector('simple',text)) WHERE state='ready';

CREATE FUNCTION boox_invalidate_page_index() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE nid text;
BEGIN
 IF TG_TABLE_NAME='boox_projection' THEN
  IF TG_OP='DELETE' THEN nid := OLD.document_id; ELSE nid := NEW.document_id; END IF;
 ELSE
  IF TG_OP='DELETE' THEN nid := split_part(OLD.native_key,'/',3); ELSE nid := split_part(NEW.native_key,'/',3); END IF;
 END IF;
 UPDATE boox_page_index SET state='queued',request_version=request_version+1,
  lease_token='',lease_until=NULL,next_at=now()+interval '10 seconds',attempts=0,
  text='',detail='Source changed; waiting to process.',updated_at=now()
 WHERE notebook_id=nid;
 RETURN NULL;
END $$;
CREATE TRIGGER boox_projection_page_index AFTER INSERT OR UPDATE OR DELETE ON boox_projection
 FOR EACH ROW EXECUTE FUNCTION boox_invalidate_page_index();
CREATE TRIGGER boox_blob_page_index AFTER INSERT OR UPDATE OR DELETE ON boox_blob_live
 FOR EACH ROW EXECUTE FUNCTION boox_invalidate_page_index();
