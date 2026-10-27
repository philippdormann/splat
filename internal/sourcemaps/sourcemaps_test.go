package sourcemaps

import (
	"path/filepath"
	"strings"
	"testing"

	"errgotrack/internal/database"
	"errgotrack/internal/projects"
)

// minimal valid v3 map: one line, one segment mapping gen col 0 to
// sources[0] line 0 col 0 ("AAAA" = four zero fields).
const testMap = `{"version":3,"sources":["app.ts"],"sourcesContent":["line1\nline2\nline3\nline4\nline5"],"names":["main"],"mappings":"AAAAA"}`

func TestVLQRoundTrip(t *testing.T) {
	// "AAAAA" decodes to five zeros; "C" decodes to 1; "D" to -1.
	nums, err := decodeVLQ("AAAAA")
	if err != nil || len(nums) != 5 {
		t.Fatalf("decode AAAAA = %v, %v", nums, err)
	}
	for _, n := range nums {
		if n != 0 {
			t.Fatalf("want zeros, got %v", nums)
		}
	}
	nums, _ = decodeVLQ("C")
	if len(nums) != 1 || nums[0] != 1 {
		t.Fatalf("decode C = %v, want [1]", nums)
	}
	nums, _ = decodeVLQ("D")
	if len(nums) != 1 || nums[0] != -1 {
		t.Fatalf("decode D = %v, want [-1]", nums)
	}
	if _, err := decodeVLQ("A$"); err == nil {
		t.Fatal("want error on bad base64 char")
	}
}

func TestLookup(t *testing.T) {
	m, err := parseMap([]byte(testMap))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	loc, ok := m.lookup(1, 0)
	if !ok {
		t.Fatal("expected mapping at 1:0")
	}
	if loc.Source != "app.ts" || loc.Line != 1 || loc.Column != 0 {
		t.Fatalf("wrong loc: %+v", loc)
	}
	if loc.Function != "main" {
		t.Fatalf("function = %q, want main", loc.Function)
	}
	if len(loc.Context) == 0 {
		t.Fatal("expected sourcesContent context")
	}
	if _, ok := m.lookup(2, 0); ok {
		t.Fatal("line 2 has no segments, want unmapped")
	}
	if _, err := parseMap([]byte(`{"version":2}`)); err == nil {
		t.Fatal("want error for non-v3 map")
	}
}

func TestMatchLen(t *testing.T) {
	full := "https://cdn.example.com/static/app.min.js"
	if matchLen(full, full) <= len(full) {
		t.Fatal("exact match should outrank suffix match")
	}
	if matchLen(full, "/static/app.min.js") < 0 {
		t.Fatal("want suffix match")
	}
	if matchLen(full, "x/static/app.min.js") >= 0 {
		t.Fatal("suffix must start on a path boundary")
	}
	if matchLen(full, "other.js") >= 0 {
		t.Fatal("want no match")
	}
	if stripQuery(full+"?v=2#x") != full {
		t.Fatal("stripQuery failed")
	}
}

func setup(t *testing.T) (*Store, int64) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	p, err := projects.NewStore(db).Create("P", "p", "production", "", 90)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return NewStore(db), p.ID
}

func TestUploadListDelete(t *testing.T) {
	s, pid := setup(t)
	if _, err := s.Upload(pid, "1.0", "", []byte(testMap)); err == nil {
		t.Fatal("want error on empty bundle URL")
	}
	if _, err := s.Upload(pid, "1.0", "app.min.js", []byte("not json")); err == nil {
		t.Fatal("want error on invalid map")
	}
	big := make([]byte, MaxMapBytes+1)
	if _, err := s.Upload(pid, "1.0", "app.min.js", big); err == nil {
		t.Fatal("want error on oversized map")
	}
	meta, err := s.Upload(pid, "1.0", "https://cdn.example.com/app.min.js", []byte(testMap))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if meta.SizeBytes != int64(len(testMap)) {
		t.Fatalf("size = %d", meta.SizeBytes)
	}
	// Re-upload replaces.
	if _, err := s.Upload(pid, "1.0", "https://cdn.example.com/app.min.js", []byte(testMap)); err != nil {
		t.Fatalf("re-upload: %v", err)
	}
	list, err := s.List(pid)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	// Resolution through the store: suffix match on bundle URL.
	loc, ok := s.Resolve(pid, "1.0", "https://cdn.example.com/app.min.js?v=9", 1, 5)
	if !ok || loc.Source != "app.ts" || loc.Line != 1 {
		t.Fatalf("resolve = %+v, %v", loc, ok)
	}
	// Unknown release falls back to nothing here (only 1.0 uploaded)...
	if _, ok := s.Resolve(pid, "9.9", "https://cdn.example.com/app.min.js", 1, 0); ok {
		t.Fatal("want unmapped for unknown release")
	}
	// ...but an empty-release map acts as the fallback.
	if _, err := s.Upload(pid, "", "https://cdn.example.com/app.min.js", []byte(testMap)); err != nil {
		t.Fatalf("upload fallback: %v", err)
	}
	if _, ok := s.Resolve(pid, "9.9", "https://cdn.example.com/app.min.js", 1, 0); !ok {
		t.Fatal("want fallback to empty-release map")
	}
	if err := s.Delete(pid, meta.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = s.List(pid)
	if len(list) != 1 { // only the empty-release map remains
		t.Fatalf("list after delete = %d, want 1", len(list))
	}
	if !strings.HasPrefix(list[0].Release, "") {
		t.Fatal("unreachable")
	}
}
