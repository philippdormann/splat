package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/philippdormann/splat/internal/auth"
)

func quotaEnvelope(eventID string) string {
	return "{\"event_id\":\"" + eventID + "\"}\n{\"type\":\"event\"}\n" +
		"{\"event_id\":\"" + eventID + "\",\"level\":\"error\",\"message\":\"boom\"}\n"
}

func postEnvelope(t *testing.T, s *Server, projectID int64, key, eventID string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/"+itoa(projectID)+"/envelope/", strings.NewReader(quotaEnvelope(eventID)))
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+key)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Header().Get("Retry-After")
}

func jsonUnmarshal(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

func TestQuotaRejectsOverLimitIngestion(t *testing.T) {
	s, a, _, ps := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	adminCookie := sessionCookie(t, a, admin.ID)
	p, err := ps.Create("Quota", "quota", "production", "go", 0)
	if err != nil {
		t.Fatal(err)
	}

	// Baseline: no overrides, ingestion accepted.
	if code, _ := postEnvelope(t, s, p.ID, p.PublicKey, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); code != 200 {
		t.Fatalf("first envelope = %d", code)
	}
	// Cap at 1/hour: the stored event trips the next request.
	rec := postJSON(t, s, http.MethodPut, "/api/v1/projects/"+itoa(p.ID)+"/quota", adminCookie, `{"hourly":1}`)
	if rec.Code != 200 {
		t.Fatalf("set quota = %d %s", rec.Code, rec.Body.String())
	}
	code, retryAfter := postEnvelope(t, s, p.ID, p.PublicKey, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if code != http.StatusTooManyRequests {
		t.Fatalf("over-quota envelope = %d, want 429", code)
	}
	if retryAfter == "" {
		t.Fatal("429 must carry Retry-After")
	}
	// Usage endpoint reflects the breach.
	rec = get(t, s, "/api/v1/projects/"+itoa(p.ID)+"/quota", adminCookie)
	var q struct {
		Hourly       int  `json:"hourly"`
		UsedHour     int  `json:"usedHour"`
		BreachedHour bool `json:"breachedHour"`
	}
	jsonUnmarshal(t, rec.Body.Bytes(), &q)
	if q.Hourly != 1 || q.UsedHour != 1 || !q.BreachedHour {
		t.Fatalf("quota = %+v", q)
	}
	// Lifting the cap recovers ingestion.
	rec = postJSON(t, s, http.MethodPut, "/api/v1/projects/"+itoa(p.ID)+"/quota", adminCookie, `{"hourly":0}`)
	if rec.Code != 200 {
		t.Fatalf("lift quota = %d", rec.Code)
	}
	if code, _ := postEnvelope(t, s, p.ID, p.PublicKey, "cccccccccccccccccccccccccccccccc"); code != 200 {
		t.Fatalf("envelope after lift = %d", code)
	}
}

func TestQuotaEndpointsAuthAndValidation(t *testing.T) {
	s, a, _, ps := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	member, _ := a.CreateUser("dev", "password123", auth.RoleMember)
	adminCookie := sessionCookie(t, a, admin.ID)
	memberCookie := sessionCookie(t, a, member.ID)
	p, _ := ps.Create("Q", "q", "production", "go", 0)

	// Member without grant: 404, not 403.
	if rec := get(t, s, "/api/v1/projects/"+itoa(p.ID)+"/quota", memberCookie); rec.Code != http.StatusNotFound {
		t.Fatalf("member quota = %d, want 404", rec.Code)
	}
	// Member cannot save (admin-only), admin can.
	if rec := postJSON(t, s, http.MethodPut, "/api/v1/projects/"+itoa(p.ID)+"/quota", memberCookie, `{"hourly":5}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("member save = %d, want 401", rec.Code)
	}
	if rec := postJSON(t, s, http.MethodPut, "/api/v1/projects/"+itoa(p.ID)+"/quota", adminCookie, `{"hourly":-1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative quota = %d, want 400", rec.Code)
	}
	// null inherits the default.
	rec := postJSON(t, s, http.MethodPut, "/api/v1/projects/"+itoa(p.ID)+"/quota", adminCookie, `{"hourly":null,"daily":100}`)
	var q struct {
		Hourly         int  `json:"hourly"`
		Daily          int  `json:"daily"`
		HourlyOverride *int `json:"hourlyOverride"`
		DailyOverride  *int `json:"dailyOverride"`
	}
	jsonUnmarshal(t, rec.Body.Bytes(), &q)
	if rec.Code != 200 || q.Hourly != 5000 || q.Daily != 100 || q.HourlyOverride != nil || *q.DailyOverride != 100 {
		t.Fatalf("quota = %d %+v", rec.Code, q)
	}
	// Instance defaults: member locked out, admin round-trip works.
	if rec := get(t, s, "/api/v1/settings/quotas", memberCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("member defaults = %d, want 403", rec.Code)
	}
	rec = postJSON(t, s, http.MethodPut, "/api/v1/settings/quotas", adminCookie, `{"defaultHourly":10,"defaultDaily":20}`)
	var d struct {
		DefaultHourly int `json:"defaultHourly"`
		DefaultDaily  int `json:"defaultDaily"`
	}
	jsonUnmarshal(t, rec.Body.Bytes(), &d)
	if rec.Code != 200 || d.DefaultHourly != 10 || d.DefaultDaily != 20 {
		t.Fatalf("defaults = %d %+v", rec.Code, d)
	}
	// Quota changes are audited (project save + defaults save; the
	// rejected negative save is not).
	total, _ := auditEntries(t, s, adminCookie, "?action=projects.quota_changed")
	if total != 2 {
		t.Fatalf("quota audit entries = %d, want 2", total)
	}
}
