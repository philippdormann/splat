CREATE TABLE IF NOT EXISTS sdk_sessions (
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    sid TEXT NOT NULL,
    distinct_id TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'ok',
    errors INTEGER NOT NULL DEFAULT 0,
    started TEXT NOT NULL DEFAULT '',
    duration_secs DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (now()::text),
    PRIMARY KEY (project_id, sid)
);
CREATE INDEX IF NOT EXISTS idx_sdk_sessions_release ON sdk_sessions(project_id, release);

CREATE TABLE IF NOT EXISTS sdk_session_buckets (
    day TEXT NOT NULL,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    release TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    exited INTEGER NOT NULL DEFAULT 0,
    errored INTEGER NOT NULL DEFAULT 0,
    crashed INTEGER NOT NULL DEFAULT 0,
    abnormal INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (day, project_id, release, environment)
);
