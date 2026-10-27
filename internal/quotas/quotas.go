// Package quotas implements per-project ingestion quotas (spike
// protection). Usage is counted from the events table over trailing
// hour/day windows (served by idx_events_project_time); only stored
// error events count, so logs, sessions and transactions never trip a
// quota. Limits resolve per project with NULL inheriting the instance
// defaults; 0 disables a window. Breach alerts are throttled to one per
// hour per project via breach_notified_at.
package quotas

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/philippdormann/splat/internal/database"
)

// NotifyFunc alerts about a breach. window is "hourly" or "daily".
// Implementations must be best-effort and never fail ingestion.
type NotifyFunc func(ctx context.Context, projectID int64, projectName, projectURL, window string, used, limit int)

type Store struct {
	db     *database.DB
	Notify NotifyFunc
}

func NewStore(db *database.DB) *Store { return &Store{db: db} }

// Decision is the outcome of a quota check.
type Decision struct {
	Allowed    bool
	Window     string // "hourly"/"daily" when breached
	Used       int
	Limit      int
	RetryAfter int // seconds for the Retry-After header when breached
}

// BreachCooldown bounds breach alerts while continuously over quota.
const BreachCooldown = time.Hour

var errNegative = errors.New("quota limits must be >= 0 (0 = unlimited)")

func settingInt(s *Store, key string, fallback int) int {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = ?`, key).Scan(&v); err != nil {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func (s *Store) setSetting(key string, val int) error {
	v := strconv.Itoa(val)
	if s.db.IsPostgres() {
		_, err := s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, v)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, v)
	return err
}

// Defaults returns the instance-wide caps (0 = unlimited).
func (s *Store) Defaults() (hourly, daily int) {
	return settingInt(s, "quota_events_hourly", 5000), settingInt(s, "quota_events_daily", 50000)
}

// SetDefaults stores the instance-wide caps (must be >= 0).
func (s *Store) SetDefaults(hourly, daily int) error {
	if hourly < 0 || daily < 0 {
		return errNegative
	}
	if err := s.setSetting("quota_events_hourly", hourly); err != nil {
		return err
	}
	return s.setSetting("quota_events_daily", daily)
}

// Overrides returns the per-project limits; nil means "inherit".
func (s *Store) Overrides(projectID int64) (hourly, daily *int, err error) {
	var h, d sql.NullInt64
	if err := s.db.QueryRow(`SELECT events_hourly, events_daily FROM project_quotas WHERE project_id = ?`, projectID).Scan(&h, &d); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if h.Valid {
		v := int(h.Int64)
		hourly = &v
	}
	if d.Valid {
		v := int(d.Int64)
		daily = &v
	}
	return hourly, daily, nil
}

// SetOverrides stores per-project limits; nil inherits the default.
// Values must be >= 0 (0 = unlimited for that window).
func (s *Store) SetOverrides(projectID int64, hourly, daily *int) error {
	var h, d any
	if hourly != nil {
		if *hourly < 0 {
			return errNegative
		}
		h = *hourly
	}
	if daily != nil {
		if *daily < 0 {
			return errNegative
		}
		d = *daily
	}
	if s.db.IsPostgres() {
		_, err := s.db.Exec(`INSERT INTO project_quotas (project_id, events_hourly, events_daily)
			VALUES (?, ?, ?)
			ON CONFLICT (project_id) DO UPDATE SET events_hourly = EXCLUDED.events_hourly, events_daily = EXCLUDED.events_daily`,
			projectID, h, d)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO project_quotas (project_id, events_hourly, events_daily)
		VALUES (?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET events_hourly = excluded.events_hourly, events_daily = excluded.events_daily`,
		projectID, h, d)
	return err
}

// Effective resolves the caps that actually apply (0 = unlimited).
func (s *Store) Effective(projectID int64) (hourly, daily int, err error) {
	defH, defD := s.Defaults()
	h, d, err := s.Overrides(projectID)
	if err != nil {
		return 0, 0, err
	}
	hourly, daily = defH, defD
	if h != nil {
		hourly = *h
	}
	if d != nil {
		daily = *d
	}
	return hourly, daily, nil
}

// Usage counts stored error events in the trailing hour and day.
// SDK timestamps are trusted here (served by the project/time index);
// quota is anti-accident, not anti-adversary. Cutoffs use RFC3339Nano
// to match the stored format (see retention).
func (s *Store) Usage(projectID int64) (hour, day int, err error) {
	now := time.Now().UTC()
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE project_id = ? AND timestamp >= ?`,
		projectID, now.Add(-time.Hour).Format(time.RFC3339Nano)).Scan(&hour); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE project_id = ? AND timestamp >= ?`,
		projectID, now.Add(-24*time.Hour).Format(time.RFC3339Nano)).Scan(&day); err != nil {
		return 0, 0, err
	}
	return hour, day, nil
}

// Check enforces the caps: over-quota projects are rejected. On a fresh
// breach (nothing notified within BreachCooldown) it fires Notify once
// and stamps breach_notified_at; recovery clears the stamp so the next
// breach alerts again. Store errors fail open: quota must never drop
// events by accident.
func (s *Store) Check(ctx context.Context, projectID int64, projectName, projectURL string) Decision {
	hourly, daily, err := s.Effective(projectID)
	if err != nil {
		return Decision{Allowed: true}
	}
	usedH, usedD, err := s.Usage(projectID)
	if err != nil {
		return Decision{Allowed: true}
	}
	if hourly > 0 && usedH >= hourly {
		s.onBreach(ctx, projectID, projectName, projectURL, "hourly", usedH, hourly)
		return Decision{Allowed: false, Window: "hourly", Used: usedH, Limit: hourly, RetryAfter: 300}
	}
	if daily > 0 && usedD >= daily {
		s.onBreach(ctx, projectID, projectName, projectURL, "daily", usedD, daily)
		return Decision{Allowed: false, Window: "daily", Used: usedD, Limit: daily, RetryAfter: 3600}
	}
	// Under quota: clear the breach stamp (recovery).
	_, _ = s.db.Exec(`UPDATE project_quotas SET breach_notified_at = NULL WHERE project_id = ?`, projectID)
	return Decision{Allowed: true, Used: usedH, Limit: hourly}
}

func (s *Store) onBreach(ctx context.Context, projectID int64, projectName, projectURL, window string, used, limit int) {
	if s.Notify == nil {
		return
	}
	var notifiedAt sql.NullString
	_ = s.db.QueryRow(`SELECT breach_notified_at FROM project_quotas WHERE project_id = ?`, projectID).Scan(&notifiedAt)
	if notifiedAt.Valid {
		if t, err := time.Parse(time.RFC3339, notifiedAt.String); err == nil && time.Since(t) < BreachCooldown {
			return
		}
	}
	// Ensure the row exists for the stamp (limits may be pure defaults).
	h, d, _ := s.Overrides(projectID)
	_ = s.SetOverrides(projectID, h, d)
	_, _ = s.db.Exec(`UPDATE project_quotas SET breach_notified_at = ? WHERE project_id = ?`,
		time.Now().UTC().Format(time.RFC3339), projectID)
	s.Notify(ctx, projectID, projectName, projectURL, window, used, limit)
}
