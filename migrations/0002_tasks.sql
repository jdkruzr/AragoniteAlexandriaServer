CREATE TABLE IF NOT EXISTS loom_tasks (
    task_id text PRIMARY KEY,
    title text,
    detail text,
    status text NOT NULL DEFAULT 'needsAction',
    importance text,
    due_time bigint NOT NULL DEFAULT 0,
    completed_time bigint NOT NULL DEFAULT 0,
    completed_at bigint,
    last_modified bigint NOT NULL DEFAULT 0,
    recurrence text,
    is_reminder_on boolean NOT NULL DEFAULT false,
    links text,
    deleted boolean NOT NULL DEFAULT false,
    ical_blob text,
    created_at bigint NOT NULL,
    updated_at bigint NOT NULL,
    forestnote_notebook_id text,
    forestnote_page_id text,
    forestnote_notebook_name text,
    forestnote_source text
);
CREATE INDEX IF NOT EXISTS loom_tasks_active_idx ON loom_tasks(updated_at DESC) WHERE deleted = false;
CREATE INDEX IF NOT EXISTS loom_tasks_forestnote_notebook_idx
    ON loom_tasks(forestnote_notebook_id) WHERE forestnote_notebook_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS loom_task_sync_state (
    adapter_id text PRIMARY KEY,
    last_sync_token text,
    last_sync_at bigint NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS loom_task_sync_map (
    task_id text NOT NULL REFERENCES loom_tasks(task_id) ON DELETE CASCADE,
    adapter_id text NOT NULL,
    remote_id text NOT NULL,
    remote_etag text,
    last_pushed_at bigint NOT NULL DEFAULT 0,
    last_pulled_at bigint NOT NULL DEFAULT 0,
    last_seen_at bigint NOT NULL DEFAULT 0,
    PRIMARY KEY(task_id, adapter_id)
);
CREATE INDEX IF NOT EXISTS loom_task_sync_remote_idx ON loom_task_sync_map(adapter_id, remote_id);
