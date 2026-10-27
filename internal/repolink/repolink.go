// Package repolink persists the repository integration: one linked forge
// repository per project plus the external issues created from (or
// tracked against) errgotrack issues. The forge APIs themselves live in
// internal/forge; this package is storage only.
package repolink

import (
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/forge"
)

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

type Repo struct {
	ProjectID int64
	Provider  string
	BaseURL   string
	Owner     string
	Repo      string
	HasToken  bool
	UpdatedAt string
}

// Get returns the linked repo, or nil when the project has none.
func (s *Store) Get(projectID int64) (*Repo, error) {
	var r Repo
	var token string
	err := s.db.QueryRow(`SELECT project_id, provider, base_url, owner, repo, token, updated_at
		FROM project_repos WHERE project_id = ?`, projectID).
		Scan(&r.ProjectID, &r.Provider, &r.BaseURL, &r.Owner, &r.Repo, &token, &r.UpdatedAt)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	r.HasToken = token != ""
	return &r, nil
}

// Token returns the stored secret for API calls. Empty Token in input
// keeps the existing secret (lets the UI send "leave blank to keep").
func (s *Store) Token(projectID int64) (string, error) {
	var token string
	if err := s.db.QueryRow(`SELECT token FROM project_repos WHERE project_id = ?`, projectID).Scan(&token); err != nil {
		return "", err
	}
	return token, nil
}

// Save upserts the linked repo. Validation (provider, owner/repo, token
// presence, base URL) mirrors forge.Config so bad configs fail fast here
// instead of at first use.
func (s *Store) Save(projectID int64, provider, baseURL, owner, repo, token string) (*Repo, error) {
	provider = strings.TrimSpace(provider)
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if keep, err := s.keepToken(projectID, token); err != nil {
		return nil, err
	} else {
		token = keep
	}
	cfg := forge.Config{Provider: provider, BaseURL: baseURL, Owner: owner, Repo: repo, Token: token}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if s.db.IsPostgres() {
		_, err := s.db.Exec(`INSERT INTO project_repos
			(project_id, provider, base_url, owner, repo, token, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (project_id) DO UPDATE SET
				provider = EXCLUDED.provider, base_url = EXCLUDED.base_url,
				owner = EXCLUDED.owner, repo = EXCLUDED.repo,
				token = EXCLUDED.token, updated_at = EXCLUDED.updated_at`,
			projectID, provider, strings.TrimSpace(baseURL), owner, repo, token, now)
		if err != nil {
			return nil, err
		}
	} else {
		_, err := s.db.Exec(`INSERT INTO project_repos
			(project_id, provider, base_url, owner, repo, token, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (project_id) DO UPDATE SET
				provider = excluded.provider, base_url = excluded.base_url,
				owner = excluded.owner, repo = excluded.repo,
				token = excluded.token, updated_at = excluded.updated_at`,
			projectID, provider, strings.TrimSpace(baseURL), owner, repo, token, now)
		if err != nil {
			return nil, err
		}
	}
	return s.Get(projectID)
}

// keepToken resolves "blank means keep the stored secret" for updates.
func (s *Store) keepToken(projectID int64, token string) (string, error) {
	if strings.TrimSpace(token) != "" {
		return token, nil
	}
	existing, err := s.Token(projectID)
	if err != nil {
		if isNoRows(err) {
			return "", nil // Validate will reject the missing token
		}
		return "", err
	}
	return existing, nil
}

// Delete unlinks the repository. Issue links are kept: they are history,
// and the external issues still exist.
func (s *Store) Delete(projectID int64) error {
	_, err := s.db.Exec(`DELETE FROM project_repos WHERE project_id = ?`, projectID)
	return err
}

// Config hydrates a forge.Config for API calls (token included).
func (s *Store) Config(projectID int64) (forge.Config, error) {
	var provider, baseURL, owner, repo, token string
	if err := s.db.QueryRow(`SELECT provider, base_url, owner, repo, token
		FROM project_repos WHERE project_id = ?`, projectID).
		Scan(&provider, &baseURL, &owner, &repo, &token); err != nil {
		if isNoRows(err) {
			return forge.Config{}, errNoRepo
		}
		return forge.Config{}, err
	}
	return forge.Config{Provider: provider, BaseURL: baseURL, Owner: owner, Repo: repo, Token: token}, nil
}

type Link struct {
	ID        int64
	IssueID   int64
	Provider  string
	Repo      string
	Number    int64
	URL       string
	Title     string
	CreatedAt string
}

// Links lists a issue's external issues, oldest first.
func (s *Store) Links(issueID int64) ([]Link, error) {
	rows, err := s.db.Query(`SELECT id, issue_id, provider, repo, number, url, title, created_at
		FROM issue_links WHERE issue_id = ? ORDER BY id`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Link{}
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.IssueID, &l.Provider, &l.Repo, &l.Number, &l.URL, &l.Title, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// AddLink records an external issue. Duplicates (same repo + number) are
// ignored so double-clicks and retries stay idempotent.
func (s *Store) AddLink(issueID int64, provider, repo string, number int64, url, title string) (Link, error) {
	var existing Link
	err := s.db.QueryRow(`SELECT id, issue_id, provider, repo, number, url, title, created_at
		FROM issue_links WHERE issue_id = ? AND provider = ? AND repo = ? AND number = ?`,
		issueID, provider, repo, number).
		Scan(&existing.ID, &existing.IssueID, &existing.Provider, &existing.Repo,
			&existing.Number, &existing.URL, &existing.Title, &existing.CreatedAt)
	if err == nil {
		return existing, nil
	}
	if !isNoRows(err) {
		return Link{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if s.db.IsPostgres() {
		var l Link
		err := s.db.QueryRow(`INSERT INTO issue_links
			(issue_id, provider, repo, number, url, title, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id, issue_id, provider, repo, number, url, title, created_at`,
			issueID, provider, repo, number, url, title, now).
			Scan(&l.ID, &l.IssueID, &l.Provider, &l.Repo, &l.Number, &l.URL, &l.Title, &l.CreatedAt)
		return l, err
	}
	res, err := s.db.Exec(`INSERT INTO issue_links
		(issue_id, provider, repo, number, url, title, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, issueID, provider, repo, number, url, title, now)
	if err != nil {
		return Link{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Link{}, err
	}
	return Link{ID: id, IssueID: issueID, Provider: provider, Repo: repo, Number: number, URL: url, Title: title, CreatedAt: now}, nil
}

// RemoveLink unlinks an external issue (scoped to the issue: one issue
// cannot unlink another's links). The external issue itself is untouched.
func (s *Store) RemoveLink(issueID, linkID int64) error {
	_, err := s.db.Exec(`DELETE FROM issue_links WHERE id = ? AND issue_id = ?`, linkID, issueID)
	return err
}
