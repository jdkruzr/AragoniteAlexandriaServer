-- Legacy handles used the Gateway cookie directly. Lazily replace their backend
-- and reconcile channel grants at the next authenticated connection. Do not
-- extend frontend expiry or revive revoked/expired capabilities.
UPDATE boox_grant SET backend_expires_at=LEAST(backend_expires_at,now())
WHERE kind='session' AND backend_session_id=id;
