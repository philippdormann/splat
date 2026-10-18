package httpserver

import (
	"net/http"
	"strconv"
	"strings"

	"errgotrack/internal/dashboard"
	"errgotrack/internal/platforms"
	"errgotrack/internal/projects"
)

// isUniqueConstraintErr reports whether err came from violating a SQLite
// UNIQUE constraint (e.g. a duplicate slug), as opposed to some other
// failure (lock contention, disk error, ...) that deserves its own message
// instead of being misreported as "slug already taken".
func isUniqueConstraintErr(err error) bool {
	return strings.Contains(strings.ToUpper(err.Error()), "UNIQUE CONSTRAINT")
}

type projectListItem struct {
	ID                 int64
	Name               string
	Slug               string
	DefaultEnvironment string
	Enabled            bool
	RetentionDays      int
	PlatformLabel      string
	CreatedAt          string
}

type projectsListPageData struct {
	PageBase
	Projects []projectListItem
}

func toListItem(p *projects.Project) projectListItem {
	return projectListItem{
		ID: p.ID, Name: p.Name, Slug: p.Slug, DefaultEnvironment: p.DefaultEnvironment,
		Enabled: p.Enabled, RetentionDays: p.RetentionDays, PlatformLabel: platforms.Get(p.Platform).Label,
		CreatedAt: p.CreatedAt.Format("2006-01-02"),
	}
}

func (s *Server) projectsList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Projects.List()
	if err != nil {
		s.Logger.Error("list projects", "error", err)
	}
	data := projectsListPageData{PageBase: s.pageBase(w, r, "projects")}
	for _, p := range list {
		data.Projects = append(data.Projects, toListItem(p))
	}
	s.render.Page(w, "projects_list", data)
}

type projectFormData struct {
	PageBase
	Error     string
	Platforms []platforms.Platform
}

func (s *Server) projectNewPage(w http.ResponseWriter, r *http.Request) {
	s.render.Page(w, "project_new", projectFormData{PageBase: s.pageBase(w, r, "projects"), Platforms: platforms.All})
}

func (s *Server) projectCreate(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	name := r.FormValue("name")
	slug := r.FormValue("slug")
	defaultEnv := r.FormValue("default_environment")
	platform := r.FormValue("platform")
	retention, _ := strconv.Atoi(r.FormValue("retention_days"))

	if name == "" || slug == "" {
		s.render.Page(w, "project_new", projectFormData{PageBase: s.pageBase(w, r, "projects"), Error: "Name and slug are required.", Platforms: platforms.All})
		return
	}
	p, err := s.Projects.Create(name, slug, defaultEnv, platform, retention)
	if err != nil {
		s.Logger.Error("create project", "error", err, "slug", slug)
		errMsg := "Could not create project - is the slug already taken?"
		if !isUniqueConstraintErr(err) {
			errMsg = "Could not create project - an internal error occurred, check server logs."
		}
		s.render.Page(w, "project_new", projectFormData{PageBase: s.pageBase(w, r, "projects"), Error: errMsg, Platforms: platforms.All})
		return
	}
	s.Notifications.UpsertDefaultRule(p.ID)
	http.Redirect(w, r, "/projects/"+strconv.FormatInt(p.ID, 10), http.StatusSeeOther)
}

type projectDetailPageData struct {
	PageBase
	Project         *projects.Project
	DSN             string
	Stats           dashboard.ProjectStats
	Environments    []string
	Platforms       []platforms.Platform
	Platform        platforms.Platform
	PlatformInstall string
	PlatformSnippet string
}

func (s *Server) projectDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.Projects.Get(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dsn, _ := projects.DSN(s.PublicURL, p)
	stats, _ := s.Dashboard.ProjectStats(id)
	envs, _ := s.Dashboard.DistinctEnvironments(id)
	platform := platforms.Get(p.Platform)

	s.render.Page(w, "project_detail", projectDetailPageData{
		PageBase: s.pageBase(w, r, "projects"), Project: p, DSN: dsn, Stats: stats, Environments: envs,
		Platforms: platforms.All, Platform: platform, PlatformInstall: platform.Install, PlatformSnippet: platform.Snippet(dsn, p.DefaultEnvironment),
	})
}

func (s *Server) projectUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	enabled := r.FormValue("enabled") == "on"
	if err := s.Projects.SetEnabled(id, enabled); err != nil {
		s.Logger.Error("update project enabled", "error", err)
	}
	retention, _ := strconv.Atoi(r.FormValue("retention_days"))
	if err := s.Projects.UpdateSettings(id, r.FormValue("name"), r.FormValue("default_environment"), r.FormValue("platform"), retention); err != nil {
		s.Logger.Error("update project settings", "error", err)
	}
	http.Redirect(w, r, "/projects/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) projectRotate(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.Projects.RotateKey(id); err != nil {
		s.Logger.Error("rotate project key", "error", err)
	}
	http.Redirect(w, r, "/projects/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) projectDelete(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.Projects.Delete(id); err != nil {
		s.Logger.Error("delete project", "error", err)
		http.Error(w, "could not delete project - check server logs", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}
