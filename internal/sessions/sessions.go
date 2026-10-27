// Package sessions ingests Sentry session envelopes and aggregates
// release health from them: crash-free sessions/users, session-health
// timeseries, and the per-release rollup behind the releases view.
//
// Two shapes arrive on the same envelope endpoint: individual session
// updates (type "session", upserted by sid) and aggregate buckets
// (type "sessions", summed per day/release/environment). Crash-free
// session counts use both; per-user rates use individual sessions only,
// since aggregate buckets cannot be deduplicated across days without
// per-user rows.
package sessions

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"errgotrack/internal/database"
)

// SessionUpdate is one "session" envelope item.
// Reference: https://develop.sentry.dev/sdk/sessions/
type SessionUpdate struct {
	SID         string         `json:"sid"`
	DistinctID  any            `json:"did"`
	Started     string         `json:"started"`
	Timestamp   string         `json:"timestamp"`
	Duration    float64        `json:"duration"`
	Status      string         `json:"status"`
	Errors      int            `json:"errors"`
	Attrs       map[string]any `json:"attrs"`
	Release     string         `json:"release"`
	Environment string         `json:"environment"`
}

// AggregateBucket is one entry of a "sessions" envelope item.
type AggregateBucket struct {
	Started     string `json:"started"`
	Exited      int    `json:"exited"`
	Errored     int    `json:"errored"`
	Crashed     int    `json:"crashed"`
	Abnormal    int    `json:"abnormal"`
	Release     string `json:"release"`
	Environment string `json:"environment"`
}

type aggregatesPayload struct {
	Aggregates []AggregateBucket `json:"aggregates"`
	Attrs      map[string]any    `json:"attrs"`
}

func strAttr(attrs map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := attrs[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func distinctID(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// JSON numbers decode as float64; render integrally when whole.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%v", t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

var knownStatuses = map[string]bool{
	"ok": true, "exited": true, "errored": true, "crashed": true, "abnormal": true,
}

func normalizeStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if knownStatuses[s] {
		return s
	}
	// Empty (session init) and unknown statuses count as healthy rather
	// than crashing the math.
	return "ok"
}

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

// UpsertSession records one individual session update, keyed by sid.
func (s *Store) UpsertSession(projectID int64, raw []byte) error {
	var u SessionUpdate
	if err := json.Unmarshal(raw, &u); err != nil {
		return fmt.Errorf("decode session: %w", err)
	}
	if strings.TrimSpace(u.SID) == "" {
		return fmt.Errorf("session missing sid")
	}
	release := u.Release
	if release == "" {
		release = strAttr(u.Attrs, "release")
	}
	env := u.Environment
	if env == "" {
		env = strAttr(u.Attrs, "environment")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`
		INSERT INTO sdk_sessions (project_id, sid, distinct_id, release, environment, status, errors, started, duration_secs, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id, sid) DO UPDATE SET
			distinct_id = excluded.distinct_id,
			release = excluded.release,
			environment = excluded.environment,
			status = excluded.status,
			errors = excluded.errors,
			started = excluded.started,
			duration_secs = excluded.duration_secs,
			updated_at = excluded.updated_at`,
		projectID, u.SID, distinctID(u.DistinctID), release, env,
		normalizeStatus(u.Status), u.Errors, u.Started, u.Duration, now)
	return err
}

// AddAggregates folds one "sessions" envelope item into per-day buckets.
func (s *Store) AddAggregates(projectID int64, raw []byte) error {
	var p aggregatesPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("decode session aggregates: %w", err)
	}
	defRelease := strAttr(p.Attrs, "release")
	defEnv := strAttr(p.Attrs, "environment")
	for _, b := range p.Aggregates {
		release := b.Release
		if release == "" {
			release = defRelease
		}
		env := b.Environment
		if env == "" {
			env = defEnv
		}
		day := bucketDay(b.Started)
		_, err := s.db.Exec(`
			INSERT INTO sdk_session_buckets (day, project_id, release, environment, exited, errored, crashed, abnormal)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (day, project_id, release, environment) DO UPDATE SET
				exited = sdk_session_buckets.exited + excluded.exited,
				errored = sdk_session_buckets.errored + excluded.errored,
				crashed = sdk_session_buckets.crashed + excluded.crashed,
				abnormal = sdk_session_buckets.abnormal + excluded.abnormal`,
			day, projectID, release, env, b.Exited, b.Errored, b.Crashed, b.Abnormal)
		if err != nil {
			return err
		}
	}
	return nil
}

// bucketDay reduces an aggregate start timestamp to its UTC calendar day.
// Unparseable input lands in today's bucket rather than being dropped.
func bucketDay(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	if t, err := time.Parse("2006-01-02T15:04:05", strings.TrimSuffix(ts, "Z")); err == nil {
		return t.Format("2006-01-02")
	}
	return time.Now().UTC().Format("2006-01-02")
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// CrashFree aggregates release health over the window. Release "" means
// all releases; the zero time means all-time.
func (s *Store) CrashFree(projectID int64, release string, since time.Time) (Summary, error) {
	var out Summary
	// Individual sessions: healthy = anything but crashed/abnormal.
	var total, bad int64
	var totalUsers, badUsers int64
	q := `SELECT COUNT(*), SUM(CASE WHEN status IN ('crashed','abnormal') THEN 1 ELSE 0 END),
		COUNT(DISTINCT CASE WHEN distinct_id != '' THEN distinct_id END),
		COUNT(DISTINCT CASE WHEN distinct_id != '' AND status IN ('crashed','abnormal') THEN distinct_id END)
		FROM sdk_sessions WHERE project_id = ? AND updated_at >= ?`
	qargs := []any{projectID, fmtTime(since)}
	if release != "" {
		q += ` AND release = ?`
		qargs = append(qargs, release)
	}
	var badNull, usersNull, badUsersNull sql.NullInt64
	if err := s.db.QueryRow(q, qargs...).Scan(&total, &badNull, &usersNull, &badUsersNull); err != nil {
		return out, err
	}
	bad = badNull.Int64
	totalUsers = usersNull.Int64
	badUsers = badUsersNull.Int64

	// Aggregate buckets add session counts (not users).
	var bexited, berrored, bcrashed, babnormal sql.NullInt64
	bq := `SELECT SUM(exited), SUM(errored), SUM(crashed), SUM(abnormal)
		FROM sdk_session_buckets WHERE project_id = ? AND day >= ?`
	day := since.UTC().Format("2006-01-02")
	if since.IsZero() {
		day = ""
	}
	bargs := []any{projectID, day}
	if release != "" {
		bq += ` AND release = ?`
		bargs = append(bargs, release)
	}
	if err := s.db.QueryRow(bq, bargs...).Scan(&bexited, &berrored, &bcrashed, &babnormal); err != nil {
		return out, err
	}
	total += bexited.Int64 + berrored.Int64 + bcrashed.Int64 + babnormal.Int64
	bad += bcrashed.Int64 + babnormal.Int64

	out.TotalSessions = total
	out.TotalUsers = totalUsers
	if total > 0 {
		out.CrashFreeSessions = float64(total-bad) / float64(total) * 100
	}
	if totalUsers > 0 {
		out.CrashFreeUsers = float64(totalUsers-badUsers) / float64(totalUsers) * 100
	}
	return out, nil
}

// Summary is crash-free health over a window.
type Summary struct {
	TotalSessions     int64   `json:"totalSessions"`
	TotalUsers        int64   `json:"users"`
	CrashFreeSessions float64 `json:"crashFreeSessions"`
	CrashFreeUsers    float64 `json:"crashFreeUsers"`
}

// HealthPoint is one timeseries bucket for the session-health area chart.
type HealthPoint struct {
	Bucket  string `json:"bucket"`
	Healthy int64  `json:"healthy"`
	Crashed int64  `json:"crashed"`
}

// HealthTimeseries buckets individual sessions by start day plus aggregate
// buckets by their day. Buckets use the same day granularity as the
// aggregates table, which keeps both sources comparable.
func (s *Store) HealthTimeseries(projectID int64, since time.Time) ([]HealthPoint, error) {
	byBucket := map[string]*HealthPoint{}
	var order []string
	add := func(bucket string, healthy, crashed int64) {
		p, ok := byBucket[bucket]
		if !ok {
			p = &HealthPoint{Bucket: bucket}
			byBucket[bucket] = p
			order = append(order, bucket)
		}
		p.Healthy += healthy
		p.Crashed += crashed
	}

	rows, err := s.db.Query(`
		SELECT substr(started, 1, 10) AS day,
			SUM(CASE WHEN status IN ('crashed','abnormal') THEN 0 ELSE 1 END),
			SUM(CASE WHEN status IN ('crashed','abnormal') THEN 1 ELSE 0 END)
		FROM sdk_sessions WHERE project_id = ? AND updated_at >= ?
		GROUP BY day ORDER BY day`, projectID, fmtTime(since))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day string
		var h, c sql.NullInt64
		if err := rows.Scan(&day, &h, &c); err != nil {
			rows.Close()
			return nil, err
		}
		if day == "" {
			day = since.UTC().Format("2006-01-02")
		}
		add(day, h.Int64, c.Int64)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	brows, err := s.db.Query(`
		SELECT day, SUM(exited + errored), SUM(crashed + abnormal)
		FROM sdk_session_buckets WHERE project_id = ? AND day >= ?
		GROUP BY day ORDER BY day`, projectID, since.UTC().Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer brows.Close()
	for brows.Next() {
		var day string
		var h, c sql.NullInt64
		if err := brows.Scan(&day, &h, &c); err != nil {
			return nil, err
		}
		add(day, h.Int64, c.Int64)
	}
	if err := brows.Err(); err != nil {
		return nil, err
	}

	out := make([]HealthPoint, 0, len(order))
	for _, b := range order {
		out = append(out, *byBucket[b])
	}
	return out, nil
}

// ReleaseHealth is one row of the releases view.
type ReleaseHealth struct {
	Release           string  `json:"release"`
	Events            int64   `json:"events"`
	Issues            int64   `json:"issues"`
	Sessions          int64   `json:"sessions"`
	Users             int64   `json:"users"`
	Transactions      int64   `json:"transactions"`
	CrashFreeSessions float64 `json:"crashFreeSessions"`
	CrashFreeUsers    float64 `json:"crashFreeUsers"`
	FirstSeen         string  `json:"firstSeen"`
	LastSeen          string  `json:"lastSeen"`
}

// Releases rolls every release seen by a project into one row each,
// most recently active first.
func (s *Store) Releases(projectID int64) ([]ReleaseHealth, error) {
	db := s.db
	byRelease := map[string]*ReleaseHealth{}
	var order []string
	touch := func(release string) *ReleaseHealth {
		r, ok := byRelease[release]
		if !ok {
			r = &ReleaseHealth{Release: release}
			byRelease[release] = r
			order = append(order, release)
		}
		return r
	}
	lastActivity := map[string]string{}

	rows, err := db.Query(`
		SELECT COALESCE(NULLIF(release, ''), '(none)'),
			COUNT(*), COUNT(DISTINCT issue_id),
			MIN(timestamp), MAX(timestamp)
		FROM events WHERE project_id = ? GROUP BY 1 ORDER BY MAX(timestamp) DESC`, projectID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var release, first, last string
		var events, issues int64
		if err := rows.Scan(&release, &events, &issues, &first, &last); err != nil {
			rows.Close()
			return nil, err
		}
		r := touch(release)
		r.Events, r.Issues, r.FirstSeen, r.LastSeen = events, issues, first, last
		lastActivity[release] = last
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	srows, err := db.Query(`
		SELECT COALESCE(NULLIF(release, ''), '(none)'), COUNT(*),
			COUNT(DISTINCT CASE WHEN distinct_id != '' THEN distinct_id END),
			MIN(started), MAX(updated_at)
		FROM sdk_sessions WHERE project_id = ? GROUP BY 1`, projectID)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var release, first, last string
		var n, users int64
		if err := srows.Scan(&release, &n, &users, &first, &last); err != nil {
			srows.Close()
			return nil, err
		}
		r := touch(release)
		r.Sessions += n
		r.Users += users
		if first != "" && (r.FirstSeen == "" || first < r.FirstSeen) {
			r.FirstSeen = first
		}
		if last != "" && (r.LastSeen == "" || last > r.LastSeen) {
			r.LastSeen = last
			lastActivity[release] = last
		}
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, err
	}

	// Transactions (distributed traces) also record their release.
	trows, err := db.Query(`
		SELECT COALESCE(NULLIF(release, ''), '(none)'), COUNT(*),
			MIN(timestamp), MAX(timestamp)
		FROM transactions WHERE project_id = ? GROUP BY 1`, projectID)
	if err != nil {
		return nil, err
	}
	for trows.Next() {
		var release, first, last string
		var n int64
		if err := trows.Scan(&release, &n, &first, &last); err != nil {
			trows.Close()
			return nil, err
		}
		r := touch(release)
		r.Transactions += n
		if first != "" && (r.FirstSeen == "" || first < r.FirstSeen) {
			r.FirstSeen = first
		}
		if last != "" && (r.LastSeen == "" || last > r.LastSeen) {
			r.LastSeen = last
			lastActivity[release] = last
		}
	}
	trows.Close()
	if err := trows.Err(); err != nil {
		return nil, err
	}

	// Crash-free per release, all-time.
	for _, release := range order {
		rel := release
		if rel == "(none)" {
			rel = ""
		}
		sum, err := s.CrashFree(projectID, rel, time.Time{})
		if err != nil {
			return nil, err
		}
		byRelease[release].Sessions = sum.TotalSessions
		byRelease[release].Users = sum.TotalUsers
		byRelease[release].CrashFreeSessions = sum.CrashFreeSessions
		byRelease[release].CrashFreeUsers = sum.CrashFreeUsers
	}

	// Most recent activity first.
	sort.SliceStable(order, func(i, j int) bool {
		return lastActivity[order[i]] > lastActivity[order[j]]
	})
	out := make([]ReleaseHealth, 0, len(order))
	for _, release := range order {
		out = append(out, *byRelease[release])
	}
	return out, nil
}

// ReleaseEnvRow breaks one release down by environment.
type ReleaseEnvRow struct {
	Environment string
	Events      int64
	Sessions    int64
}

// ReleaseEnvironments counts a release's events and sessions per
// environment, busiest first. Release "" reads as "(none)", matching the
// Releases rollup's label for empty releases.
func (s *Store) ReleaseEnvironments(projectID int64, release string) ([]ReleaseEnvRow, error) {
	db := s.db
	byEnv := map[string]*ReleaseEnvRow{}
	var order []string
	touch := func(env string) *ReleaseEnvRow {
		if env == "" {
			env = "(none)"
		}
		r, ok := byEnv[env]
		if !ok {
			r = &ReleaseEnvRow{Environment: env}
			byEnv[env] = r
			order = append(order, env)
		}
		return r
	}

	rows, err := db.Query(`
		SELECT COALESCE(NULLIF(environment, ''), '(none)'), COUNT(*)
		FROM events WHERE project_id = ? AND release = ? GROUP BY 1`, projectID, release)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var env string
		var n int64
		if err := rows.Scan(&env, &n); err != nil {
			rows.Close()
			return nil, err
		}
		touch(env).Events = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	srows, err := db.Query(`
		SELECT COALESCE(NULLIF(environment, ''), '(none)'), COUNT(*)
		FROM sdk_sessions WHERE project_id = ? AND release = ? GROUP BY 1`, projectID, release)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var env string
		var n int64
		if err := srows.Scan(&env, &n); err != nil {
			srows.Close()
			return nil, err
		}
		touch(env).Sessions = n
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(order, func(i, j int) bool {
		a, b := byEnv[order[i]], byEnv[order[j]]
		if a.Events != b.Events {
			return a.Events > b.Events
		}
		return a.Sessions > b.Sessions
	})
	out := make([]ReleaseEnvRow, 0, len(order))
	for _, env := range order {
		out = append(out, *byEnv[env])
	}
	return out, nil
}
