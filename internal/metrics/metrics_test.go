package metrics

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestExpositionFormat(t *testing.T) {
	r := NewRegistry()
	r.Counter("events_total", "Events stored.")
	r.CounterVec("events_rejected_total", "Rejected.", []string{"reason"})
	r.Inc("events_total")
	r.Inc("events_rejected_total", "auth")
	r.Inc("events_rejected_total", "auth")
	r.GaugeFunc("queue_depth", "Pending.", func() float64 { return 7 })
	// Unknown metrics and arity mismatches must not panic request paths.
	r.Inc("nope")
	r.Inc("events_rejected_total")

	rec := httptest.NewRecorder()
	r.Handler()(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		"# HELP events_total Events stored.",
		"# TYPE events_total counter",
		"events_total 1",
		"# HELP events_rejected_total Rejected.",
		`events_rejected_total{reason="auth"} 2`,
		"# TYPE queue_depth gauge",
		"queue_depth 7",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestConcurrentInc(t *testing.T) {
	r := NewRegistry()
	r.CounterVec("hits", "Hits.", []string{"route"})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.Inc("hits", "/x")
			}
		}()
	}
	wg.Wait()
	rec := httptest.NewRecorder()
	r.Handler()(rec, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(rec.Body.String(), `hits{route="/x"} 5000`) {
		t.Fatalf("lost increments:\n%s", rec.Body.String())
	}
}
