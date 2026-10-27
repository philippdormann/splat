-- Alert rules upgrade: each rule gets a human name and its own channel
-- selection (empty/NULL channels = all enabled channels, preserving the
-- old global behavior). Plus instance-level SMTP config for email alerts.
ALTER TABLE notification_rules ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_rules ADD COLUMN channels TEXT NOT NULL DEFAULT '[]';

CREATE TABLE smtp_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    host TEXT NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 587,
    username TEXT NOT NULL DEFAULT '',
    password TEXT NOT NULL DEFAULT '',
    from_address TEXT NOT NULL DEFAULT '',
    to_address TEXT NOT NULL DEFAULT '',
    use_tls INTEGER NOT NULL DEFAULT 1,
    enabled INTEGER NOT NULL DEFAULT 0
);
INSERT INTO smtp_config (id) VALUES (1);
