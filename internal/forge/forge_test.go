package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	if err := (Config{Provider: "github", Owner: "o", Repo: "r", Token: "t"}).Validate(); err != nil {
		t.Fatalf("github default base: %v", err)
	}
	if err := (Config{Provider: "gitea", Owner: "o", Repo: "r", Token: "t"}).Validate(); err == nil {
		t.Fatal("gitea without base URL must fail")
	}
	if err := (Config{Provider: "nope", Owner: "o", Repo: "r", Token: "t"}).Validate(); err == nil {
		t.Fatal("unknown provider must fail")
	}
	if err := (Config{Provider: "github", Owner: "", Repo: "r", Token: "t"}).Validate(); err == nil {
		t.Fatal("missing owner must fail")
	}
	if err := (Config{Provider: "github", Owner: "o", Repo: "r", Token: ""}).Validate(); err == nil {
		t.Fatal("missing token must fail")
	}
}

// TestCreateIssuePerProvider drives the full client (paths, auth headers,
// payload keys, response parsing) against local stubs for all four
// providers.
func TestCreateIssuePerProvider(t *testing.T) {
	cases := []struct {
		provider   string
		pathSuffix string
		authHeader string
		authValue  string
		titleKey   string
		createBody string
		number     int64
		url        string
	}{
		{"github", "/repos/o/r/issues", "Authorization", "Bearer tok", "body",
			`{"number": 12, "html_url": "https://github.com/o/r/issues/12", "title": "Bug"}`,
			12, "https://github.com/o/r/issues/12"},
		{"gitlab", "/api/v4/projects/o/r/issues", "PRIVATE-TOKEN", "tok", "description",
			`{"iid": 7, "web_url": "https://gitlab.com/o/r/-/issues/7", "title": "Bug"}`,
			7, "https://gitlab.com/o/r/-/issues/7"},
		{"gitea", "/api/v1/repos/o/r/issues", "Authorization", "token tok", "body",
			`{"number": 3, "html_url": "https://git.example.com/o/r/issues/3", "title": "Bug"}`,
			3, "https://git.example.com/o/r/issues/3"},
		{"forgejo", "/api/v1/repos/o/r/issues", "Authorization", "token tok", "body",
			`{"number": 4, "html_url": "https://code.example.com/o/r/issues/4", "title": "Bug"}`,
			4, "https://code.example.com/o/r/issues/4"},
	}
	for _, tc := range cases {
		var gotAuth, gotTitle, gotBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, tc.pathSuffix) {
				t.Errorf("%s: path = %s", tc.provider, r.URL.Path)
			}
			gotAuth = r.Header.Get(tc.authHeader)
			var payload map[string]string
			json.NewDecoder(r.Body).Decode(&payload)
			gotTitle, gotBody = payload["title"], payload[tc.titleKey]
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(tc.createBody))
		}))
		cfg := Config{Provider: tc.provider, BaseURL: srv.URL, Owner: "o", Repo: "r", Token: "tok"}
		created, err := NewClient().CreateIssue(context.Background(), cfg, "Bug", "boom")
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", tc.provider, err)
		}
		if created.Number != tc.number || created.URL != tc.url {
			t.Fatalf("%s: created = %+v", tc.provider, created)
		}
		if gotAuth != tc.authValue || gotTitle != "Bug" || gotBody == "" {
			t.Fatalf("%s: auth=%q title=%q body=%q", tc.provider, gotAuth, gotTitle, gotBody)
		}
	}
}

func TestCreateIssueErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "Not Found"}`))
	}))
	defer srv.Close()
	cfg := Config{Provider: "github", BaseURL: srv.URL, Owner: "o", Repo: "missing", Token: "tok"}
	_, err := NewClient().CreateIssue(context.Background(), cfg, "Bug", "boom")
	if err == nil || !strings.Contains(err.Error(), "Not Found") {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewClient().CreateIssue(context.Background(), cfg, "  ", "boom"); err == nil {
		t.Fatal("empty title must fail")
	}
}

func TestCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/repos/o/r") {
			t.Errorf("check path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"full_name": "o/r"}`))
	}))
	defer srv.Close()
	cfg := Config{Provider: "github", BaseURL: srv.URL, Owner: "o", Repo: "r", Token: "tok"}
	if err := NewClient().Check(context.Background(), cfg); err != nil {
		t.Fatalf("check: %v", err)
	}
}
