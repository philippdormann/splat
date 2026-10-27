CREATE TABLE IF NOT EXISTS issue_notes (
    id BIGSERIAL PRIMARY KEY,
    issue_id BIGINT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    body TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (now()::text)
);
CREATE INDEX IF NOT EXISTS idx_issue_notes_issue ON issue_notes(issue_id, id);
