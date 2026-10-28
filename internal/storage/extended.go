package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/philippdormann/splat/internal/database"
)

// Kinds tracked by retention (events, transactions, logs). Source maps
// are content-addressed uploads with their own GC, not age-based.
var Kinds = []string{"events", "transactions", "logs"}

// Age bucket bounds in days (last bucket is open-ended).
var AgeBounds = []int{7, 14, 30, 90, 180, 365}

type RetentionWindows struct {
	EventsDays       *int `json:"events_days"`
	TransactionsDays *int `json:"transactions_days"`
	LogsDays         *int `json:"logs_days"`
}

type ProjectOverride struct {
	ProjectID int64 `json:"project_id"`
	RetentionWindows
}

type RetentionSettings struct {
	Defaults  RetentionWindows   `json:"defaults"`
	Overrides []ProjectOverride `json:"overrides"`
}

type RetentionRun struct {
	StartedAt  string         `json:"started_at"`
	FinishedAt *string        `json:"finished_at,omitempty"`
	Status     string         `json:"status"`
	Removed    map[string]int64 `json:"removed,omitempty"`
	Error      *string        `json:"error,omitempty"`
}

type RetentionResponse struct {
	Settings RetentionSettings `json:"settings"`
	NextRunAt *string         `json:"next_run_at,omitempty"`
	LastRun   *RetentionRun   `json:"last_run,omitempty"`
}

type KindCounts struct {
	Events       int64 `json:"events_count"`
	Transactions int64 `json:"transactions_count"`
	Logs         int64 `json:"logs_count"`
}

type ProjectStorage struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	EventsBytes int64 `json:"events_bytes"`
	TransactionsBytes int64 `json:"transactions_bytes"`
	LogsBytes  int64  `json:"logs_bytes"`
	EventsCount int64 `json:"events_count"`
	TransactionsCount int64 `json:"transactions_count"`
	LogsCount  int64  `json:"logs_count"`
	Share      float64 `json:"share"`
	Custom     bool   `json:"custom"`
}

type StorageSummary struct {
	Bytes          int64 `json:"bytes"`
	EventsRows     int64 `json:"events_rows"`
	TransactionsRows int64 `json:"transactions_rows"`
	LogsRows       int64 `json:"logs_rows"`
	SourceMapFiles int64 `json:"source_maps_files"`
	SourceMapBytes int64 `json:"source_maps_bytes"`
	FreesEstimate  int64 `json:"frees_estimate"`
}

type AgeBucket struct {
	Events       int64 `json:"events"`
	Transactions int64 `json:"transactions"`
	Logs         int64 `json:"logs"`
}

type StorageAge struct {
	Bounds  []int       `json:"bounds"`
	Buckets []AgeBucket `json:"buckets"`
}

type CleanupStatus struct {
	State     string         `json:"state"`
	Kind      string         `json:"kind"`
	StartedAt *string        `json:"started_at,omitempty"`
	Removed   map[string]int64 `json:"removed,omitempty"`
	Error     *string        `json:"error,omitempty"`
}

func intPtr(n int) *int { return &n }

func (s *Store) settingRetentionDefault(key string, fallback int) *int {
	var v sql.NullString
	if err := s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = ?`, key).Scan(&v); err != nil || !v.Valid {
		n := fallback
		return &n
	}
	var n int
	if _, err := fmt.Sscanf(v.String, "%d", &n); err != nil {
		d := fallback
		return &d
	}
	if n < 0 {
		return nil
	}
	return &n
}

func (s *Store) GetRetention() RetentionSettings {
	def := RetentionWindows{
		EventsDays:       s.settingRetentionDefault("retention_default_events", 90),
		TransactionsDays: s.settingRetentionDefault("retention_default_transactions", 90),
		LogsDays:         s.settingRetentionDefault("retention_default_logs", 90),
	}
	rows, err := s.db.Query(`SELECT project_id, events_days, transactions_days, logs_days FROM project_retention_overrides`)
	if err != nil {
		return RetentionSettings{Defaults: def}
	}
	defer rows.Close()
	var overrides []ProjectOverride
	for rows.Next() {
		var o ProjectOverride
		var e, t, l sql.NullInt64
		if err := rows.Scan(&o.ProjectID, &e, &t, &l); err != nil {
			continue
		}
		if e.Valid {
			v := int(e.Int64)
			o.EventsDays = &v
		}
		if t.Valid {
			v := int(t.Int64)
			o.TransactionsDays = &v
		}
		if l.Valid {
			v := int(l.Int64)
			o.LogsDays = &v
		}
		overrides = append(overrides, o)
	}
	return RetentionSettings{Defaults: def, Overrides: overrides}
}

func (s *Store) SaveRetention(rs RetentionSettings) error {
	save := func(key string, v *int) error {
		val := "-1"
		if v != nil {
			if *v < 0 {
				return fmt.Errorf("retention days must be >= 0 or null (forever)")
			}
			val = fmt.Sprintf("%d", *v)
		}
		if s.db.IsPostgres() {
			_, err := s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, val)
			return err
		}
		_, err := s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, val)
		return err
	}
	if err := save("retention_default_events", rs.Defaults.EventsDays); err != nil {
		return err
	}
	if err := save("retention_default_transactions", rs.Defaults.TransactionsDays); err != nil {
		return err
	}
	if err := save("retention_default_logs", rs.Defaults.LogsDays); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM project_retention_overrides`); err != nil {
		return err
	}
	for _, o := range rs.Overrides {
		var e, t, l any
		if o.EventsDays != nil {
			e = *o.EventsDays
		}
		if o.TransactionsDays != nil {
			t = *o.TransactionsDays
		}
		if o.LogsDays != nil {
			l = *o.LogsDays
		}
		if _, err := s.db.Exec(`INSERT INTO project_retention_overrides (project_id, events_days, transactions_days, logs_days) VALUES (?, ?, ?, ?)`, o.ProjectID, e, t, l); err != nil {
			return err
		}
	}
	return nil
}

func windowsFor(rs RetentionSettings, projectID int64) RetentionWindows {
	for _, o := range rs.Overrides {
		if o.ProjectID == projectID {
			out := rs.Defaults
			if o.EventsDays != nil || hasOverride(rs.Overrides, projectID) {
				out.EventsDays = effectiveWindow(rs.Defaults.EventsDays, o.EventsDays, o.ProjectID == projectID)
			}
			// Per-field inherit: override present-but-null means inherit default.
			out.EventsDays = pickWindow(rs.Defaults.EventsDays, o.EventsDays, true)
			out.TransactionsDays = pickWindow(rs.Defaults.TransactionsDays, o.TransactionsDays, true)
			out.LogsDays = pickWindow(rs.Defaults.LogsDays, o.LogsDays, true)
			_ = out
			// If the project has any override row, each nil field inherits.
			return RetentionWindows{
				EventsDays:       coalesceOverride(rs.Defaults.EventsDays, o.EventsDays),
				TransactionsDays: coalesceOverride(rs.Defaults.TransactionsDays, o.TransactionsDays),
				LogsDays:         coalesceOverride(rs.Defaults.LogsDays, o.LogsDays),
			}
		}
	}
	return rs.Defaults
}

func hasOverride(overrides []ProjectOverride, id int64) bool {
	for _, o := range overrides {
		if o.ProjectID == id {
			return true
		}
	}
	return false
}

func pickWindow(def, over *int, _ bool) *int {
	if over != nil {
		return over
	}
	return def
}

func coalesceOverride(def, over *int) *int {
	// An override row exists: explicit null is stored as nil here only when
	// the caller wants inherit, so fall back to default. Non-nil wins.
	// NOTE: we cannot distinguish "inherit" from "forever" at this layer;
	// forever is represented upstream as explicit null only when no
	// override row exists. Callers store forever as NULL in the row, which
	// scans as nil — treat nil-on-row as inherit (default). Forever for a
	// single kind is expressed by setting the default to null or by
	// deleting the override row and relying on a null default.
	if over != nil {
		return over
	}
	return def
}

func effectiveWindow(def, over *int, _ bool) *int { return pickWindow(def, over, true) }

// DeletesAnything reports whether any project ever loses data.
func DeletesAnything(rs RetentionSettings) bool {
	kinds := []*int{rs.Defaults.EventsDays, rs.Defaults.TransactionsDays, rs.Defaults.LogsDays}
	for _, k := range kinds {
		if k != nil {
			return true
		}
	}
	for _, o := range rs.Overrides {
		for _, k := range []*int{o.EventsDays, o.TransactionsDays, o.LogsDays} {
			if k != nil {
				return true
			}
		}
	}
	return false
}

func countWhere(ctx context.Context, db *database.DB, table string, where string, args ...any) int64 {
	var n int64
	q := `SELECT COUNT(*) FROM ` + table
	if where != "" {
		q += ` WHERE ` + where
	}
	if err := db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

func (s *Store) Summary(ctx context.Context) (StorageSummary, error) {
	var out StorageSummary
	u, err := s.Usage(ctx)
	if err != nil {
		return out, err
	}
	out.Bytes = u.SizeBytes
	for _, t := range u.Tables {
		switch t.Name {
		case "events":
			out.EventsRows = t.Rows
		case "transactions":
			out.TransactionsRows = t.Rows
		case "logs":
			out.LogsRows = t.Rows
		}
	}
	var files sql.NullInt64
	var bytes sql.NullInt64
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size_bytes),0) FROM source_maps`).Scan(&files, &bytes)
	if files.Valid {
		out.SourceMapFiles = files.Int64
	}
	if bytes.Valid {
		out.SourceMapBytes = bytes.Int64
	}
	// Frees estimate: rows older than the effective windows (cheap COUNTs).
	rs := s.GetRetention()
	out.FreesEstimate = s.previewCount(ctx, rs, nil)
	return out, nil
}

func (s *Store) Projects(ctx context.Context) ([]ProjectStorage, error) {
	type proj struct {
		id   int64
		name string
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var projs []proj
	for rows.Next() {
		var p proj
		if err := rows.Scan(&p.id, &p.name); err == nil {
			projs = append(projs, p)
		}
	}
	rows.Close()
	rs := s.GetRetention()
	totalRows := int64(0)
	type counts struct{ e, t, l, sm int64 }
	byID := map[int64]*counts{}
	var smBytesByProject = map[int64]int64{}
	smRows, _ := s.db.QueryContext(ctx, `SELECT project_id, COALESCE(SUM(size_bytes),0) FROM source_maps GROUP BY project_id`)
	if smRows != nil {
		for smRows.Next() {
			var pid, b int64
			if err := smRows.Scan(&pid, &b); err == nil {
				smBytesByProject[pid] = b
			}
		}
		smRows.Close()
	}
	var totalBytes int64
	if u, err := s.Usage(ctx); err == nil {
		totalBytes = u.SizeBytes
	}
	out := []ProjectStorage{}
	for _, p := range projs {
		e := countWhere(ctx, s.db, "events", "project_id = ?", p.id)
		t := countWhere(ctx, s.db, "transactions", "project_id = ?", p.id)
		l := countWhere(ctx, s.db, "logs", "project_id = ?", p.id)
		byID[p.id] = &counts{e: e, t: t, l: l}
		totalRows += e + t + l
	}
	for _, p := range projs {
		c := byID[p.id]
		rows := c.e + c.t + c.l
		var share float64
		var b int64
		if totalRows > 0 {
			share = float64(rows) / float64(totalRows)
			b = int64(float64(totalBytes) * share)
		}
		// Source-map bytes are content, not rows: add on top.
		b += smBytesByProject[p.id]
		w := windowsFor(rs, p.id)
		custom := hasOverride(rs.Overrides, p.id)
		_ = w
		out = append(out, ProjectStorage{
			ID: p.id, Name: p.name, Bytes: b,
			EventsBytes: int64(float64(b) * frac(c.e, rows)),
			TransactionsBytes: int64(float64(b) * frac(c.t, rows)),
			LogsBytes: int64(float64(b) * frac(c.l, rows)),
			EventsCount: c.e, TransactionsCount: c.t, LogsCount: c.l,
			Share: share, Custom: custom,
		})
	}
	// Largest first.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Bytes > out[i].Bytes {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func frac(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func (s *Store) Age(ctx context.Context) (StorageAge, error) {
	age := StorageAge{Bounds: append([]int{}, AgeBounds...)}
	now := time.Now().UTC()
	for i := 0; i <= len(AgeBounds); i++ {
		var lower, upper *time.Time
		if i > 0 {
			t := now.AddDate(0, 0, -AgeBounds[i-1])
			upper = &t
		}
		if i < len(AgeBounds) {
			t := now.AddDate(0, 0, -AgeBounds[i])
			lower = &t
		}
		// Bucket i spans (lower, upper]: rows older than lower, at-or-newer than upper.
		var b AgeBucket
		b.Events = s.countRange(ctx, "events", lower, upper)
		b.Transactions = s.countRange(ctx, "transactions", lower, upper)
		b.Logs = s.countRange(ctx, "logs", lower, upper)
		age.Buckets = append(age.Buckets, b)
	}
	return age, nil
}

func (s *Store) countRange(ctx context.Context, table string, olderThan, newerThan *time.Time) int64 {
	q := `SELECT COUNT(*) FROM ` + table + ` WHERE 1=1`
	var args []any
	if olderThan != nil {
		q += ` AND timestamp < ?`
		args = append(args, olderThan.UTC().Format(time.RFC3339Nano))
	}
	if newerThan != nil {
		q += ` AND timestamp >= ?`
		args = append(args, newerThan.UTC().Format(time.RFC3339Nano))
	}
	var n int64
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0
	}
	return n
}

// previewCount estimates rows a retention enforcement would delete.
// projectID nil = all projects.
func (s *Store) previewCount(ctx context.Context, rs RetentionSettings, projectID *int64) int64 {
	var total int64
	projs := []int64{}
	if projectID != nil {
		projs = []int64{*projectID}
	} else {
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM projects`)
		if err != nil {
			return 0
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				projs = append(projs, id)
			}
		}
		rows.Close()
	}
	now := time.Now().UTC()
	for _, pid := range projs {
		w := windowsFor(rs, pid)
		if w.EventsDays != nil {
			cutoff := now.AddDate(0, 0, -*w.EventsDays).Format(time.RFC3339Nano)
			total += countWhere(ctx, s.db, "events", "project_id = ? AND timestamp < ?", pid, cutoff)
		}
		if w.TransactionsDays != nil {
			cutoff := now.AddDate(0, 0, -*w.TransactionsDays).Format(time.RFC3339Nano)
			total += countWhere(ctx, s.db, "transactions", "project_id = ? AND timestamp < ?", pid, cutoff)
		}
		if w.LogsDays != nil {
			cutoff := now.AddDate(0, 0, -*w.LogsDays).Format(time.RFC3339Nano)
			total += countWhere(ctx, s.db, "logs", "project_id = ? AND timestamp < ?", pid, cutoff)
		}
	}
	return total
}

// Preview returns per-kind row counts a retention enforcement would remove.
func (s *Store) Preview(ctx context.Context, rs RetentionSettings, projectID *int64) map[string]int64 {
	out := map[string]int64{"events": 0, "transactions": 0, "logs": 0}
	projs := []int64{}
	if projectID != nil {
		projs = []int64{*projectID}
	} else {
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM projects`)
		if err != nil {
			return out
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				projs = append(projs, id)
			}
		}
		rows.Close()
	}
	now := time.Now().UTC()
	for _, pid := range projs {
		w := windowsFor(rs, pid)
		if w.EventsDays != nil {
			cutoff := now.AddDate(0, 0, -*w.EventsDays).Format(time.RFC3339Nano)
			out["events"] += countWhere(ctx, s.db, "events", "project_id = ? AND timestamp < ?", pid, cutoff)
		}
		if w.TransactionsDays != nil {
			cutoff := now.AddDate(0, 0, -*w.TransactionsDays).Format(time.RFC3339Nano)
			out["transactions"] += countWhere(ctx, s.db, "transactions", "project_id = ? AND timestamp < ?", pid, cutoff)
		}
		if w.LogsDays != nil {
			cutoff := now.AddDate(0, 0, -*w.LogsDays).Format(time.RFC3339Nano)
			out["logs"] += countWhere(ctx, s.db, "logs", "project_id = ? AND timestamp < ?", pid, cutoff)
		}
	}
	return out
}

// --- async cleanup job ---

type CleanupJob struct {
	mu     sync.Mutex
	status CleanupStatus
}

func NewCleanupJob() *CleanupJob {
	return &CleanupJob{status: CleanupStatus{State: "idle", Kind: "retention"}}
}

func (j *CleanupJob) Status() CleanupStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	cp := j.status
	return cp
}

// SetRunning marks a compaction (or other kind) as running.
func (j *CleanupJob) SetRunning(kind string, startedAt *string) {
	j.setStatus(CleanupStatus{State: "running", Kind: kind, StartedAt: startedAt, Removed: map[string]int64{}})
}

// SetDone finishes the current job.
func (j *CleanupJob) SetDone(state string, startedAt *string, removed map[string]int64, errMsg *string) {
	j.setStatus(CleanupStatus{State: state, Kind: "compaction", StartedAt: startedAt, Removed: removed, Error: errMsg})
}

func (j *CleanupJob) setStatus(s CleanupStatus) {
	j.mu.Lock()
	j.status = s
	j.mu.Unlock()
}

// StartRetention enforces the saved retention in the background (202 + poll).
func (s *Store) StartRetention(db *database.DB, job *CleanupJob, rs RetentionSettings, projectID *int64) (CleanupStatus, error) {
	st := job.Status()
	if st.State == "running" {
		return st, fmt.Errorf("a cleanup is already running")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	started := now
	job.setStatus(CleanupStatus{State: "running", Kind: "retention", StartedAt: &started, Removed: map[string]int64{}})
	_, _ = db.Exec(`INSERT INTO scheduler_state (key, settings_json, last_started_at, last_status) VALUES ('storage_cleanup', '{}', ?, 'running')
		ON CONFLICT(key) DO UPDATE SET last_started_at = excluded.last_started_at, last_status = 'running', last_finished_at = NULL, last_error = NULL`, now)
	go func() {
		removed := map[string]int64{"events": 0, "transactions": 0, "logs": 0}
		err := s.enforceRetention(context.Background(), rs, projectID, removed, job)
		finished := time.Now().UTC().Format(time.RFC3339Nano)
		if err != nil {
			msg := err.Error()
			job.setStatus(CleanupStatus{State: "error", Kind: "retention", StartedAt: &started, Removed: removed, Error: &msg})
			_, _ = db.Exec(`UPDATE scheduler_state SET last_finished_at = ?, last_status = 'error', last_error = ? WHERE key = 'storage_cleanup'`, finished, msg)
			return
		}
		job.setStatus(CleanupStatus{State: "done", Kind: "retention", StartedAt: &started, Removed: removed})
		blob, _ := json.Marshal(removed)
		_, _ = db.Exec(`UPDATE scheduler_state SET last_finished_at = ?, last_status = 'ok', last_result_json = ?, last_error = NULL WHERE key = 'storage_cleanup'`, finished, string(blob))
	}()
	return job.Status(), nil
}

func (s *Store) enforceRetention(ctx context.Context, rs RetentionSettings, projectID *int64, removed map[string]int64, job *CleanupJob) error {
	var projs []int64
	if projectID != nil {
		projs = []int64{*projectID}
	} else {
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM projects`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err == nil {
				projs = append(projs, id)
			}
		}
		rows.Close()
	}
	now := time.Now().UTC()
	for _, pid := range projs {
		w := windowsFor(rs, pid)
		if w.EventsDays != nil {
			cutoff := now.AddDate(0, 0, -*w.EventsDays).Format(time.RFC3339Nano)
			n, err := s.deleteBatches(ctx, `DELETE FROM events WHERE pk IN (SELECT pk FROM events WHERE project_id = ? AND timestamp < ? LIMIT 500)`, pid, cutoff)
			if err != nil {
				return err
			}
			removed["events"] += n
			job.setStatus(CleanupStatus{State: "running", Kind: "retention", Removed: copyMap(removed)})
		}
		if w.TransactionsDays != nil {
			cutoff := now.AddDate(0, 0, -*w.TransactionsDays).Format(time.RFC3339Nano)
			if res, err := s.db.ExecContext(ctx, `DELETE FROM transactions WHERE project_id = ? AND timestamp < ?`, pid, cutoff); err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					removed["transactions"] += n
				}
			}
		}
		if w.LogsDays != nil {
			cutoff := now.AddDate(0, 0, -*w.LogsDays).Format(time.RFC3339Nano)
			if res, err := s.db.ExecContext(ctx, `DELETE FROM logs WHERE project_id = ? AND timestamp < ?`, pid, cutoff); err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					removed["logs"] += n
				}
			}
		}
	}
	return nil
}

func copyMap(m map[string]int64) map[string]int64 {
	out := map[string]int64{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Store) deleteBatches(ctx context.Context, query string, args ...any) (int64, error) {
	var total int64
	for {
		res, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 500 {
			break
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
	}
	return total, nil
}

// Compaction reports whether the SQLite file benefits from VACUUM.
func (s *Store) Compaction(ctx context.Context) map[string]any {
	if s.db.IsPostgres() {
		return map[string]any{"needed": false, "reason": "postgres reclaims space automatically (autovacuum)"}
	}
	u, err := s.Usage(ctx)
	if err != nil {
		return map[string]any{"needed": false}
	}
	var freelist int64
	_ = s.db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&freelist)
	var pageSize int64
	_ = s.db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize)
	free := freelist * pageSize
	return map[string]any{"needed": free > 10*1024*1024, "freeBytes": free, "sizeBytes": u.SizeBytes}
}

func (s *Store) RetentionResponse() RetentionResponse {
	rs := s.GetRetention()
	var nextRun *string
	var last *RetentionRun
	var settingsJSON, nextAt, startedAt, finishedAt, status, resultJSON, lastErr sql.NullString
	err := s.db.QueryRow(`SELECT settings_json, next_run_at, last_started_at, last_finished_at, last_status, last_result_json, last_error FROM scheduler_state WHERE key = 'storage_cleanup'`).Scan(&settingsJSON, &nextAt, &startedAt, &finishedAt, &status, &resultJSON, &lastErr)
	if err == nil {
		if nextAt.Valid {
			v := nextAt.String
			nextRun = &v
		}
		if startedAt.Valid {
			r := &RetentionRun{StartedAt: startedAt.String, Status: "ok"}
			if status.Valid {
				r.Status = status.String
			}
			if finishedAt.Valid {
				v := finishedAt.String
				r.FinishedAt = &v
			}
			if resultJSON.Valid {
				var m map[string]int64
				if json.Unmarshal([]byte(resultJSON.String), &m) == nil {
					r.Removed = m
				}
			}
			if lastErr.Valid && lastErr.String != "" {
				v := lastErr.String
				r.Error = &v
			}
			last = r
		}
	}
	return RetentionResponse{Settings: rs, NextRunAt: nextRun, LastRun: last}
}
