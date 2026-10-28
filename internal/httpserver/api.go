// JSON API for the shadcn-svelte SPA (see frontend/).
//
// The previous server-rendered Go templates + HTMX UI has been replaced by
// a Svelte 5 + shadcn-svelte frontend built with bun (frontend/, embedded
// into the binary as web/dist). This file is the data layer it talks to:
// all endpoints live under /api/v1/, speak JSON, and authenticate via the
// same session cookie as before (401 JSON when missing instead of a login
// redirect).
//
// NOTE on CSRF: the JSON API relies on SameSite=Lax session cookies and
// same-origin fetch (the SPA is served from the same host). The legacy
// form-post CSRF double-submit check is kept for the Sentry ingestion
// endpoints' rate limiting only, not applied here.
package httpserver

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/apikeys"
	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/auth"
	"github.com/philippdormann/splat/internal/dashboard"
	"github.com/philippdormann/splat/internal/events"
	"github.com/philippdormann/splat/internal/issues"
	"github.com/philippdormann/splat/internal/platforms"
	"github.com/philippdormann/splat/internal/projects"
	"github.com/philippdormann/splat/internal/sentryprotocol"
	"github.com/philippdormann/splat/internal/sessions"
	"github.com/philippdormann/splat/internal/sourcemaps"
	"github.com/philippdormann/splat/internal/tracing"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// encoding/json escapes <, >, & by default, so untrusted event data
	// (exception messages containing "</script>") cannot break out of a
	// JSON context - the Svelte frontend then renders values as text, never
	// as raw HTML.
	json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// maxAPIBodyBytes bounds a management-API request body. Ingestion has its
// own (larger) cap; here we just need to keep a single authenticated
// request from exhausting memory.
const maxAPIBodyBytes = 4 << 20 // 4 MiB

func decodeJSON(r *http.Request, v any) bool {
	if r.Body == nil {
		return false
	}
	r.Body = http.MaxBytesReader(nil, r.Body, maxAPIBodyBytes)
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v) == nil
}

// --- auth ---

func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	token := auth.SessionTokenFromRequest(r)
	userID, ok := s.Auth.ValidateSession(token)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"username":      s.Auth.UsernameOf(userID),
		"role":          s.Auth.RoleOf(userID),
		"totpEnabled":   s.Auth.TOTPEnabled(userID),
		"totpRequired":  s.Auth.TOTPRequired(),
	})
}

func (s *Server) apiLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if s.Auth.PasswordLoginDisabled() {
		writeAPIError(w, http.StatusUnauthorized, "password login is disabled - use single sign-on")
		return
	}
	userID, err := s.Auth.Authenticate(input.Username, input.Password)
	if err != nil {
		s.auditAs(0, input.Username, clientIP(r), audit.LoginFailed, "user", "", `{"via":"password"}`)
		writeAPIError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	// Second factor enforced: password success mints a short-lived
	// signed challenge for the 2FA step instead of a session. Under
	// instance-wide enforcement users without a factor enroll inline.
	if s.Auth.TOTPEnabled(userID) {
		challenge, err := s.Auth.IssueLoginChallenge(userID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"twoFactorRequired": true, "challenge": challenge})
		return
	}
	if s.Auth.TOTPRequired() {
		challenge, err := s.Auth.IssueLoginChallenge(userID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"twoFactorRequired": true, "enrollRequired": true, "challenge": challenge})
		return
	}
	token, expires, err := s.Auth.CreateSession(userID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	auth.SetSessionCookie(w, token, expires, s.CookieSecure)
	s.auditAs(userID, s.Auth.UsernameOf(userID), clientIP(r), audit.Login, "user", strconv.FormatInt(userID, 10), `{"via":"password"}`)
	_ = s.Auth.TouchLastLogin(userID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	s.audit(r, audit.Logout, "session", "", "{}")
	s.Auth.DeleteSession(auth.SessionTokenFromRequest(r))
	auth.ClearSessionCookie(w, s.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- dashboard ---

func (s *Server) apiDashboard(w http.ResponseWriter, r *http.Request) {
	rangeValue := r.URL.Query().Get("range")
	valid := false
	for _, ro := range dashboardRanges {
		if ro.Value == rangeValue {
			valid = true
		}
	}
	if !valid {
		rangeValue = "24h"
	}
	since, previousSince := rangeToSince(rangeValue)

	// Member sessions see only granted projects; no grants means an empty
	// dashboard rather than a leak. Admins and Bearer keys are unscoped
	// here (keys keep their per-endpoint project binding elsewhere).
	var scope []int64
	if ids, ok := s.accessScope(r); !ok {
		scope = []int64{-1}
	} else if ids != nil {
		scope = ids
		if len(scope) == 0 {
			scope = []int64{-1}
		}
	}
	hasProjects := false
	if scope == nil {
		projectList, _ := s.Projects.List()
		hasProjects = len(projectList) > 0
	} else {
		hasProjects = len(scope) > 0 && scope[0] != -1
	}

	stats, err := s.Dashboard.Stats(since, previousSince, scope...)
	if err != nil {
		s.Logger.Error("dashboard stats", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}

	out := map[string]any{
		"range": rangeValue,
		"stats": map[string]any{
			"totalEvents":      stats.TotalEvents,
			"unresolvedIssues": stats.UnresolvedIssues,
			"newIssues":        stats.NewIssues,
			"affectedProjects": stats.AffectedProjects,
			"eventsToday":      stats.EventsToday,
			"uniqueIssues":     stats.UniqueIssues,
			"eventsDeltaPct":   stats.EventsDeltaPct,
			"eventsDeltaLabel": stats.EventsDeltaLabel,
		},
		"hasData":     stats.TotalEvents > 0 || stats.UnresolvedIssues > 0,
		"hasProjects": hasProjects,
	}

	charts := map[string]any{}
	if eventsOverTime, _ := s.Dashboard.EventsOverTime(since, scope...); eventsOverTime != nil {
		charts["eventsOverTime"] = pointsJSON(eventsOverTime)
	}
	if byProject, _ := s.Dashboard.ByProject(since, scope...); byProject != nil {
		charts["byProject"] = pointsJSON(byProject)
	}
	if topExceptions, _ := s.Dashboard.TopExceptionTypes(since, scope...); topExceptions != nil {
		charts["topExceptions"] = pointsJSON(topExceptions)
	}
	if byEnv, _ := s.Dashboard.ByEnvironment(since, scope...); byEnv != nil {
		charts["byEnvironment"] = pointsJSON(byEnv)
	}
	if newCount, recurringCount, _ := s.Dashboard.NewVsRecurring(since, scope...); true {
		charts["newVsRecurring"] = map[string]any{"new": newCount, "recurring": recurringCount}
	}
	if byStatus, _ := s.Dashboard.IssuesByStatus(since, scope...); byStatus != nil {
		charts["byStatus"] = pointsJSON(byStatus)
	}
	out["charts"] = charts

	var recent []map[string]any
	if recents, _ := s.Dashboard.RecentIssues(10, scope...); recents != nil {
		for _, ri := range recents {
			recent = append(recent, map[string]any{
				"id": ri.ID, "title": ri.Title, "level": ri.Level,
				"status": ri.Status, "projectName": ri.ProjectName, "environment": ri.Environment,
				"eventCount": ri.EventCount, "lastSeen": ri.LastSeen,
				"lastSeenRelative": relativeTime(ri.LastSeen),
			})
		}
	}
	out["recentIssues"] = recent
	writeJSON(w, http.StatusOK, out)
}

func pointsJSON(points []dashboard.Point) []map[string]any {
	out := make([]map[string]any, 0, len(points))
	for _, p := range points {
		out = append(out, map[string]any{"label": p.Label, "value": p.Value})
	}
	return out
}

// --- issues ---

func (s *Server) apiIssuesList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := issues.ListFilter{Limit: issuesPerPage, OrderBy: q.Get("sort"), Query: q.Get("q"), Environment: q.Get("environment"), Status: issues.Status(q.Get("status"))}
	if id, err := strconv.ParseInt(q.Get("project"), 10, 64); err == nil {
		f.ProjectID = id
	}
	// A project-bound key only ever sees its own project, regardless of
	// the query parameter it sends.
	if a := s.authenticate(r); !a.admin && a.key != nil && a.key.ProjectID.Valid {
		f.ProjectID = a.key.ProjectID.Int64
	}
	page := 1
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		page = p
	}
	// A member session only ever sees granted projects.
	if ids, ok := s.accessScope(r); !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"issues": []any{}, "total": 0, "page": page, "totalPages": 1,
			"projects": []any{}, "environments": []string{},
		})
		return
	} else if ids != nil {
		if f.ProjectID != 0 {
			allowed := false
			for _, id := range ids {
				if id == f.ProjectID {
					allowed = true
					break
				}
			}
			if !allowed {
				writeAPIError(w, http.StatusNotFound, "not found")
				return
			}
		} else {
			f.ProjectIDs = ids
		}
	}
	f.Offset = (page - 1) * issuesPerPage

	list, total, err := s.Issues.List(f)
	if err != nil {
		s.Logger.Error("list issues", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	names := s.projectNameCache()
	ids := make([]int64, len(list))
	for i, iss := range list {
		ids[i] = iss.ID
	}
	sparklines, _ := s.Issues.Sparkline24h(ids)

	rows := make([]map[string]any, 0, len(list))
	for _, iss := range list {
		rows = append(rows, map[string]any{
			"id": iss.ID, "title": iss.Title, "culprit": iss.Culprit, "level": iss.Level,
			"status":      string(iss.Status),
			"projectName": names[iss.ProjectID], "environment": iss.Environment,
			"eventCount": iss.EventCount, "userCount": iss.UserCount,
			"lastSeen": iss.LastSeen, "lastSeenRelative": relativeTime(iss.LastSeen),
			"sparkline": sparklines[iss.ID],
		})
	}
	totalPages := (total + issuesPerPage - 1) / issuesPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	allProjects, _ := s.Projects.List()
	if ids, ok := s.accessScope(r); !ok {
		allProjects = nil
	} else if ids != nil {
		allowed := make(map[int64]bool, len(ids))
		for _, id := range ids {
			allowed[id] = true
		}
		filtered := allProjects[:0]
		for _, p := range allProjects {
			if allowed[p.ID] {
				filtered = append(filtered, p)
			}
		}
		allProjects = filtered
	}
	projJSON := make([]map[string]any, 0, len(allProjects))
	for _, p := range allProjects {
		projJSON = append(projJSON, map[string]any{"id": p.ID, "name": p.Name})
	}
	envs, _ := s.Dashboard.DistinctEnvironments(f.ProjectID)
	writeJSON(w, http.StatusOK, map[string]any{
		"issues": rows, "total": total, "page": page, "totalPages": totalPages,
		"projects": projJSON, "environments": envs,
	})
}

func (s *Server) apiIssueDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	issue, err := s.Issues.Get(id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	// 404 (not 403) on cross-project access: a scoped key must not learn
	// whether other projects' issues exist.
	if !s.canAccessProject(r, issue.ProjectID) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	project, err := s.Projects.Get(issue.ProjectID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}

	var event *events.Event
	eventID := r.URL.Query().Get("event")
	event, err = s.resolveIssueEvent(issue.ID, eventID)
	if err != nil {
		// Issue with no events yet (possible right after grouping edge
		// cases): return the issue without event detail.
		writeJSON(w, http.StatusOK, map[string]any{
			"issue":   issueJSON(issue),
			"project": map[string]any{"id": project.ID, "name": project.Name},
		})
		return
	}

	var excValues sentryprotocol.ExceptionValues
	_ = json.Unmarshal([]byte(event.StacktraceJSON), &excValues)
	var frames []sentryprotocol.Frame
	if len(excValues.Values) > 0 {
		if st := excValues.Values[len(excValues.Values)-1].Stacktrace; st != nil {
			frames = st.Frames
		}
	}
	var tags map[string]string
	_ = json.Unmarshal([]byte(event.TagsJSON), &tags)
	var crumbs sentryprotocol.BreadcrumbValues
	_ = json.Unmarshal([]byte(event.BreadcrumbsJSON), &crumbs)
	sort.SliceStable(crumbs.Values, func(i, j int) bool {
		ti, oki := crumbs.Values[i].ParsedTimestamp()
		tj, okj := crumbs.Values[j].ParsedTimestamp()
		if !oki || !okj {
			return false
		}
		return ti.Before(tj)
	})

	contexts := parseContexts(event.ContextsJSON)
	traceID := traceIDFromContexts(contexts)
	traceTmpl, _ := s.Tracing.GetURLTemplate()

	eventsList, _ := s.Events.ListByIssue(issue.ID, 100, 0)
	evJSON := make([]map[string]any, 0, len(eventsList))
	for _, e := range eventsList {
		evJSON = append(evJSON, map[string]any{"eventId": e.EventID, "timestamp": e.Timestamp, "message": e.Message})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"issue":   issueJSON(issue),
		"project": map[string]any{"id": project.ID, "name": project.Name},
		"tags":    kvJSON(sortedStringMap(tags, true)),
		"user":    userJSON(buildUserView(event.UserJSON)),
		"sdk":     sdkJSON(buildSDKView(event.SDKJSON)),
		"request": requestJSON(buildRequestView(event.RequestJSON)),
		"contexts": func() []map[string]any {
			out := make([]map[string]any, 0, len(contexts))
			for _, c := range contexts {
				out = append(out, map[string]any{"name": c.Name, "fields": kvJSON(c.Fields)})
			}
			return out
		}(),
		"breadcrumbs": func() []map[string]any {
			out := make([]map[string]any, 0, len(crumbs.Values))
			for _, c := range crumbs.Values {
				ts := ""
				if t, ok := c.ParsedTimestamp(); ok {
					ts = t.Format("15:04:05.000")
				}
				data := flattenToStrings(c.Data)
				kv := sortedStringMap(data, true)
				m := map[string]string{}
				for _, p := range kv {
					m[p.Key] = p.Value
				}
				out = append(out, map[string]any{"timestamp": ts, "type": c.Category, "level": c.Level, "message": c.Message, "data": m})
			}
			return out
		}(),
		"stacktrace": framesJSON(buildFrameViews(frames), func(file string, line, col int) (sourcemaps.MappedLoc, bool) {
			if s.SourceMaps == nil {
				return sourcemaps.MappedLoc{}, false
			}
			return s.SourceMaps.Resolve(issue.ProjectID, event.Release, file, line, col)
		}),
		"events": evJSON,
		"latestEventId": func() any {
			if latest, err := s.Events.Latest(issue.ID); err == nil {
				return latest.EventID
			}
			return nil
		}(),
		"currentEventId": event.EventID,
		"prevEventId": func() any {
			if prev, err := s.Events.Neighbor(issue.ID, event.Timestamp, "prev"); err == nil {
				return prev.EventID
			}
			return nil
		}(),
		"nextEventId": func() any {
			if next, err := s.Events.Neighbor(issue.ID, event.Timestamp, "next"); err == nil {
				return next.EventID
			}
			return nil
		}(),
		"rawEvent": rawEventJSON(event, issue),
		"notes": func() []map[string]any {
			notes, err := s.Issues.Notes(issue.ID)
			if err != nil {
				return []map[string]any{}
			}
			out := make([]map[string]any, 0, len(notes))
			for _, n := range notes {
				out = append(out, issueNoteJSON(n))
			}
			return out
		}(),
		"links": func() []map[string]any {
			if s.RepoLinks == nil {
				return []map[string]any{}
			}
			links, err := s.RepoLinks.Links(issue.ID)
			if err != nil {
				return []map[string]any{}
			}
			out := make([]map[string]any, 0, len(links))
			for _, l := range links {
				out = append(out, linkJSON(l))
			}
			return out
		}(),
		"traceId":  traceID,
		"traceURL": tracing.BuildURL(traceTmpl, traceID),
	})
}

// rawEventJSON rebuilds the Sentry-shaped event document from stored
// columns for debugging ("what did the SDK actually send?"). Stored JSON
// columns pass through verbatim; empty ones are omitted.
func rawEventJSON(event *events.Event, issue *issues.Issue) map[string]any {
	raw := func(s string) any {
		s = strings.TrimSpace(s)
		if s == "" || s == "[]" || s == "{}" {
			return nil
		}
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil
		}
		return v
	}
	out := map[string]any{
		"event_id":    event.EventID,
		"timestamp":   event.Timestamp.UTC().Format(time.RFC3339Nano),
		"level":       issue.Level,
		"environment": event.Environment,
		"release":     event.Release,
		"message":     event.Message,
	}
	if event.TransactionName != "" {
		out["transaction"] = event.TransactionName
	}
	if event.ExceptionType != "" || event.ExceptionValue != "" {
		if ex := raw(event.StacktraceJSON); ex != nil {
			out["exception"] = ex
		} else {
			out["exception"] = map[string]any{
				"values": []any{map[string]any{"type": event.ExceptionType, "value": event.ExceptionValue}},
			}
		}
	}
	for _, k := range []string{"tags", "extra", "breadcrumbs", "user", "sdk", "request", "contexts"} {
		var src string
		switch k {
		case "tags":
			src = event.TagsJSON
		case "extra":
			src = event.ExtraJSON
		case "breadcrumbs":
			src = event.BreadcrumbsJSON
		case "user":
			src = event.UserJSON
		case "sdk":
			src = event.SDKJSON
		case "request":
			src = event.RequestJSON
		case "contexts":
			src = event.ContextsJSON
		}
		if v := raw(src); v != nil {
			out[k] = v
		}
	}
	if event.Culprit != "" {
		out["culprit"] = event.Culprit
	}
	return out
}

func (s *Server) apiIssueSetStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		Status           string `json:"status"`
		MutedUntil       string `json:"mutedUntil"`
		ResolveInRelease bool   `json:"resolveInRelease"`
	}
	if !decodeJSON(r, &input) {
		// Also accept form posts for curl convenience.
		_ = r.ParseForm()
		input.Status = r.FormValue("status")
		input.MutedUntil = r.FormValue("muted_until")
	}
	status := issues.Status(strings.ToLower(input.Status))
	if status != issues.StatusResolved && status != issues.StatusIgnored && status != issues.StatusUnresolved && status != issues.StatusMuted {
		writeAPIError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if issue, err := s.Issues.Get(id); err != nil || !s.canAccessProject(r, issue.ProjectID) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if status == issues.StatusResolved && input.ResolveInRelease {
		if err := s.Issues.ResolveInNextRelease(id); err != nil {
			s.Logger.Error("resolve issue in next release", "error", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if status == issues.StatusMuted {
		var until time.Time
		if input.MutedUntil != "" {
			var err error
			until, err = time.Parse(time.RFC3339, input.MutedUntil)
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, "mutedUntil must be RFC3339")
				return
			}
		}
		if err := s.Issues.SetMuted(id, until); err != nil {
			s.Logger.Error("set issue muted", "error", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if err := s.Issues.SetStatus(id, status); err != nil {
		s.Logger.Error("set issue status", "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func issueNoteJSON(n issues.Note) map[string]any {
	return map[string]any{
		"id": n.ID, "issueId": n.IssueID, "body": n.Body,
		"createdAt": n.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// issueForNotes resolves the issue and enforces project scoping (404, not
// 403, so scoped keys can't probe other projects).
func (s *Server) issueForNotes(w http.ResponseWriter, r *http.Request) (*issues.Issue, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	issue, err := s.Issues.Get(id)
	if err != nil || !s.canAccessProject(r, issue.ProjectID) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return nil, false
	}
	return issue, true
}

func (s *Server) apiIssueNotesList(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueForNotes(w, r)
	if !ok {
		return
	}
	notes, err := s.Issues.Notes(issue.ID)
	if err != nil {
		s.Logger.Error("list notes", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(notes))
	for _, n := range notes {
		out = append(out, issueNoteJSON(n))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiIssueNotesCreate(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueForNotes(w, r)
	if !ok {
		return
	}
	var input struct {
		Body string `json:"body"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	n, err := s.Issues.AddNote(issue.ID, input.Body)
	if err != nil {
		if errors.Is(err, issues.ErrEmptyNote) || errors.Is(err, issues.ErrNoteTooLarge) {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.Logger.Error("add note", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, issueNoteJSON(n))
}

func (s *Server) apiIssueNotesDelete(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueForNotes(w, r)
	if !ok {
		return
	}
	noteID, err := strconv.ParseInt(r.PathValue("noteId"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Issues.DeleteNote(issue.ID, noteID); err != nil {
		s.Logger.Error("delete note", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiIssuesBulk(w http.ResponseWriter, r *http.Request) {
	// Bulk spans projects, so project-bound keys are refused outright.
	if a := s.authenticate(r); !a.admin && a.key != nil && a.key.ProjectID.Valid {
		writeAPIError(w, http.StatusForbidden, "project-bound keys cannot bulk-update across projects")
		return
	}
	var input struct {
		IDs    []int64 `json:"ids"`
		Status string  `json:"status"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	status := issues.Status(strings.ToLower(input.Status))
	if status != issues.StatusResolved && status != issues.StatusIgnored && status != issues.StatusUnresolved && status != issues.StatusMuted {
		writeAPIError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if len(input.IDs) == 0 || len(input.IDs) > 100 {
		writeAPIError(w, http.StatusBadRequest, "ids must contain 1-100 issues")
		return
	}
	// Enforce per-project access: a caller may only touch issues in
	// projects they can access. Unknown issues return 404 to avoid
	// leaking existence across project boundaries.
	for _, id := range input.IDs {
		iss, err := s.Issues.Get(id)
		if err != nil || !s.canAccessProject(r, iss.ProjectID) {
			writeAPIError(w, http.StatusNotFound, "not found")
			return
		}
	}
	if err := s.Issues.BulkSetStatus(input.IDs, status); err != nil {
		s.Logger.Error("bulk set status", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func issueJSON(iss *issues.Issue) map[string]any {
	var mutedUntil any
	if !iss.MutedUntil.IsZero() {
		mutedUntil = iss.MutedUntil
	}
	return map[string]any{
		"id": iss.ID, "title": iss.Title,
		"exceptionType": iss.ExceptionType, "exceptionValue": iss.ExceptionValue,
		"culprit": iss.Culprit, "level": iss.Level, "status": string(iss.Status),
		"eventCount": iss.EventCount, "userCount": iss.UserCount,
		"firstSeen": iss.FirstSeen, "lastSeen": iss.LastSeen,
		"environment": iss.Environment, "release": iss.Release,
		"mutedUntil":        mutedUntil,
		"resolvedInRelease": iss.ResolvedInRelease,
	}
}

func kvJSON(kvs []keyValue) []map[string]string {
	out := make([]map[string]string, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, map[string]string{"key": kv.Key, "value": kv.Value})
	}
	return out
}

func userJSON(u *userView) any {
	if u == nil {
		return nil
	}
	display := u.Username
	if display == "" {
		display = u.Email
	}
	if display == "" {
		display = u.ID
	}
	return map[string]any{"username": display, "extra": kvJSON(u.Extra)}
}

func sdkJSON(v *sdkView) any {
	if v == nil {
		return nil
	}
	return map[string]any{"name": v.Name, "integrations": v.Integrations}
}

func requestJSON(v *requestView) any {
	if v == nil {
		return nil
	}
	return map[string]any{"url": v.URL, "queryParams": kvJSON(v.QueryParams)}
}

func framesJSON(frames []frameView, resolve func(file string, line, col int) (sourcemaps.MappedLoc, bool)) []map[string]any {
	out := make([]map[string]any, 0, len(frames))
	for _, f := range frames {
		var contextLine string
		var pre, post []string
		for _, l := range f.Lines {
			if l.Current {
				contextLine = l.Text
			} else if contextLine == "" {
				pre = append(pre, l.Text)
			} else {
				post = append(post, l.Text)
			}
		}
		vars := make([]map[string]string, 0, len(f.Vars))
		keys := make([]string, 0, len(f.Vars))
		for k := range f.Vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			vars = append(vars, map[string]string{"key": k, "value": f.Vars[k]})
		}
		out = append(out, map[string]any{
			"file": f.Filename, "line": f.LineNo, "col": f.ColNo, "function": f.Function,
			"module": "", "contextLine": contextLine,
			"preContext": pre, "postContext": post, "vars": vars, "inApp": f.InApp,
			"original": func() any {
				if resolve == nil {
					return nil
				}
				if loc, ok := resolve(f.Filename, f.LineNo, f.ColNo); ok {
					return loc
				}
				return nil
			}(),
		})
	}
	return out
}

// --- projects ---

func derefInt(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func (s *Server) apiProjectsList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Projects.List()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}	// A project-bound key sees only its own project, not the fleet.
	if a := s.authenticate(r); !a.admin && a.key != nil && a.key.ProjectID.Valid {
		filtered := list[:0]
		for _, p := range list {
			if p.ID == a.key.ProjectID.Int64 {
				filtered = append(filtered, p)
			}
		}
		list = filtered
	}
	// A member session sees only granted projects (direct or via teams).
	if ids, ok := s.accessScope(r); !ok {
		list = nil
	} else if ids != nil {
		allowed := make(map[int64]bool, len(ids))
		for _, id := range ids {
			allowed[id] = true
		}
		filtered := list[:0]
		for _, p := range list {
			if allowed[p.ID] {
				filtered = append(filtered, p)
			}
		}
		list = filtered
	}
	// Query + pagination + batched stats (Rustrak-style list_stats).
	// ?query= filters name/slug (case-insensitive), ?page=&per_page=
	// paginate (cap 100), ?stats_period=24h enables the stats block.
	q := r.URL.Query()
	query := strings.ToLower(strings.TrimSpace(q.Get("query")))
	if query != "" {
		filtered := list[:0]
		for _, p := range list {
			if strings.Contains(strings.ToLower(p.Name), query) || strings.Contains(strings.ToLower(p.Slug), query) {
				filtered = append(filtered, p)
			}
		}
		list = filtered
	}
	page := 1
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 0 {
		page = n
	}
	perPage := 25
	if n, err := strconv.Atoi(q.Get("per_page")); err == nil && n > 0 {
		perPage = n
	}
	if perPage > 100 {
		perPage = 100
	}
	total := len(list)
	totalPages := (total + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * perPage
	end := start + perPage
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	window := list[start:end]

	// Stats: batched over the page only. stats_period=24h (default) or
	// Nd/Nh; absent stats_period the rows carry no stats block.
	statsOn := q.Get("stats_period") != ""
	periodHours := 24
	if sp := q.Get("stats_period"); sp != "" && sp != "24h" {
		if strings.HasSuffix(sp, "h") {
			if n, err := strconv.Atoi(strings.TrimSuffix(sp, "h")); err == nil && n > 0 {
				periodHours = n
			}
		} else if strings.HasSuffix(sp, "d") {
			if n, err := strconv.Atoi(strings.TrimSuffix(sp, "d")); err == nil && n > 0 {
				periodHours = n * 24
			}
		}
	}
	var batch map[int64]dashboard.ProjectListStats
	if statsOn && len(window) > 0 {
		ids := make([]int64, 0, len(window))
		for _, p := range window {
			ids = append(ids, p.ID)
		}
		batch, _ = s.Dashboard.ProjectListStats(ids, periodHours)
	}
	out := make([]map[string]any, 0, len(window))
	for _, p := range window {
		plat := platforms.Get(p.Platform)
		row := map[string]any{
			"id": p.ID, "name": p.Name, "slug": p.Slug,
			"defaultEnvironment": p.DefaultEnvironment, "enabled": p.Enabled,
			"retentionDays": p.RetentionDays, "platform": p.Platform,
			"platformLabel": plat.Label, "createdAt": p.CreatedAt,
		}
		if statsOn && batch != nil {
			if st, ok := batch[p.ID]; ok {
				row["stats"] = map[string]any{
					"trend": st.Trend, "events": map[string]any{"current": st.Events.Current, "previous": derefInt(st.Events.Previous)},
					"new_issues": map[string]any{"current": st.NewIssues.Current, "previous": derefInt(st.NewIssues.Previous)},
					"open_issues": st.OpenIssues, "fatal_issues": st.FatalIssues, "total_events": st.TotalEvents,
				}
			}
		} else {
			legacy, _ := s.Dashboard.ProjectStats(p.ID)
			row["events7d"] = legacy.Events7d
			row["unresolved"] = legacy.Unresolved
		}
		out = append(out, row)
	}
	// Paginated envelope when the caller asked for paging/stats; legacy
	// callers (no query params) keep the bare array.
	if q.Get("page") == "" && q.Get("per_page") == "" && q.Get("query") == "" && q.Get("stats_period") == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": out, "total_count": total, "total_pages": totalPages,
		"page": page, "per_page": perPage,
	})
}

func (s *Server) apiProjectCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name               string `json:"name"`
		Slug               string `json:"slug"`
		DefaultEnvironment string `json:"defaultEnvironment"`
		Platform           string `json:"platform"`
		RetentionDays      int    `json:"retentionDays"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Slug) == "" {
		writeAPIError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	p, err := s.Projects.Create(input.Name, input.Slug, input.DefaultEnvironment, input.Platform, input.RetentionDays)
	if err != nil {
		s.Logger.Error("create project", "error", err, "slug", input.Slug)
		if isUniqueConstraintErr(err) {
			writeAPIError(w, http.StatusConflict, "slug already taken")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "could not create project")
		return
	}
	s.Notifications.UpsertDefaultRule(p.ID)
	s.audit(r, audit.ProjectCreated, "project", strconv.FormatInt(p.ID, 10), "{}")
	s.apiProjectDetailByID(w, r, p.ID)
}

func (s *Server) apiProjectDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	s.apiProjectDetailByID(w, r, id)
}

func (s *Server) apiProjectDetailByID(w http.ResponseWriter, r *http.Request, id int64) {
	p, err := s.Projects.Get(id)
	if err != nil || !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	dsn, _ := projects.DSN(s.PublicURL, p)
	stats, _ := s.Dashboard.ProjectStats(id)
	envs, _ := s.Dashboard.DistinctEnvironments(id)
	platform := platforms.Get(p.Platform)
	writeJSON(w, http.StatusOK, map[string]any{
		"project": map[string]any{
			"id": p.ID, "name": p.Name, "slug": p.Slug,
			"defaultEnvironment": p.DefaultEnvironment, "enabled": p.Enabled,
			"retentionDays": p.RetentionDays, "platform": p.Platform,
			"createdAt": p.CreatedAt,
		},
		"dsn": dsn, "stats": map[string]any{"events7d": stats.Events7d, "unresolved": stats.Unresolved},
		"environments": envs, "platformLabel": platform.Label,
		"platformInstall": platform.Install, "platformSnippet": platform.Snippet(dsn, p.DefaultEnvironment),
		"platforms": platformsJSON(),
	})
}

func (s *Server) apiProjectUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		Name               *string `json:"name"`
		DefaultEnvironment *string `json:"defaultEnvironment"`
		Platform           *string `json:"platform"`
		RetentionDays      *int    `json:"retentionDays"`
		Enabled            *bool   `json:"enabled"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	p, err := s.Projects.Get(id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	name := p.Name
	if input.Name != nil {
		name = *input.Name
	}
	defEnv := p.DefaultEnvironment
	if input.DefaultEnvironment != nil {
		defEnv = *input.DefaultEnvironment
	}
	plat := p.Platform
	if input.Platform != nil {
		plat = *input.Platform
	}
	ret := p.RetentionDays
	if input.RetentionDays != nil {
		ret = *input.RetentionDays
	}
	if input.Enabled != nil {
		if err := s.Projects.SetEnabled(id, *input.Enabled); err != nil {
			s.Logger.Error("update project enabled", "error", err)
		}
	}
	if err := s.Projects.UpdateSettings(id, name, defEnv, plat, ret); err != nil {
		s.Logger.Error("update project settings", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.ProjectUpdated, "project", strconv.FormatInt(id, 10), "{}")
	s.apiProjectDetailByID(w, r, id)
}

func (s *Server) apiProjectRotate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	key, err := s.Projects.RotateKey(id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = key
	s.audit(r, audit.ProjectKeyRotated, "project", strconv.FormatInt(id, 10), "{}")
	s.apiProjectDetailByID(w, r, id)
}

func (s *Server) apiProjectDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Projects.Delete(id); err != nil {
		s.Logger.Error("delete project", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "could not delete project")
		return
	}
	s.audit(r, audit.ProjectDeleted, "project", strconv.FormatInt(id, 10), "{}")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiPlatforms(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, platformsJSON())
}

func overviewPeriod(r *http.Request) (string, time.Time) {
	period := r.URL.Query().Get("period")
	since := time.Now().Add(-24 * time.Hour)
	switch period {
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		since = time.Now().Add(-30 * 24 * time.Hour)
	default:
		period = "24h"
	}
	return period, since
}

// apiProjectTestEvent ingests one synthetic event through the real
// pipeline and returns the grouped issue for linking.
func (s *Server) apiProjectTestEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	project, err := s.Projects.Get(id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if s.ingest == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "ingestion not configured")
		return
	}
	issueID, err := s.ingest.SendTestEvent(r.Context(), project)
	if err != nil {
		s.Logger.Error("test event failed", "error", err, "project_id", id)
		writeAPIError(w, http.StatusInternalServerError, "test event failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issueId": issueID})
}

// apiProjectReleases lists every release seen by a project with event,
// issue and session-health rollups, most recently active first.
func (s *Server) apiProjectReleases(w http.ResponseWriter, r *http.Request) {
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
	if s.Sessions == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "sessions not configured")
		return
	}
	rel, err := s.Sessions.Releases(id)
	if err != nil {
		s.Logger.Error("project releases", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, rel)
}

// apiProjectReleaseDetail renders one release: its rollup row, an
// environment breakdown, and the issues first seen while it was the
// newest release ("what broke in this release").
//
// Release names travel URL-encoded (they may contain slashes and spaces);
// the mux decodes {release} back before it arrives here. The rollup
// labels an empty release "(none)", so that name maps back to "".
func (s *Server) apiProjectReleaseDetail(w http.ResponseWriter, r *http.Request) {
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
	if s.Sessions == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "sessions not configured")
		return
	}
	raw := r.PathValue("release")
	actual := raw
	if actual == "(none)" {
		actual = ""
	}

	rels, err := s.Sessions.Releases(id)
	if err != nil {
		s.Logger.Error("project release detail", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var summary *sessions.ReleaseHealth
	for i := range rels {
		if rels[i].Release == raw {
			summary = &rels[i]
			break
		}
	}
	if summary == nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}

	// Window: from this release's first sighting until the next-newer
	// release appears. Timestamps are RFC3339, so string order is time
	// order; unparsable values fail open to the epoch (start) or unbounded
	// (end) rather than 404ing a real release.
	ordered := append([]sessions.ReleaseHealth{}, rels...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].FirstSeen < ordered[j].FirstSeen })
	start := time.Time{}
	var end *time.Time
	for i := range ordered {
		if ordered[i].Release != summary.Release {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, ordered[i].FirstSeen); err == nil {
			start = t
		}
		for _, next := range ordered {
			if next.FirstSeen > ordered[i].FirstSeen {
				if t, err := time.Parse(time.RFC3339Nano, next.FirstSeen); err == nil {
					if end == nil || t.Before(*end) {
						end = &t
					}
				}
			}
		}
		break
	}

	envRows, err := s.Sessions.ReleaseEnvironments(id, actual)
	if err != nil {
		s.Logger.Error("project release environments", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	envJSON := make([]map[string]any, 0, len(envRows))
	for _, e := range envRows {
		envJSON = append(envJSON, map[string]any{
			"environment": e.Environment, "events": e.Events, "sessions": e.Sessions,
		})
	}

	newIssues, err := s.Issues.NewSince(id, start, end, 20)
	if err != nil {
		s.Logger.Error("project release new issues", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	issueJSON := make([]map[string]any, 0, len(newIssues))
	for _, iss := range newIssues {
		issueJSON = append(issueJSON, map[string]any{
			"id": iss.ID, "title": iss.Title, "level": iss.Level,
			"status": string(iss.Status), "eventCount": iss.EventCount,
			"firstSeen":         iss.FirstSeen.UTC().Format(time.RFC3339),
			"firstSeenRelative": relativeTime(iss.FirstSeen),
			"lastSeenRelative":  relativeTime(iss.LastSeen),
			"environment":       iss.Environment,
		})
	}

	var windowEnd any
	if end != nil {
		windowEnd = end.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"release": summary.Release, "summary": summary,
		"windowStart": start.UTC().Format(time.RFC3339), "windowEnd": windowEnd,
		"environments": envJSON, "newIssues": issueJSON,
	})
}

// apiProjectSessionSummary returns crash-free rates plus the health
// timeseries for the overview tiles.
func (s *Server) apiProjectSessionSummary(w http.ResponseWriter, r *http.Request) {
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
	if s.Sessions == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "sessions not configured")
		return
	}
	period, since := overviewPeriod(r)
	sum, err := s.Sessions.CrashFree(id, "", since)
	if err != nil {
		s.Logger.Error("session summary", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	series, err := s.Sessions.HealthTimeseries(id, since)
	if err != nil {
		s.Logger.Error("session timeseries", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"period": period, "summary": sum, "timeseries": series,
	})
}

// apiProjectOverview serves the per-project bento dashboard: counters,
// severity-stacked volume and top issues for a 24h/7d/30d window.
// Latency is deliberately absent: Splat is an error tracker and does
// not collect transactions - the UI renders an honest empty state for it.
// Session health has its own endpoint below.
func (s *Server) apiProjectOverview(w http.ResponseWriter, r *http.Request) {
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
	period, since := overviewPeriod(r)

	events, err := s.Dashboard.ProjectEvents(id, since)
	if err != nil {
		s.Logger.Error("project overview events", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	newIssues, _ := s.Dashboard.NewIssues(id, since)
	stats, _ := s.Dashboard.ProjectStats(id)

	sev, _ := s.Dashboard.EventsBySeverity(id, since)
	buckets := map[string]map[string]int64{}
	var order []string
	for _, p := range sev {
		if _, ok := buckets[p.Bucket]; !ok {
			buckets[p.Bucket] = map[string]int64{}
			order = append(order, p.Bucket)
		}
		buckets[p.Bucket][p.Level] = p.Value
	}
	volume := make([]map[string]any, 0, len(order))
	for _, b := range order {
		volume = append(volume, map[string]any{"bucket": b, "counts": buckets[b]})
	}

	top, _, err := s.Issues.List(issues.ListFilter{ProjectID: id, Status: issues.StatusUnresolved, OrderBy: "event_count", Limit: 5})
	if err != nil {
		s.Logger.Error("project overview top issues", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	topJSON := make([]map[string]any, 0, len(top))
	for _, iss := range top {
		topJSON = append(topJSON, map[string]any{
			"id": iss.ID, "title": iss.Title, "level": iss.Level, "status": string(iss.Status),
			"eventCount": iss.EventCount, "lastSeen": iss.LastSeen, "lastSeenRelative": relativeTime(iss.LastSeen),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"period": period, "events": events, "newIssues": newIssues, "openIssues": stats.Unresolved,
		"volumeBySeverity": volume, "topIssues": topJSON,
	})
}

// platformsJSON exposes platforms without the Snippet func value (funcs
// do not JSON-marshal), which the frontend does not need in list form.
func platformsJSON() []map[string]string {
	out := make([]map[string]string, 0, len(platforms.All))
	for _, p := range platforms.All {
		out = append(out, map[string]string{"id": p.ID, "label": p.Label, "language": p.Language})
	}
	return out
}

// --- settings ---

func (s *Server) apiSettingsData() map[string]any {
	traceTmpl, _ := s.Tracing.GetURLTemplate()
	return map[string]any{
		"traceURLTemplate": traceTmpl,
	}
}

func (s *Server) apiSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.apiSettingsData())
}

func (s *Server) apiSettingsSaveTracing(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URLTemplate string `json:"urlTemplate"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.Tracing.SetURLTemplate(input.URLTemplate); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.SettingsSaved, "tracing", "", "{}")
	writeJSON(w, http.StatusOK, s.apiSettingsData())
}

func (s *Server) apiStorageUsage(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "storage reporting not configured")
		return
	}
	u, err := s.Storage.Usage(r.Context())
	if err != nil {
		s.Logger.Error("storage usage", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) apiStorageSweep(w http.ResponseWriter, r *http.Request) {
	if s.Retention == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "retention sweeper not configured")
		return
	}
	deleted, byProject, err := s.Retention.SweepNow(r.Context())
	if err != nil {
		s.Logger.Error("manual retention sweep", "error", err)
	}
	out := map[string]any{"deleted": deleted}
	bp := map[string]int64{}
	for id, n := range byProject {
		bp[strconv.FormatInt(id, 10)] = n
	}
	out["byProject"] = bp
	s.audit(r, audit.SettingsSaved, "storage", "", `{"op":"sweep"}`)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiStorageVacuum(w http.ResponseWriter, r *http.Request) {
	if s.Storage == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "storage reporting not configured")
		return
	}
	if err := s.Storage.Vacuum(r.Context()); err != nil {
		s.Logger.Error("vacuum", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "vacuum failed: "+err.Error())
		return
	}
	s.audit(r, audit.SettingsSaved, "storage", "", `{"op":"vacuum"}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- source maps ---

func (s *Server) apiSourceMapsList(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if s.SourceMaps == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "source maps not configured")
		return
	}
	list, err := s.SourceMaps.List(id)
	if err != nil {
		s.Logger.Error("list source maps", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// apiSourceMapsUpload accepts either a JSON body
// {release, bundleUrl, content} or a multipart form with fields
// "release", "bundleUrl" and a "file" upload.
func (s *Server) apiSourceMapsUpload(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if s.SourceMaps == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "source maps not configured")
		return
	}
	if _, err := s.Projects.Get(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "project not found")
		return
	}
	var release, bundleURL string
	var content []byte
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(sourcemaps.MaxMapBytes + (1 << 20)); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid multipart body: "+err.Error())
			return
		}
		release = r.FormValue("release")
		bundleURL = r.FormValue("bundleUrl")
		f, _, err := r.FormFile("file")
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "missing file part")
			return
		}
		defer f.Close()
		content, err = io.ReadAll(io.LimitReader(f, sourcemaps.MaxMapBytes+1))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "cannot read file: "+err.Error())
			return
		}
	} else {
		var input struct {
			Release   string `json:"release"`
			BundleURL string `json:"bundleUrl"`
			Content   string `json:"content"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, sourcemaps.MaxMapBytes+(1<<20))
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		release, bundleURL, content = input.Release, input.BundleURL, []byte(input.Content)
	}
	meta, err := s.SourceMaps.Upload(id, release, bundleURL, content)
	if err != nil {
		s.Logger.Error("upload source map", "error", err, "project_id", id)
		if errors.Is(err, sourcemaps.ErrTooLarge) || errors.Is(err, sourcemaps.ErrUnsupported) {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

func (s *Server) apiSourceMapsDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	mapID, err := strconv.ParseInt(r.PathValue("mapId"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if s.SourceMaps == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "source maps not configured")
		return
	}
	if err := s.SourceMaps.Delete(id, mapID); err != nil {
		s.Logger.Error("delete source map", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// apiOpenAPI serves the embedded OpenAPI document. Public (no auth): it
// describes the API surface without exposing any data.
func (s *Server) apiOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPIDoc)
}

// --- api keys (admin session only) ---

func apiKeyJSON(k apikeys.Key) map[string]any {
	out := map[string]any{
		"id": k.ID, "name": k.Name, "prefix": k.Prefix,
		"projectId": k.ProjectIDJSON(), "scopes": k.Scopes,
		"expiresAt": k.ExpiresAt, "revoked": k.Revoked,
		"lastUsedAt": k.LastUsed, "createdAt": k.CreatedAt,
	}
	if k.Secret != "" {
		out["secret"] = k.Secret
	}
	return out
}

func (s *Server) apiKeysList(w http.ResponseWriter, r *http.Request) {
	keys, err := s.APIKeys.List()
	if err != nil {
		s.Logger.Error("list api keys", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, apiKeyJSON(k))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiKeysCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name      string   `json:"name"`
		ProjectID *int64   `json:"projectId"`
		Scopes    []string `json:"scopes"`
		ExpiresAt string   `json:"expiresAt"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	var projectID sql.NullInt64
	if input.ProjectID != nil {
		if _, err := s.Projects.Get(*input.ProjectID); err != nil {
			writeAPIError(w, http.StatusBadRequest, "unknown project")
			return
		}
		projectID = sql.NullInt64{Int64: *input.ProjectID, Valid: true}
	}
	k, err := s.APIKeys.Create(input.Name, projectID, input.Scopes, input.ExpiresAt)
	if err != nil {
		s.Logger.Error("create api key", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.APIKeyChanged, "api_key", strconv.FormatInt(k.ID, 10), `{"op":"create"}`)
	writeJSON(w, http.StatusCreated, apiKeyJSON(k))
}

func (s *Server) apiKeysRotate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	k, err := s.APIKeys.Rotate(id)
	if err != nil {
		s.Logger.Error("rotate api key", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.APIKeyChanged, "api_key", strconv.FormatInt(id, 10), `{"op":"rotate"}`)
	writeJSON(w, http.StatusOK, apiKeyJSON(k))
}

func (s *Server) apiKeysRevoke(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.APIKeys.Revoke(id); err != nil {
		s.Logger.Error("revoke api key", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.APIKeyChanged, "api_key", strconv.FormatInt(id, 10), `{"op":"revoke"}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiKeysDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.APIKeys.Delete(id); err != nil {
		s.Logger.Error("delete api key", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.APIKeyChanged, "api_key", strconv.FormatInt(id, 10), `{"op":"delete"}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// resolveIssueEvent returns the requested event (?event=id) or the latest
// event for the issue when no explicit id is given.
func (s *Server) resolveIssueEvent(issueID int64, eventID string) (*events.Event, error) {
	if eventID != "" {
		return s.Events.Get(eventID)
	}
	return s.Events.Latest(issueID)
}
