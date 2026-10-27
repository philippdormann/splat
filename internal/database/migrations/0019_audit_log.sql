-- Audit log: append-only record of authentication and management
-- actions. actor_user_id intentionally has no foreign key: entries must
-- survive (and never fail because of) user deletion; the username is
-- denormalized for the same reason. Viewed by admins only; retention
-- is the operator's choice (never swept automatically).
CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    actor_user_id INTEGER NOT NULL DEFAULT 0,
    actor_username TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL DEFAULT '',
    entity_id TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '{}',
    ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_audit_log_action ON audit_log(action, id);
