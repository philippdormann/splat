-- Per-project notifications: named integrations (credentials) + rule links (routing).
-- No data migration: fresh installs only (not yet deployed).
CREATE TABLE integrations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    provider TEXT NOT NULL CHECK (provider IN ('telegram','slack','discord','teams','sheets','smtp','generic')),
    credentials TEXT NOT NULL DEFAULT '{}',
    is_enabled INTEGER NOT NULL DEFAULT 1,
    failure_count INTEGER NOT NULL DEFAULT 0,
    last_failure TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE notification_rule_integrations (
    rule_id INTEGER NOT NULL REFERENCES notification_rules(id) ON DELETE CASCADE,
    integration_id INTEGER NOT NULL REFERENCES integrations(id) ON DELETE CASCADE,
    routing_override TEXT NOT NULL DEFAULT '{}',
    PRIMARY KEY (rule_id, integration_id)
);
CREATE INDEX idx_rule_integrations_rule ON notification_rule_integrations(rule_id);
CREATE INDEX idx_rule_integrations_integration ON notification_rule_integrations(integration_id);

-- Queue now targets a specific integration; channel stays as provider label
-- for readability, routing_override snapshots the rule link at enqueue time.
ALTER TABLE notification_queue ADD COLUMN integration_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notification_queue ADD COLUMN routing_override TEXT NOT NULL DEFAULT '{}';
ALTER TABLE notification_queue ADD COLUMN provider TEXT NOT NULL DEFAULT '';

DROP TABLE IF EXISTS telegram_config;
DROP TABLE IF EXISTS webhook_channels;
DROP TABLE IF EXISTS smtp_config;
DROP TABLE IF EXISTS generic_webhooks;
