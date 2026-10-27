CREATE TABLE IF NOT EXISTS logs (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    timestamp TEXT NOT NULL,
    received_at TEXT NOT NULL DEFAULT (now()::text),
    level TEXT NOT NULL DEFAULT 'info',
    message TEXT NOT NULL DEFAULT '',
    trace_id TEXT NOT NULL DEFAULT '',
    span_id TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    attributes_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_logs_project_time ON logs(project_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_logs_trace ON logs(trace_id);

CREATE TABLE IF NOT EXISTS transactions (
    id BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    trace_id TEXT NOT NULL DEFAULT '',
    span_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    release TEXT NOT NULL DEFAULT '',
    environment TEXT NOT NULL DEFAULT '',
    start_timestamp TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    duration_ms REAL NOT NULL DEFAULT 0,
    span_count BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_transactions_project_time ON transactions(project_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_transactions_trace ON transactions(trace_id);

CREATE TABLE IF NOT EXISTS spans (
    id BIGSERIAL PRIMARY KEY,
    transaction_id BIGINT NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    project_id BIGINT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
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
CREATE INDEX IF NOT EXISTS idx_spans_transaction ON spans(transaction_id);
CREATE INDEX IF NOT EXISTS idx_spans_trace ON spans(trace_id);
