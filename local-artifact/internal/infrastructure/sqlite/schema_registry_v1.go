package sqlite

const registrySchemaV1 = `
CREATE TABLE IF NOT EXISTS workspaces (
workspace_id TEXT PRIMARY KEY,
roots_json TEXT NOT NULL,
owner TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL,
last_seen_at TEXT NOT NULL
);
`
