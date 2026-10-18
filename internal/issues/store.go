// Package issues implements issue grouping: turning a stream of events into
// deduplicated, trackable issues.
package issues

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusUnresolved Status = "unresolved"
	StatusResolved   Status = "resolved"
	StatusIgnored    Status = "ignored"
)

type Issue struct {
	ID             int64
	ProjectID      int64
	Fingerprint    string
	Title          string
	ExceptionType  string
	ExceptionValue string
	Culprit        string
	Level          string
	Status         Status
	EventCount     int64
	UserCount      int64
	FirstSeen      time.Time
	LastSeen       time.Time
	Environment    string
	Release        string
}

var ErrNotFound = errors.New("issue not found")

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// UpsertResult tells the caller whether this event started a new issue or
// reopened a previously-resolved one, so the notification layer can decide
// whether to alert.
type UpsertResult struct {
	Issue    *Issue
	IsNew    bool
	Reopened bool
}

// Upsert groups one incoming event into an issue: creates a new issue on
// first occurrence of a fingerprint, otherwise bumps the existing issue's
// counters. A resolved issue that sees a new occurrence is reopened; an
// ignored issue stays ignored (that's the point of ignoring it).
//
// Runs under BEGIN IMMEDIATE so concurrent ingestion requests serialize on
// SQLite's single writer instead of racing a read-then-write.
func (s *Store) Upsert(ctx context.Context, projectID int64, fingerprint string, title, excType, excValue, culprit, level, environment, release string, seenAt time.Time) (*UpsertResult, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	issue, err := s.upsertTx(ctx, conn, projectID, fingerprint, title, excType, excValue, culprit, level, environment, release, seenAt)
	if err != nil {
		conn.ExecContext(ctx, "ROLLBACK")
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, err
	}
	return issue, nil
}

func (s *Store) upsertTx(ctx context.Context, conn *sql.Conn, projectID int64, fingerprint string, title, excType, excValue, culprit, level, environment, release string, seenAt time.Time) (*UpsertResult, error) {
	var id int64
	var status string
	seenStr := seenAt.UTC().Format(time.RFC3339Nano)

	err := conn.QueryRowContext(ctx, `SELECT id, status FROM issues WHERE project_id = ? AND fingerprint = ?`, projectID, fingerprint).Scan(&id, &status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := conn.ExecContext(ctx, `
			INSERT INTO issues (project_id, fingerprint, title, exception_type, exception_value, culprit, level, status, event_count, user_count, first_seen, last_seen, environment, release)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'unresolved', 1, 0, ?, ?, ?, ?)`,
			projectID, fingerprint, title, excType, excValue, culprit, level, seenStr, seenStr, environment, release)
		if err != nil {
			return nil, fmt.Errorf("insert issue: %w", err)
		}
		newID, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		issue, err := s.getTx(ctx, conn, newID)
		if err != nil {
			return nil, err
		}
		return &UpsertResult{Issue: issue, IsNew: true}, nil

	case err != nil:
		return nil, fmt.Errorf("lookup issue: %w", err)
	}

	newStatus := status
	reopened := false
	if Status(status) == StatusResolved {
		newStatus = string(StatusUnresolved)
		reopened = true
	}

	_, err = conn.ExecContext(ctx, `
		UPDATE issues SET
			event_count = event_count + 1,
			last_seen = ?,
			status = ?,
			title = ?,
			exception_value = ?,
			culprit = ?,
			environment = ?,
			release = ?
		WHERE id = ?`,
		seenStr, newStatus, title, excValue, culprit, environment, release, id)
	if err != nil {
		return nil, fmt.Errorf("update issue: %w", err)
	}

	issue, err := s.getTx(ctx, conn, id)
	if err != nil {
		return nil, err
	}
	return &UpsertResult{Issue: issue, IsNew: false, Reopened: reopened}, nil
}

const selectCols = `id, project_id, fingerprint, title, exception_type, exception_value, culprit, level, status, event_count, user_count, first_seen, last_seen, environment, release`

func scan(row interface{ Scan(...any) error }) (*Issue, error) {
	var iss Issue
	var firstSeen, lastSeen, status string
	if err := row.Scan(&iss.ID, &iss.ProjectID, &iss.Fingerprint, &iss.Title, &iss.ExceptionType, &iss.ExceptionValue,
		&iss.Culprit, &iss.Level, &status, &iss.EventCount, &iss.UserCount, &firstSeen, &lastSeen, &iss.Environment, &iss.Release); err != nil {
		return nil, err
	}
	iss.Status = Status(status)
	iss.FirstSeen, _ = time.Parse(time.RFC3339Nano, firstSeen)
	iss.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeen)
	return &iss, nil
}

func (s *Store) getTx(ctx context.Context, conn *sql.Conn, id int64) (*Issue, error) {
	row := conn.QueryRowContext(ctx, `SELECT `+selectCols+` FROM issues WHERE id = ?`, id)
	return scan(row)
}

func (s *Store) Get(id int64) (*Issue, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM issues WHERE id = ?`, id)
	iss, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return iss, err
}

type ListFilter struct {
	ProjectID   int64 // 0 = all projects
	Environment string
	Status      Status // "" = any
	Query       string // substring match against title/exception_type
	Limit       int
	Offset      int
	OrderBy     string // "last_seen" (default), "event_count", "first_seen"
}

func (s *Store) List(f ListFilter) ([]*Issue, int, error) {
	where := "WHERE 1=1"
	var args []any
	if f.ProjectID != 0 {
		where += " AND project_id = ?"
		args = append(args, f.ProjectID)
	}
	if f.Environment != "" {
		where += " AND environment = ?"
		args = append(args, f.Environment)
	}
	if f.Status != "" {
		where += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.Query != "" {
		where += " AND (title LIKE ? OR exception_type LIKE ?)"
		like := "%" + f.Query + "%"
		args = append(args, like, like)
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM issues `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order := "last_seen DESC"
	switch f.OrderBy {
	case "event_count":
		order = "event_count DESC"
	case "first_seen":
		order = "first_seen DESC"
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := fmt.Sprintf(`SELECT %s FROM issues %s ORDER BY %s LIMIT ? OFFSET ?`, selectCols, where, order)
	args = append(args, limit, f.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []*Issue
	for rows.Next() {
		iss, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, iss)
	}
	return out, total, rows.Err()
}

// Sparkline24h returns, per issue id, 24 hourly event counts covering the
// last 24 hours (oldest hour first, zero-filled where an hour had no
// events) - one batched query for however many issues are on the current
// page, rather than one query per row.
func (s *Store) Sparkline24h(ids []int64) (map[int64][]int64, error) {
	out := make(map[int64][]int64, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	for _, id := range ids {
		out[id] = make([]int64, 24)
	}

	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)
	const hourFormat = "2006-01-02 15"
	// Index 23 must be the current (partial) hour, not the one before it -
	// bucket i holds events from (23-i) hours ago, so "now" always lands in
	// the last, rightmost bar instead of silently matching no bucket at all.
	bucketIndex := make(map[string]int, 24)
	for i := 0; i < 24; i++ {
		bucketIndex[now.Add(-time.Duration(23-i)*time.Hour).Format(hourFormat)] = i
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, since.Format(time.RFC3339Nano))

	query := fmt.Sprintf(`
		SELECT issue_id, strftime('%%Y-%%m-%%d %%H', timestamp) AS bucket, COUNT(*)
		FROM events WHERE issue_id IN (%s) AND timestamp >= ?
		GROUP BY issue_id, bucket`, placeholders)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var issueID int64
		var bucket string
		var count int64
		if err := rows.Scan(&issueID, &bucket, &count); err != nil {
			return nil, err
		}
		if idx, ok := bucketIndex[bucket]; ok {
			if counts, ok := out[issueID]; ok {
				counts[idx] = count
			}
		}
	}
	return out, rows.Err()
}

func (s *Store) SetStatus(id int64, status Status) error {
	_, err := s.db.Exec(`UPDATE issues SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) BulkSetStatus(ids []int64, status Status) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`UPDATE issues SET status = ? WHERE id = ?`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for _, id := range ids {
		if _, err := stmt.Exec(status, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
