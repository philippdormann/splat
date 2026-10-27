CREATE TABLE IF NOT EXISTS tracing_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    url_template TEXT NOT NULL DEFAULT ''
);
INSERT INTO tracing_config (id, url_template) VALUES (1, '') ON CONFLICT (id) DO NOTHING;
