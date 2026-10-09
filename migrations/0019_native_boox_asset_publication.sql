-- A retained version's creation date is provenance. Restoring that version is
-- a new live publication, which timestamp-based native clients must discover.
ALTER TABLE boox_blob_live ADD COLUMN published_at timestamptz;
UPDATE boox_blob_live l SET published_at=v.created_at
 FROM boox_blob_version v WHERE v.id=l.version_id;
ALTER TABLE boox_blob_live ALTER COLUMN published_at SET DEFAULT clock_timestamp();
ALTER TABLE boox_blob_live ALTER COLUMN published_at SET NOT NULL;
