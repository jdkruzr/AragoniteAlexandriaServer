-- Native ownership is independent of Alexandria authentication. The first
-- enrollment binds an empty library; established installations retain identity.
CREATE TABLE boox_identity (
 id smallint PRIMARY KEY CHECK (id=1),
 native_uid text CHECK (native_uid ~ '^[A-Za-z0-9_-]{1,128}$'),
 bound_at timestamptz
);
INSERT INTO boox_identity(id) VALUES(1);
UPDATE boox_identity SET native_uid=(
 SELECT 'ps_' || replace(library_id::text,'-','') FROM alexandria_library_runtime WHERE singleton
), bound_at=now()
WHERE EXISTS (SELECT 1 FROM boox_device);
