-- User-defined generic webhooks: any HTTPS endpoint gets the same JSON
-- alert payload as the fixed channels, optionally signed with HMAC-SHA256
-- (X-Errgotrack-Signature) and with custom headers merged in.
CREATE TABLE generic_webhooks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    secret TEXT NOT NULL DEFAULT '',
    headers TEXT NOT NULL DEFAULT '{}',
    enabled INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
