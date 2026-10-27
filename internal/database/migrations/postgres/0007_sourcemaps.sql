CREATE TABLE IF NOT EXISTS source_maps (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    release TEXT NOT NULL DEFAULT '',
    bundle_url TEXT NOT NULL,
    content TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (now()::text),
    UNIQUE(project_id, release, bundle_url)
);
CREATE INDEX IF NOT EXISTS idx_source_maps_lookup ON source_maps(project_id, release);
