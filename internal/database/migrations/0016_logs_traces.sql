-- Everything the SDK sends, stored: structured log records ("log"
-- envelope items) and distributed traces (transaction items + their
-- spans). Events/sessions stay where they were. All three tables honor
-- per-project retention_days via the sweeper, same as events.
CREATE TABLE logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    timestamp TEXT NOT NULL,
    received_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL DEFAULT '',
    trace_id TEXT NOT NULL DEFAULT '',
    span_id TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    attributes_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_logs_project_time ON logs(project_id, timestamp);
CREATE INDEX idx_logs_trace ON logs(trace_id);

CREATE TABLE transactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    trace_id TEXT NOT NULL DEFAULT '',
    span_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    start_timestamp TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    duration_ms REAL NOT NULL DEFAULT 0,
    span_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_transactions_project_time ON transactions(project_id, timestamp);
CREATE INDEX idx_transactions_trace ON transactions(trace_id);

CREATE TABLE spans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    transaction_id INTEGER NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    trace_id TEXT NOT NULL DEFAULT '',
    span_id TEXT NOT NULL DEFAULT '',
    parent_span_id TEXT NOT NULL DEFAULT '',
    op TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    start_timestamp TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    duration_ms REAL NOT NULL DEFAULT 0
);
CREATE INDEX idx_spans_transaction ON spans(transaction_id);
CREATE INDEX idx_spans_trace ON spans(trace_id);
