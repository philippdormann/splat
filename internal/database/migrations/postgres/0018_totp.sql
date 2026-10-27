-- Postgres port of 0018_totp.sql.
CREATE TABLE IF NOT EXISTS user_totp (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- AES-256-GCM ciphertext (base64 nonce+ciphertext), keyed by
    -- SHA256(SPLAT_SESSION_SECRET). Never stored in cleartext.
    secret_enc TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TEXT NOT NULL DEFAULT (now()::text),
    verified_at TEXT
);
CREATE TABLE IF NOT EXISTS user_recovery_codes (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at TEXT,
    created_at TEXT NOT NULL DEFAULT (now()::text),
    PRIMARY KEY (user_id, code_hash)
);
CREATE TABLE IF NOT EXISTS instance_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO instance_settings (key, value) VALUES ('totp_required', '0')
    ON CONFLICT (key) DO NOTHING;
