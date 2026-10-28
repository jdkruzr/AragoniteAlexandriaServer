-- Every page's header counts recognition pages that need attention; this keeps
-- the count cheap as finished and cleared attempts accumulate.
CREATE INDEX alexandria_recognition_job_attention ON alexandria_recognition_job(state)
 WHERE state IN ('failed','blocked') AND cleaned_at IS NULL AND superseded_by IS NULL;
