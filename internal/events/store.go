// Package events persists individual error events and supports search across
// them.
package events

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Event struct {
	EventID         string
	IssueID         int64
	ProjectID       int64
	Environment     string
	Release         string
	Timestamp       time.Time
	ReceivedAt      time.Time
	Message         string
	ExceptionType   string
	ExceptionValue  string
	Culprit         string
	TransactionName string
	StacktraceJSON  string
	TagsJSON        string
	ExtraJSON       string
	BreadcrumbsJSON string
	UserJSON        string
	SDKJSON         string
	RequestJSON     string
	ContextsJSON    string
}

var ErrNotFound = errors.New("event not found")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Insert(e *Event) error {
	_, err := s.db.Exec(`
		INSERT INTO events (event_id, issue_id, project_id, environment, release, timestamp, message,
			exception_type, exception_value, culprit, transaction_name, stacktrace_json, tags_json, extra_json,
			breadcrumbs_json, user_json, sdk_json, request_json, contexts_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.EventID, e.IssueID, e.ProjectID, e.Environment, e.Release, e.Timestamp.UTC().Format(time.RFC3339Nano), e.Message,
		e.ExceptionType, e.ExceptionValue, e.Culprit, e.TransactionName, e.StacktraceJSON, e.TagsJSON, e.ExtraJSON,
		e.BreadcrumbsJSON, e.UserJSON, e.SDKJSON, e.RequestJSON, e.ContextsJSON)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

const selectCols = `event_id, issue_id, project_id, environment, release, timestamp, received_at, message,
	exception_type, exception_value, culprit, transaction_name, stacktrace_json, tags_json, extra_json,
	breadcrumbs_json, user_json, sdk_json, request_json, contexts_json`

func scan(row interface{ Scan(...any) error }) (*Event, error) {
	var e Event
	var ts, recv string
	if err := row.Scan(&e.EventID, &e.IssueID, &e.ProjectID, &e.Environment, &e.Release, &ts, &recv, &e.Message,
		&e.ExceptionType, &e.ExceptionValue, &e.Culprit, &e.TransactionName, &e.StacktraceJSON, &e.TagsJSON, &e.ExtraJSON,
		&e.BreadcrumbsJSON, &e.UserJSON, &e.SDKJSON, &e.RequestJSON, &e.ContextsJSON); err != nil {
		return nil, err
	}
	e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
	e.ReceivedAt, _ = time.Parse(time.RFC3339Nano, recv)
	return &e, nil
}

func (s *Store) Get(eventID string) (*Event, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM events WHERE event_id = ?`, eventID)
	e, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// Latest returns the most recent event for an issue - used as the
// representative occurrence shown on the issue detail page by default.
func (s *Store) Latest(issueID int64) (*Event, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM events WHERE issue_id = ? ORDER BY timestamp DESC LIMIT 1`, issueID)
	e, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

func (s *Store) ListByIssue(issueID int64, limit, offset int) ([]*Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT `+selectCols+` FROM events WHERE issue_id = ? ORDER BY timestamp DESC LIMIT ? OFFSET ?`, issueID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Neighbor returns the event immediately before or after (by timestamp)
// the given event within the same issue, for the detail page's prev/next
// navigation. dir must be "prev" or "next".
func (s *Store) Neighbor(issueID int64, ts time.Time, dir string) (*Event, error) {
	op, order := "<", "DESC"
	if dir == "next" {
		op, order = ">", "ASC"
	}
	query := fmt.Sprintf(`SELECT %s FROM events WHERE issue_id = ? AND timestamp %s ? ORDER BY timestamp %s LIMIT 1`, selectCols, op, order)
	row := s.db.QueryRow(query, issueID, ts.UTC().Format(time.RFC3339Nano))
	e, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

type SearchFilter struct {
	ProjectID   int64
	Environment string
	Release     string
	Text        string // full-text search over message/exception type/value
	Since       time.Time
	Until       time.Time
	Limit       int
	Offset      int
}

// Search finds individual events, using FTS5 when free text is given and
// falling back to plain indexed filters otherwise so a filter-only query
// doesn't pay for a full-text match.
func (s *Store) Search(f SearchFilter) ([]*Event, error) {
	where := "WHERE 1=1"
	var args []any
	joinFTS := ""

	// Built from f.Text rather than checking f.Text != "" directly: a
	// whitespace-only search string is non-empty but produces no FTS5
	// terms, and "events_fts MATCH ''" is a syntax error, not an empty
	// result - so the emptiness check has to happen after tokenizing.
	if ftsTerms := ftsQuery(f.Text); ftsTerms != "" {
		joinFTS = "JOIN events_fts ON events_fts.rowid = events.pk"
		where += " AND events_fts MATCH ?"
		args = append(args, ftsTerms)
	}
	if f.ProjectID != 0 {
		where += " AND events.project_id = ?"
		args = append(args, f.ProjectID)
	}
	if f.Environment != "" {
		where += " AND events.environment = ?"
		args = append(args, f.Environment)
	}
	if f.Release != "" {
		where += " AND events.release = ?"
		args = append(args, f.Release)
	}
	if !f.Since.IsZero() {
		where += " AND events.timestamp >= ?"
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	if !f.Until.IsZero() {
		where += " AND events.timestamp <= ?"
		args = append(args, f.Until.UTC().Format(time.RFC3339Nano))
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	cols := prefixed("events.", selectCols)
	query := fmt.Sprintf(`SELECT %s FROM events %s %s ORDER BY events.timestamp DESC LIMIT ? OFFSET ?`, cols, joinFTS, where)
	args = append(args, limit, f.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("search events: %w", err)
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ftsQuery turns free text into an FTS5 query that matches any of the
// whitespace-separated terms as a prefix, which behaves closer to what a
// user expects from a search box than FTS5's default strict phrase match.
func ftsQuery(text string) string {
	words := strings.Fields(text)
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"*`
	}
	return strings.Join(quoted, " OR ")
}

// prefixed qualifies each column in the shared selectCols list with a table
// prefix, needed once the FTS join makes "events." disambiguation necessary.
func prefixed(prefix, cols string) string {
	fields := strings.Split(strings.ReplaceAll(strings.ReplaceAll(cols, "\n", ""), "\t", ""), ",")
	for i, c := range fields {
		fields[i] = prefix + strings.TrimSpace(c)
	}
	return strings.Join(fields, ", ")
}
