CREATE TABLE IF NOT EXISTS webhook_channels (
    channel TEXT PRIMARY KEY,
    webhook_url TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0
);
INSERT INTO webhook_channels (channel, webhook_url, enabled) VALUES
    ('slack', '', 0),
    ('discord', '', 0),
    ('teams', '', 0)
ON CONFLICT (channel) DO NOTHING;
