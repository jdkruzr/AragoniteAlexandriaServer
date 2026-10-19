-- Notebook admission limits are independent of provider credentials and skips.
CREATE TABLE alexandria_recognition_allow_large (
 source text NOT NULL CHECK(source IN ('boox','client')),
 notebook_id text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(source,notebook_id)
);
CREATE TRIGGER aa_purge_guard BEFORE INSERT OR UPDATE ON alexandria_recognition_allow_large
 FOR EACH ROW EXECUTE FUNCTION boox_guard_purged_derived();

CREATE FUNCTION alexandria_recognition_page_limit() RETURNS integer LANGUAGE plpgsql STABLE AS $$
DECLARE v text;
BEGIN
 SELECT value#>>'{}' INTO v FROM alexandria_settings WHERE key='recognition_max_pages';
 IF v ~ '^[0-9]{1,5}$' AND v::integer BETWEEN 1 AND 20000 THEN RETURN v::integer; END IF;
 RETURN 100;
END $$;
CREATE FUNCTION alexandria_recognition_page_count(src text,nid text) RETURNS integer LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN src='boox' THEN coalesce((SELECT jsonb_array_length(boox_search_page_ids(body->'pageNameList'))
 FROM boox_projection WHERE document_id=nid AND domain='notebook' AND body->>'type'='1' AND NOT body ? 'commitType'),0)
 ELSE (SELECT count(*)::integer FROM fn_page WHERE notebook_id=nid AND deleted_at IS NULL) END;
$$;
CREATE FUNCTION alexandria_recognition_size_limited(src text,nid text,observed integer DEFAULT 0) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT greatest(observed,alexandria_recognition_page_count(src,nid))>alexandria_recognition_page_limit()
 AND NOT EXISTS(SELECT 1 FROM alexandria_recognition_allow_large WHERE source=src AND notebook_id=nid);
$$;

-- No per-page jobs are manufactured for a notebook rejected at admission.
-- Existing results and explicit pause/skip/cancel choices remain untouched.
CREATE FUNCTION alexandria_guard_recognition_size() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE src text; nid text;
BEGIN
 IF NEW.state NOT IN ('queued','processing') THEN RETURN NEW; END IF;
 src:=CASE WHEN TG_TABLE_NAME='boox_page_index' THEN 'boox' ELSE 'client' END;
 IF src='boox' THEN nid:=NEW.notebook_id;
 ELSE SELECT notebook_id INTO nid FROM fn_page WHERE id=NEW.page_id; END IF;
 IF NOT alexandria_recognition_size_limited(src,nid) THEN RETURN NEW; END IF;
 IF TG_OP='INSERT' OR NEW.state='processing' THEN RETURN NULL; END IF;
 IF OLD.state IN ('paused','skipped','cancelled') THEN RETURN NULL; END IF;
 IF OLD.state='ready' THEN
  IF src='boox' THEN
   IF NEW.text IS NOT DISTINCT FROM OLD.text THEN RETURN NULL; END IF;
  ELSE
   -- Retain the cached result, but do not leave a changed page marked ready.
   IF NEW.dirtied_at IS NOT DISTINCT FROM OLD.dirtied_at THEN RETURN NULL; END IF;
  END IF;
 END IF;
 NEW.state:='cancelled'; NEW.detail:='Notebook exceeds the recognition page limit. Review its size override in Jobs.';
 NEW.lease_until:=NULL;
 IF src='boox' THEN NEW.lease_token:=''; ELSE NEW.lease_token:=NULL; END IF;
 NEW.job_id:=OLD.job_id;
 RETURN NEW;
END $$;
CREATE TRIGGER zz_recognition_page_limit BEFORE INSERT OR UPDATE ON boox_page_index
 FOR EACH ROW EXECUTE FUNCTION alexandria_guard_recognition_size();
CREATE TRIGGER zz_recognition_page_limit BEFORE INSERT OR UPDATE ON alexandria_page_dirty
 FOR EACH ROW EXECUTE FUNCTION alexandria_guard_recognition_size();

CREATE FUNCTION alexandria_retire_oversized_recognition(src text,nid text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 IF NOT alexandria_recognition_size_limited(src,nid) THEN RETURN; END IF;
 IF src='boox' THEN
 UPDATE boox_page_index SET state='cancelled',detail='Notebook exceeds the recognition page limit. Review its size override in Jobs.',
 request_version=request_version+1,lease_token='',lease_until=NULL,updated_at=now()
 WHERE notebook_id=nid AND state IN ('queued','processing');
 ELSE
 UPDATE alexandria_page_dirty SET state='cancelled',detail='Notebook exceeds the recognition page limit. Review its size override in Jobs.',lease_token=NULL,lease_until=NULL
 WHERE page_id IN (SELECT id FROM fn_page WHERE notebook_id=nid) AND state IN ('queued','processing');
 END IF;
END $$;
-- Client sync can grow a notebook past the cap without touching its old pages.
CREATE FUNCTION alexandria_recognition_size_changed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.notebook_id=OLD.notebook_id AND NEW.deleted_at IS NOT DISTINCT FROM OLD.deleted_at THEN RETURN NULL; END IF;
 PERFORM alexandria_retire_oversized_recognition('client',NEW.notebook_id);
 RETURN NULL;
END $$;
CREATE TRIGGER zz_recognition_size_changed AFTER INSERT OR UPDATE ON fn_page
 FOR EACH ROW EXECUTE FUNCTION alexandria_recognition_size_changed();
-- Apply the default to pending work, retaining job identities and cached results.
SELECT alexandria_retire_oversized_recognition('boox',notebook_id) FROM (SELECT DISTINCT notebook_id FROM boox_page_index WHERE state IN ('queued','processing')) n;
SELECT alexandria_retire_oversized_recognition('client',notebook_id) FROM (SELECT DISTINCT p.notebook_id FROM alexandria_page_dirty d JOIN fn_page p ON p.id=d.page_id WHERE d.state IN ('queued','processing')) n;
