-- Existing reviews belong to BOOX; new runs select one explicit source.
ALTER TABLE alexandria_recognition_run ADD COLUMN source text NOT NULL DEFAULT 'boox'
 CHECK(source IN ('boox','client'));
CREATE INDEX alexandria_recognition_run_source ON alexandria_recognition_run(source,created_at DESC);
