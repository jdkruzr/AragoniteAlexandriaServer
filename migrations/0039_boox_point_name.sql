-- ksync's V2 fallback asks for point files at the flat key <uid>/point/<name>;
-- Alexandria answers it from <uid>/note/<doc>/point/<name>, found by name.
CREATE INDEX boox_blob_live_point_name ON boox_blob_live (split_part(native_key,'/',5))
 WHERE split_part(native_key,'/',2)='note' AND split_part(native_key,'/',4)='point';
