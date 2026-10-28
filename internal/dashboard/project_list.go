package dashboard

import (
	"fmt"
	"strings"
	"time"
)

// Batched project-list stats (Rustrak-style list_stats): two queries per
// page, zero-filled per id, current-vs-previous deltas anchored to now.

const ListTrendBuckets = 12

type MetricDelta struct {
	Current  int64  `json:"current"`
	Previous *int64 `json:"previous,omitempty"`
}

type ProjectListStats struct {
	Trend     []int64     `json:"trend"`
	Events    MetricDelta `json:"events"`
	NewIssues MetricDelta `json:"new_issues"`
	OpenIssues int64      `json:"open_issues"`
	FatalIssues int64     `json:"fatal_issues"`
	TotalEvents int64     `json:"total_events"`
}

func zeroStats() ProjectListStats {
	prev := int64(0)
	return ProjectListStats{
		Trend:     make([]int64, ListTrendBuckets),
		Events:    MetricDelta{Previous: &prev},
		NewIssues: MetricDelta{Previous: &prev},
	}
}

// ProjectListStatsBatch aggregates a page of projects in bulk. periodHours
// is the "current" window (24 = events/24h); the comparison window is the
// same length immediately before. The trend is 12 buckets over the current
// window counting events (issue-distinct would cost a join per row).
func (s *Store) ProjectListStats(ids []int64, periodHours int) (map[int64]ProjectListStats, error) {
	out := map[int64]ProjectListStats{}
	for _, id := range ids {
		out[id] = zeroStats()
	}
	if len(ids) == 0 {
		return out, nil
	}
	if periodHours <= 0 {
		periodHours = 24
	}
	if periodHours > 24*30 {
		periodHours = 24 * 30
	}
	bucketSecs := int64(periodHours * 3600 / ListTrendBuckets)
	if bucketSecs < 1 {
		bucketSecs = 1
	}
	now := time.Now().UTC()
	first := now.Add(-time.Duration(2*ListTrendBuckets) * time.Duration(bucketSecs) * time.Second)
	currentStart := now.Add(-time.Duration(ListTrendBuckets) * time.Duration(bucketSecs) * time.Second)

	idSet := map[int64]bool{}
	for _, id := range ids {
		idSet[id] = true
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}

	// Query 1: event timestamps in the full 2-window span. Bucket in Go
	// (timestamps are RFC3339 text in both dialects).
	rows, err := s.db.Query(`SELECT project_id, timestamp FROM events WHERE project_id IN (`+placeholders+`) AND timestamp >= ?`, append(args, fmtTime(first))...)
	if err == nil {
		for rows.Next() {
			var pid int64
			var ts string
			if err := rows.Scan(&pid, &ts); err != nil {
				continue
			}
			if !idSet[pid] {
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, ts)
			if err != nil {
				if t, err = time.Parse(time.RFC3339, ts); err != nil {
					continue
				}
			}
			idx := int(t.Sub(first).Seconds()) / int(bucketSecs)
			if idx < 0 {
				continue
			}
			st := out[pid]
			if idx < ListTrendBuckets {
				if st.Events.Previous != nil {
					*st.Events.Previous++
				}
			} else {
				st.Events.Current++
				slot := idx - ListTrendBuckets
				if slot >= len(st.Trend) {
					slot = len(st.Trend) - 1
				}
				st.Trend[slot]++
			}
			out[pid] = st
		}
		rows.Close()
	}

	// Query 2: totals per project.
	{
		totArgs := make([]any, len(args))
		copy(totArgs, args)
		trows, err := s.db.Query(`SELECT project_id, COUNT(*) FROM events WHERE project_id IN (`+placeholders+`) GROUP BY project_id`, totArgs...)
		if err == nil {
			for trows.Next() {
				var pid, n int64
				if err := trows.Scan(&pid, &n); err == nil {
					if st, ok := out[pid]; ok {
						st.TotalEvents = n
						out[pid] = st
					}
				}
			}
			trows.Close()
		}
	}

	// Query 3: open + fatal issue counts.
	{
		iargs := make([]any, len(args))
		copy(iargs, args)
		irows, err := s.db.Query(`SELECT project_id, COUNT(*), COALESCE(SUM(CASE WHEN level = 'fatal' THEN 1 ELSE 0 END),0) FROM issues WHERE project_id IN (`+placeholders+`) AND status = 'unresolved' GROUP BY project_id`, iargs...)
		if err == nil {
			for irows.Next() {
				var pid, open, fatal int64
				if err := irows.Scan(&pid, &open, &fatal); err == nil {
					if st, ok := out[pid]; ok {
						st.OpenIssues = open
						st.FatalIssues = fatal
						out[pid] = st
					}
				}
			}
			irows.Close()
		}
	}

	// Query 4: new issues current vs previous window.
	for _, pid := range ids {
		var cur, prev int64
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM issues WHERE project_id = ? AND first_seen >= ?`, pid, fmtTime(currentStart)).Scan(&cur)
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM issues WHERE project_id = ? AND first_seen >= ? AND first_seen < ?`, pid, fmtTime(first), fmtTime(currentStart)).Scan(&prev)
		st := out[pid]
		st.NewIssues.Current = cur
		p := prev
		st.NewIssues.Previous = &p
		out[pid] = st
	}

	_ = fmt.Sprintf
	return out, nil
}
