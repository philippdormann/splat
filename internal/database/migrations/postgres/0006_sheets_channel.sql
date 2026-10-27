INSERT INTO webhook_channels (channel, webhook_url, enabled) VALUES ('sheets', '', 0)
ON CONFLICT (channel) DO NOTHING;
