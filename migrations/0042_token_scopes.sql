-- API tokens carry an access scope: 'full' (every token so far) or 'read',
-- which may look but not change anything. OAuth codes carry the scope the
-- owner chose on the consent page, and tokens issued through OAuth remember
-- which registered client asked, so Settings can name the app.
ALTER TABLE alexandria_api_tokens
  ADD COLUMN scope text NOT NULL DEFAULT 'full' CHECK (scope IN ('read','full')),
  ADD COLUMN oauth_client_id text COLLATE "C";

ALTER TABLE alexandria_oauth_codes
  ADD COLUMN scope text NOT NULL DEFAULT 'full' CHECK (scope IN ('read','full'));
