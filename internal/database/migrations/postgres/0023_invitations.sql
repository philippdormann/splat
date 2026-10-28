-- Token invitations for team onboarding (manual-share, no email in v1).
CREATE TABLE IF NOT EXISTS invitations (
    token TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    username TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin','member')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','revoked')),
    expires_at TEXT NOT NULL,
    invited_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (now()::text),
    accepted_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_invitations_email ON invitations(email);
CREATE INDEX IF NOT EXISTS idx_invitations_status ON invitations(status);

ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login TEXT;
