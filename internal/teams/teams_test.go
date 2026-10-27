package teams

import (
	"path/filepath"
	"testing"

	"github.com/philippdormann/splat/internal/auth"
	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/projects"
)

func testDB(t *testing.T) (*database.DB, *auth.Store, *Store, int64) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := auth.NewStore(db, "test-session-secret")
	if _, err := a.CreateUser("admin", "password123", auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	pstore := projects.NewStore(db)
	p, err := pstore.Create("App", "app", "production", "python", 0)
	if err != nil {
		t.Fatal(err)
	}
	return db, a, NewStore(db), p.ID
}

func TestDirectAndTeamAccess(t *testing.T) {
	_, a, tm, projectID := testDB(t)
	member, err := a.CreateUser("dev", "password123", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := tm.CanAccess(member.ID, projectID); ok {
		t.Fatal("member without grants must not have access")
	}
	if err := tm.AddProjectMember(projectID, member.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := tm.CanAccess(member.ID, projectID); !ok {
		t.Fatal("direct grant must give access")
	}
	if err := tm.RemoveProjectMember(projectID, member.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := tm.CanAccess(member.ID, projectID); ok {
		t.Fatal("revoked direct grant must remove access")
	}

	team, err := tm.Create("Backend", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tm.AddMember(team.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := tm.CanAccess(member.ID, projectID); ok {
		t.Fatal("team membership alone must not grant project access")
	}
	if err := tm.GrantProject(team.ID, projectID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := tm.CanAccess(member.ID, projectID); !ok {
		t.Fatal("team grant must give access")
	}
	got, err := tm.AccessibleProjects(member.ID)
	if err != nil || len(got) != 1 || got[0] != projectID {
		t.Fatalf("accessible = %v, %v", got, err)
	}
	if err := tm.RevokeProject(team.ID, projectID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := tm.CanAccess(member.ID, projectID); ok {
		t.Fatal("revoked team grant must remove access")
	}
}

func TestAuthRolesAndGuards(t *testing.T) {
	_, a, _, _ := testDB(t)
	member, err := a.CreateUser("dev", "password123", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.RoleOf(member.ID); got != auth.RoleMember {
		t.Fatalf("role = %q", got)
	}
	if _, err := a.CreateUser("dev", "password123", auth.RoleMember); err == nil {
		t.Fatal("duplicate username must fail")
	}
	if err := a.SetRole(member.ID, "superuser"); err == nil {
		t.Fatal("invalid role must fail")
	}
	// No admin can be demoted, even when other admins exist.
	admins, _ := a.ListUsers()
	var adminID int64
	for _, u := range admins {
		if u.Role == auth.RoleAdmin {
			adminID = u.ID
		}
	}
	if err := a.SetRole(adminID, auth.RoleMember); err == nil {
		t.Fatal("demoting an admin must fail")
	}
	// Promoting a member still works.
	if err := a.SetRole(member.ID, auth.RoleAdmin); err != nil {
		t.Fatalf("promoting a member must work: %v", err)
	}
	if err := a.SetRole(member.ID, auth.RoleAdmin); err != nil {
		t.Fatalf("re-setting the same role must work: %v", err)
	}
	// Cannot delete the only remaining users down to zero.
	if err := a.DeleteUser(member.ID); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteUser(adminID); err == nil {
		t.Fatal("deleting the last user must fail")
	}
}
