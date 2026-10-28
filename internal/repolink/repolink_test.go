package repolink

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/philippdormann/splat/internal/database"
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
	return NewStore(db, "test-session-secret-32-bytes-minimum!!"), 1
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

func TestTokenEncryptedAtRest(t *testing.T) {
	s, projectID := testStore(t)
	const secret = "ghp_super-secret-token"
	if _, err := s.Save(projectID, "github", "", "octo", "app", secret); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := s.db.QueryRow(`SELECT token FROM project_repos WHERE project_id = ?`, projectID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, secret) {
		t.Fatalf("forge token stored in plaintext: %q", raw)
	}
	if !strings.HasPrefix(raw, "enc:v1:") {
		t.Fatalf("forge token not sealed: %q", raw)
	}
	got, err := s.Token(projectID)
	if err != nil || got != secret {
		t.Fatalf("token = %q, %v", got, err)
	}
	cfg, err := s.Config(projectID)
	if err != nil || cfg.Token != secret {
		t.Fatalf("config token = %+v, %v", cfg, err)
	}
	// Blank-token save keeps the sealed secret and still decrypts.
	if _, err := s.Save(projectID, "github", "", "octo", "app2", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Token(projectID); got != secret {
		t.Fatalf("keep token = %q", got)
	}
}

func TestLegacyPlaintextTokenMigrates(t *testing.T) {
	s, projectID := testStore(t)
	if _, err := s.db.Exec(`INSERT INTO project_repos (project_id, provider, base_url, owner, repo, token, updated_at)
		VALUES (?, 'github', '', 'o', 'r', 'legacy-tok', '2024-01-01T00:00:00Z')
		ON CONFLICT (project_id) DO UPDATE SET token = excluded.token`, projectID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Token(projectID); err != nil || got != "legacy-tok" {
		t.Fatalf("legacy token = %q, %v", got, err)
	}
	if n, err := s.MigratePlaintextSecrets(); err != nil || n != 1 {
		t.Fatalf("migrate = %d, %v", n, err)
	}
	var raw string
	if err := s.db.QueryRow(`SELECT token FROM project_repos WHERE project_id = ?`, projectID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "enc:v1:") || strings.Contains(raw, "legacy-tok") {
		t.Fatalf("not sealed after migrate: %q", raw)
	}
}
