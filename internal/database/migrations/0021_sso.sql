-- SSO (OIDC) identity: email for matching/linking, issuer+subject for
-- the login lookup. Uniqueness holds for non-NULL values only (SQLite
-- and Postgres both allow multiple NULLs in a unique index), so
-- password-only users are unaffected.
ALTER TABLE users ADD COLUMN email TEXT;
ALTER TABLE users ADD COLUMN sso_subject TEXT;
ALTER TABLE users ADD COLUMN sso_issuer TEXT;
CREATE UNIQUE INDEX idx_users_email ON users(email);
CREATE UNIQUE INDEX idx_users_sso ON users(sso_issuer, sso_subject);
INSERT INTO instance_settings (key, value) VALUES ('password_login_disabled', '0');
