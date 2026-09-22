-- Preserve immutable migration history and rename existing content in place.
ALTER TABLE loom_settings RENAME TO alexandria_settings;
ALTER TABLE loom_sources RENAME TO alexandria_sources;
ALTER TABLE loom_objects RENAME TO alexandria_objects;
ALTER TABLE loom_note_content RENAME TO alexandria_note_content;
ALTER TABLE loom_embeddings RENAME TO alexandria_embeddings;
ALTER TABLE loom_jobs RENAME TO alexandria_jobs;
ALTER TABLE loom_outbox RENAME TO alexandria_outbox;
ALTER TABLE loom_import_runs RENAME TO alexandria_import_runs;
ALTER TABLE loom_tasks RENAME TO alexandria_tasks;
ALTER TABLE loom_task_sync_state RENAME TO alexandria_task_sync_state;
ALTER TABLE loom_task_sync_map RENAME TO alexandria_task_sync_map;
ALTER TABLE loom_users RENAME TO alexandria_users;
ALTER TABLE loom_api_tokens RENAME TO alexandria_api_tokens;
ALTER TYPE loom_job_status RENAME TO alexandria_job_status;

DO $$
DECLARE item record;
BEGIN
  FOR item IN SELECT c.relname,c.relkind FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
    WHERE n.nspname=current_schema() AND c.relkind IN ('i','S') AND c.relname LIKE 'loom\_%'
      AND c.relname NOT LIKE 'loom_schema_migrations%'
  LOOP
    IF item.relkind = 'S' THEN
      EXECUTE format('ALTER SEQUENCE %I RENAME TO %I', item.relname, 'alexandria_' || substr(item.relname,6));
    ELSE
      EXECUTE format('ALTER INDEX %I RENAME TO %I', item.relname, 'alexandria_' || substr(item.relname,6));
    END IF;
  END LOOP;
END $$;
