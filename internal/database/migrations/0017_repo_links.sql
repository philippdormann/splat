-- Repository integration: one linked forge repository per project
-- (GitHub, GitLab, Gitea, Forgejo) plus the external issues created or
-- tracked from errgotrack issues. Tokens are stored like every other
-- secret in this schema (plaintext, masked in the API); the database file
-- itself is the trust boundary.
CREATE TABLE project_repos (
    project_id INTEGER PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    provider TEXT NOT NULL DEFAULT '',
    base_url TEXT NOT NULL DEFAULT '',
    owner TEXT NOT NULL DEFAULT '',
    repo TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE issue_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    provider TEXT NOT NULL DEFAULT '',
    repo TEXT NOT NULL DEFAULT '',
    number INTEGER NOT NULL DEFAULT 0,
    url TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_issue_links_issue ON issue_links(issue_id, id);
