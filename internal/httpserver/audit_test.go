package httpserver

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/philippdormann/splat/internal/auth"
)

func auditEntries(t *testing.T, s *Server, cookie, query string) (int, []map[string]any) {
	t.Helper()
	rec := get(t, s, "/api/v1/audit-log"+query, cookie)
	if rec.Code != 200 {
		t.Fatalf("audit-log = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Entries []map[string]any `json:"entries"`
		Total   int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Total, body.Entries
}

func TestAuditLogRecordsAdminActions(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	adminCookie := sessionCookie(t, a, admin.ID)

	rec := postJSON(t, s, http.MethodPost, "/api/v1/users", adminCookie, `{"username":"mallory","password":"password123","role":"member"}`)
	if rec.Code != 201 {
		t.Fatalf("create user = %d", rec.Code)
	}
	total, entries := auditEntries(t, s, adminCookie, "")
	if total == 0 || len(entries) == 0 {
		t.Fatal("audit log is empty after admin action")
	}
	newest := entries[0]
	if newest["action"] != "users.created" || newest["actorUsername"] != "admin" {
		t.Fatalf("newest = %v", newest)
	}
	total, _ = auditEntries(t, s, adminCookie, "?action=users.")
	if total != 1 {
		t.Fatalf("filtered total = %d", total)
	}
	total, _ = auditEntries(t, s, adminCookie, "?action=auth.")
	if total != 0 {
		t.Fatalf("auth filter total = %d", total)
	}
}

func TestAuditLogAuthEvents(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	adminCookie := sessionCookie(t, a, admin.ID)

	// Failed password login names the claimed user with no actor id.
	rec := postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"admin","password":"wrongpassword"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d", rec.Code)
	}
	_, entries := auditEntries(t, s, adminCookie, "?action=auth.login_failed")
	if len(entries) != 1 || entries[0]["actorUsername"] != "admin" || entries[0]["actorUserId"].(float64) != 0 {
		t.Fatalf("failed login entry = %v", entries)
	}
	// Successful login records the actor (the "auth.login" prefix also
	// matches the earlier auth.login_failed entry).
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"admin","password":"password123"}`)
	if rec.Code != 200 {
		t.Fatalf("login = %d", rec.Code)
	}
	_, entries = auditEntries(t, s, adminCookie, "?action=auth.login")
	if len(entries) != 2 || entries[0]["action"] != "auth.login" || entries[0]["actorUsername"] != "admin" {
		t.Fatalf("login entries = %v", entries)
	}
}

func TestAuditLogAdminOnly(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	member, _ := a.CreateUser("dev", "password123", auth.RoleMember)
	_ = admin
	memberCookie := sessionCookie(t, a, member.ID)

	if rec := get(t, s, "/api/v1/audit-log", memberCookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("member audit-log = %d, want 401", rec.Code)
	}
	if rec := get(t, s, "/api/v1/audit-log", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon audit-log = %d, want 401", rec.Code)
	}
}
