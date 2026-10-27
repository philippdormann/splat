// Package issues implements issue grouping: turning a stream of events into
// deduplicated, trackable issues.
package issues

import (
	"context"
	"database/sql"
	"github.com/philippdormann/splat/internal/database"
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
	StatusMuted      Status = "muted"
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
	// MutedUntil is set only while Status is muted; zero means an
	// indefinite mute. A new event past the timestamp unmutes the issue.
	MutedUntil time.Time
	// ResolvedInRelease pins a resolve to the issue's release at resolve
	// time (see ResolveInNextRelease). Empty means a plain manual resolve.
	ResolvedInRelease string
}

var ErrNotFound = errors.New("issue not found")

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

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
// ignored issue stays ignored (that's the point of ignoring it). A muted
// issue stays muted - unless its mute expired, in which case the event
// unmutes it back to unresolved (reported as reopened so alert rules can
// fire on the renewed activity).
//
// On SQLite runs under BEGIN IMMEDIATE so concurrent ingestion requests
// serialize on SQLite's single writer instead of racing a read-then-write.
// On Postgres it uses a regular transaction with SELECT ... FOR UPDATE,
// letting Postgres row locking serialize the same race.
func (s *Store) Upsert(ctx context.Context, projectID int64, fingerprint string, title, excType, excValue, culprit, level, environment, release string, seenAt time.Time) (*UpsertResult, error) {
	if s.db.IsPostgres() {
		return s.upsertPostgres(ctx, projectID, fingerprint, title, excType, excValue, culprit, level, environment, release, seenAt)
	}
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

// muteExpired reports whether a mute timestamp has lapsed as of now. Empty
// or unparseable means an indefinite (or corrupt, fail-quiet) mute: never
// expires, the safe direction for a silencing feature.
func muteExpired(mutedUntil string, now time.Time) bool {
	if mutedUntil == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339Nano, mutedUntil)
	if err != nil {
		return false
	}
	return !until.After(now)
}

// nextStatus resolves the status transition for a new occurrence of an
// existing issue, shared by both upsert paths. release is the incoming
// event's release.
func nextStatus(status, mutedUntil, resolvedInRelease, release string, seenAt time.Time) (newStatus string, mutedOut string, resolvedOut string, reopened bool) {
	newStatus, mutedOut, resolvedOut = status, mutedUntil, resolvedInRelease
	switch Status(status) {
	case StatusResolved:
		// Pinned resolve: same-release events keep it resolved; a
		// different non-empty release means the bug survived the release
		// and reopens the issue. Plain resolves (empty pin) reopen on
		// anything, as before. String comparison is deliberate: releases
		// are opaque operator strings, not necessarily semver.
		if resolvedInRelease == "" || (release != "" && release != resolvedInRelease) {
			newStatus = string(StatusUnresolved)
			resolvedOut = ""
			reopened = true
		}
	case StatusMuted:
		if muteExpired(mutedUntil, seenAt) {
			newStatus = string(StatusUnresolved)
			mutedOut = ""
			reopened = true
		}
	}
	return newStatus, mutedOut, resolvedOut, reopened
}

// upsertPostgres is the Postgres variant of Upsert: one transaction, row
// lock on the matching issue when it exists, INSERT ... RETURNING id when
// it does not.
func (s *Store) upsertPostgres(ctx context.Context, projectID int64, fingerprint, title, excType, excValue, culprit, level, environment, release string, seenAt time.Time) (*UpsertResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	seenStr := seenAt.UTC().Format(time.RFC3339Nano)
	var id int64
	var status string
	var mutedUntil string
	var resolvedInRelease string
	err = tx.QueryRowContext(ctx, `SELECT id, status, muted_until, resolved_in_release FROM issues WHERE project_id = ? AND fingerprint = ? FOR UPDATE`, projectID, fingerprint).Scan(&id, &status, &mutedUntil, &resolvedInRelease)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var newID int64
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO issues (project_id, fingerprint, title, exception_type, exception_value, culprit, level, status, event_count, user_count, first_seen, last_seen, environment, release)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'unresolved', 1, 0, ?, ?, ?, ?) RETURNING id`,
			projectID, fingerprint, title, excType, excValue, culprit, level, seenStr, seenStr, environment, release).Scan(&newID); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("insert issue: %w", err)
		}
		row := tx.QueryRowContext(ctx, `SELECT `+selectCols+` FROM issues WHERE id = ?`, newID)
		issue, err := scan(row)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &UpsertResult{Issue: issue, IsNew: true}, nil
	case err != nil:
		tx.Rollback()
		return nil, fmt.Errorf("lookup issue: %w", err)
	}
	newStatus, mutedOut, resolvedOut, reopened := nextStatus(status, mutedUntil, resolvedInRelease, release, seenAt)
	if _, err := tx.ExecContext(ctx, `
		UPDATE issues SET
			event_count = event_count + 1,
			last_seen = ?,
			status = ?,
			muted_until = ?,
			resolved_in_release = ?,
			title = ?,
			exception_value = ?,
			culprit = ?,
			environment = ?,
			release = ?
		WHERE id = ?`,
		seenStr, newStatus, mutedOut, resolvedOut, title, excValue, culprit, environment, release, id); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("update issue: %w", err)
	}
	row := tx.QueryRowContext(ctx, `SELECT `+selectCols+` FROM issues WHERE id = ?`, id)
	issue, err := scan(row)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &UpsertResult{Issue: issue, IsNew: false, Reopened: reopened}, nil
}

func (s *Store) upsertTx(ctx context.Context, conn *database.Conn, projectID int64, fingerprint string, title, excType, excValue, culprit, level, environment, release string, seenAt time.Time) (*UpsertResult, error) {
	var id int64
	var status string
	var mutedUntil string
	var resolvedInRelease string
	seenStr := seenAt.UTC().Format(time.RFC3339Nano)

	err := conn.QueryRowContext(ctx, `SELECT id, status, muted_until, resolved_in_release FROM issues WHERE project_id = ? AND fingerprint = ?`, projectID, fingerprint).Scan(&id, &status, &mutedUntil, &resolvedInRelease)
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

	newStatus, mutedOut, resolvedOut, reopened := nextStatus(status, mutedUntil, resolvedInRelease, release, seenAt)

	_, err = conn.ExecContext(ctx, `
		UPDATE issues SET
			event_count = event_count + 1,
			last_seen = ?,
			status = ?,
			muted_until = ?,
			resolved_in_release = ?,
			title = ?,
			exception_value = ?,
			culprit = ?,
			environment = ?,
			release = ?
		WHERE id = ?`,
		seenStr, newStatus, mutedOut, resolvedOut, title, excValue, culprit, environment, release, id)
	if err != nil {
		return nil, fmt.Errorf("update issue: %w", err)
	}

	issue, err := s.getTx(ctx, conn, id)
	if err != nil {
		return nil, err
	}
	return &UpsertResult{Issue: issue, IsNew: false, Reopened: reopened}, nil
}

const selectCols = `id, project_id, fingerprint, title, exception_type, exception_value, culprit, level, status, event_count, user_count, first_seen, last_seen, environment, release, muted_until, resolved_in_release`

func scan(row interface{ Scan(...any) error }) (*Issue, error) {
	var iss Issue
	var firstSeen, lastSeen, status, mutedUntil string
	if err := row.Scan(&iss.ID, &iss.ProjectID, &iss.Fingerprint, &iss.Title, &iss.ExceptionType, &iss.ExceptionValue,
		&iss.Culprit, &iss.Level, &status, &iss.EventCount, &iss.UserCount, &firstSeen, &lastSeen, &iss.Environment, &iss.Release, &mutedUntil, &iss.ResolvedInRelease); err != nil {
		return nil, err
	}
	iss.Status = Status(status)
	iss.FirstSeen, _ = time.Parse(time.RFC3339Nano, firstSeen)
	iss.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeen)
	if mutedUntil != "" {
		iss.MutedUntil, _ = time.Parse(time.RFC3339Nano, mutedUntil)
	}
	return &iss, nil
}

func (s *Store) getTx(ctx context.Context, conn *database.Conn, id int64) (*Issue, error) {
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
	ProjectID   int64   // 0 = all projects (or all of ProjectIDs when set)
	ProjectIDs  []int64 // optional scope: member sessions pass their grants
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
	} else if len(f.ProjectIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(f.ProjectIDs)), ",")
		where += " AND project_id IN (" + placeholders + ")"
		for _, id := range f.ProjectIDs {
			args = append(args, id)
		}
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

	out := []*Issue{}
	for rows.Next() {
		iss, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, iss)
	}
	return out, total, rows.Err()
}

// NewSince lists issues first seen at or after `since` (and before `until`
// when non-nil), biggest by lifetime event count first. Release pages use
// it to answer "what broke in this release": the window runs from the
// release's first sighting until the next-newer release appears.
func (s *Store) NewSince(projectID int64, since time.Time, until *time.Time, limit int) ([]*Issue, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	where := "WHERE project_id = ? AND first_seen >= ?"
	args := []any{projectID, since.UTC().Format(time.RFC3339Nano)}
	if until != nil {
		where += " AND first_seen < ?"
		args = append(args, until.UTC().Format(time.RFC3339Nano))
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT `+selectCols+` FROM issues `+where+` ORDER BY event_count DESC, last_seen DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Issue{}
	for rows.Next() {
		iss, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, iss)
	}
	return out, rows.Err()
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

	bucketExpr := `strftime('%Y-%m-%d %H', timestamp)`
	if s.db.IsPostgres() {
		bucketExpr = `to_char(timestamp::timestamptz, 'YYYY-MM-DD HH24')`
	}
	query := fmt.Sprintf(`
		SELECT issue_id, %s AS bucket, COUNT(*)
		FROM events WHERE issue_id IN (%s) AND timestamp >= ?
		GROUP BY issue_id, bucket`, bucketExpr, placeholders)
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
	if status == StatusMuted {
		return s.SetMuted(id, time.Time{})
	}
	// Leaving muted clears the mute timestamp; resolving plainly clears
	// any release pin (see ResolveInNextRelease). Any other transition
	// leaves the rest alone.
	_, err := s.db.Exec(`UPDATE issues SET status = ?, muted_until = '', resolved_in_release = '' WHERE id = ?`, status, id)
	return err
}

// ResolveInNextRelease resolves the issue pinned to its current release:
// later events from the same release keep it resolved, while an event
// with a different release reopens it (the fix didn't hold). Issues with
// no release recorded behave like a plain resolve.
func (s *Store) ResolveInNextRelease(id int64) error {
	issue, err := s.Get(id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE issues SET status = 'resolved', muted_until = '', resolved_in_release = ? WHERE id = ?`,
		issue.Release, id)
	return err
}

// SetMuted mutes an issue, optionally until a timestamp (zero = forever).
func (s *Store) SetMuted(id int64, until time.Time) error {
	mutedUntil := ""
	if !until.IsZero() {
		mutedUntil = until.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(`UPDATE issues SET status = 'muted', muted_until = ? WHERE id = ?`, mutedUntil, id)
	return err
}

func (s *Store) BulkSetStatus(ids []int64, status Status) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	// Bulk mute is always indefinite; any other bulk transition clears a
	// possibly lingering mute timestamp or release pin.
	stmt, err := tx.Prepare(`UPDATE issues SET status = ?, muted_until = '', resolved_in_release = '' WHERE id = ?`)
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

// MaxNoteBytes caps a note at 10 KiB of text - generous for an operator
// memo, small enough that notes can't bloat the database.
const MaxNoteBytes = 10 << 10

type Note struct {
	ID        int64
	IssueID   int64
	Body      string
	CreatedAt time.Time
}

var ErrEmptyNote = errors.New("note body is required")
var ErrNoteTooLarge = errors.New("note exceeds 10 KiB")

func (s *Store) Notes(issueID int64) ([]Note, error) {
	rows, err := s.db.Query(`SELECT id, issue_id, body, created_at FROM issue_notes WHERE issue_id = ? ORDER BY id`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		var created string
		if err := rows.Scan(&n.ID, &n.IssueID, &n.Body, &created); err != nil {
			return nil, err
		}
		n.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) AddNote(issueID int64, body string) (Note, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Note{}, ErrEmptyNote
	}
	if len(body) > MaxNoteBytes {
		return Note{}, ErrNoteTooLarge
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if s.db.IsPostgres() {
		var n Note
		var created string
		err := s.db.QueryRow(`INSERT INTO issue_notes (issue_id, body, created_at) VALUES (?, ?, ?) RETURNING id, issue_id, body, created_at`,
			issueID, body, now).Scan(&n.ID, &n.IssueID, &n.Body, &created)
		if err != nil {
			return Note{}, err
		}
		n.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		return n, nil
	}
	res, err := s.db.Exec(`INSERT INTO issue_notes (issue_id, body, created_at) VALUES (?, ?, ?)`, issueID, body, now)
	if err != nil {
		return Note{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Note{}, err
	}
	return Note{ID: id, IssueID: issueID, Body: body, CreatedAt: time.Now().UTC()}, nil
}

func (s *Store) DeleteNote(issueID, noteID int64) error {
	_, err := s.db.Exec(`DELETE FROM issue_notes WHERE id = ? AND issue_id = ?`, noteID, issueID)
	return err
}
