-- BOOX tags: the firmware's TagModel rows, synced by ksync as NOTE_TREE
-- documents with modeType 1 (no "type"). One row is one tag on one page of
-- one notebook; tags made in the tag manager have no notebook or page.
-- Status 0 is enabled; 1 (deleted in the tag manager) and 2 (removed) are not.
CREATE VIEW boox_tag AS
 SELECT t.document_id AS tag_id,coalesce(t.body->>'documentId','') AS notebook_id,
 coalesce(t.body->>'pageUniqueId','') AS page_id,btrim(t.body->>'content') AS content,
 CASE WHEN jsonb_typeof(t.body->'createdAt')='number' THEN to_timestamp((t.body->>'createdAt')::numeric/1000) END AS created_at
 FROM boox_projection t WHERE t.domain='notebook' AND t.body->>'modeType'='1' AND NOT t.body ? 'type'
 AND NOT t.body ? 'commitType' AND t.body->>'uniqueId'=t.document_id
 AND coalesce(t.body->>'status','0')='0' AND btrim(coalesce(t.body->>'content',''))<>'';
CREATE INDEX boox_projection_tag_page ON boox_projection((body->>'documentId'),(body->>'pageUniqueId'))
 WHERE domain='notebook' AND body->>'modeType'='1';

-- A page's tags rank like keywords (weight B) in keyword search. They are
-- not part of the page text, so tagging never re-embeds a page.
CREATE FUNCTION boox_page_tags(nid text,pid text) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT coalesce(string_agg(DISTINCT content,' ' ORDER BY content),'') FROM boox_tag WHERE notebook_id=nid AND page_id=pid
$$;
CREATE OR REPLACE FUNCTION boox_refresh_search_page(nid text,pid text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO alexandria_note_content(note_key,page,title_text,keywords,body_text,source,source_document_id,source_page_id,source_revision)
 SELECT note_key,0,title,boox_page_tags(nid,pid),body,'boox-page',document_id,page_id,revision FROM boox_search_page_text WHERE document_id=nid AND page_id=pid
 ON CONFLICT(note_key,page) DO UPDATE SET title_text=excluded.title_text,keywords=excluded.keywords,body_text=excluded.body_text,source_revision=excluded.source_revision,indexed_at=now();
 DELETE FROM alexandria_note_content c WHERE c.source='boox-page' AND c.source_document_id=nid AND c.source_page_id=pid
 AND NOT EXISTS(SELECT 1 FROM boox_search_page_text p WHERE p.note_key=c.note_key);
END $$;
CREATE FUNCTION boox_search_tag_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' AND OLD.domain='notebook' AND OLD.body->>'modeType'='1' THEN
  PERFORM boox_refresh_search_page(coalesce(OLD.body->>'documentId',''),coalesce(OLD.body->>'pageUniqueId',''));
 END IF;
 IF TG_OP<>'DELETE' AND NEW.domain='notebook' AND NEW.body->>'modeType'='1' THEN
  PERFORM boox_refresh_search_page(coalesce(NEW.body->>'documentId',''),coalesce(NEW.body->>'pageUniqueId',''));
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER boox_search_tag_changed AFTER INSERT OR UPDATE OR DELETE ON boox_projection
 FOR EACH ROW EXECUTE FUNCTION boox_search_tag_changed();

-- Backfill tags already received. Only keywords change, so nothing is re-embedded.
UPDATE alexandria_note_content c SET keywords=boox_page_tags(c.source_document_id,c.source_page_id)
 WHERE c.source='boox-page' AND c.keywords IS DISTINCT FROM boox_page_tags(c.source_document_id,c.source_page_id);
