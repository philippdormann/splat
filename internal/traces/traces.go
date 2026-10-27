// Package traces stores the distributed traces SDKs send as envelope
// items with {"type":"transaction"}: one row per transaction plus its
// span list. This is trace *visibility* for an error tracker - recent
// traces per project and a span listing per trace id - not an APM
// backend: no metrics, no profiling, no sampling decisions.
package traces

import (
	"strings"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/sentryprotocol"
)

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

type Transaction struct {
	ID          int64
	ProjectID   int64
	EventID     string
	Name        string
	TraceID     string
	SpanID      string
	Status      string
	Release     string
	Environment string
	Start       time.Time
	End         time.Time
	DurationMS  float64
	SpanCount   int64
}

type Span struct {
	ID            int64
	TransactionID int64
	TraceID       string
	SpanID        string
	ParentSpanID  string
	Op            string
	Description   string
	Status        string
	Start         time.Time
	End           time.Time
	DurationMS    float64
}

// MaxSpans caps stored spans per transaction: SDKs can emit hundreds per
// transaction and readBody already bounds the request, so this only drops
// pathological overflow, keeping the earliest spans.
const MaxSpans = 500

// Insert persists one parsed transaction and its spans. Retried deliveries
// (same event_id) are deduplicated: the second insert is a no-op.
func (s *Store) Insert(projectID int64, defaultEnv string, txn *sentryprotocol.Transaction) error {
	env := txn.Environment
	if env == "" {
		env = defaultEnv
	}
	tc := txn.Trace()
	start, _ := parseTime(txn.StartTimestamp)
	end, ok := parseTime(txn.Timestamp)
	if !ok {
		end = start
	}
	durationMS := 0.0
	if d := end.Sub(start).Seconds() * 1000; d > 0 {
		durationMS = d
	}
	name := txn.Name
	if name == "" {
		name = "<unlabeled transaction>"
	}
	eventID := txn.EventID
	if eventID == "" {
		eventID = newEventID()
	}

	spans := txn.Spans
	if len(spans) > MaxSpans {
		spans = spans[:MaxSpans]
	}

	if s.db.IsPostgres() {
		var id int64
		err := s.db.QueryRow(`INSERT INTO transactions
			(event_id, project_id, name, trace_id, span_id, status, release, environment,
			 start_timestamp, timestamp, duration_ms, span_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (event_id) DO NOTHING
			RETURNING id`,
			eventID, projectID, name, tc.TraceID, tc.SpanID, tc.Status,
			txn.Release, env,
			start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano),
			durationMS, len(spans)).Scan(&id)
		if err != nil {
			// DO NOTHING yields no row on conflict: already stored.
			if strings.Contains(err.Error(), "no rows") {
				return nil
			}
			return err
		}
		return s.insertSpans(id, projectID, tc.TraceID, spans)
	}
	res, err := s.db.Exec(`INSERT INTO transactions
		(event_id, project_id, name, trace_id, span_id, status, release, environment,
		 start_timestamp, timestamp, duration_ms, span_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (event_id) DO NOTHING`,
		eventID, projectID, name, tc.TraceID, tc.SpanID, tc.Status,
		txn.Release, env,
		start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano),
		durationMS, len(spans))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil // duplicate delivery
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	return s.insertSpans(id, projectID, tc.TraceID, spans)
}

func (s *Store) insertSpans(txnID, projectID int64, traceID string, spans []sentryprotocol.Span) error {
	if len(spans) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO spans
		(transaction_id, project_id, trace_id, span_id, parent_span_id, op,
		 description, status, start_timestamp, timestamp, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, sp := range spans {
		start, end := sp.StartEnd()
		desc := sp.Description
		if len(desc) > 500 {
			desc = desc[:500] + "…"
		}
		if _, err := stmt.Exec(txnID, projectID, firstNonEmpty(sp.TraceID, traceID),
			sp.SpanID, sp.ParentSpanID, sp.Op, desc, sp.Status,
			start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano),
			sp.DurationMS()); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Recent lists the newest transactions of a project.
func (s *Store) Recent(projectID int64, limit int) ([]Transaction, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, project_id, event_id, name, trace_id, span_id,
		status, release, environment, start_timestamp, timestamp, duration_ms, span_count
		FROM transactions WHERE project_id = ? ORDER BY timestamp DESC, id DESC LIMIT ?`,
		projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Transaction{}
	for rows.Next() {
		var t Transaction
		var start, end string
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.EventID, &t.Name, &t.TraceID,
			&t.SpanID, &t.Status, &t.Release, &t.Environment,
			&start, &end, &t.DurationMS, &t.SpanCount); err != nil {
			return nil, err
		}
		t.Start, _ = time.Parse(time.RFC3339Nano, start)
		t.End, _ = time.Parse(time.RFC3339Nano, end)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Trace returns every stored transaction and span for one trace id within
// a project (a trace can span several transactions across services).
func (s *Store) Trace(projectID int64, traceID string) ([]Transaction, []Span, error) {
	txRows, err := s.db.Query(`SELECT id, project_id, event_id, name, trace_id, span_id,
		status, release, environment, start_timestamp, timestamp, duration_ms, span_count
		FROM transactions WHERE project_id = ? AND trace_id = ? ORDER BY timestamp`,
		projectID, traceID)
	if err != nil {
		return nil, nil, err
	}
	txns := []Transaction{}
	for txRows.Next() {
		var t Transaction
		var start, end string
		if err := txRows.Scan(&t.ID, &t.ProjectID, &t.EventID, &t.Name, &t.TraceID,
			&t.SpanID, &t.Status, &t.Release, &t.Environment,
			&start, &end, &t.DurationMS, &t.SpanCount); err != nil {
			txRows.Close()
			return nil, nil, err
		}
		t.Start, _ = time.Parse(time.RFC3339Nano, start)
		t.End, _ = time.Parse(time.RFC3339Nano, end)
		txns = append(txns, t)
	}
	txRows.Close()
	if err := txRows.Err(); err != nil {
		return nil, nil, err
	}

	spanRows, err := s.db.Query(`SELECT id, transaction_id, trace_id, span_id, parent_span_id,
		op, description, status, start_timestamp, timestamp, duration_ms
		FROM spans WHERE project_id = ? AND trace_id = ? ORDER BY start_timestamp`,
		projectID, traceID)
	if err != nil {
		return nil, nil, err
	}
	defer spanRows.Close()
	spans := []Span{}
	for spanRows.Next() {
		var sp Span
		var start, end string
		if err := spanRows.Scan(&sp.ID, &sp.TransactionID, &sp.TraceID, &sp.SpanID,
			&sp.ParentSpanID, &sp.Op, &sp.Description, &sp.Status,
			&start, &end, &sp.DurationMS); err != nil {
			return nil, nil, err
		}
		sp.Start, _ = time.Parse(time.RFC3339Nano, start)
		sp.End, _ = time.Parse(time.RFC3339Nano, end)
		spans = append(spans, sp)
	}
	return txns, spans, spanRows.Err()
}

// SweepBefore deletes transactions older than cutoff (spans cascade via
// FK), returning the transaction count removed.
func (s *Store) SweepBefore(projectID int64, cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM transactions WHERE project_id = ? AND timestamp < ?`,
		projectID, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
