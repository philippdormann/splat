package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"errgotrack/internal/apikeys"
	"errgotrack/internal/auth"
	"errgotrack/internal/dashboard"
	"errgotrack/internal/database"
	"errgotrack/internal/events"
	"errgotrack/internal/issues"
	"errgotrack/internal/notifications"
	"errgotrack/internal/projects"
	"errgotrack/internal/retention"
	"errgotrack/internal/sessions"
	"errgotrack/internal/sourcemaps"
	"errgotrack/internal/storage"
	"errgotrack/internal/teams"
	"errgotrack/internal/tracing"
)

// fullTestServer wires every store the RBAC paths touch.
func fullTestServer(t *testing.T) (*Server, *auth.Store, *teams.Store, *projects.Store) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "rbac.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	authStore := auth.NewStore(db)
	teamsStore := teams.NewStore(db)
	projectsStore := projects.NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := New(&Server{
		Auth:          authStore,
		Projects:      projectsStore,
		Issues:        issues.NewStore(db),
		Events:        events.NewStore(db),
		Notifications: notifications.NewStore(db, logger),
		Dashboard:     dashboard.NewStore(db),
		Tracing:       tracing.NewStore(db),
		Retention:     retention.NewSweeper(db, logger),
		Storage:       storage.NewStore(db, ""),
		SourceMaps:    sourcemaps.NewStore(db),
		Sessions:      sessions.NewStore(db),
		APIKeys:       apikeys.NewStore(db),
		Teams:         teamsStore,
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
	return "errgotrack_session=" + token
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
	_ = tm
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
