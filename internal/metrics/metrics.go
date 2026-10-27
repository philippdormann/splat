// Package metrics exposes a minimal Prometheus text endpoint with zero
// dependencies. Only counters, label vectors with fixed label names, and
// pull-time gauges are supported - deliberately not a client library.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

type counter struct {
	help   string
	labels []string // fixed label names, in order
	vals   map[string]float64
}

type gauge struct {
	help string
	fn   func() float64
}

type Registry struct {
	mu       sync.Mutex
	counters map[string]*counter
	gauges   map[string]*gauge
	order    []string // registration order for stable output
}

func NewRegistry() *Registry {
	return &Registry{counters: map[string]*counter{}, gauges: map[string]*gauge{}}
}

func (r *Registry) Counter(name, help string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.counters[name]; !ok {
		r.counters[name] = &counter{help: help, vals: map[string]float64{}}
		r.order = append(r.order, "c:"+name)
	}
}

func (r *Registry) CounterVec(name, help string, labels []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.counters[name]; !ok {
		r.counters[name] = &counter{help: help, labels: append([]string{}, labels...), vals: map[string]float64{}}
		r.order = append(r.order, "c:"+name)
	}
}

func encodeLabels(names, values []string) string {
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `%s=%q`, n, values[i])
	}
	return b.String()
}

// Inc adds 1 to the counter (or label combination). Unknown metrics or
// arity mismatches are ignored rather than panicking a request path.
func (r *Registry) Inc(name string, values ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.counters[name]
	if !ok || len(values) != len(c.labels) {
		return
	}
	c.vals[encodeLabels(c.labels, values)]++
}

// GaugeFunc registers a value sampled at scrape time (e.g. DB counts).
func (r *Registry) GaugeFunc(name, help string, fn func() float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.gauges[name]; !ok {
		r.gauges[name] = &gauge{help: help, fn: fn}
		r.order = append(r.order, "g:"+name)
	}
}

func esc(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// Handler renders the Prometheus text exposition format. Snapshots are
// copied under lock; gauge callbacks (which hit the database) run outside
// it and must never block metric increments.
func (r *Registry) Handler() http.HandlerFunc {
	type snapCounter struct {
		help string
		vals map[string]float64
	}
	type snapGauge struct {
		help string
		fn   func() float64
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		r.mu.Lock()
		order := append([]string{}, r.order...)
		counters := make(map[string]snapCounter, len(r.counters))
		for name, c := range r.counters {
			vals := make(map[string]float64, len(c.vals))
			for k, v := range c.vals {
				vals[k] = v
			}
			counters[name] = snapCounter{help: c.help, vals: vals}
		}
		gauges := make(map[string]snapGauge, len(r.gauges))
		for name, g := range r.gauges {
			gauges[name] = snapGauge{help: g.help, fn: g.fn}
		}
		r.mu.Unlock()

		var b strings.Builder
		for _, key := range order {
			kind, name := key[:2], key[2:]
			if kind == "c:" {
				c, ok := counters[name]
				if !ok {
					continue
				}
				fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n", name, esc(c.help), name)
				labels := make([]string, 0, len(c.vals))
				for l := range c.vals {
					labels = append(labels, l)
				}
				sort.Strings(labels)
				for _, l := range labels {
					if l == "" {
						fmt.Fprintf(&b, "%s %v\n", name, c.vals[l])
					} else {
						fmt.Fprintf(&b, "%s{%s} %v\n", name, l, c.vals[l])
					}
				}
			} else {
				g, ok := gauges[name]
				if !ok {
					continue
				}
				fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %v\n", name, esc(g.help), name, name, g.fn())
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(b.String()))
	}
}
