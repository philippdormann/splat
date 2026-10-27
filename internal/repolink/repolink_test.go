package repolink

import (
	"path/filepath"
	"testing"

	"errgotrack/internal/database"
)

func testStore(t *testing.T) (*Store, int64) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "repolink.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO projects (name, slug, public_key) VALUES ('t', 't', 'k')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO issues (project_id, fingerprint, title, first_seen, last_seen) VALUES (1, 'fp', 't', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	return NewStore(db), 1
}

func TestRepoSaveGetDelete(t *testing.T) {
	s, projectID := testStore(t)
	if r, err := s.Get(projectID); err != nil || r != nil {
		t.Fatalf("get empty = %v, %v", r, err)
	}
	if _, err := s.Save(projectID, "github", "", "octo", "app", "tok"); err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(projectID)
	if err != nil || r == nil || r.Owner != "octo" || !r.HasToken {
		t.Fatalf("get = %+v, %v", r, err)
	}
	// Blank token keeps the stored secret.
	if _, err := s.Save(projectID, "github", "", "octo", "app2", ""); err != nil {
		t.Fatal(err)
	}
	token, _ := s.Token(projectID)
	if token != "tok" {
		t.Fatalf("token = %q", token)
	}
	if _, err := s.Save(projectID, "nope", "", "o", "r", "t"); err == nil {
		t.Fatal("unknown provider must fail")
	}
	if err := s.Delete(projectID); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.Get(projectID); r != nil {
		t.Fatalf("get after delete = %+v", r)
	}
}

func TestLinksCRUD(t *testing.T) {
	s, _ := testStore(t)
	links, err := s.Links(1)
	if err != nil || len(links) != 0 {
		t.Fatalf("links = %v, %v", links, err)
	}
	l1, err := s.AddLink(1, "github", "o/r", 12, "https://github.com/o/r/issues/12", "Bug")
	if err != nil || l1.ID == 0 {
		t.Fatalf("add = %+v, %v", l1, err)
	}
	// Duplicate add is idempotent.
	l2, err := s.AddLink(1, "github", "o/r", 12, "https://github.com/o/r/issues/12", "Bug")
	if err != nil || l2.ID != l1.ID {
		t.Fatalf("duplicate add = %+v, %v", l2, err)
	}
	links, _ = s.Links(1)
	if len(links) != 1 {
		t.Fatalf("links = %v", links)
	}
	if err := s.RemoveLink(1, l1.ID); err != nil {
		t.Fatal(err)
	}
	links, _ = s.Links(1)
	if len(links) != 0 {
		t.Fatalf("links after remove = %v", links)
	}
	// Scoped removal: another issue's id must not remove this issue's link.
	l3, _ := s.AddLink(1, "github", "o/r", 13, "u", "Bug")
	if err := s.RemoveLink(999, l3.ID); err != nil {
		t.Fatal(err)
	}
	links, _ = s.Links(1)
	if len(links) != 1 {
		t.Fatalf("cross-issue remove must not delete, got %v", links)
	}
}
