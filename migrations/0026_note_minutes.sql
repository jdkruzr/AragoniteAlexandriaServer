-- Note minutes: owner-requested transcription of native notebook recordings and a
-- summary combining transcripts with recognized handwriting. Derived, server-only
-- state; native notebooks, devices and Talk Insights files are never written.

-- Separate encrypted credentials for the transcription and summary providers.
ALTER TABLE alexandria_provider_settings ADD COLUMN transcription_credential bytea NOT NULL DEFAULT '';
ALTER TABLE alexandria_provider_settings ADD COLUMN summary_credential bytea NOT NULL DEFAULT '';

-- One transcript per recording body. A changed recording is a new row; settings
-- changes are detected through config_hash when minutes are requested again.
CREATE TABLE alexandria_transcription (
 source text NOT NULL CHECK (source IN ('boox')),
 notebook_id text COLLATE "C" NOT NULL,
 resource_key text COLLATE "C" NOT NULL,
 sha256 text NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','processing','ready','failed')),
 lease_token text NOT NULL DEFAULT '',
 lease_until timestamptz,
 next_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 text text NOT NULL DEFAULT '',
 segments jsonb NOT NULL DEFAULT '[]',
 language text NOT NULL DEFAULT '',
 duration_ms bigint NOT NULL DEFAULT 0,
 model text NOT NULL DEFAULT '',
 config_hash text NOT NULL DEFAULT '',
 detail text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(source,notebook_id,resource_key,sha256)
);
CREATE INDEX alexandria_transcription_due ON alexandria_transcription(next_at) WHERE state IN ('queued','processing');

-- The current minutes for a notebook. Recordings are fixed at request time;
-- staleness is computed by comparing input_hash with current inputs.
CREATE TABLE alexandria_note_minutes (
 source text NOT NULL CHECK (source IN ('boox')),
 notebook_id text COLLATE "C" NOT NULL,
 request_id uuid NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','processing','ready','failed','cancelled')),
 lease_token text NOT NULL DEFAULT '',
 lease_until timestamptz,
 next_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 recordings jsonb NOT NULL DEFAULT '[]',
 input_hash text NOT NULL DEFAULT '',
 topic text NOT NULL DEFAULT '',
 keywords jsonb NOT NULL DEFAULT '[]',
 key_points jsonb NOT NULL DEFAULT '[]',
 conclusion text NOT NULL DEFAULT '',
 metadata jsonb NOT NULL DEFAULT '{}',
 model text NOT NULL DEFAULT '',
 detail text NOT NULL DEFAULT '',
 requested_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(source,notebook_id)
);
CREATE INDEX alexandria_note_minutes_due ON alexandria_note_minutes(next_at) WHERE state IN ('queued','processing');

-- Searchable minutes: topic and keywords rank like titles, then points,
-- conclusion and transcripts. Only ready minutes of live notebooks are indexed.
CREATE VIEW boox_search_minutes_text AS
 SELECT 'boox-minutes:'||m.notebook_id AS note_key,m.notebook_id AS document_id,
 coalesce(n.body->>'title','Untitled')||' — '||m.topic AS title,
 coalesce((SELECT string_agg(k,' ' ORDER BY o) FROM jsonb_array_elements_text(m.keywords) WITH ORDINALITY w(k,o)),'') AS keywords,
 concat_ws(E'\n',m.topic,(SELECT string_agg(p->>'text',E'\n' ORDER BY o) FROM jsonb_array_elements(m.key_points) WITH ORDINALITY k(p,o)),m.conclusion,
  (SELECT string_agg(t.text,E'\n' ORDER BY r.o) FROM jsonb_array_elements(m.recordings) WITH ORDINALITY r(v,o)
   JOIN alexandria_transcription t ON t.source=m.source AND t.notebook_id=m.notebook_id AND t.resource_key=r.v->>'key' AND t.sha256=r.v->>'sha256' AND t.state='ready')) AS body,
 m.input_hash AS revision
 FROM alexandria_note_minutes m JOIN boox_search_notebooks n ON n.document_id=m.notebook_id
 WHERE m.source='boox' AND m.state='ready';

CREATE FUNCTION boox_refresh_search_minutes(nid text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO alexandria_note_content(note_key,page,title_text,keywords,body_text,source,source_document_id,source_revision)
 SELECT note_key,0,title,keywords,body,'boox-minutes',document_id,revision FROM boox_search_minutes_text WHERE document_id=nid AND btrim(body)<>''
 ON CONFLICT(note_key,page) DO UPDATE SET title_text=excluded.title_text,keywords=excluded.keywords,body_text=excluded.body_text,source_revision=excluded.source_revision,indexed_at=now();
 DELETE FROM alexandria_note_content c WHERE c.source='boox-minutes' AND c.source_document_id=nid
 AND NOT EXISTS(SELECT 1 FROM boox_search_minutes_text p WHERE p.note_key=c.note_key AND btrim(p.body)<>'');
END $$;
CREATE FUNCTION boox_search_minutes_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='boox_projection' THEN
  PERFORM boox_refresh_search_minutes(CASE WHEN TG_OP='DELETE' THEN OLD.document_id ELSE NEW.document_id END);
 ELSE
  IF TG_OP<>'INSERT' THEN PERFORM boox_refresh_search_minutes(OLD.notebook_id); END IF;
  IF TG_OP<>'DELETE' THEN PERFORM boox_refresh_search_minutes(NEW.notebook_id); END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER boox_search_minutes_changed AFTER INSERT OR UPDATE OR DELETE ON alexandria_note_minutes
 FOR EACH ROW EXECUTE FUNCTION boox_search_minutes_changed();
CREATE TRIGGER boox_search_minutes_projection AFTER INSERT OR UPDATE OR DELETE ON boox_projection
 FOR EACH ROW EXECUTE FUNCTION boox_search_minutes_changed();

CREATE OR REPLACE VIEW alexandria_search_live AS
 SELECT c.note_key,c.page,c.title_text,c.body_text,c.search_document,'client'::text AS source,
 'Notebook page'::text AS kind,p.notebook_id,coalesce(n.name,'') AS title,p.id AS page_id,0::integer AS page_number
 FROM alexandria_note_content c JOIN fn_page p ON c.note_key='forestnote://'||p.notebook_id||'/'||p.id AND p.deleted_at IS NULL
 JOIN fn_notebook n ON n.id=p.notebook_id AND n.deleted_at IS NULL WHERE c.source='forestnote'
 UNION ALL
 SELECT c.note_key,c.page,c.title_text,c.body_text,c.search_document,'boox','Recognized page',p.document_id,p.title,p.page_id,p.page_number
 FROM alexandria_note_content c JOIN boox_search_page_text p ON c.note_key=p.note_key AND c.body_text=p.body AND c.source_revision=p.revision WHERE c.source='boox-page'
 UNION ALL
 SELECT c.note_key,c.page,c.title_text,c.body_text,c.search_document,'boox','Reading',p.document_id,p.title,'',0
 FROM alexandria_note_content c JOIN boox_search_reading_text p ON c.note_key=p.note_key AND c.body_text=p.body AND c.source_revision=p.revision WHERE c.source='boox-reading'
 UNION ALL
 SELECT c.note_key,c.page,c.title_text,c.body_text,c.search_document,'boox','Minutes',p.document_id,p.title,'',0
 FROM alexandria_note_content c JOIN boox_search_minutes_text p ON c.note_key=p.note_key AND c.body_text=p.body AND c.source_revision=p.revision WHERE c.source='boox-minutes';
