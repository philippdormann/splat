// Shared view helpers for the JSON API: range selection, pagination size,
// stack-frame/context shaping, settings masking. These are the pure,
// template-independent pieces of the old server-rendered UI, kept because
// the Svelte frontend needs the same shaping done server-side.
package httpserver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/sentryprotocol"
)

// --- dashboard ranges ---

type rangeOption struct {
	Value string
	Label string
}

var dashboardRanges = []rangeOption{
	{"1h", "Last hour"},
	{"24h", "Last 24 hours"},
	{"7d", "Last 7 days"},
	{"30d", "Last 30 days"},
}

func rangeToSince(value string) (since, previousSince time.Time) {
	now := time.Now()
	var d time.Duration
	switch value {
	case "1h":
		d = time.Hour
	case "7d":
		d = 7 * 24 * time.Hour
	case "30d":
		d = 30 * 24 * time.Hour
	default:
		d = 24 * time.Hour
	}
	return now.Add(-d), now.Add(-2 * d)
}

// --- issues ---

const issuesPerPage = 50

// projectNameCache gives the issue list a cheap id->name lookup without a
// join for every row - the project count is small (a handful per VPS
// deployment), so one List() call per request is plenty.
func (s *Server) projectNameCache() map[int64]string {
	out := map[int64]string{}
	list, err := s.Projects.List()
	if err != nil {
		return out
	}
	for _, p := range list {
		out[p.ID] = p.Name
	}
	return out
}

// --- stack frames ---

// frameView precomputes a stack frame's source context as numbered lines
// (the SDK sends pre_context/post_context as separate arrays with no line
// numbers attached - this stitches them into one ordered, numbered list)
// plus its captured local variables, when the SDK included them.
type frameView struct {
	Filename string
	Function string
	LineNo   int
	ColNo    int
	InApp    bool
	Lines    []frameLine
	Vars     map[string]string
}

type frameLine struct {
	Number  int
	Text    string
	Current bool
}

// displayVars renders each captured local variable for display regardless
// of whether the SDK sent it as a plain string or as structured JSON (a
// dict/list-valued local serializes as nested JSON, not a repr() string -
// see the Frame.Vars doc comment).
func displayVars(vars map[string]json.RawMessage) map[string]string {
	if vars == nil {
		return nil
	}
	out := make(map[string]string, len(vars))
	for k, raw := range vars {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			out[k] = s
		} else {
			out[k] = string(raw)
		}
	}
	return out
}

func buildFrameViews(frames []sentryprotocol.Frame) []frameView {
	out := make([]frameView, 0, len(frames))
	for _, f := range frames {
		fv := frameView{Filename: f.Filename, Function: f.Function, LineNo: f.LineNo, ColNo: f.ColNo, InApp: f.InApp, Vars: displayVars(f.Vars)}
		start := f.LineNo - len(f.PreContext)
		for i, text := range f.PreContext {
			fv.Lines = append(fv.Lines, frameLine{Number: start + i, Text: text})
		}
		if f.ContextLine != "" {
			fv.Lines = append(fv.Lines, frameLine{Number: f.LineNo, Text: f.ContextLine, Current: true})
		}
		for i, text := range f.PostContext {
			fv.Lines = append(fv.Lines, frameLine{Number: f.LineNo + 1 + i, Text: text})
		}
		out = append(out, fv)
	}
	return out
}

// --- contexts / generic key-values ---

// contextView is one named entry from Sentry's free-form "contexts" object
// (browser, os, device, runtime, trace, culture, or an SDK/framework-specific
// one) flattened to string fields for display. The "type" key, when present,
// just restates the context's own name, so it's dropped as redundant.
type contextView struct {
	Name   string
	Fields []keyValue
}

type keyValue struct {
	Key   string
	Value string
}

func parseContexts(raw string) []contextView {
	var contexts map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &contexts); err != nil {
		return nil
	}
	names := make([]string, 0, len(contexts))
	for name := range contexts {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]contextView, 0, len(names))
	for _, name := range names {
		fields := contexts[name]
		keys := make([]string, 0, len(fields))
		for k := range fields {
			if k == "type" {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		cv := contextView{Name: name}
		for _, k := range keys {
			cv.Fields = append(cv.Fields, keyValue{Key: k, Value: fmt.Sprintf("%v", fields[k])})
		}
		out = append(out, cv)
	}
	return out
}

// traceIDFromContexts finds the trace_id in the event's "trace" context, if
// the SDK captured distributed tracing info.
func traceIDFromContexts(contexts []contextView) string {
	for _, c := range contexts {
		if c.Name != "trace" {
			continue
		}
		for _, f := range c.Fields {
			if f.Key == "trace_id" {
				return f.Value
			}
		}
	}
	return ""
}

func flattenToStrings(raw json.RawMessage) map[string]string {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	out := make(map[string]string, len(data))
	for k, v := range data {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

// --- projects ---

// isUniqueConstraintErr reports whether err came from violating a UNIQUE
// constraint (e.g. a duplicate slug) on either backend: SQLite reports
// "UNIQUE constraint failed", Postgres reports 'duplicate key value
// violates unique constraint "..."'. Anything else (lock contention, disk
// error, ...) deserves its own message instead of being misreported as
// "slug already taken".
func isUniqueConstraintErr(err error) bool {
	return strings.Contains(strings.ToUpper(err.Error()), "UNIQUE CONSTRAINT")
}

// --- settings ---

var webhookChannels = []string{"slack", "discord", "teams", "sheets"}

func isWebhookChannel(channel string) bool {
	for _, c := range webhookChannels {
		if c == channel {
			return true
		}
	}
	return false
}

func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return strings.Repeat("•", len(token))
	}
	return token[:4] + strings.Repeat("•", len(token)-8) + token[len(token)-4:]
}
