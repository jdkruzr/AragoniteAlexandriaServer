-- The device's revocable session handle is independent of Gateway's shorter
-- backend cookie. Preserve existing handles without reviving expired grants.
ALTER TABLE boox_grant ADD COLUMN backend_session_id text;
ALTER TABLE boox_grant ADD COLUMN backend_expires_at timestamptz;
UPDATE boox_grant SET backend_session_id=id,backend_expires_at=expires_at WHERE kind='session';
ALTER TABLE boox_grant ADD CONSTRAINT boox_session_backend_pair CHECK (
 (backend_session_id IS NULL) = (backend_expires_at IS NULL)
);
