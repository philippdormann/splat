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
	rows, err := s.db.QueryContext(ctx, `SELECT id, retention_days FROM projects WHERE retention_days > 0`)
	if err != nil {
		s.logger.Error("retention: list projects", "error", err)
		return
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
				s.logger.Error("retention: delete batch", "error", err, "project_id", p.id)
				break
			}
			n, _ := res.RowsAffected()
			if n < batchSize {
				break
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}
}
