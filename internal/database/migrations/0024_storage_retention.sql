-- Retention settings (Rustrak-style): per-kind windows with per-project overrides.
-- NULL = keep forever, integer days otherwise. Defaults live in
-- instance_settings so fresh installs start at 90 days for every kind.
CREATE TABLE project_retention_overrides (
    project_id INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    events_days INTEGER,
    transactions_days INTEGER,
    logs_days INTEGER
);
INSERT INTO instance_settings (key, value) VALUES
    ('retention_default_events', '90'),
    ('retention_default_transactions', '90'),
    ('retention_default_logs', '90')
ON CONFLICT(key) DO NOTHING;

-- Scheduler state for the storage cleanup job (next run + last run).
CREATE TABLE scheduler_state (
    key TEXT PRIMARY KEY,
    settings_json TEXT NOT NULL DEFAULT '{}',
    next_run_at TEXT,
    last_started_at TEXT,
    last_finished_at TEXT,
    last_status TEXT,
    last_result_json TEXT,
    last_error TEXT
);
