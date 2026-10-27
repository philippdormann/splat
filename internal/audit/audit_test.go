package audit

import (
	"path/filepath"
	"testing"

	"github.com/philippdormann/splat/internal/database"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

func TestRecordAndList(t *testing.T) {
	s := testStore(t)
	if err := s.Record(7, "admin", Login, "user", "7", `{"via":"password"}`, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(0, "ghost", LoginFailed, "user", "", "{}", "10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	entries, total, err := s.List(50, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(entries) != 2 {
		t.Fatalf("total = %d", total)
	}
	// Newest first.
	if entries[0].Action != LoginFailed || entries[0].ActorUserID != 0 || entries[0].ActorUsername != "ghost" {
		t.Fatalf("newest = %+v", entries[0])
	}
	if entries[1].Action != Login || entries[1].ActorUserID != 7 || entries[1].IP != "127.0.0.1" {
		t.Fatalf("oldest = %+v", entries[1])
	}
}

func TestListFilterAndPaging(t *testing.T) {
	s := testStore(t)
	for i := range 5 {
		action := UserCreated
		if i%2 == 0 {
			action = Login
		}
		if err := s.Record(1, "admin", action, "user", "1", "{}", ""); err != nil {
			t.Fatal(err)
		}
	}
	entries, total, err := s.List(50, 0, "auth.")
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(entries) != 3 {
		t.Fatalf("filtered total = %d", total)
	}
	entries, total, err = s.List(2, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || len(entries) != 2 {
		t.Fatalf("paged = %d/%d", total, len(entries))
	}
	entries, _, err = s.List(500, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Fatalf("capped list = %d", len(entries))
	}
}
