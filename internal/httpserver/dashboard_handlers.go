package httpserver

import (
	"html/template"
	"net/http"
	"time"
)

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

type dashboardPageData struct {
	PageBase
	Range       string
	Ranges      []rangeOption
	HasData     bool
	HasProjects bool
	Stats   struct {
		TotalEvents      int64
		UnresolvedIssues int64
		NewIssues        int64
		AffectedProjects int64
		EventsToday      int64
		UniqueIssues     int64
		EventsDeltaPct   float64
		EventsDeltaLabel string
	}
	ChartEventsOverTime template.JS
	ChartByProject      template.JS
	ChartTopExceptions  template.JS
	ChartByEnvironment  template.JS
	ChartNewVsRecurring template.JS
	ChartByStatus       template.JS
	RecentIssues        []dashboardRecentIssue
}

type dashboardRecentIssue struct {
	ID               int64
	Title            string
	Level            string
	ProjectName      string
	Environment      string
	EventCount       int64
	LastSeenRelative string
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
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

	dash := s.Dashboard
	stats, err := dash.Stats(since, previousSince)
	if err != nil {
		s.Logger.Error("dashboard stats", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	projectList, _ := s.Projects.List()

	data := dashboardPageData{
		PageBase:    s.pageBase(w, r, "dashboard"),
		Range:       rangeValue,
		Ranges:      dashboardRanges,
		HasData:     stats.TotalEvents > 0 || stats.UnresolvedIssues > 0,
		HasProjects: len(projectList) > 0,
	}
	data.Stats.TotalEvents = stats.TotalEvents
	data.Stats.UnresolvedIssues = stats.UnresolvedIssues
	data.Stats.NewIssues = stats.NewIssues
	data.Stats.AffectedProjects = stats.AffectedProjects
	data.Stats.EventsToday = stats.EventsToday
	data.Stats.UniqueIssues = stats.UniqueIssues
	data.Stats.EventsDeltaPct = stats.EventsDeltaPct
	data.Stats.EventsDeltaLabel = stats.EventsDeltaLabel

	if data.HasData {
		eventsOverTime, _ := dash.EventsOverTime(since)
		byProject, _ := dash.ByProject(since)
		topExceptions, _ := dash.TopExceptionTypes(since)
		byEnv, _ := dash.ByEnvironment(since)
		newCount, recurringCount, _ := dash.NewVsRecurring(since)
		byStatus, _ := dash.IssuesByStatus(since)

		data.ChartEventsOverTime = lineChart(eventsOverTime)
		data.ChartByProject = barChart(byProject)
		data.ChartTopExceptions = barChart(topExceptions)
		data.ChartByEnvironment = doughnutChart(byEnv)
		data.ChartNewVsRecurring = newVsRecurringChart(newCount, recurringCount)
		data.ChartByStatus = issuesByStatusChart(byStatus)

		recent, _ := dash.RecentIssues(10)
		for _, ri := range recent {
			data.RecentIssues = append(data.RecentIssues, dashboardRecentIssue{
				ID: ri.ID, Title: ri.Title, Level: ri.Level, ProjectName: ri.ProjectName,
				Environment: ri.Environment, EventCount: ri.EventCount, LastSeenRelative: relativeTime(ri.LastSeen),
			})
		}
	}

	s.render.Page(w, "dashboard", data)
}
