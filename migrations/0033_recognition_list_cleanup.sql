-- List cleanup preserves request history and the source queue. A restored page
-- gets a new request ID through the existing recognition-request triggers.
ALTER TABLE alexandria_recognition_job ADD COLUMN cleaned_at timestamptz;
