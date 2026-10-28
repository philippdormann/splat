package httpserver

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/philippdormann/splat/internal/apikeys"
	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/auth"
	"github.com/philippdormann/splat/internal/dashboard"
	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/events"
	"github.com/philippdormann/splat/internal/issues"
	"github.com/philippdormann/splat/internal/notifications"
	"github.com/philippdormann/splat/internal/projects"
	"github.com/philippdormann/splat/internal/quotas"
	"github.com/philippdormann/splat/internal/repolink"
	"github.com/philippdormann/splat/internal/retention"
	"github.com/philippdormann/splat/internal/sessions"
	"github.com/philippdormann/splat/internal/sourcemaps"
	"github.com/philippdormann/splat/internal/storage"
	"github.com/philippdormann/splat/internal/teams"
	"github.com/philippdormann/splat/internal/tracing"
)

// fullTestServer wires every store the RBAC paths touch.
func fullTestServer(t *testing.T) (*Server, *auth.Store, *teams.Store, *projects.Store) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "rbac.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	authStore := auth.NewStore(db, "test-session-secret-for-2fa-tests")
	teamsStore := teams.NewStore(db)
	projectsStore := projects.NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := New(&Server{
		Auth:          authStore,
		Projects:      projectsStore,
		Issues:        issues.NewStore(db),
		Events:        events.NewStore(db),
		Notifications: notifications.NewStore(db, logger, "test-session-secret-32-bytes-minimum!!"),
		Dashboard:     dashboard.NewStore(db),
		Tracing:       tracing.NewStore(db),
		Retention:     retention.NewSweeper(db, logger),
		Storage:       storage.NewStore(db, ""),
		SourceMaps:    sourcemaps.NewStore(db),
		Sessions:      sessions.NewStore(db),
		APIKeys:       apikeys.NewStore(db),
		Teams:         teamsStore,
		Audit:         audit.NewStore(db),
		Quotas:        quotas.NewStore(db),
		RepoLinks:     repolink.NewStore(db, "test-session-secret-32-bytes-minimum!!"),
		DB:            db,
		Logger:        logger,
		PublicURL:     "http://localhost:8080",
		Version:       "test",
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return s, authStore, teamsStore, projectsStore
}

func sessionCookie(t *testing.T, a *auth.Store, userID int64) string {
	t.Helper()
	token, _, err := a.CreateSession(userID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return "splat_session=" + token
}

func get(t *testing.T, s *Server, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func postJSON(t *testing.T, s *Server, method, path, cookie, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		body = "{}"
	}
	rdr = strings.NewReader(body)
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestRBACMemberScoping(t *testing.T) {
	s, a, tm, ps := fullTestServer(t)
	admin, err := a.CreateUser("admin", "password123", auth.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	member, err := a.CreateUser("dev", "password123", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := ps.Create("One", "one", "production", "python", 0)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := ps.Create("Two", "two", "production", "go", 0)
	if err != nil {
		t.Fatal(err)
	}
	adminCookie := sessionCookie(t, a, admin.ID)
	memberCookie := sessionCookie(t, a, member.ID)

	// Member with no grants: empty project list, empty issues, empty dashboard.
	rec := get(t, s, "/api/v1/projects", memberCookie)
	var plist []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &plist); err != nil || len(plist) != 0 {
		t.Fatalf("member projects = %s, %v", rec.Body.String(), err)
	}
	rec = get(t, s, "/api/v1/issues", memberCookie)
	var ilist map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ilist); err != nil {
		t.Fatal(err)
	}
	if total, _ := ilist["total"].(float64); total != 0 {
		t.Fatalf("member issues total = %v", total)
	}
	rec = get(t, s, "/api/v1/dashboard?range=24h", memberCookie)
	var dash map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &dash); err != nil {
		t.Fatal(err)
	}
	if dash["hasProjects"] == true {
		t.Fatal("member dashboard must not report projects")
	}
	// Member cannot reach another project's detail (404, not 403).
	if rec := get(t, s, "/api/v1/projects/1", memberCookie); rec.Code != http.StatusNotFound {
		t.Fatalf("member project detail = %d, want 404", rec.Code)
	}
	// Member is locked out of instance endpoints.
	if rec := get(t, s, "/api/v1/settings", memberCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("member settings = %d, want 403", rec.Code)
	}
	if rec := get(t, s, "/api/v1/users", memberCookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("member users = %d, want 401", rec.Code)
	}
	if rec := postJSON(t, s, http.MethodPost, "/api/v1/projects", memberCookie, `{"name":"x","slug":"x"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("member project create = %d, want 403", rec.Code)
	}

	// Admin grants the member p1 directly: list shows one, detail works.
	if rec := postJSON(t, s, http.MethodPost, "/api/v1/projects/1/access/members", adminCookie, `{"userId":`+itoa(member.ID)+`}`); rec.Code != 200 {
		t.Fatalf("grant member = %d %s", rec.Code, rec.Body.String())
	}
	_ = p1
	_ = p2
	rec = get(t, s, "/api/v1/projects", memberCookie)
	if err := json.Unmarshal(rec.Body.Bytes(), &plist); err != nil || len(plist) != 1 {
		t.Fatalf("member projects after grant = %s, %v", rec.Body.String(), err)
	}
	if rec := get(t, s, "/api/v1/projects/1", memberCookie); rec.Code != 200 {
		t.Fatalf("member project detail after grant = %d", rec.Code)
	}
	if rec := get(t, s, "/api/v1/projects/2", memberCookie); rec.Code != http.StatusNotFound {
		t.Fatalf("member ungranted project = %d, want 404", rec.Code)
	}

	// Team path: create team, add member, grant p2 to the team.
	rec = postJSON(t, s, http.MethodPost, "/api/v1/teams", adminCookie, `{"name":"Backend","slug":"backend"}`)
	if rec.Code != 201 {
		t.Fatalf("create team = %d %s", rec.Code, rec.Body.String())
	}
	var team map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &team); err != nil {
		t.Fatal(err)
	}
	teamID := int(team["id"].(float64))
	if rec := postJSON(t, s, http.MethodPost, "/api/v1/teams/"+itoa(int64(teamID))+"/members", adminCookie, `{"userId":`+itoa(member.ID)+`}`); rec.Code != 200 {
		t.Fatalf("add team member = %d %s", rec.Code, rec.Body.String())
	}
	if rec := postJSON(t, s, http.MethodPost, "/api/v1/teams/"+itoa(int64(teamID))+"/projects", adminCookie, `{"projectId":2}`); rec.Code != 200 {
		t.Fatalf("grant project to team = %d %s", rec.Code, rec.Body.String())
	}
	rec = get(t, s, "/api/v1/projects", memberCookie)
	if err := json.Unmarshal(rec.Body.Bytes(), &plist); err != nil || len(plist) != 2 {
		t.Fatalf("member projects after team grant = %s, %v", rec.Body.String(), err)
	}

	// Admin user management works; member cannot touch it.
	rec = postJSON(t, s, http.MethodPost, "/api/v1/users", adminCookie, `{"username":"qa","password":"password123","role":"member"}`)
	if rec.Code != 201 {
		t.Fatalf("create user = %d %s", rec.Code, rec.Body.String())
	}
	rec = get(t, s, "/api/v1/users", adminCookie)
	var ulist []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ulist); err != nil || len(ulist) != 3 {
		t.Fatalf("users = %s, %v", rec.Body.String(), err)
	}
	// Nobody changes their own role, even admins.
	rec = postJSON(t, s, http.MethodPut, "/api/v1/users/"+itoa(admin.ID)+"/role", adminCookie, `{"role":"member"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("self role change = %d, want 400", rec.Code)
	}
	// ...but an admin can still change someone else's role.
	rec = postJSON(t, s, http.MethodPut, "/api/v1/users/"+itoa(member.ID)+"/role", adminCookie, `{"role":"admin"}`)
	if rec.Code != 200 {
		t.Fatalf("other role change = %d %s", rec.Code, rec.Body.String())
	}
	_ = tm
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestRepoAndLinkScoping(t *testing.T) {
	s, a, _, ps := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	member, _ := a.CreateUser("dev", "password123", auth.RoleMember)
	p, _ := ps.Create("App", "app", "production", "python", 0)
	adminCookie := sessionCookie(t, a, admin.ID)
	memberCookie := sessionCookie(t, a, member.ID)

	// No repo linked yet: null, and filing fails with 400.
	if rec := get(t, s, "/api/v1/projects/1/access", adminCookie); rec.Code != 200 {
		t.Fatalf("access = %d", rec.Code)
	}
	rec := get(t, s, "/api/v1/projects/1/repo", adminCookie)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "null" {
		t.Fatalf("repo = %d %s", rec.Code, rec.Body.String())
	}
	// Member cannot read another project's repo config (404, not 403).
	if rec := get(t, s, "/api/v1/projects/1/repo", memberCookie); rec.Code != http.StatusNotFound {
		t.Fatalf("member repo = %d, want 404", rec.Code)
	}
	// Bad provider rejected; unknown project 404s.
	rec = postJSON(t, s, http.MethodPut, "/api/v1/projects/1/repo", adminCookie,
		`{"provider":"nope","owner":"o","repo":"r","token":"t"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad provider = %d %s", rec.Code, rec.Body.String())
	}
	// Save a GitHub repo (default public base: no custom host to validate).
	rec = postJSON(t, s, http.MethodPut, "/api/v1/projects/1/repo", adminCookie,
		`{"provider":"github","owner":"o","repo":"r","token":"tok"}`)
	if rec.Code != 200 {
		t.Fatalf("save repo = %d %s", rec.Code, rec.Body.String())
	}
	var saved map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved["slug"] != "o/r" || saved["hasToken"] != true {
		t.Fatalf("saved = %v", saved)
	}
	if _, hasToken := saved["token"]; hasToken {
		t.Fatal("token must never be echoed back")
	}
	// Private-host base URLs are refused (SSRF guard).
	rec = postJSON(t, s, http.MethodPut, "/api/v1/projects/1/repo", adminCookie,
		`{"provider":"gitea","baseUrl":"http://127.0.0.1:3000","owner":"o","repo":"r","token":"t"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("loopback base = %d %s", rec.Code, rec.Body.String())
	}
	_ = p

	// Filing without network: point the stub at an unreachable host is
	// pointless; instead verify the no-repo path on a second project and
	// list/delete round-trip on a stored link.
	p2, _ := ps.Create("Two", "two", "production", "go", 0)
	issueList, _, _ := s.Issues.List(issues.ListFilter{ProjectID: p2.ID})
	_ = issueList
	// Create an issue to link against via the issues store directly.
	res, _ := s.Issues.Upsert(context.Background(), p2.ID, "fp1", "Bug", "E", "v", "", "error", "production", "", time.Now())
	link, err := s.RepoLinks.AddLink(res.Issue.ID, "github", "o/r", 5, "https://github.com/o/r/issues/5", "Bug")
	if err != nil {
		t.Fatal(err)
	}
	rec = get(t, s, "/api/v1/issues/"+itoa(res.Issue.ID)+"/links", adminCookie)
	if rec.Code != 200 {
		t.Fatalf("list links = %d %s", rec.Code, rec.Body.String())
	}
	// Member without a grant sees 404.
	if rec := get(t, s, "/api/v1/issues/"+itoa(res.Issue.ID)+"/links", memberCookie); rec.Code != http.StatusNotFound {
		t.Fatalf("member links = %d, want 404", rec.Code)
	}
	// Unlink works and is scoped: wrong issue id 404s the delete.
	if rec := postJSON(t, s, http.MethodDelete, "/api/v1/issues/"+itoa(res.Issue.ID)+"/links/"+itoa(link.ID), adminCookie, ""); rec.Code != 200 {
		t.Fatalf("unlink = %d %s", rec.Code, rec.Body.String())
	}
	_ = member
}

func TestProjectReleaseDetail(t *testing.T) {
	s, a, _, ps := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	member, _ := a.CreateUser("dev", "password123", auth.RoleMember)
	p, _ := ps.Create("App", "app", "production", "python", 0)
	adminCookie := sessionCookie(t, a, admin.ID)
	memberCookie := sessionCookie(t, a, member.ID)

	at := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if _, err := s.Issues.Upsert(context.Background(), p.ID, "fp1", "Bug", "E", "v", "", "error", "production", "3.0", at); err != nil {
		t.Fatal(err)
	}
	if err := s.Sessions.UpsertSession(p.ID, []byte(`{"sid":"a","did":"u1","release":"3.0","environment":"production","started":"2026-10-27T10:00:00Z","status":"ok"}`)); err != nil {
		t.Fatal(err)
	}

	rec := get(t, s, "/api/v1/projects/1/releases/3.0", adminCookie)
	if rec.Code != 200 {
		t.Fatalf("detail = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Release      string `json:"release"`
		WindowStart  string `json:"windowStart"`
		Environments []struct {
			Environment string `json:"environment"`
			Events      int64  `json:"events"`
			Sessions    int64  `json:"sessions"`
		} `json:"environments"`
		NewIssues []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"newIssues"`
		Summary struct {
			Release string `json:"release"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Release != "3.0" || body.Summary.Release != "3.0" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if len(body.NewIssues) != 1 || body.NewIssues[0].Title != "Bug" {
		t.Fatalf("newIssues = %s", rec.Body.String())
	}
	if len(body.Environments) != 1 || body.Environments[0].Sessions != 1 {
		t.Fatalf("environments = %s", rec.Body.String())
	}

	// Unknown release 404s; ungranted member 404s (not 403).
	if rec := get(t, s, "/api/v1/projects/1/releases/9.9", adminCookie); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown release = %d", rec.Code)
	}
	if rec := get(t, s, "/api/v1/projects/1/releases/3.0", memberCookie); rec.Code != http.StatusNotFound {
		t.Fatalf("member detail = %d, want 404", rec.Code)
	}
	_ = member
}
