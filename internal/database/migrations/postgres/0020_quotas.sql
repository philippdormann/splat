-- Postgres port of 0020_quotas.sql.
CREATE TABLE IF NOT EXISTS project_quotas (
    project_id BIGINT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    events_hourly BIGINT,
    events_daily BIGINT,
    breach_notified_at TEXT
);
INSERT INTO instance_settings (key, value) VALUES ('quota_events_hourly', '5000')
    ON CONFLICT (key) DO NOTHING;
INSERT INTO instance_settings (key, value) VALUES ('quota_events_daily', '50000')
    ON CONFLICT (key) DO NOTHING;
