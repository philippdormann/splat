// Package logs stores the structured log records SDKs send as envelope
// items with {"type":"log"} (Sentry's logs product: logger.info/warn/...).
// They are queryable per project by level, text, and trace - and unlike
// events they never create issues or notify.
package logs

import (
	"encoding/json"
	"strings"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/sentryprotocol"
)

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

type Entry struct {
	ID          int64
	ProjectID   int64
	Timestamp   time.Time
	Level       string
	Message     string
	TraceID     string
	SpanID      string
	Release     string
	Environment string
	Attributes  string // raw JSON object
}

// MaxBatch caps stored records per envelope item: a single "log" item can
// carry hundreds of records and readBody already bounds the request at
// 2MiB, so this only drops pathological overflow, oldest-first preserved.
const MaxBatch = 1000

// InsertBatch persists one parsed log batch. Attributes may carry release
// / environment hints under well-known keys; otherwise the project's
// default environment applies and release stays empty.
func (s *Store) InsertBatch(projectID int64, defaultEnv string, batch *sentryprotocol.LogBatch) error {
	if batch == nil || len(batch.Items) == 0 {
		return nil
	}
	items := batch.Items
	if len(items) > MaxBatch {
		items = items[:MaxBatch]
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO logs
		(project_id, timestamp, level, message, trace_id, span_id, release, environment, attributes_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, rec := range items {
		attrs := strings.TrimSpace(string(rec.Attributes))
		if attrs == "" || attrs == "null" {
			attrs = "{}"
		}
		release, env := attrHint(rec.Attributes, "release", "sentry.release"), attrHint(rec.Attributes, "environment", "sentry.environment")
		if env == "" {
			env = defaultEnv
		}
		msg := rec.Text()
		if len(msg) > 4000 {
			msg = msg[:4000] + "…"
		}
		if _, err := stmt.Exec(projectID,
			rec.ParsedTimestamp().UTC().Format(time.RFC3339Nano),
			rec.NormalizedLevel(), msg,
			rec.TraceID, rec.SpanID, release, env, attrs); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// attrHint reads a best-effort string attribute trying each key in order.
func attrHint(raw []byte, keys ...string) string {
	// Minimal parse without encoding/json into map[string]any: attributes
	// are small and this path runs per record.
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err == nil {
		for _, k := range keys {
			if v := m[k]; v != "" {
				return v
			}
		}
	}
	return ""
}

type Filter struct {
	Level   string // "" = any
	Query   string // substring on message
	TraceID string
	Limit   int
	Offset  int
}

func (s *Store) List(projectID int64, f Filter) ([]Entry, error) {
	where := "WHERE project_id = ?"
	args := []any{projectID}
	if f.Level != "" {
		where += " AND level = ?"
		args = append(args, f.Level)
	}
	if f.TraceID != "" {
		where += " AND trace_id = ?"
		args = append(args, f.TraceID)
	}
	if f.Query != "" {
		where += " AND message LIKE ?"
		args = append(args, "%"+f.Query+"%")
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, project_id, timestamp, level, message,
		trace_id, span_id, release, environment, attributes_json
		FROM logs `+where+` ORDER BY timestamp DESC, id DESC LIMIT ? OFFSET ?`,
		append(args, limit, f.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var ts string
		if err := rows.Scan(&e.ID, &e.ProjectID, &ts, &e.Level, &e.Message,
			&e.TraceID, &e.SpanID, &e.Release, &e.Environment, &e.Attributes); err != nil {
			return nil, err
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SweepBefore deletes records older than cutoff for retention, returning
// the row count removed.
func (s *Store) SweepBefore(projectID int64, cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM logs WHERE project_id = ? AND timestamp < ?`,
		projectID, cutoff.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
