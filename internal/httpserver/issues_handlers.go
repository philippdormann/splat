package httpserver

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"errgotrack/internal/events"
	"errgotrack/internal/issues"
	"errgotrack/internal/sentryprotocol"
	"errgotrack/internal/tracing"
)

type filterParams struct {
	Query       string
	ProjectID   int64
	Environment string
	Status      string
	OrderBy     string
	Page        int
}

func parseFilter(r *http.Request) filterParams {
	q := r.URL.Query()
	f := filterParams{
		Query:       q.Get("q"),
		Environment: q.Get("environment"),
		Status:      q.Get("status"),
		OrderBy:     q.Get("sort"),
		Page:        1,
	}
	if id, err := strconv.ParseInt(q.Get("project"), 10, 64); err == nil {
		f.ProjectID = id
	}
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		f.Page = p
	}
	return f
}

func (f filterParams) queryBase() string {
	v := url.Values{}
	if f.Query != "" {
		v.Set("q", f.Query)
	}
	if f.ProjectID != 0 {
		v.Set("project", strconv.FormatInt(f.ProjectID, 10))
	}
	if f.Environment != "" {
		v.Set("environment", f.Environment)
	}
	if f.Status != "" {
		v.Set("status", f.Status)
	}
	if f.OrderBy != "" {
		v.Set("sort", f.OrderBy)
	}
	encoded := v.Encode()
	if encoded != "" {
		encoded += "&"
	}
	return encoded
}

type issueRow struct {
	ID               int64
	Title            string
	Culprit          string
	Level            string
	ProjectName      string
	Environment      string
	EventCount       int64
	UserCount        int64
	LastSeenRelative string
	Sparkline        template.HTML
}

// sparklineSVG renders 24 hourly counts as a tiny bar chart, Bugsink-style:
// a quick at-a-glance trend per issue row without pulling in Chart.js for
// what could be 50 canvases on one page. Bars scale to the row's own max,
// not shared across rows, so shape is comparable but height is not.
func sparklineSVG(counts []int64) template.HTML {
	if len(counts) == 0 {
		return ""
	}
	const barW, gap, h = 2, 1, 20
	var max int64
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	var bars strings.Builder
	for i, c := range counts {
		x := i * (barW + gap)
		barH := 1.5
		if max > 0 {
			barH = 1.5 + float64(c)/float64(max)*float64(h-2)
		}
		fmt.Fprintf(&bars, `<rect x="%d" y="%.1f" width="%d" height="%.1f"/>`, x, float64(h)-barH, barW, barH)
	}
	width := len(counts)*(barW+gap) - gap
	return template.HTML(fmt.Sprintf(
		`<svg width="%d" height="%d" viewBox="0 0 %d %d" class="sparkline" aria-hidden="true">%s</svg>`,
		width, h, width, h, bars.String()))
}

type pageLink struct {
	Page  int
	Label string
}

type issuesTableData struct {
	PageBase
	Issues     []issueRow
	Total      int
	TotalPages int
	PageLinks  []pageLink
	QueryBase  string
}

type issuesListPageData struct {
	issuesTableData
	Filter       filterParams
	Projects     []*projectRow
	Environments []string
}

const issuesPerPage = 50

func (s *Server) buildIssuesTable(w http.ResponseWriter, r *http.Request, f filterParams) issuesTableData {
	list, total, err := s.Issues.List(issues.ListFilter{
		ProjectID:   f.ProjectID,
		Environment: f.Environment,
		Status:      issues.Status(f.Status),
		Query:       f.Query,
		OrderBy:     f.OrderBy,
		Limit:       issuesPerPage,
		Offset:      (f.Page - 1) * issuesPerPage,
	})
	if err != nil {
		s.Logger.Error("list issues", "error", err)
	}

	projectNames := s.projectNameCache()
	ids := make([]int64, len(list))
	for i, iss := range list {
		ids[i] = iss.ID
	}
	sparklines, err := s.Issues.Sparkline24h(ids)
	if err != nil {
		s.Logger.Error("load sparklines", "error", err)
	}

	rows := make([]issueRow, 0, len(list))
	for _, iss := range list {
		rows = append(rows, issueRow{
			ID: iss.ID, Title: iss.Title, Culprit: iss.Culprit, Level: iss.Level,
			ProjectName: projectNames[iss.ProjectID], Environment: iss.Environment,
			EventCount: iss.EventCount, UserCount: iss.UserCount, LastSeenRelative: relativeTime(iss.LastSeen),
			Sparkline: sparklineSVG(sparklines[iss.ID]),
		})
	}

	totalPages := (total + issuesPerPage - 1) / issuesPerPage
	if totalPages < 1 {
		totalPages = 1
	}
	var links []pageLink
	for p := 1; p <= totalPages; p++ {
		links = append(links, pageLink{Page: p, Label: strconv.Itoa(p)})
	}

	return issuesTableData{
		PageBase:   s.pageBase(w, r, "issues"),
		Issues:     rows,
		Total:      total,
		TotalPages: totalPages,
		PageLinks:  links,
		QueryBase:  f.queryBase(),
	}
}

func (s *Server) issuesList(w http.ResponseWriter, r *http.Request) {
	f := parseFilter(r)
	table := s.buildIssuesTable(w, r, f)

	if r.Header.Get("HX-Request") == "true" {
		s.render.Fragment(w, "issues_list", "issues_table", table)
		return
	}

	allProjects, err := s.Projects.List()
	if err != nil {
		s.Logger.Error("list projects", "error", err)
	}
	dash := s.Dashboard
	envs, _ := dash.DistinctEnvironments(f.ProjectID)

	data := issuesListPageData{
		issuesTableData: table,
		Filter:          f,
		Environments:    envs,
	}
	for _, p := range allProjects {
		data.Projects = append(data.Projects, &projectRow{ID: p.ID, Name: p.Name})
	}
	s.render.Page(w, "issues_list", data)
}

func (s *Server) issuesBulk(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	status := issues.Status(r.URL.Query().Get("status"))
	if status != issues.StatusResolved && status != issues.StatusIgnored && status != issues.StatusUnresolved {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	var ids []int64
	for _, idStr := range r.Form["ids"] {
		if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		if err := s.Issues.BulkSetStatus(ids, status); err != nil {
			s.Logger.Error("bulk status update", "error", err)
		}
	}
	f := parseFilter(r)
	s.render.Fragment(w, "issues_list", "issues_table", s.buildIssuesTable(w, r, f))
}

type issueDetailPageData struct {
	PageBase
	Issue       *issues.Issue
	Project     *projectRow
	Event       eventView
	Frames      []frameView
	Tags        []keyValue
	User        *userView
	SDK         *sdkView
	Request     *requestView
	Breadcrumbs []breadcrumbView
	Contexts    []contextView
	Extra       []keyValue
	TraceID     string
	TraceURL    string
	PrevEventID string
	NextEventID string
}

type eventView struct {
	EventID     string
	Environment string
	Release     string
	Timestamp   string
	Transaction string
}

// frameView precomputes a stack frame's source context as numbered lines
// (the SDK sends pre_context/post_context as separate arrays with no line
// numbers attached - this stitches them into one ordered, numbered list
// since html/template can't do that arithmetic itself) plus its captured
// local variables, when the SDK included them.
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

type breadcrumbView struct {
	Timestamp string // formatted display string, or "" if the breadcrumb had none
	Category  string
	Level     string
	Message   string
	Data      []keyValue
}

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

func (s *Server) issueDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	issue, err := s.Issues.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	project, err := s.Projects.Get(issue.ProjectID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var ev *events.Event
	if eventID := r.URL.Query().Get("event"); eventID != "" {
		ev, err = s.Events.Get(eventID)
	} else {
		ev, err = s.Events.Latest(issue.ID)
	}
	if err != nil {
		s.render.Page(w, "issue_detail", issueDetailPageData{PageBase: s.pageBase(w, r, "issues"), Issue: issue, Project: &projectRow{ID: project.ID, Name: project.Name}})
		return
	}

	var excValues sentryprotocol.ExceptionValues
	json.Unmarshal([]byte(ev.StacktraceJSON), &excValues)
	var frames []sentryprotocol.Frame
	if len(excValues.Values) > 0 {
		if st := excValues.Values[len(excValues.Values)-1].Stacktrace; st != nil {
			frames = st.Frames
		}
	}

	var tags map[string]string
	json.Unmarshal([]byte(ev.TagsJSON), &tags)
	var extra map[string]any
	json.Unmarshal([]byte(ev.ExtraJSON), &extra)

	var crumbs sentryprotocol.BreadcrumbValues
	json.Unmarshal([]byte(ev.BreadcrumbsJSON), &crumbs)
	// Chronological order: only reorder pairs where both sides have a
	// parseable timestamp, so breadcrumbs lacking one (rare - SDKs always
	// timestamp these) keep their original SDK-sent relative order instead
	// of being arbitrarily placed by a fabricated time.
	sort.SliceStable(crumbs.Values, func(i, j int) bool {
		ti, oki := crumbs.Values[i].ParsedTimestamp()
		tj, okj := crumbs.Values[j].ParsedTimestamp()
		if !oki || !okj {
			return false
		}
		return ti.Before(tj)
	})
	var crumbViews []breadcrumbView
	for _, c := range crumbs.Values {
		bv := breadcrumbView{Category: c.Category, Level: c.Level, Message: c.Message, Data: sortedStringMap(flattenToStrings(c.Data), true)}
		if t, ok := c.ParsedTimestamp(); ok {
			bv.Timestamp = t.Format("15:04:05.000")
		}
		crumbViews = append(crumbViews, bv)
	}

	prevEvent, _ := s.Events.Neighbor(issue.ID, ev.Timestamp, "prev")
	nextEvent, _ := s.Events.Neighbor(issue.ID, ev.Timestamp, "next")

	contexts := parseContexts(ev.ContextsJSON)
	traceID := traceIDFromContexts(contexts)
	traceURLTemplate, _ := s.Tracing.GetURLTemplate()

	data := issueDetailPageData{
		PageBase: s.pageBase(w, r, "issues"),
		Issue:    issue,
		Project:  &projectRow{ID: project.ID, Name: project.Name},
		Event: eventView{
			EventID: ev.EventID, Environment: ev.Environment, Release: ev.Release,
			Timestamp:   ev.Timestamp.Format("2006-01-02 15:04:05 MST"),
			Transaction: ev.TransactionName,
		},
		Frames:      buildFrameViews(frames),
		Tags:        sortedStringMap(tags, true),
		User:        buildUserView(ev.UserJSON),
		SDK:         buildSDKView(ev.SDKJSON),
		Request:     buildRequestView(ev.RequestJSON),
		Breadcrumbs: crumbViews,
		Contexts:    contexts,
		Extra:       sortedAnyMap(extra, true),
		TraceID:     traceID,
		TraceURL:    tracing.BuildURL(traceURLTemplate, traceID),
	}
	if prevEvent != nil {
		data.PrevEventID = prevEvent.EventID
	}
	if nextEvent != nil {
		data.NextEventID = nextEvent.EventID
	}
	s.render.Page(w, "issue_detail", data)
}

func (s *Server) issueSetStatus(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	status := issues.Status(r.FormValue("status"))
	if status != issues.StatusResolved && status != issues.StatusIgnored && status != issues.StatusUnresolved {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	if err := s.Issues.SetStatus(id, status); err != nil {
		s.Logger.Error("set issue status", "error", err)
	}
	http.Redirect(w, r, fmt.Sprintf("/issues/%d", id), http.StatusSeeOther)
}

// projectNameCache gives the issue list a cheap id->name lookup without a
// join for every row rendered - the project count is small (a handful per
// VPS deployment), so one List() call per request is plenty.
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

type projectRow struct {
	ID   int64
	Name string
}
