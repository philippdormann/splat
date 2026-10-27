-- Operator notes on issues: solo-dev memory ("fixed in staging,
-- watching") without a comments system. Cascade with the issue.
CREATE TABLE issue_notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    body TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_issue_notes_issue ON issue_notes(issue_id, id);
