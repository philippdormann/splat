// Package forge links a project to its code repository and works with
// issues there: it creates external issues from errgotrack issues and
// checks the connection, against four providers - GitHub, GitLab, Gitea,
// and Forgejo (which speaks Gitea's API).
//
// Only issue creation is implemented, deliberately: this is the "file this
// bug where the code lives" bridge, not a git client. Tokens are stored
// alongside the repo config (see internal/database migrations) and masked
// in the API; custom base URLs pass the same SSRF validation as webhook
// URLs since a token-bearing request goes to them.
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ProviderGitHub  = "github"
	ProviderGitLab  = "gitlab"
	ProviderGitea   = "gitea"
	ProviderForgejo = "forgejo"
)

// ValidProvider reports whether p names a supported forge.
func ValidProvider(p string) bool {
	switch p {
	case ProviderGitHub, ProviderGitLab, ProviderGitea, ProviderForgejo:
		return true
	}
	return false
}

// DefaultBaseURL is the public API root used when no custom base URL is
// configured. Gitea/Forgejo are self-hosted by definition, so they have
// no default and require one.
func DefaultBaseURL(provider string) string {
	switch provider {
	case ProviderGitHub:
		return "https://api.github.com"
	case ProviderGitLab:
		return "https://gitlab.com"
	default:
		return ""
	}
}

// Config is one project's linked repository.
type Config struct {
	Provider string
	BaseURL  string // blank = provider default
	Owner    string
	Repo     string
	Token    string
}

// RepoSlug renders "owner/repo" for display.
func (c Config) RepoSlug() string { return c.Owner + "/" + c.Repo }

// baseURL resolves the effective API root. Custom hosts are validated
// against SSRF at save time (see the repo API: webhooks.ValidateURL),
// not here - mirroring how webhook delivery works: validate once when an
// admin pastes the URL, then call it on every use.
func (c Config) baseURL() (string, error) {
	base := strings.TrimSuffix(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL(c.Provider)
	}
	if base == "" {
		return "", fmt.Errorf("a base URL is required for %s (self-hosted)", c.Provider)
	}
	return base, nil
}

// Validate reports whether the config is complete enough to attempt API
// calls. It does not touch the network; use Check for that.
func (c Config) Validate() error {
	if !ValidProvider(c.Provider) {
		return fmt.Errorf("unknown provider %q (want github, gitlab, gitea or forgejo)", c.Provider)
	}
	if strings.TrimSpace(c.Owner) == "" || strings.TrimSpace(c.Repo) == "" {
		return fmt.Errorf("owner and repo are required")
	}
	if strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("an API token is required")
	}
	_, err := c.baseURL()
	return err
}

// Client posts to forge APIs with a bounded timeout.
type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 15 * time.Second}}
}

// CreatedIssue is what came back from the forge.
type CreatedIssue struct {
	Number int64
	URL    string
	Title  string
}

func (c *Client) do(ctx context.Context, method, url, tokenHeader, token string, payload any) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set(tokenHeader, token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("forge request: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return b, resp.StatusCode, nil
}

// apiError extracts the forge's own error message (all four providers
// return a JSON "message" on failure) for display.
func apiError(provider string, status int, body []byte) error {
	var parsed struct {
		Message any `json:"message"`
	}
	msg := ""
	if json.Unmarshal(body, &parsed) == nil {
		switch m := parsed.Message.(type) {
		case string:
			msg = m
		case []any:
			// GitLab validation errors come back as {message: {field: [...]}}.
			b, _ := json.Marshal(m)
			msg = string(b)
		case map[string]any:
			b, _ := json.Marshal(m)
			msg = string(b)
		}
	}
	msg = strings.TrimSpace(msg)
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("%s API error %d: %s", provider, status, msg)
}

// CreateIssue opens an issue on the linked repository and returns its
// number and web URL.
func (c *Client) CreateIssue(ctx context.Context, cfg Config, title, body string) (CreatedIssue, error) {
	if err := cfg.Validate(); err != nil {
		return CreatedIssue{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return CreatedIssue{}, fmt.Errorf("title is required")
	}
	base, _ := cfg.baseURL()
	switch cfg.Provider {
	case ProviderGitHub:
		endpoint := base + "/repos/" + url.PathEscape(cfg.Owner) + "/" + url.PathEscape(cfg.Repo) + "/issues"
		respBody, status, err := c.do(ctx, http.MethodPost, endpoint, "Authorization", "Bearer "+cfg.Token,
			map[string]string{"title": title, "body": body})
		if err != nil {
			return CreatedIssue{}, err
		}
		if status < 200 || status >= 300 {
			return CreatedIssue{}, apiError("GitHub", status, respBody)
		}
		var out struct {
			Number  int64  `json:"number"`
			HTMLURL string `json:"html_url"`
			Title   string `json:"title"`
		}
		if err := json.Unmarshal(respBody, &out); err != nil {
			return CreatedIssue{}, fmt.Errorf("decode GitHub response: %w", err)
		}
		return CreatedIssue{Number: out.Number, URL: out.HTMLURL, Title: out.Title}, nil
	case ProviderGitLab:
		endpoint := base + "/api/v4/projects/" + url.PathEscape(cfg.Owner+"/"+cfg.Repo) + "/issues"
		respBody, status, err := c.do(ctx, http.MethodPost, endpoint, "PRIVATE-TOKEN", cfg.Token,
			map[string]string{"title": title, "description": body})
		if err != nil {
			return CreatedIssue{}, err
		}
		if status < 200 || status >= 300 {
			return CreatedIssue{}, apiError("GitLab", status, respBody)
		}
		var out struct {
			IID    int64  `json:"iid"`
			WebURL string `json:"web_url"`
			Title  string `json:"title"`
		}
		if err := json.Unmarshal(respBody, &out); err != nil {
			return CreatedIssue{}, fmt.Errorf("decode GitLab response: %w", err)
		}
		return CreatedIssue{Number: out.IID, URL: out.WebURL, Title: out.Title}, nil
	case ProviderGitea, ProviderForgejo:
		name := "Gitea"
		if cfg.Provider == ProviderForgejo {
			name = "Forgejo"
		}
		endpoint := base + "/api/v1/repos/" + url.PathEscape(cfg.Owner) + "/" + url.PathEscape(cfg.Repo) + "/issues"
		respBody, status, err := c.do(ctx, http.MethodPost, endpoint, "Authorization", "token "+cfg.Token,
			map[string]string{"title": title, "body": body})
		if err != nil {
			return CreatedIssue{}, err
		}
		if status < 200 || status >= 300 {
			return CreatedIssue{}, apiError(name, status, respBody)
		}
		var out struct {
			Number  int64  `json:"number"`
			HTMLURL string `json:"html_url"`
			Title   string `json:"title"`
		}
		if err := json.Unmarshal(respBody, &out); err != nil {
			return CreatedIssue{}, fmt.Errorf("decode %s response: %w", name, err)
		}
		return CreatedIssue{Number: out.Number, URL: out.HTMLURL, Title: out.Title}, nil
	default:
		return CreatedIssue{}, fmt.Errorf("unknown provider %q", cfg.Provider)
	}
}

// Check verifies the repo exists and the token can read it, without
// creating anything. Used by the "test connection" button.
func (c *Client) Check(ctx context.Context, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	base, _ := cfg.baseURL()
	var endpoint, tokenHeader, token string
	switch cfg.Provider {
	case ProviderGitHub:
		endpoint = base + "/repos/" + url.PathEscape(cfg.Owner) + "/" + url.PathEscape(cfg.Repo)
		tokenHeader, token = "Authorization", "Bearer "+cfg.Token
	case ProviderGitLab:
		endpoint = base + "/api/v4/projects/" + url.PathEscape(cfg.Owner+"/"+cfg.Repo)
		tokenHeader, token = "PRIVATE-TOKEN", cfg.Token
	case ProviderGitea, ProviderForgejo:
		endpoint = base + "/api/v1/repos/" + url.PathEscape(cfg.Owner) + "/" + url.PathEscape(cfg.Repo)
		tokenHeader, token = "Authorization", "token "+cfg.Token
	default:
		return fmt.Errorf("unknown provider %q", cfg.Provider)
	}
	respBody, status, err := c.do(ctx, http.MethodGet, endpoint, tokenHeader, token, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return apiError(cfg.Provider, status, respBody)
	}
	return nil
}
