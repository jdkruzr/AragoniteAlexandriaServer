-- Re-recognizing a page no longer clears its text while it waits (the queue
-- keeps the last result; a change on the device still clears it through
-- boox_invalidate_page_index). Search therefore shows any page with text,
-- whatever its queue state, so a library re-run doesn't take pages out of
-- search or re-embed unchanged text.
CREATE OR REPLACE VIEW boox_search_page_text AS
 SELECT 'boox-page:'||jsonb_build_array(p.notebook_id,p.page_id)::text AS note_key,
 p.notebook_id AS document_id,p.page_id,ids.ordinality::integer AS page_number,
 coalesce(n.body->>'title','Untitled') AS title,p.text AS body,
 p.request_version::text||':'||p.input_version AS revision
 FROM boox_page_index p JOIN boox_search_notebooks n ON n.document_id=p.notebook_id
 CROSS JOIN LATERAL (SELECT id,min(ordinality) AS ordinality FROM jsonb_array_elements_text(boox_search_page_ids(n.body->'pageNameList')) WITH ORDINALITY listed(id,ordinality) GROUP BY id) ids
 WHERE ids.id=p.page_id AND btrim(p.text)<>'';
