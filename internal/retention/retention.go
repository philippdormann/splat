// Package retention deletes old individual events in small batches so
// ingestion is never blocked by a long-running cleanup transaction. Issue
// summaries (title, counts, first/last seen) live in a separate table and
// are never touched here - only the raw events backing them age out.
package retention

import (
	"context"
	"errgotrack/internal/database"
	"log/slog"
	"time"
)

const batchSize = 500

type Sweeper struct {
	db     *database.DB
	logger *slog.Logger
}

func NewSweeper(db *database.DB, logger *slog.Logger) *Sweeper {
	return &Sweeper{db: db, logger: logger}
}

func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.sweepOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

func (s *Sweeper) sweepOnce(ctx context.Context) {
	deleted, _, err := s.SweepNow(ctx)
	if err != nil {
		s.logger.Error("retention sweep", "error", err)
		return
	}
	if deleted > 0 {
		s.logger.Info("retention sweep deleted events", "count", deleted)
	}
}

// SweepNow deletes expired events once across all projects and reports how
// many rows went away per project. It is the same bounded-batch loop the
// background worker runs, exposed so the storage API can offer a "clean up
// now" button with honest feedback.
func (s *Sweeper) SweepNow(ctx context.Context) (deleted int64, byProject map[int64]int64, err error) {
	byProject = map[int64]int64{}
	var firstErr error
	rows, err := s.db.QueryContext(ctx, `SELECT id, retention_days FROM projects WHERE retention_days > 0`)
	if err != nil {
		s.logger.Error("retention: list projects", "error", err)
		return 0, byProject, err
	}
	type project struct {
		id   int64
		days int
	}
	var projects []project
	for rows.Next() {
		var p project
		if err := rows.Scan(&p.id, &p.days); err == nil {
			projects = append(projects, p)
		}
	}
	rows.Close()

	for _, p := range projects {
		cutoff := time.Now().AddDate(0, 0, -p.days).UTC().Format(time.RFC3339Nano)
		for {
			res, err := s.db.ExecContext(ctx, `
				DELETE FROM events WHERE pk IN (
					SELECT pk FROM events WHERE project_id = ? AND timestamp < ? LIMIT ?
				)`, p.id, cutoff, batchSize)
			if err != nil {
				// One project's failure must not abort the whole sweep;
				// remember it, log it, move on to the next project.
				s.logger.Error("retention: delete batch", "error", err, "project_id", p.id)
				if firstErr == nil {
					firstErr = err
				}
				break
			}
			n, _ := res.RowsAffected()
			deleted += n
			byProject[p.id] += n
			if n < batchSize {
				break
			}
			select {
			case <-ctx.Done():
				return deleted, byProject, ctx.Err()
			default:
			}
		}
		// Logs and traces honor the same per-project window: they are
		// telemetry, not history, and would otherwise grow unbounded while
		// events age out. Spans cascade from their transaction's FK.
		// Failures here are logged but never fail the sweep: events are
		// the load-bearing table.
		if res, err := s.db.ExecContext(ctx,
			`DELETE FROM logs WHERE project_id = ? AND timestamp < ?`, p.id, cutoff); err != nil {
			s.logger.Error("retention: delete logs", "error", err, "project_id", p.id)
		} else if n, _ := res.RowsAffected(); n > 0 {
			deleted += n
			byProject[p.id] += n
		}
		if res, err := s.db.ExecContext(ctx,
			`DELETE FROM transactions WHERE project_id = ? AND timestamp < ?`, p.id, cutoff); err != nil {
			s.logger.Error("retention: delete transactions", "error", err, "project_id", p.id)
		} else if n, _ := res.RowsAffected(); n > 0 {
			deleted += n
			byProject[p.id] += n
		}
	}
	return deleted, byProject, firstErr
}
