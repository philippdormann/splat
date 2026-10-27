-- Postgres port of 0001_init.sql. Timestamps stay TEXT (RFC3339Nano) on
-- purpose so every store query works unchanged on both backends; string
-- comparison on UTC RFC3339 orders chronologically.
CREATE TABLE IF NOT EXISTS projects (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    public_key TEXT NOT NULL UNIQUE,
    enabled INTEGER NOT NULL DEFAULT 1,
    default_environment TEXT NOT NULL DEFAULT 'production',
    retention_days INTEGER NOT NULL DEFAULT 90,
    created_at TEXT NOT NULL DEFAULT (now()::text)
);

CREATE TABLE IF NOT EXISTS issues (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    title TEXT NOT NULL,
    exception_type TEXT NOT NULL DEFAULT '',
    exception_value TEXT NOT NULL DEFAULT '',
    culprit TEXT NOT NULL DEFAULT '',
    level TEXT NOT NULL DEFAULT 'error',
    status TEXT NOT NULL DEFAULT 'unresolved',
    event_count BIGINT NOT NULL DEFAULT 0,
    user_count BIGINT NOT NULL DEFAULT 0,
    first_seen TEXT NOT NULL,
    last_seen TEXT NOT NULL,
    environment TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    UNIQUE(project_id, fingerprint)
);
CREATE INDEX IF NOT EXISTS idx_issues_project_status ON issues(project_id, status);
CREATE INDEX IF NOT EXISTS idx_issues_last_seen ON issues(last_seen);

CREATE TABLE IF NOT EXISTS events (
    pk BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE,
    issue_id BIGINT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    timestamp TEXT NOT NULL,
    received_at TEXT NOT NULL DEFAULT (now()::text),
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
CREATE INDEX IF NOT EXISTS idx_events_issue ON events(issue_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_events_project_time ON events(project_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_events_environment ON events(project_id, environment);
-- Full-text search on Postgres uses to_tsvector at query time (see
-- events.Store.Search); this GIN index keeps it indexed without FTS5.
CREATE INDEX IF NOT EXISTS idx_events_fts ON events USING gin (to_tsvector('english', coalesce(message,'') || ' ' || coalesce(exception_type,'') || ' ' || coalesce(exception_value,'')));

CREATE TABLE IF NOT EXISTS admin_users (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (now()::text)
);

CREATE TABLE IF NOT EXISTS sessions (
    token TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS telegram_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    bot_token TEXT NOT NULL DEFAULT '',
    chat_id TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0
);
INSERT INTO telegram_config (id, bot_token, chat_id, enabled) VALUES (1, '', '', 0) ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS notification_rules (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT REFERENCES projects(id) ON DELETE CASCADE,
    environment TEXT NOT NULL DEFAULT '',
    notify_new_issue INTEGER NOT NULL DEFAULT 1,
    notify_threshold INTEGER NOT NULL DEFAULT 0,
    notify_reopened INTEGER NOT NULL DEFAULT 1,
    cooldown_seconds INTEGER NOT NULL DEFAULT 300
);

CREATE TABLE IF NOT EXISTS notification_queue (
    id BIGSERIAL PRIMARY KEY,
    issue_id BIGINT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending',
    next_attempt_at TEXT NOT NULL DEFAULT (now()::text),
    created_at TEXT NOT NULL DEFAULT (now()::text),
    last_error TEXT NOT NULL DEFAULT '',
    channel TEXT NOT NULL DEFAULT 'telegram'
);
CREATE INDEX IF NOT EXISTS idx_notif_queue_status ON notification_queue(status, next_attempt_at);
