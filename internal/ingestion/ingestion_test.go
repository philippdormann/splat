package ingestion

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/events"
	"errgotrack/internal/issues"
	"errgotrack/internal/logs"
	"errgotrack/internal/notifications"
	"errgotrack/internal/projects"
	"errgotrack/internal/sessions"
	"errgotrack/internal/traces"
)

func newTestHandler(t testing.TB) (*Handler, *projects.Project) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	projStore := projects.NewStore(db)
	p, err := projStore.Create("Demo", "demo", "production", "", 90)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(discard{}, nil))
	h := &Handler{
		Projects:      projStore,
		Issues:        issues.NewStore(db),
		Events:        events.NewStore(db),
		Notifications: notifications.NewStore(db, logger),
		Sessions:      sessions.NewStore(db),
		Logs:          logs.NewStore(db),
		Traces:        traces.NewStore(db),
		Logger:        logger,
		PublicURL:     "http://localhost:8080",
	}
	return h, p
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func mux(h *Handler) http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("POST /api/{project}/envelope/", h.Envelope)
	m.HandleFunc("POST /api/{project}/store/", h.Store)
	return m
}

func djangoLikeEnvelope(eventID, publicKey string) []byte {
	header := map[string]any{"event_id": eventID}
	item := map[string]any{"type": "event"}
	event := map[string]any{
		"event_id": eventID, "timestamp": float64(time.Now().Unix()), "level": "error",
		"environment": "production", "platform": "python",
		"exception": map[string]any{"values": []map[string]any{{
			"type": "KeyError", "value": "'user_id'",
			"stacktrace": map[string]any{"frames": []map[string]any{
				{"filename": "app/views.py", "function": "profile_view", "module": "app.views", "lineno": 17, "in_app": true},
			}},
		}}},
	}
	hb, _ := json.Marshal(header)
	ib, _ := json.Marshal(item)
	eb, _ := json.Marshal(event)
	return []byte(strings.Join([]string{string(hb), string(ib), string(eb), ""}, "\n"))
}

func TestEnvelopeIngestsDjangoLikeException(t *testing.T) {
	h, p := newTestHandler(t)
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	eventID := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body := djangoLikeEnvelope(eventID, p.PublicKey)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/"+strconv.FormatInt(p.ID, 10)+"/envelope/", strings.NewReader(string(body)))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+p.PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post envelope: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	ev, err := h.Events.Get(eventID)
	if err != nil {
		t.Fatalf("event not stored: %v", err)
	}
	if ev.ExceptionType != "KeyError" {
		t.Fatalf("exception type = %q", ev.ExceptionType)
	}

	issueList, total, err := h.Issues.List(issues.ListFilter{ProjectID: p.ID})
	if err != nil || total != 1 {
		t.Fatalf("expected exactly 1 issue, got total=%d err=%v", total, err)
	}
	if issueList[0].EventCount != 1 {
		t.Fatalf("event count = %d", issueList[0].EventCount)
	}
}

func TestEnvelopeRejectsWrongProjectID(t *testing.T) {
	h, p := newTestHandler(t)
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	body := djangoLikeEnvelope("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", p.PublicKey)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/999999/envelope/", strings.NewReader(string(body)))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+p.PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for mismatched project id, got %d", resp.StatusCode)
	}
}

func TestEnvelopeRejectsUnknownKey(t *testing.T) {
	h, p := newTestHandler(t)
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	body := djangoLikeEnvelope("cccccccccccccccccccccccccccccccc", "not-a-real-key")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/"+strconv.FormatInt(p.ID, 10)+"/envelope/", strings.NewReader(string(body)))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key=not-a-real-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown key, got %d", resp.StatusCode)
	}
}

func TestEnvelopeRejectsDisabledProject(t *testing.T) {
	h, p := newTestHandler(t)
	if err := h.Projects.SetEnabled(p.ID, false); err != nil {
		t.Fatalf("disable project: %v", err)
	}
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	body := djangoLikeEnvelope("dddddddddddddddddddddddddddddddd", p.PublicKey)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/"+strconv.FormatInt(p.ID, 10)+"/envelope/", strings.NewReader(string(body)))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+p.PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for disabled project, got %d", resp.StatusCode)
	}
}

func TestStoreEndpointLegacyFormat(t *testing.T) {
	h, p := newTestHandler(t)
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	event := map[string]any{
		"event_id": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "timestamp": float64(time.Now().Unix()),
		"level": "warning", "message": "legacy store smoke test",
	}
	b, _ := json.Marshal(event)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/"+strconv.FormatInt(p.ID, 10)+"/store/", strings.NewReader(string(b)))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+p.PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if _, err := h.Events.Get("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"); err != nil {
		t.Fatalf("legacy event not stored: %v", err)
	}
}

func TestRepeatedEventsIncrementSameIssue(t *testing.T) {
	h, p := newTestHandler(t)
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	for i := 0; i < 3; i++ {
		body := djangoLikeEnvelope("repeat-event-"+strconv.Itoa(i), p.PublicKey)
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/"+strconv.FormatInt(p.ID, 10)+"/envelope/", strings.NewReader(string(body)))
		req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+p.PublicKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		resp.Body.Close()
	}

	list, total, err := h.Issues.List(issues.ListFilter{ProjectID: p.ID})
	if err != nil || total != 1 {
		t.Fatalf("expected 1 issue after 3 matching events, got total=%d err=%v", total, err)
	}
	if list[0].EventCount != 3 {
		t.Fatalf("expected event_count=3, got %d", list[0].EventCount)
	}
}

// TestEnvelopeAcceptsNonStringFrameVars guards against a real bug found by
// running Flask's sentry-sdk integration against this server: despite the
// field's name, a captured local variable isn't always repr()'d to a plain
// string - a dict/list-valued local (e.g. Flask's WSGI "environ") can arrive
// as nested JSON. Frame.Vars used to be map[string]string, which made the
// *entire event* fail json.Unmarshal and get silently dropped (envelope
// still returned 200, nothing stored) the instant any frame had one
// non-string local - a real Flask request handler exception, not a
// synthetic edge case.
func TestEnvelopeAcceptsNonStringFrameVars(t *testing.T) {
	h, p := newTestHandler(t)
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	eventID := "ffffffffffffffffffffffffffffffff"
	header := map[string]any{"event_id": eventID}
	item := map[string]any{"type": "event"}
	event := map[string]any{
		"event_id": eventID, "timestamp": float64(time.Now().Unix()), "level": "error",
		"exception": map[string]any{"values": []map[string]any{{
			"type": "KeyError", "value": "'missing_key'",
			"stacktrace": map[string]any{"frames": []map[string]any{
				{
					"filename": "app.py", "function": "handler", "lineno": 10, "in_app": true,
					"vars": map[string]any{
						"request_id": "abc-123",                                   // plain string, like before
						"environ":    map[string]any{"a": 1, "b": []int{1, 2, 3}}, // nested object - the shape that broke it
						"count":      42,                                          // bare number
						"flag":       true,                                        // bare bool
					},
				},
			}},
		}}},
	}
	hb, _ := json.Marshal(header)
	ib, _ := json.Marshal(item)
	eb, _ := json.Marshal(event)
	body := strings.Join([]string{string(hb), string(ib), string(eb), ""}, "\n")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/"+strconv.FormatInt(p.ID, 10)+"/envelope/", strings.NewReader(body))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+p.PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post envelope: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	if _, err := h.Events.Get(eventID); err != nil {
		t.Fatalf("event with non-string frame vars was silently dropped: %v", err)
	}
}

// TestEnvelopePersistsFullPythonSDKEventShape sends a representative
// sentry-sdk (Django) payload with every optional section populated -
// tags, user, sdk, request (with headers/cookies/env/query_string/data),
// breadcrumbs, and extra - and checks each one survives ingestion into its
// own column verbatim. The display layer (internal/httpserver) is
// responsible for redacting secrets at render time; this test only proves
// the pipeline doesn't drop or mangle data before it gets that far.
func TestEnvelopePersistsFullPythonSDKEventShape(t *testing.T) {
	h, p := newTestHandler(t)
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	eventID := "11111111111111111111111111111111"
	header := map[string]any{"event_id": eventID}
	item := map[string]any{"type": "event"}
	event := map[string]any{
		"event_id": eventID, "timestamp": float64(time.Now().Unix()), "level": "error",
		"environment": "production", "platform": "python", "release": "myapp@1.4.0",
		"transaction": "checkout_view",
		"exception": map[string]any{"values": []map[string]any{{
			"type": "ValueError", "value": "invalid cart total",
			"stacktrace": map[string]any{"frames": []map[string]any{
				{"filename": "app/checkout.py", "function": "checkout_view", "lineno": 42, "in_app": true},
			}},
		}}},
		"tags": map[string]any{"url": "/checkout/"},
		"user": map[string]any{"id": "42", "email": "alice@example.com"},
		"sdk":  map[string]any{"name": "sentry.python.django", "version": "2.18.0", "integrations": []string{"django"}},
		"request": map[string]any{
			"url": "http://example.com/checkout/", "method": "POST",
			"query_string": "ref=email",
			"headers":      map[string]any{"User-Agent": "Mozilla/5.0", "Authorization": "Bearer secret-token"},
			"cookies":      map[string]any{"sessionid": "abc123"},
			"env":          map[string]any{"REMOTE_ADDR": "203.0.113.5"},
			"data":         map[string]any{"username": "alice"},
		},
		"breadcrumbs": map[string]any{"values": []map[string]any{
			{"timestamp": float64(time.Now().Add(-time.Minute).Unix()), "category": "navigation", "message": "visited /cart/"},
		}},
		"extra": map[string]any{"cart_total": 59.99},
	}
	hb, _ := json.Marshal(header)
	ib, _ := json.Marshal(item)
	eb, _ := json.Marshal(event)
	body := strings.Join([]string{string(hb), string(ib), string(eb), ""}, "\n")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/"+strconv.FormatInt(p.ID, 10)+"/envelope/", strings.NewReader(body))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+p.PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post envelope: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	ev, err := h.Events.Get(eventID)
	if err != nil {
		t.Fatalf("event not stored: %v", err)
	}
	if ev.TransactionName != "checkout_view" || ev.Release != "myapp@1.4.0" {
		t.Fatalf("got transaction=%q release=%q", ev.TransactionName, ev.Release)
	}
	for name, got := range map[string]string{
		"tags": ev.TagsJSON, "user": ev.UserJSON, "sdk": ev.SDKJSON,
		"request": ev.RequestJSON, "breadcrumbs": ev.BreadcrumbsJSON, "extra": ev.ExtraJSON,
	} {
		if got == "" || got == "{}" || got == "null" {
			t.Errorf("%s was not persisted (got %q)", name, got)
		}
	}
	// The raw columns intentionally keep everything, secrets included - the
	// redaction documented in internal/httpserver/eventfields.go happens at
	// render time, not storage time, so the admin's own account of what an
	// SDK actually sent is never silently altered.
	if !strings.Contains(ev.RequestJSON, "secret-token") {
		t.Fatal("expected the raw request JSON to still contain the full payload prior to any display-time redaction")
	}
}

func TestEnvelopeStoresSessionItems(t *testing.T) {
	h, p := newTestHandler(t)
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	header, _ := json.Marshal(map[string]any{"event_id": "ssssssssssssssssssssssssssssssss"})
	sessItem, _ := json.Marshal(map[string]any{"type": "session"})
	sess, _ := json.Marshal(map[string]any{
		"sid": "sid-1", "did": "user-9", "status": "crashed",
		"attrs": map[string]any{"release": "9.9", "environment": "production"},
	})
	aggrItem, _ := json.Marshal(map[string]any{"type": "sessions"})
	aggr, _ := json.Marshal(map[string]any{
		"attrs":      map[string]any{"release": "9.9", "environment": "production"},
		"aggregates": []map[string]any{{"started": "2026-10-27T10:00:00Z", "exited": 4, "crashed": 1}},
	})
	body := strings.Join([]string{string(header), string(sessItem), string(sess), string(aggrItem), string(aggr), ""}, "\n")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/"+strconv.FormatInt(p.ID, 10)+"/envelope/", strings.NewReader(body))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+p.PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	sum, err := h.Sessions.CrashFree(p.ID, "9.9", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("crashfree: %v", err)
	}
	// 1 individual (crashed) + 5 aggregated (4 exited, 1 crashed).
	if sum.TotalSessions != 6 {
		t.Fatalf("sessions = %d, want 6", sum.TotalSessions)
	}
	if sum.TotalUsers != 1 || sum.CrashFreeUsers != 0 {
		t.Fatalf("users = %d crash-free = %v", sum.TotalUsers, sum.CrashFreeUsers)
	}
}

func TestEnvelopeIngestsLogsAndTransactions(t *testing.T) {
	h, p := newTestHandler(t)
	srv := httptest.NewServer(mux(h))
	defer srv.Close()

	header, _ := json.Marshal(map[string]any{"event_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	logPayload := `{"items": [
		{"timestamp": 1719398400, "severity_number": 9, "body": "user signed in", "trace_id": "t9"},
		{"timestamp": 1719398401, "severity_number": 17, "body": "payment failed"}
	]}`
	txnPayload := `{"event_id": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "transaction": "GET /checkout",
		"release": "2.0.0", "start_timestamp": 1719398400.0, "timestamp": 1719398400.5,
		"contexts": {"trace": {"trace_id": "t9", "span_id": "s9", "op": "http.server"}},
		"spans": [{"trace_id": "t9", "span_id": "s10", "parent_span_id": "s9", "op": "db",
			"start_timestamp": 1719398400.1, "timestamp": 1719398400.2}]}`
	logItem, _ := json.Marshal(map[string]any{"type": "log", "item_count": 2, "length": len(logPayload)})
	txnItem, _ := json.Marshal(map[string]any{"type": "transaction", "length": len(txnPayload)})

	body := strings.Join([]string{
		string(header), string(logItem), logPayload,
		string(txnItem), txnPayload, "",
	}, "\n")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/"+strconv.FormatInt(p.ID, 10)+"/envelope/", strings.NewReader(body))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+p.PublicKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post envelope: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	entries, err := h.Logs.List(p.ID, logs.Filter{})
	if err != nil || len(entries) != 2 {
		t.Fatalf("logs = %v, %v", entries, err)
	}
	if entries[0].Level != "error" || entries[1].TraceID != "t9" {
		t.Fatalf("entries = %+v", entries)
	}

	recent, err := h.Traces.Recent(p.ID, 10)
	if err != nil || len(recent) != 1 {
		t.Fatalf("transactions = %v, %v", recent, err)
	}
	if recent[0].Name != "GET /checkout" || recent[0].Release != "2.0.0" || recent[0].SpanCount != 1 {
		t.Fatalf("txn = %+v", recent[0])
	}
	txns, spans, err := h.Traces.Trace(p.ID, "t9")
	if err != nil || len(txns) != 1 || len(spans) != 1 {
		t.Fatalf("trace = %d/%d, %v", len(txns), len(spans), err)
	}
}
