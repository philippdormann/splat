// Package ingestion implements the Sentry-compatible HTTP endpoints Django's
// official sentry-sdk posts to: the modern envelope endpoint and the legacy
// store endpoint. See internal/sentryprotocol for the wire format itself.
package ingestion

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"errgotrack/internal/apikeys"
	"errgotrack/internal/events"
	"errgotrack/internal/issues"
	"errgotrack/internal/notifications"
	"errgotrack/internal/projects"
	"errgotrack/internal/sentryprotocol"
	"errgotrack/internal/sessions"
)

const maxBodyBytes = 2 << 20 // 2MiB: generous for a Django traceback + breadcrumbs, small enough to bound memory per request

type Handler struct {
	Projects      *projects.Store
	APIKeys       *apikeys.Store // optional: Bearer fallback for DSN-less ingestion
	Issues        *issues.Store
	Events        *events.Store
	Notifications *notifications.Store
	Sessions      *sessions.Store // optional: nil disables session tracking
	Logger        *slog.Logger
	PublicURL     string
}

// Envelope handles POST /api/{project}/envelope/.
func (h *Handler) Envelope(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	env, err := sentryprotocol.ParseEnvelope(body)
	if err != nil {
		h.Logger.Warn("envelope parse failed", "error", err)
		http.Error(w, `{"error":"malformed envelope"}`, http.StatusBadRequest)
		return
	}

	authKey := authKeyFromRequest(r)
	if authKey == "" {
		authKey = sentryprotocol.KeyFromDSN(env.Header.DSN)
	}
	project, ok := h.resolveProject(w, r, authKey)
	if !ok {
		return
	}

	eventID := env.Header.EventID
	for _, item := range env.Items {
		switch item.Header.Type {
		case "session":
			if h.Sessions != nil {
				if err := h.Sessions.UpsertSession(project.ID, item.Payload); err != nil {
					h.Logger.Warn("session item parse failed", "project", project.Slug, "error", err)
				}
			}
			continue
		case "sessions":
			if h.Sessions != nil {
				if err := h.Sessions.AddAggregates(project.ID, item.Payload); err != nil {
					h.Logger.Warn("session aggregates parse failed", "project", project.Slug, "error", err)
				}
			}
			continue
		case "event", "":
		default:
			continue // transactions, attachments, client_reports, etc: accepted, not stored
		}
		ev, err := sentryprotocol.ParseEventPayload(item.Payload)
		if err != nil {
			h.Logger.Warn("event item parse failed", "project", project.Slug, "error", err)
			continue
		}
		if ev.EventID == "" {
			ev.EventID = eventID
		}
		h.ingestEvent(r.Context(), project, ev)
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":"%s"}`, eventID)
}

// Store handles POST /api/{project}/store/, the legacy pre-envelope
// endpoint some SDK configurations and versions still use: a bare event
// JSON body, no envelope wrapper.
func (h *Handler) Store(w http.ResponseWriter, r *http.Request) {
	body, ok := h.readBody(w, r)
	if !ok {
		return
	}
	authKey := authKeyFromRequest(r)
	project, ok := h.resolveProject(w, r, authKey)
	if !ok {
		return
	}
	ev, err := sentryprotocol.ParseEventPayload(body)
	if err != nil {
		h.Logger.Warn("store event parse failed", "project", project.Slug, "error", err)
		http.Error(w, `{"error":"malformed event"}`, http.StatusBadRequest)
		return
	}
	if ev.EventID == "" {
		ev.EventID = newEventID()
	}
	h.ingestEvent(r.Context(), project, ev)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":"%s"}`, ev.EventID)
}

func (h *Handler) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var reader io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, `{"error":"invalid gzip body"}`, http.StatusBadRequest)
			return nil, false
		}
		defer gz.Close()
		reader = gz
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, `{"error":"payload too large"}`, http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, `{"error":"could not read body"}`, http.StatusBadRequest)
		}
		return nil, false
	}
	return body, true
}

func (h *Handler) resolveProject(w http.ResponseWriter, r *http.Request, authKey string) (*projects.Project, bool) {
	if authKey == "" {
		// No DSN credential at all: a Bearer API key with ingest scope
		// authenticates by path project id instead.
		return h.resolveProjectByKey(w, r)
	}
	project, err := h.Projects.GetByPublicKey(authKey)
	if err != nil {
		// Wrong DSN key, but maybe a Bearer API key was also sent.
		if p, ok := h.resolveProjectByKey(w, r); ok {
			return p, true
		}
		http.Error(w, `{"error":"invalid project key"}`, http.StatusUnauthorized)
		return nil, false
	}
	if !project.Enabled {
		http.Error(w, `{"error":"project disabled"}`, http.StatusForbidden)
		return nil, false
	}
	// The URL's {project} segment is the DSN's numeric project id; verify
	// it matches the key's actual project so one key can't be replayed
	// against another project's ingestion path.
	if idStr := r.PathValue("project"); idStr != "" {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil && id != project.ID {
			http.Error(w, `{"error":"project key does not match project id"}`, http.StatusUnauthorized)
			return nil, false
		}
	}
	return project, true
}

// resolveProjectByKey authenticates ingestion with a Bearer API key instead
// of the DSN public key. The path's numeric project id selects the project,
// so the key's project binding (if any) is enforced against it - a key
// bound to project A cannot ingest into project B.
func (h *Handler) resolveProjectByKey(w http.ResponseWriter, r *http.Request) (*projects.Project, bool) {
	secret := apikeys.BearerToken(r)
	if secret == "" || h.APIKeys == nil {
		http.Error(w, `{"error":"missing authentication"}`, http.StatusUnauthorized)
		return nil, false
	}
	k, err := h.APIKeys.Validate(secret)
	if err != nil || !k.HasScope(apikeys.ScopeIngest) {
		http.Error(w, `{"error":"invalid project key"}`, http.StatusUnauthorized)
		return nil, false
	}
	id, err := strconv.ParseInt(r.PathValue("project"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid project id"}`, http.StatusUnauthorized)
		return nil, false
	}
	if k.ProjectID.Valid && k.ProjectID.Int64 != id {
		http.Error(w, `{"error":"key not valid for this project"}`, http.StatusForbidden)
		return nil, false
	}
	project, err := h.Projects.Get(id)
	if err != nil || !project.Enabled {
		http.Error(w, `{"error":"invalid project key"}`, http.StatusUnauthorized)
		return nil, false
	}
	h.APIKeys.TouchLastUsed(k.ID)
	return project, true
}

func authKeyFromRequest(r *http.Request) string {
	if auth := r.Header.Get("X-Sentry-Auth"); auth != "" {
		return sentryprotocol.ParseAuthHeader(auth)
	}
	return r.URL.Query().Get("sentry_key")
}

func newEventID() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

// ingestEvent persists one parsed Sentry event, groups it into an issue,
// and evaluates it for notification. Errors are logged, not surfaced to the
// client: the SDK fires-and-forgets, and a 5xx just trains it to retry the
// exact payload, which doesn't help either side.
func (h *Handler) ingestEvent(ctx context.Context, project *projects.Project, ev *sentryprotocol.Event) *issues.UpsertResult {
	ts := ev.ParsedTimestamp()
	environment := ev.Environment
	if environment == "" {
		environment = project.DefaultEnvironment
	}
	level := ev.Level
	if level == "" {
		level = "error"
	}

	excType, excValue, culprit := summarize(ev)
	title := issueTitle(excType, excValue, ev.Message.Text)
	fingerprint := issues.Fingerprint(ev)

	result, err := h.Issues.Upsert(ctx, project.ID, fingerprint, title, excType, excValue, culprit, level, environment, ev.Release, ts)
	if err != nil {
		h.Logger.Error("issue upsert failed", "error", err, "project", project.Slug)
		return nil
	}

	tagsJSON, _ := json.Marshal(ev.Tags)
	breadcrumbsJSON, _ := json.Marshal(ev.Breadcrumbs)
	record := &events.Event{
		EventID:         ev.EventID,
		IssueID:         result.Issue.ID,
		ProjectID:       project.ID,
		Environment:     environment,
		Release:         ev.Release,
		Timestamp:       ts,
		Message:         firstNonEmpty(ev.Message.Text, excValue),
		ExceptionType:   excType,
		ExceptionValue:  excValue,
		Culprit:         culprit,
		TransactionName: ev.Transaction,
		StacktraceJSON:  orEmptyObject(marshalOrNil(ev.Exception)),
		TagsJSON:        orEmptyObject(tagsJSON),
		ExtraJSON:       orEmptyObject(ev.Extra),
		BreadcrumbsJSON: orEmptyObject(breadcrumbsJSON),
		UserJSON:        orEmptyObject(ev.User),
		SDKJSON:         orEmptyObject(ev.SDK),
		RequestJSON:     orEmptyObject(ev.Request),
		ContextsJSON:    orEmptyObject(ev.Contexts),
	}
	if err := h.Events.Insert(record); err != nil {
		h.Logger.Error("event insert failed", "error", err, "project", project.Slug)
		return nil
	}

	h.Notifications.Evaluate(ctx, notifications.AlertEvent{
		IssueID:       result.Issue.ID,
		ProjectID:     project.ID,
		ProjectName:   project.Name,
		Environment:   environment,
		ExceptionType: excType,
		Message:       firstNonEmpty(excValue, ev.Message.Text),
		EventCount:    result.Issue.EventCount,
		IsNew:         result.IsNew,
		Reopened:      result.Reopened,
		Status:        string(result.Issue.Status),
		Timestamp:     ts,
		IssueURL:      fmt.Sprintf("%s/issues/%d", h.PublicURL, result.Issue.ID),
	})
	return result
}

// SendTestEvent ingests one synthetic event through the real pipeline so
// operators can verify a project's DSN end to end. It returns the grouped
// issue's id for linking.
func (h *Handler) SendTestEvent(ctx context.Context, project *projects.Project) (int64, error) {
	ts := time.Now().UTC()
	ev := &sentryprotocol.Event{
		EventID:     newEventID(),
		Timestamp:   json.RawMessage(strconv.Quote(ts.Format(time.RFC3339Nano))),
		Level:       "error",
		Platform:    "other",
		Environment: project.DefaultEnvironment,
		Exception: &sentryprotocol.ExceptionValues{Values: []sentryprotocol.ExceptionValue{{
			Type:  "TestError",
			Value: "This is a test event sent from Errgotrack - safe to resolve once received.",
			Stacktrace: &sentryprotocol.Stacktrace{Frames: []sentryprotocol.Frame{{
				Filename: "errgotrack/test.py", Function: "send_test_event",
				LineNo: 1, InApp: true, ContextLine: "raise TestError('hello from errgotrack')",
			}}},
		}}},
		Tags: sentryprotocol.Tags{"test-event": "true"},
	}
	result := h.ingestEvent(ctx, project, ev)
	if result == nil {
		return 0, fmt.Errorf("test event failed to ingest")
	}
	return result.Issue.ID, nil
}

// summarize picks the exception type/value/culprit used for the issue title
// and grouping from the deepest exception in the chain (the one that
// actually propagated), matching Fingerprint's choice.
func summarize(ev *sentryprotocol.Event) (excType, excValue, culprit string) {
	if ev.Exception == nil || len(ev.Exception.Values) == 0 {
		return "", "", ev.Transaction
	}
	exc := ev.Exception.Values[len(ev.Exception.Values)-1]
	culprit = ev.Transaction
	if culprit == "" && exc.Stacktrace != nil {
		for i := len(exc.Stacktrace.Frames) - 1; i >= 0; i-- {
			f := exc.Stacktrace.Frames[i]
			if f.InApp {
				culprit = f.Function
				break
			}
		}
	}
	return exc.Type, exc.Value, culprit
}

func issueTitle(excType, excValue, message string) string {
	switch {
	case excType != "":
		if excValue != "" {
			return excType + ": " + truncate(excValue, 200)
		}
		return excType
	case message != "":
		return truncate(message, 200)
	default:
		return "<unlabeled event>"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func marshalOrNil(v any) []byte {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func orEmptyObject(b []byte) string {
	if len(b) == 0 {
		return "{}"
	}
	return string(b)
}
