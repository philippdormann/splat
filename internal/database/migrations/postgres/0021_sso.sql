-- Postgres port of 0021_sso.sql.
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS sso_subject TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS sso_issuer TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_sso ON users(sso_issuer, sso_subject);
INSERT INTO instance_settings (key, value) VALUES ('password_login_disabled', '0')
    ON CONFLICT (key) DO NOTHING;
