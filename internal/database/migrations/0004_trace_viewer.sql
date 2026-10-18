-- Stores the URL template used to link a captured trace_id out to whatever
-- external tracing backend (Jaeger, Tempo, Honeycomb, ...) the operator
-- already runs. This is deliberately NOT a full APM/span-storage feature -
-- errgotrack stays an error tracker; it just makes the trace_id it already
-- captures from event contexts clickable.
CREATE TABLE tracing_config (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    url_template TEXT NOT NULL DEFAULT ''
);
INSERT INTO tracing_config (id, url_template) VALUES (1, '');
