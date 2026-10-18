-- Google Sheets reuses the webhook_channels table: the "webhook" is a
-- Google Apps Script Web App URL the operator deploys and pastes in,
-- which appends a row per alert. No Google credentials touch this server.
INSERT INTO webhook_channels (channel, webhook_url, enabled) VALUES ('sheets', '', 0);
