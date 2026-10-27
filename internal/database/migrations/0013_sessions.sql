-- SDK session tracking: individual session updates upserted by
-- (project, sid), plus per-day aggregate buckets for the "sessions"
-- envelope items SDKs send when buffering is on. Both feed crash-free
-- math; per-user rates use individual sessions only (aggregates cannot be
-- deduplicated across buckets without per-user rows).
CREATE TABLE sdk_sessions (
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    sid TEXT NOT NULL,
    distinct_id TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'ok',
    errors INTEGER NOT NULL DEFAULT 0,
    started TEXT NOT NULL DEFAULT '',
    duration_secs REAL NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (project_id, sid)
);
CREATE INDEX idx_sdk_sessions_release ON sdk_sessions(project_id, release);

CREATE TABLE sdk_session_buckets (
    day TEXT NOT NULL,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    release TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    exited INTEGER NOT NULL DEFAULT 0,
    errored INTEGER NOT NULL DEFAULT 0,
    crashed INTEGER NOT NULL DEFAULT 0,
    abnormal INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, project_id, release, environment)
);
