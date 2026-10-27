-- TOTP two-factor auth: per-user encrypted secrets (pending until
-- verified), single-use recovery codes, and instance security settings
-- (e.g. whether 2FA is required for every login).
CREATE TABLE user_totp (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- AES-256-GCM ciphertext (base64 nonce+ciphertext), keyed by
    -- SHA256(SPLAT_SESSION_SECRET). Never stored in cleartext.
    secret_enc TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    verified_at TEXT
);
CREATE TABLE user_recovery_codes (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    used_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (user_id, code_hash)
);
CREATE TABLE instance_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO instance_settings (key, value) VALUES ('totp_required', '0');
