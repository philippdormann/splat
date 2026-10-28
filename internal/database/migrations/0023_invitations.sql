-- Token invitations for team onboarding (manual-share, no email in v1).
-- Admins create a pending invite (email + username + role); the link
-- /invite/<token> is copied to the clipboard and shared out-of-band.
-- 48h TTL, single-use (pending -> accepted|revoked).
CREATE TABLE invitations (
    token TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    username TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin','member')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','revoked')),
    expires_at TEXT NOT NULL,
    invited_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    accepted_at TEXT
);
CREATE INDEX idx_invitations_email ON invitations(email);
CREATE INDEX idx_invitations_status ON invitations(status);

-- Track last login for the team roster (updated on password + SSO login).
ALTER TABLE users ADD COLUMN last_login TEXT;
