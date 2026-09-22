CREATE TABLE alexandria_library_runtime (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 library_id uuid NOT NULL UNIQUE,
 mode text NOT NULL DEFAULT 'active' CHECK(mode IN ('active','read_only','maintenance','suspended')),
 updated_at timestamptz NOT NULL DEFAULT now()
);
