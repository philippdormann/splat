package httpserver

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/logs"
	"github.com/philippdormann/splat/internal/traces"
)

// --- logs & traces (project-scoped reads) ---

func logJSON(e logs.Entry) map[string]any {
	var attrs any
	if e.Attributes == "" || json.Unmarshal([]byte(e.Attributes), &attrs) != nil {
		attrs = map[string]any{}
	}
	return map[string]any{
		"id": e.ID, "timestamp": e.Timestamp.UTC().Format(time.RFC3339Nano),
		"level": e.Level, "message": e.Message,
		"traceId": e.TraceID, "spanId": e.SpanID,
		"release": e.Release, "environment": e.Environment,
		"attributes": attrs,
	}
}

func (s *Server) apiProjectLogs(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Projects.Get(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if s.Logs == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "logs not configured")
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	entries, err := s.Logs.List(id, logs.Filter{
		Level:   strings.ToLower(strings.TrimSpace(q.Get("level"))),
		Query:   strings.TrimSpace(q.Get("q")),
		TraceID: strings.TrimSpace(q.Get("trace")),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		s.Logger.Error("project logs", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, logJSON(e))
	}
	writeJSON(w, http.StatusOK, out)
}

func traceTxnJSON(t traces.Transaction) map[string]any {
	return map[string]any{
		"id": t.ID, "eventId": t.EventID, "name": t.Name,
		"traceId": t.TraceID, "spanId": t.SpanID, "status": t.Status,
		"release": t.Release, "environment": t.Environment,
		"start":      t.Start.UTC().Format(time.RFC3339Nano),
		"end":        t.End.UTC().Format(time.RFC3339Nano),
		"durationMs": t.DurationMS, "spanCount": t.SpanCount,
	}
}

func traceSpanJSON(sp traces.Span) map[string]any {
	return map[string]any{
		"id": sp.ID, "traceId": sp.TraceID, "spanId": sp.SpanID,
		"parentSpanId": sp.ParentSpanID, "op": sp.Op, "description": sp.Description,
		"status":     sp.Status,
		"start":      sp.Start.UTC().Format(time.RFC3339Nano),
		"end":        sp.End.UTC().Format(time.RFC3339Nano),
		"durationMs": sp.DurationMS,
	}
}

func (s *Server) apiProjectTraces(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Projects.Get(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if s.Traces == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "traces not configured")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	txns, err := s.Traces.Recent(id, limit)
	if err != nil {
		s.Logger.Error("project traces", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(txns))
	for _, t := range txns {
		out = append(out, traceTxnJSON(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// apiProjectLatency serves percentile rollups of stored transaction
// durations for the overview Latency card: overall p50/p75/p95/p99 plus
// per-name p50/p95. These are percentiles of the SDK-sampled
// transactions Splat stored, not of all requests - hence the count and
// sampled flag so the UI can qualify small samples.
func (s *Server) apiProjectLatency(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Projects.Get(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if s.Traces == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "traces not configured")
		return
	}
	period, since := overviewPeriod(r)
	sum, err := s.Traces.Latency(id, since)
	if err != nil {
		s.Logger.Error("project latency", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	byName := make([]map[string]any, 0, len(sum.ByName))
	for _, n := range sum.ByName {
		byName = append(byName, map[string]any{
			"name": n.Name, "count": n.Count, "p50": n.P50, "p95": n.P95,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"period": period, "count": sum.Count,
		"p50": sum.P50, "p75": sum.P75, "p95": sum.P95, "p99": sum.P99,
		"sampled": sum.Sampled, "byName": byName, "nameTruncated": sum.NameTrunc,
	})
}

func (s *Server) apiProjectTraceDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Projects.Get(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if s.Traces == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "traces not configured")
		return
	}
	traceID := strings.TrimSpace(r.PathValue("traceId"))
	if traceID == "" {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	txns, spans, err := s.Traces.Trace(id, traceID)
	if err != nil {
		s.Logger.Error("project trace detail", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(txns) == 0 {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	txnJSON := make([]map[string]any, 0, len(txns))
	for _, t := range txns {
		txnJSON = append(txnJSON, traceTxnJSON(t))
	}
	spanJSON := make([]map[string]any, 0, len(spans))
	for _, sp := range spans {
		spanJSON = append(spanJSON, traceSpanJSON(sp))
	}
	// Logs sharing the trace id ride along: one call renders the trace's
	// spans and the log lines the SDK emitted inside them.
	logList := []map[string]any{}
	if s.Logs != nil {
		if entries, err := s.Logs.List(id, logs.Filter{TraceID: traceID, Limit: 200}); err == nil {
			for _, e := range entries {
				logList = append(logList, logJSON(e))
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"traceId": traceID, "transactions": txnJSON, "spans": spanJSON, "logs": logList,
	})
}
