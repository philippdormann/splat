-- API keys for automation: ingestion without a DSN, read/write access to
-- the management API without a session cookie. Only the SHA256 hash is
-- stored - the secret itself is shown once at creation.
CREATE TABLE api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL DEFAULT '',
    key_hash TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL DEFAULT '',
    project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
    scopes TEXT NOT NULL DEFAULT '["read"]',
    expires_at TEXT NOT NULL DEFAULT '',
    revoked INTEGER NOT NULL DEFAULT 0,
    last_used_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);
