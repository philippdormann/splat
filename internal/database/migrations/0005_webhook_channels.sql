-- Slack, Discord, and Microsoft Teams all use the same shape of
-- integration (a single incoming-webhook URL), unlike Telegram's bot
-- token + chat id, so they share one small table instead of three
-- near-identical ones.
CREATE TABLE webhook_channels (
    channel TEXT PRIMARY KEY,
    webhook_url TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0
);
INSERT INTO webhook_channels (channel, webhook_url, enabled) VALUES
    ('slack', '', 0),
    ('discord', '', 0),
    ('teams', '', 0);

-- Each queued alert now targets one specific channel, so Telegram failing
-- never blocks or duplicates a Slack/Discord/Teams send for the same
-- issue, and vice versa. Existing rows predate multi-channel support and
-- were always Telegram.
ALTER TABLE notification_queue ADD COLUMN channel TEXT NOT NULL DEFAULT 'telegram';
