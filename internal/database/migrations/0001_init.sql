CREATE TABLE projects (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    public_key TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1,
    default_environment TEXT NOT NULL DEFAULT 'production',
    retention_days INTEGER NOT NULL DEFAULT 90,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE issues (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    title TEXT NOT NULL,
    exception_type TEXT NOT NULL DEFAULT '',
    exception_value TEXT NOT NULL DEFAULT '',
    culprit TEXT NOT NULL DEFAULT '',
    level TEXT NOT NULL DEFAULT 'error',
    status TEXT NOT NULL DEFAULT 'unresolved',
    event_count INTEGER NOT NULL DEFAULT 0,
    user_count INTEGER NOT NULL DEFAULT 0,
    first_seen TEXT NOT NULL,
    last_seen TEXT NOT NULL,
    environment TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    UNIQUE(project_id, fingerprint)
);
CREATE INDEX idx_issues_project_status ON issues(project_id, status);
CREATE INDEX idx_issues_last_seen ON issues(last_seen);

CREATE TABLE events (
    pk INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    timestamp TEXT NOT NULL,
    received_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    message TEXT NOT NULL DEFAULT '',
    exception_type TEXT NOT NULL DEFAULT '',
    exception_value TEXT NOT NULL DEFAULT '',
    culprit TEXT NOT NULL DEFAULT '',
    transaction_name TEXT NOT NULL DEFAULT '',
    stacktrace_json TEXT NOT NULL DEFAULT '[]',
    tags_json TEXT NOT NULL DEFAULT '{}',
    extra_json TEXT NOT NULL DEFAULT '{}',
    breadcrumbs_json TEXT NOT NULL DEFAULT '[]',
    user_json TEXT NOT NULL DEFAULT '{}',
    sdk_json TEXT NOT NULL DEFAULT '{}',
    request_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_events_issue ON events(issue_id, timestamp);
CREATE INDEX idx_events_project_time ON events(project_id, timestamp);
CREATE INDEX idx_events_environment ON events(project_id, environment);

CREATE VIRTUAL TABLE events_fts USING fts5(
    message, exception_type, exception_value,
    content='events', content_rowid='pk'
);
CREATE TRIGGER events_ai AFTER INSERT ON events BEGIN
    INSERT INTO events_fts(rowid, message, exception_type, exception_value)
    VALUES (new.pk, new.message, new.exception_type, new.exception_value);
END;
CREATE TRIGGER events_ad AFTER DELETE ON events BEGIN
    INSERT INTO events_fts(events_fts, rowid, message, exception_type, exception_value)
    VALUES ('delete', old.pk, old.message, old.exception_type, old.exception_value);
END;

CREATE TABLE admin_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE sessions (
    token TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL
);

CREATE TABLE telegram_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    bot_token TEXT NOT NULL DEFAULT '',
    chat_id TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0
);
INSERT INTO telegram_config (id, bot_token, chat_id, enabled) VALUES (1, '', '', 0);

CREATE TABLE notification_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
    environment TEXT NOT NULL DEFAULT '',
    notify_new_issue INTEGER NOT NULL DEFAULT 1,
    notify_threshold INTEGER NOT NULL DEFAULT 0,
    notify_reopened INTEGER NOT NULL DEFAULT 1,
    cooldown_seconds INTEGER NOT NULL DEFAULT 300
);

CREATE TABLE notification_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    next_attempt_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_notif_queue_status ON notification_queue(status, next_attempt_at);
