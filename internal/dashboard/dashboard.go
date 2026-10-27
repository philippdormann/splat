// Package dashboard runs the read-only aggregate queries behind the main
// dashboard and per-project stat panels. Kept separate from internal/events
// and internal/issues because these are cross-cutting reporting queries,
// not part of either entity's core CRUD.
package dashboard

import (
	"database/sql"
	"github.com/philippdormann/splat/internal/database"
	"fmt"
	"strings"
	"time"
)

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

type Stats struct {
	TotalEvents      int64
	UnresolvedIssues int64
	NewIssues        int64
	AffectedProjects int64
	EventsToday      int64
	UniqueIssues     int64
	EventsDeltaPct   float64
	EventsDeltaLabel string
}

type Point struct {
	Label string
	Value int64
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// inClause builds " AND <col> IN (?,?,...)" plus args for an optional
// project scope. Empty ids mean unscoped (admin callers).
func inClause(col string, ids []int64) (string, []any) {
	if len(ids) == 0 {
		return "", nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return " AND " + col + " IN (" + placeholders + ")", args
}

func (s *Store) Stats(since, previousSince time.Time, projectIDs ...int64) (Stats, error) {
	var st Stats
	scope, scopeArgs := inClause("project_id", projectIDs)

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE timestamp >= ?`+scope, append([]any{fmtTime(since)}, scopeArgs...)...).Scan(&st.TotalEvents); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM issues WHERE status = 'unresolved'`+scope, scopeArgs...).Scan(&st.UnresolvedIssues); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM issues WHERE first_seen >= ?`+scope, append([]any{fmtTime(since)}, scopeArgs...)...).Scan(&st.NewIssues); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT project_id) FROM events WHERE timestamp >= ?`+scope, append([]any{fmtTime(since)}, scopeArgs...)...).Scan(&st.AffectedProjects); err != nil {
		return st, err
	}
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE timestamp >= ?`+scope, append([]any{fmtTime(todayStart)}, scopeArgs...)...).Scan(&st.EventsToday); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT issue_id) FROM events WHERE timestamp >= ?`+scope, append([]any{fmtTime(since)}, scopeArgs...)...).Scan(&st.UniqueIssues); err != nil {
		return st, err
	}

	var previousTotal int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE timestamp >= ? AND timestamp < ?`+scope, append([]any{fmtTime(previousSince), fmtTime(since)}, scopeArgs...)...).Scan(&previousTotal); err != nil {
		return st, err
	}
	switch {
	case previousTotal == 0 && st.TotalEvents == 0:
		st.EventsDeltaLabel = "no change"
	case previousTotal == 0:
		st.EventsDeltaPct = 100
		st.EventsDeltaLabel = "new activity"
	default:
		st.EventsDeltaPct = (float64(st.TotalEvents) - float64(previousTotal)) / float64(previousTotal) * 100
		sign := "+"
		if st.EventsDeltaPct < 0 {
			sign = ""
		}
		st.EventsDeltaLabel = fmt.Sprintf("%s%.0f%%", sign, st.EventsDeltaPct)
	}
	return st, nil
}

// bucketFormat returns an SQLite strftime format sized to the query range so
// a 1-hour window gets per-minute buckets and a 30-day window gets daily
// buckets, instead of one fixed granularity for every range.
func bucketFormat(since time.Time) string {
	span := time.Since(since)
	switch {
	case span <= 2*time.Hour:
		return "%Y-%m-%d %H:%M"
	case span <= 2*24*time.Hour:
		return "%Y-%m-%d %H:00"
	default:
		return "%Y-%m-%d"
	}
}

// pgBucketFormat is the Postgres to_char equivalent of bucketFormat.
func pgBucketFormat(since time.Time) string {
	span := time.Since(since)
	switch {
	case span <= 2*time.Hour:
		return "YYYY-MM-DD HH24:MI"
	case span <= 2*24*time.Hour:
		return "YYYY-MM-DD HH24:00"
	default:
		return "YYYY-MM-DD"
	}
}

func (s *Store) EventsOverTime(since time.Time, projectIDs ...int64) ([]Point, error) {
	scope, scopeArgs := inClause("project_id", projectIDs)
	if s.db.IsPostgres() {
		format := pgBucketFormat(since)
		rows, err := s.db.Query(`
			SELECT to_char(timestamp::timestamptz, ?) AS bucket, COUNT(*) FROM events
			WHERE timestamp >= ?`+scope+` GROUP BY bucket ORDER BY bucket`, append([]any{format, fmtTime(since)}, scopeArgs...)...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanPoints(rows)
	}
	format := bucketFormat(since)
	rows, err := s.db.Query(`
		SELECT strftime(?, timestamp) AS bucket, COUNT(*) FROM events
		WHERE timestamp >= ?`+scope+` GROUP BY bucket ORDER BY bucket`, append([]any{format, fmtTime(since)}, scopeArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPoints(rows)
}

func (s *Store) ByProject(since time.Time, projectIDs ...int64) ([]Point, error) {
	scope, scopeArgs := inClause("e.project_id", projectIDs)
	rows, err := s.db.Query(`
		SELECT p.name, COUNT(*) FROM events e JOIN projects p ON p.id = e.project_id
		WHERE e.timestamp >= ?`+scope+` GROUP BY p.id ORDER BY 2 DESC LIMIT 10`, append([]any{fmtTime(since)}, scopeArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPoints(rows)
}

func (s *Store) TopExceptionTypes(since time.Time, projectIDs ...int64) ([]Point, error) {
	scope, scopeArgs := inClause("project_id", projectIDs)
	rows, err := s.db.Query(`
		SELECT COALESCE(NULLIF(exception_type, ''), 'message'), COUNT(*) FROM events
		WHERE timestamp >= ?`+scope+` GROUP BY 1 ORDER BY 2 DESC LIMIT 10`, append([]any{fmtTime(since)}, scopeArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPoints(rows)
}

func (s *Store) ByEnvironment(since time.Time, projectIDs ...int64) ([]Point, error) {
	scope, scopeArgs := inClause("project_id", projectIDs)
	rows, err := s.db.Query(`
		SELECT COALESCE(NULLIF(environment, ''), 'unknown'), COUNT(*) FROM events
		WHERE timestamp >= ?`+scope+` GROUP BY 1 ORDER BY 2 DESC`, append([]any{fmtTime(since)}, scopeArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPoints(rows)
}

// IssuesByStatus breaks down, among issues with at least one event in
// range, how many currently sit in each status (unresolved/resolved/
// ignored) - a current snapshot of triage state for recently active
// issues, not an events-per-status count (an issue keeps its event history
// once resolved, so counting events would just reflect activity before it
// was resolved, not how much is still outstanding).
func (s *Store) IssuesByStatus(since time.Time, projectIDs ...int64) ([]Point, error) {
	scope, scopeArgs := inClause("i.project_id", projectIDs)
	rows, err := s.db.Query(`
		SELECT i.status, COUNT(DISTINCT i.id) FROM issues i
		JOIN events e ON e.issue_id = i.id
		WHERE e.timestamp >= ?`+scope+` GROUP BY i.status`, append([]any{fmtTime(since)}, scopeArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPoints(rows)
}

// NewVsRecurring counts, among events in range, how many belong to issues
// first seen in-range ("new") versus issues that already existed ("recurring").
func (s *Store) NewVsRecurring(since time.Time, projectIDs ...int64) (newCount, recurringCount int64, err error) {
	scope, scopeArgs := inClause("e.project_id", projectIDs)
	args := append([]any{fmtTime(since), fmtTime(since), fmtTime(since)}, scopeArgs...)
	err = s.db.QueryRow(`
		SELECT
			SUM(CASE WHEN i.first_seen >= ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN i.first_seen < ? THEN 1 ELSE 0 END)
		FROM events e JOIN issues i ON i.id = e.issue_id
		WHERE e.timestamp >= ?`+scope, args...).Scan(&newCount, &recurringCount)
	return
}

type RecentIssue struct {
	ID               int64
	Title            string
	Level            string
	ProjectName      string
	Environment      string
	EventCount       int64
	LastSeen         time.Time
	LastSeenRelative string
}

func (s *Store) RecentIssues(limit int, projectIDs ...int64) ([]RecentIssue, error) {
	scope, scopeArgs := inClause("i.project_id", projectIDs)
	rows, err := s.db.Query(`
		SELECT i.id, i.title, i.level, p.name, i.environment, i.event_count, i.last_seen
		FROM issues i JOIN projects p ON p.id = i.project_id
		WHERE 1=1`+scope+`
		ORDER BY i.last_seen DESC LIMIT ?`, append(scopeArgs, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecentIssue{}
	for rows.Next() {
		var ri RecentIssue
		var lastSeen string
		if err := rows.Scan(&ri.ID, &ri.Title, &ri.Level, &ri.ProjectName, &ri.Environment, &ri.EventCount, &lastSeen); err != nil {
			return nil, err
		}
		ri.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeen)
		out = append(out, ri)
	}
	return out, rows.Err()
}

type ProjectStats struct {
	Events7d   int64
	Unresolved int64
}

type SeverityPoint struct {
	Bucket string
	Level  string
	Value  int64
}

// EventsBySeverity buckets one project's events per level over the window,
// backing the overview's stacked volume chart. Level lives on issues, not
// events, hence the join.
func (s *Store) EventsBySeverity(projectID int64, since time.Time) ([]SeverityPoint, error) {
	var rows *sql.Rows
	var err error
	if s.db.IsPostgres() {
		format := pgBucketFormat(since)
		rows, err = s.db.Query(`
			SELECT to_char(e.timestamp::timestamptz, ?) AS bucket, COALESCE(NULLIF(i.level, ''), 'error'), COUNT(*)
			FROM events e JOIN issues i ON i.id = e.issue_id
			WHERE e.project_id = ? AND e.timestamp >= ?
			GROUP BY bucket, 2 ORDER BY bucket`, format, projectID, fmtTime(since))
	} else {
		format := bucketFormat(since)
		rows, err = s.db.Query(`
			SELECT strftime(?, e.timestamp) AS bucket, COALESCE(NULLIF(i.level, ''), 'error'), COUNT(*)
			FROM events e JOIN issues i ON i.id = e.issue_id
			WHERE e.project_id = ? AND e.timestamp >= ?
			GROUP BY bucket, 2 ORDER BY bucket`, format, projectID, fmtTime(since))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SeverityPoint{}
	for rows.Next() {
		var p SeverityPoint
		if err := rows.Scan(&p.Bucket, &p.Level, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// NewIssues counts a project's issues first seen inside the window.
func (s *Store) NewIssues(projectID int64, since time.Time) (int64, error) {
	var n int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM issues WHERE project_id = ? AND first_seen >= ?`,
		projectID, fmtTime(since)).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ProjectEvents counts a project's events inside the window.
func (s *Store) ProjectEvents(projectID int64, since time.Time) (int64, error) {
	var n int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE project_id = ? AND timestamp >= ?`,
		projectID, fmtTime(since)).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) ProjectStats(projectID int64) (ProjectStats, error) {
	var ps ProjectStats
	since := time.Now().AddDate(0, 0, -7)
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE project_id = ? AND timestamp >= ?`, projectID, fmtTime(since)).Scan(&ps.Events7d); err != nil {
		return ps, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM issues WHERE project_id = ? AND status = 'unresolved'`, projectID).Scan(&ps.Unresolved); err != nil {
		return ps, err
	}
	return ps, nil
}

func (s *Store) DistinctEnvironments(projectID int64) ([]string, error) {
	var rows *sql.Rows
	var err error
	if projectID > 0 {
		rows, err = s.db.Query(`SELECT DISTINCT environment FROM issues WHERE project_id = ? AND environment != '' ORDER BY 1`, projectID)
	} else {
		rows, err = s.db.Query(`SELECT DISTINCT environment FROM issues WHERE environment != '' ORDER BY 1`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func scanPoints(rows *sql.Rows) ([]Point, error) {
	out := []Point{}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.Label, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
