-- Uploaded JavaScript source maps so minified stack frames can be resolved
-- back to the original source location. One map per (project, release,
-- bundle URL); re-uploading the same triple replaces the previous map.
CREATE TABLE source_maps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    release TEXT NOT NULL DEFAULT '',
    bundle_url TEXT NOT NULL,
    content TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE(project_id, release, bundle_url)
);
CREATE INDEX idx_source_maps_lookup ON source_maps(project_id, release);
