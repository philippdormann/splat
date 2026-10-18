package notifications

import (
	"context"
	"time"
)

// RunWorker drains the notification queue on a fixed interval until ctx is
// cancelled. One goroutine, one ticker - no worker pool needed at the
// volume a single-VPS deployment sees.
func (s *Store) RunWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.DrainOnce(ctx, 20)
		}
	}
}
