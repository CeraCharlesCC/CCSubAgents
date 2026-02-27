package sqlite

const metaSchemaV1 = `
CREATE TABLE IF NOT EXISTS artifacts (
	name TEXT PRIMARY KEY,
	latest_version_id TEXT,
	deleted INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	deleted_at TEXT
);

CREATE TABLE IF NOT EXISTS versions (
	version_id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	parent_version_id TEXT,
	kind TEXT NOT NULL,
	mime_type TEXT NOT NULL,
	filename TEXT NOT NULL DEFAULT '',
	size_bytes INTEGER NOT NULL,
	payload_sha256 TEXT,
	created_at TEXT NOT NULL,
	tombstone INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_versions_name_created_at ON versions(name, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_versions_parent ON versions(parent_version_id);
`
