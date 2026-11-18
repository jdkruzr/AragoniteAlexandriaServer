-- A tablet rewrites a book's readingStatus on every reader save (Finished only
-- while its last page is showing), so what the reader says about a book is
-- kept here, beside the native record rather than in it.
CREATE TABLE boox_reading_mark (
 native_uid text NOT NULL,
 book_id text NOT NULL,
 mark text NOT NULL CHECK(mark IN ('finished','not_reading')),
 marked_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(native_uid,book_id)
);
