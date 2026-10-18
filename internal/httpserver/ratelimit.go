package httpserver

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipRateLimiter gives each client IP its own token bucket so one noisy
// Django process can't starve ingestion for the others. Idle entries are
// swept periodically so the map can't grow without bound.
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*entry
	rps      rate.Limit
	burst    int
}

type entry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newIPRateLimiter(rps float64, burst int) *ipRateLimiter {
	l := &ipRateLimiter{limiters: map[string]*entry{}, rps: rate.Limit(rps), burst: burst}
	go l.sweep()
	return l
}

func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	e, ok := l.limiters[ip]
	if !ok {
		e = &entry{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.limiters[ip] = e
	}
	e.lastSeen = time.Now()
	l.mu.Unlock()
	return e.limiter.Allow()
}

func (l *ipRateLimiter) sweep() {
	for {
		time.Sleep(10 * time.Minute)
		cutoff := time.Now().Add(-10 * time.Minute)
		l.mu.Lock()
		for ip, e := range l.limiters {
			if e.lastSeen.Before(cutoff) {
				delete(l.limiters, ip)
			}
		}
		l.mu.Unlock()
	}
}

func rateLimit(l *ipRateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !l.allow(ip) {
			http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
