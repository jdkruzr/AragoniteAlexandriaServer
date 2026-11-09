-- Changes assistants made over MCP: which app (its token's label), which
-- tool, what it was asked and what it said. The owner reviews them in
-- Settings; daily caps on costly tools count them. Kept for 90 days.
CREATE TABLE alexandria_mcp_change (
  id bigserial PRIMARY KEY,
  at timestamptz NOT NULL DEFAULT now(),
  client text NOT NULL DEFAULT '',
  tool text NOT NULL,
  arguments text NOT NULL DEFAULT '',
  result text NOT NULL DEFAULT ''
);
CREATE INDEX alexandria_mcp_change_tool ON alexandria_mcp_change(tool, at);
CREATE INDEX alexandria_mcp_change_at ON alexandria_mcp_change(at);
