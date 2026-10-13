-- Search-only BOOX projections. No native writes and no recognition requests.
ALTER TABLE alexandria_note_content ADD COLUMN source_document_id text NOT NULL DEFAULT '';
ALTER TABLE alexandria_note_content ADD COLUMN source_page_id text NOT NULL DEFAULT '';
ALTER TABLE alexandria_note_content ADD COLUMN source_revision text NOT NULL DEFAULT '';
CREATE INDEX alexandria_content_source_document ON alexandria_note_content(source,source_document_id);

-- Match the supported PageIDs representations, rejecting malformed input.
CREATE FUNCTION boox_search_page_ids(raw jsonb) RETURNS jsonb LANGUAGE plpgsql IMMUTABLE AS $$
BEGIN
 IF jsonb_typeof(raw)='string' THEN
  BEGIN raw:=(raw#>>'{}')::jsonb; EXCEPTION WHEN invalid_text_representation THEN RETURN '[]'; END;
 END IF;
 IF jsonb_typeof(raw)='object' THEN raw:=raw->'pageNameList'; END IF;
 IF jsonb_typeof(raw) IS DISTINCT FROM 'array' THEN RETURN '[]'; END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(raw) v WHERE jsonb_typeof(v)<>'string') THEN RETURN '[]'; END IF;
 RETURN raw;
END $$;

CREATE VIEW boox_search_notebooks AS
 SELECT * FROM boox_projection n WHERE n.domain='notebook' AND n.body->>'type'='1'
 AND n.body->>'uniqueId'=n.document_id AND NOT n.body ? 'commitType'
 AND coalesce(n.body->>'status','1')='1';
CREATE VIEW boox_search_reading AS
 SELECT p.* FROM boox_projection p WHERE p.domain='reading' AND NOT p.body ? 'commitType'
 AND coalesce(p.body->>'status','0')='0' AND (p.body->>'modeType'='4' OR
 (p.body->>'modeType' IN ('1','2') AND EXISTS (
 SELECT 1 FROM boox_projection parent WHERE parent.native_uid=p.native_uid
 AND parent.document_id=p.native_uid||'#'||(p.body->>'documentId')
 AND parent.body->>'uniqueId'=p.body->>'documentId' AND parent.body->>'modeType'='4'
 AND parent.domain='reading' AND NOT parent.body ? 'commitType' AND coalesce(parent.body->>'status','0')='0')));

CREATE VIEW boox_search_page_text AS
 SELECT 'boox-page:'||jsonb_build_array(p.notebook_id,p.page_id)::text AS note_key,
 p.notebook_id AS document_id,p.page_id,ids.ordinality::integer AS page_number,
 coalesce(n.body->>'title','Untitled') AS title,p.text AS body,
 p.request_version::text||':'||p.input_version AS revision
 FROM boox_page_index p JOIN boox_search_notebooks n ON n.document_id=p.notebook_id
 CROSS JOIN LATERAL (SELECT id,min(ordinality) AS ordinality FROM jsonb_array_elements_text(boox_search_page_ids(n.body->'pageNameList')) WITH ORDINALITY listed(id,ordinality) GROUP BY id) ids
 WHERE ids.id=p.page_id AND p.state='ready' AND btrim(p.text)<>'';

CREATE VIEW boox_search_reading_text AS
 SELECT 'boox-reading:'||p.document_id AS note_key,p.document_id,
 coalesce(nullif(p.body->>'title',''),nullif(p.body->>'name',''),'Reading annotation') AS title,
 concat_ws(E'\n',nullif(p.body->>'title',''),nullif(p.body->>'name',''),nullif(p.body->>'quote',''),nullif(p.body->>'note','')) AS body,
 p.revision FROM boox_search_reading p;

CREATE FUNCTION boox_refresh_search_page(nid text,pid text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO alexandria_note_content(note_key,page,title_text,body_text,source,source_document_id,source_page_id,source_revision)
 SELECT note_key,0,title,body,'boox-page',document_id,page_id,revision FROM boox_search_page_text WHERE document_id=nid AND page_id=pid
 ON CONFLICT(note_key,page) DO UPDATE SET title_text=excluded.title_text,body_text=excluded.body_text,source_revision=excluded.source_revision,indexed_at=now();
 DELETE FROM alexandria_note_content c WHERE c.source='boox-page' AND c.source_document_id=nid AND c.source_page_id=pid
 AND NOT EXISTS(SELECT 1 FROM boox_search_page_text p WHERE p.note_key=c.note_key);
END $$;
CREATE FUNCTION boox_search_page_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN PERFORM boox_refresh_search_page(OLD.notebook_id,OLD.page_id);
 ELSIF TG_OP='UPDATE' AND (OLD.notebook_id,OLD.page_id) IS DISTINCT FROM (NEW.notebook_id,NEW.page_id) THEN PERFORM boox_refresh_search_page(OLD.notebook_id,OLD.page_id); END IF;
 IF TG_OP<>'DELETE' THEN PERFORM boox_refresh_search_page(NEW.notebook_id,NEW.page_id); END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER boox_search_page_changed AFTER INSERT OR UPDATE OR DELETE ON boox_page_index
 FOR EACH ROW EXECUTE FUNCTION boox_search_page_changed();

CREATE FUNCTION boox_refresh_search_reading(doc text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO alexandria_note_content(note_key,page,title_text,body_text,source,source_document_id,source_revision)
 SELECT note_key,0,title,body,'boox-reading',document_id,revision FROM boox_search_reading_text WHERE document_id=doc AND btrim(body)<>''
 ON CONFLICT(note_key,page) DO UPDATE SET title_text=excluded.title_text,body_text=excluded.body_text,source_revision=excluded.source_revision,indexed_at=now();
 DELETE FROM alexandria_note_content c WHERE c.source='boox-reading' AND c.source_document_id=doc
 AND NOT EXISTS(SELECT 1 FROM boox_search_reading_text p WHERE p.note_key=c.note_key AND btrim(p.body)<>'');
END $$;
CREATE FUNCTION boox_search_projection_changed() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE doc text; child record;
BEGIN
 IF TG_OP='DELETE' THEN doc:=OLD.document_id; ELSE doc:=NEW.document_id; END IF;
 -- Parent visibility changes must refresh its children, including arrival after children.
 PERFORM boox_refresh_search_reading(doc);
 FOR child IN SELECT document_id FROM boox_projection WHERE domain='reading' AND native_uid||'#'||(body->>'documentId')=doc LOOP
  PERFORM boox_refresh_search_reading(child.document_id);
 END LOOP;
 -- The existing source invalidation trigger clears recognized pages; this also
 -- handles metadata without a recognition row and provides query-independent cleanup.
 DELETE FROM alexandria_note_content c WHERE c.source='boox-page' AND c.source_document_id=doc
 AND NOT EXISTS(SELECT 1 FROM boox_search_page_text p WHERE p.note_key=c.note_key);
 RETURN NULL;
END $$;
CREATE TRIGGER boox_search_projection_changed AFTER INSERT OR UPDATE OR DELETE ON boox_projection
 FOR EACH ROW EXECUTE FUNCTION boox_search_projection_changed();

-- Delete vectors immediately, including retired generations. The existing work
-- trigger still invalidates leases so late responses cannot resurrect them.
CREATE FUNCTION alexandria_remove_search_vectors() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM alexandria_embedding_vectors WHERE note_key=OLD.note_key AND page=OLD.page;
 DELETE FROM alexandria_embeddings WHERE note_key=OLD.note_key AND page=OLD.page;
 RETURN NULL;
END $$;
CREATE TRIGGER alexandria_remove_search_vectors AFTER DELETE ON alexandria_note_content
 FOR EACH ROW EXECUTE FUNCTION alexandria_remove_search_vectors();

-- Query-time source checks defend against stale projections as well as queued cleanup.
CREATE VIEW alexandria_search_live AS
 SELECT c.note_key,c.page,c.title_text,c.body_text,c.search_document,'client'::text AS source,
 'Notebook page'::text AS kind,p.notebook_id,coalesce(n.name,'') AS title,p.id AS page_id,0::integer AS page_number
 FROM alexandria_note_content c JOIN fn_page p ON c.note_key='forestnote://'||p.notebook_id||'/'||p.id AND p.deleted_at IS NULL
 JOIN fn_notebook n ON n.id=p.notebook_id AND n.deleted_at IS NULL WHERE c.source='forestnote'
 UNION ALL
 SELECT c.note_key,c.page,c.title_text,c.body_text,c.search_document,'boox','Recognized page',p.document_id,p.title,p.page_id,p.page_number
 FROM alexandria_note_content c JOIN boox_search_page_text p ON c.note_key=p.note_key AND c.body_text=p.body AND c.source_revision=p.revision WHERE c.source='boox-page'
 UNION ALL
 SELECT c.note_key,c.page,c.title_text,c.body_text,c.search_document,'boox','Reading',p.document_id,p.title,'',0
 FROM alexandria_note_content c JOIN boox_search_reading_text p ON c.note_key=p.note_key AND c.body_text=p.body AND c.source_revision=p.revision WHERE c.source='boox-reading';

-- Backfill only text that already exists. Never enqueue OCR or enable embedding.
INSERT INTO alexandria_note_content(note_key,page,title_text,body_text,source,source_document_id,source_page_id,source_revision)
 SELECT note_key,0,title,body,'boox-page',document_id,page_id,revision FROM boox_search_page_text;
INSERT INTO alexandria_note_content(note_key,page,title_text,body_text,source,source_document_id,source_revision)
 SELECT note_key,0,title,body,'boox-reading',document_id,revision FROM boox_search_reading_text WHERE btrim(body)<>'';
