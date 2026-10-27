-- Per-project ingestion quotas (spike protection). NULL limits inherit
-- the instance defaults in instance_settings (quota_events_hourly /
-- quota_events_daily); 0 means unlimited. breach_notified_at throttles
-- breach alerts to one per hour per project while continuously over.
CREATE TABLE project_quotas (
    project_id INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    events_hourly INTEGER,
    events_daily INTEGER,
    breach_notified_at TEXT
);
INSERT INTO instance_settings (key, value) VALUES ('quota_events_hourly', '5000');
INSERT INTO instance_settings (key, value) VALUES ('quota_events_daily', '50000');
