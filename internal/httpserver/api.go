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

	"errgotrack/internal/apikeys"
	"errgotrack/internal/auth"
	"errgotrack/internal/dashboard"
	"errgotrack/internal/events"
	"errgotrack/internal/issues"
	"errgotrack/internal/notifications"
	"errgotrack/internal/platforms"
	"errgotrack/internal/projects"
	"errgotrack/internal/sentryprotocol"
	"errgotrack/internal/sourcemaps"
	"errgotrack/internal/tracing"
	"errgotrack/internal/webhooks"
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

func decodeJSON(r *http.Request, v any) bool {
	if r.Body == nil {
		return false
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v) == nil
}

// --- auth ---

func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	token := auth.SessionTokenFromRequest(r)
	if _, ok := s.Auth.ValidateSession(token); !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
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
	userID, err := s.Auth.Authenticate(input.Username, input.Password)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, expires, err := s.Auth.CreateSession(userID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	auth.SetSessionCookie(w, token, expires, s.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiLogout(w http.ResponseWriter, r *http.Request) {
	s.Auth.DeleteSession(auth.SessionTokenFromRequest(r))
	auth.ClearSessionCookie(w)
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

	stats, err := s.Dashboard.Stats(since, previousSince)
	if err != nil {
		s.Logger.Error("dashboard stats", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	projectList, _ := s.Projects.List()

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
		"hasProjects": len(projectList) > 0,
	}

	charts := map[string]any{}
	if eventsOverTime, _ := s.Dashboard.EventsOverTime(since); eventsOverTime != nil {
		charts["eventsOverTime"] = pointsJSON(eventsOverTime)
	}
	if byProject, _ := s.Dashboard.ByProject(since); byProject != nil {
		charts["byProject"] = pointsJSON(byProject)
	}
	if topExceptions, _ := s.Dashboard.TopExceptionTypes(since); topExceptions != nil {
		charts["topExceptions"] = pointsJSON(topExceptions)
	}
	if byEnv, _ := s.Dashboard.ByEnvironment(since); byEnv != nil {
		charts["byEnvironment"] = pointsJSON(byEnv)
	}
	if newCount, recurringCount, _ := s.Dashboard.NewVsRecurring(since); true {
		charts["newVsRecurring"] = map[string]any{"new": newCount, "recurring": recurringCount}
	}
	if byStatus, _ := s.Dashboard.IssuesByStatus(since); byStatus != nil {
		charts["byStatus"] = pointsJSON(byStatus)
	}
	out["charts"] = charts

	var recent []map[string]any
	if recents, _ := s.Dashboard.RecentIssues(10); recents != nil {
		for _, ri := range recents {
			recent = append(recent, map[string]any{
				"id": ri.ID, "title": ri.Title, "level": ri.Level,
				"projectName": ri.ProjectName, "environment": ri.Environment,
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
	if !s.authenticate(r).inProject(issue.ProjectID) {
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
		"traceId":        traceID,
		"traceURL":       tracing.BuildURL(traceTmpl, traceID),
	})
}

func (s *Server) apiIssueSetStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if !decodeJSON(r, &input) {
		// Also accept form posts for curl convenience.
		_ = r.ParseForm()
		input.Status = r.FormValue("status")
	}
	status := issues.Status(strings.ToLower(input.Status))
	if status != issues.StatusResolved && status != issues.StatusIgnored && status != issues.StatusUnresolved {
		writeAPIError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if issue, err := s.Issues.Get(id); err != nil || !s.authenticate(r).inProject(issue.ProjectID) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Issues.SetStatus(id, status); err != nil {
		s.Logger.Error("set issue status", "error", err)
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
	if status != issues.StatusResolved && status != issues.StatusIgnored && status != issues.StatusUnresolved {
		writeAPIError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if err := s.Issues.BulkSetStatus(input.IDs, status); err != nil {
		s.Logger.Error("bulk set status", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func issueJSON(iss *issues.Issue) map[string]any {
	return map[string]any{
		"id": iss.ID, "title": iss.Title,
		"exceptionType": iss.ExceptionType, "exceptionValue": iss.ExceptionValue,
		"culprit": iss.Culprit, "level": iss.Level, "status": string(iss.Status),
		"eventCount": iss.EventCount, "userCount": iss.UserCount,
		"firstSeen": iss.FirstSeen, "lastSeen": iss.LastSeen,
		"environment": iss.Environment, "release": iss.Release,
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

func (s *Server) apiProjectsList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Projects.List()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// A project-bound key sees only its own project, not the fleet.
	if a := s.authenticate(r); !a.admin && a.key != nil && a.key.ProjectID.Valid {
		filtered := list[:0]
		for _, p := range list {
			if p.ID == a.key.ProjectID.Int64 {
				filtered = append(filtered, p)
			}
		}
		list = filtered
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		stats, _ := s.Dashboard.ProjectStats(p.ID)
		out = append(out, map[string]any{
			"id": p.ID, "name": p.Name, "slug": p.Slug,
			"defaultEnvironment": p.DefaultEnvironment, "enabled": p.Enabled,
			"retentionDays": p.RetentionDays, "platformLabel": platforms.Get(p.Platform).Label,
			"events7d": stats.Events7d, "unresolved": stats.Unresolved,
		})
	}
	writeJSON(w, http.StatusOK, out)
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
	if err != nil || !s.authenticate(r).inProject(id) {
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiPlatforms(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, platformsJSON())
}

// platformsJSON exposes platforms without the Snippet func value (funcs
// do not JSON-marshal), which the frontend does not need in list form.
func platformsJSON() []map[string]string {
	out := make([]map[string]string, 0, len(platforms.All))
	for _, p := range platforms.All {
		out = append(out, map[string]string{"id": p.ID, "label": p.Label})
	}
	return out
}

// --- settings ---

func (s *Server) apiSettingsData() map[string]any {
	cfg, _ := s.Notifications.GetTelegramConfig()
	traceTmpl, _ := s.Tracing.GetURLTemplate()
	webhooks := map[string]any{}
	for _, channel := range webhookChannels {
		wc, _ := s.Notifications.GetWebhookConfig(channel)
		webhooks[channel] = map[string]any{"urlMasked": maskToken(wc.URL), "enabled": wc.Enabled}
	}
	smtp, _ := s.Notifications.GetSMTPConfig()
	generic, _ := s.Notifications.GenericWebhooks()
	genericJSON := make([]map[string]any, 0, len(generic))
	for _, h := range generic {
		genericJSON = append(genericJSON, genericWebhookJSON(h))
	}
	return map[string]any{
		"telegram":       map[string]any{"chatId": cfg.ChatID, "enabled": cfg.Enabled},
		"botTokenMasked": maskToken(cfg.BotToken),
		"webhooks":       webhooks,
		"smtp": map[string]any{
			"host": smtp.Host, "port": smtp.Port, "username": smtp.Username,
			"passwordMasked": maskToken(smtp.Password), "from": smtp.From, "to": smtp.To,
			"useTLS": smtp.UseTLS, "enabled": smtp.Enabled,
		},
		"traceURLTemplate": traceTmpl,
		"genericWebhooks":  genericJSON,
	}
}

func (s *Server) apiSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.apiSettingsData())
}

func (s *Server) apiSettingsSaveTelegram(w http.ResponseWriter, r *http.Request) {
	var input struct {
		BotToken string `json:"botToken"`
		ChatID   string `json:"chatId"`
		Enabled  bool   `json:"enabled"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	cfg, _ := s.Notifications.GetTelegramConfig()
	if input.BotToken != "" && !strings.Contains(input.BotToken, "•") {
		cfg.BotToken = input.BotToken
	}
	cfg.ChatID = input.ChatID
	cfg.Enabled = input.Enabled
	if err := s.Notifications.SetTelegramConfig(cfg); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, s.apiSettingsData())
}

func (s *Server) apiSettingsTestTelegram(w http.ResponseWriter, r *http.Request) {
	err := s.Notifications.SendTest(r.Context())
	msg := "Test notification sent."
	ok := true
	if err != nil {
		msg = "Failed to send: " + err.Error()
		ok = false
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "message": msg})
}

func (s *Server) apiSettingsSaveWebhook(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	if !isWebhookChannel(channel) {
		writeAPIError(w, http.StatusNotFound, "unknown channel")
		return
	}
	var input struct {
		URL     string `json:"url"`
		Enabled bool   `json:"enabled"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	cfg, _ := s.Notifications.GetWebhookConfig(channel)
	if input.URL != "" && !strings.Contains(input.URL, "•") {
		if err := webhooks.ValidateURL(input.URL); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Not saved: "+err.Error())
			return
		}
		cfg.URL = input.URL
	}
	cfg.Enabled = input.Enabled
	if err := s.Notifications.SetWebhookConfig(channel, cfg); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, s.apiSettingsData())
}

func (s *Server) apiSettingsTestWebhook(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	if !isWebhookChannel(channel) {
		writeAPIError(w, http.StatusNotFound, "unknown channel")
		return
	}
	err := s.Notifications.SendTestWebhook(r.Context(), channel)
	msg := "Test notification sent."
	ok := true
	if err != nil {
		msg = "Failed to send: " + err.Error()
		ok = false
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "message": msg})
}

func genericWebhookJSON(w notifications.GenericWebhook) map[string]any {
	return map[string]any{
		"id": w.ID, "name": w.Name, "url": w.URL,
		"hasSecret": w.Secret != "", "headers": w.Headers, "enabled": w.Enabled,
	}
}

func (s *Server) apiGenericWebhooksList(w http.ResponseWriter, r *http.Request) {
	hooks, err := s.Notifications.GenericWebhooks()
	if err != nil {
		s.Logger.Error("list generic webhooks", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, genericWebhookJSON(h))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiGenericWebhooksCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name    string            `json:"name"`
		URL     string            `json:"url"`
		Secret  string            `json:"secret"`
		Headers map[string]string `json:"headers"`
		Enabled bool              `json:"enabled"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if input.URL != "" {
		if err := webhooks.ValidateURL(input.URL); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Not saved: "+err.Error())
			return
		}
	}
	h, err := s.Notifications.CreateGenericWebhook(notifications.GenericWebhook{
		Name: input.Name, URL: input.URL, Secret: input.Secret,
		Headers: input.Headers, Enabled: input.Enabled,
	})
	if err != nil {
		s.Logger.Error("create generic webhook", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, genericWebhookJSON(h))
}

func (s *Server) apiGenericWebhooksUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		Name    string            `json:"name"`
		URL     string            `json:"url"`
		Secret  string            `json:"secret"`
		Headers map[string]string `json:"headers"`
		Enabled bool              `json:"enabled"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if input.URL != "" {
		if err := webhooks.ValidateURL(input.URL); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Not saved: "+err.Error())
			return
		}
	}
	// Blank secret keeps the stored one (mirrors the bot-token convention).
	hooks, err := s.Notifications.GenericWebhooks()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	secret := input.Secret
	for _, h := range hooks {
		if h.ID == id && secret == "" {
			secret = h.Secret
		}
	}
	h, err := s.Notifications.UpdateGenericWebhook(notifications.GenericWebhook{
		ID: id, Name: input.Name, URL: input.URL, Secret: secret,
		Headers: input.Headers, Enabled: input.Enabled,
	})
	if err != nil {
		s.Logger.Error("update generic webhook", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, genericWebhookJSON(h))
}

func (s *Server) apiGenericWebhooksDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Notifications.DeleteGenericWebhook(id); err != nil {
		s.Logger.Error("delete generic webhook", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiGenericWebhooksTest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	err = s.Notifications.SendTestGeneric(r.Context(), id)
	msg := "Test notification sent."
	ok := true
	if err != nil {
		msg = "Failed to send: " + err.Error()
		ok = false
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "message": msg})
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
	writeJSON(w, http.StatusOK, s.apiSettingsData())
}

func (s *Server) apiSettingsSaveSMTP(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		From     string `json:"from"`
		To       string `json:"to"`
		UseTLS   bool   `json:"useTLS"`
		Enabled  bool   `json:"enabled"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	cfg, _ := s.Notifications.GetSMTPConfig()
	cfg.Host, cfg.Username, cfg.From, cfg.To = input.Host, input.Username, input.From, input.To
	if input.Port > 0 {
		cfg.Port = input.Port
	}
	// A masked password submitted unchanged means "keep the existing
	// secret" - only overwrite on a real new value.
	if input.Password != "" && !strings.Contains(input.Password, "•") {
		cfg.Password = input.Password
	}
	cfg.UseTLS, cfg.Enabled = input.UseTLS, input.Enabled
	if err := s.Notifications.SetSMTPConfig(cfg); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, s.apiSettingsData())
}

func (s *Server) apiSettingsTestSMTP(w http.ResponseWriter, r *http.Request) {
	err := s.Notifications.SendTestEmail(r.Context())
	msg := "Test notification sent."
	ok := true
	if err != nil {
		msg = "Failed to send: " + err.Error()
		ok = false
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "message": msg})
}

// --- alert rules ---

func ruleJSON(r notifications.Rule) map[string]any {
	var projectID any
	if r.ProjectID.Valid {
		projectID = r.ProjectID.Int64
	}
	return map[string]any{
		"id": r.ID, "name": r.Name, "projectId": projectID,
		"environment": r.Environment, "notifyNewIssue": r.NotifyNewIssue,
		"notifyThreshold": r.NotifyThreshold, "notifyReopened": r.NotifyReopened,
		"cooldownSeconds": r.CooldownSeconds, "channels": r.Channels,
	}
}

func (s *Server) apiRulesList(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Notifications.Rules()
	if err != nil {
		s.Logger.Error("list rules", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, ruleJSON(rule))
	}
	writeJSON(w, http.StatusOK, out)
}

type ruleInput struct {
	Name            string   `json:"name"`
	ProjectID       *int64   `json:"projectId"`
	Environment     string   `json:"environment"`
	NotifyNewIssue  bool     `json:"notifyNewIssue"`
	NotifyThreshold int64    `json:"notifyThreshold"`
	NotifyReopened  bool     `json:"notifyReopened"`
	CooldownSeconds int      `json:"cooldownSeconds"`
	Channels        []string `json:"channels"`
}

func (s *Server) apiRulesCreate(w http.ResponseWriter, r *http.Request) {
	var input ruleInput
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	rule, err := s.Notifications.CreateRule(input.toRule(0))
	if err != nil {
		s.Logger.Error("create rule", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ruleJSON(rule))
}

func (s *Server) apiRulesUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input ruleInput
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	rule, err := s.Notifications.UpdateRule(input.toRule(id))
	if err != nil {
		s.Logger.Error("update rule", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ruleJSON(rule))
}

func (s *Server) apiRulesDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Notifications.DeleteRule(id); err != nil {
		s.Logger.Error("delete rule", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (i ruleInput) toRule(id int64) notifications.Rule {
	r := notifications.Rule{
		ID: id, Name: i.Name, Environment: i.Environment,
		NotifyNewIssue: i.NotifyNewIssue, NotifyThreshold: i.NotifyThreshold,
		NotifyReopened: i.NotifyReopened, CooldownSeconds: i.CooldownSeconds,
		Channels: i.Channels,
	}
	if i.ProjectID != nil {
		r.ProjectID = sql.NullInt64{Int64: *i.ProjectID, Valid: true}
	}
	return r
}

// --- storage ---

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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- source maps ---

func (s *Server) apiSourceMapsList(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.authenticate(r).inProject(id) {
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
	if !s.authenticate(r).inProject(id) {
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
		if !decodeJSON(r, &input) {
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
	if !s.authenticate(r).inProject(id) {
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
