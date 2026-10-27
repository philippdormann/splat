package httpserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/forge"
	"github.com/philippdormann/splat/internal/repolink"
	"github.com/philippdormann/splat/internal/webhooks"
)

// --- repository integration (forge issues) ---
//
// Reading a project's repo config needs project access; writing it
// (including the token) and testing the connection are admin-only: the
// token posts to a third-party API as whoever owns it. Creating and
// unlinking external issues from an errgotrack issue needs project
// access - that is the day-to-day "work with issues" flow for members.

func repoJSON(r *repolink.Repo) map[string]any {
	effectiveBase := strings.TrimSpace(r.BaseURL)
	if effectiveBase == "" {
		effectiveBase = forge.DefaultBaseURL(r.Provider)
	}
	return map[string]any{
		"provider": r.Provider, "baseUrl": r.BaseURL, "effectiveBaseUrl": effectiveBase,
		"owner": r.Owner, "repo": r.Repo, "slug": r.Owner + "/" + r.Repo,
		"hasToken": r.HasToken, "updatedAt": r.UpdatedAt,
	}
}

func (s *Server) apiProjectRepoGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Projects.Get(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	repo, err := s.RepoLinks.Get(id)
	if err != nil {
		s.Logger.Error("get repo", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if repo == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, repoJSON(repo))
}

func (s *Server) apiProjectRepoSave(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Projects.Get(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		Provider string `json:"provider"`
		BaseURL  string `json:"baseUrl"`
		Owner    string `json:"owner"`
		Repo     string `json:"repo"`
		Token    string `json:"token"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// SSRF guard on custom hosts: the token goes to this host on every
	// call, so resolve it once here like webhook URLs.
	if strings.TrimSpace(input.BaseURL) != "" {
		if err := webhooks.ValidateURL(strings.TrimSpace(input.BaseURL)); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	repo, err := s.RepoLinks.Save(id, input.Provider, input.BaseURL, input.Owner, input.Repo, input.Token)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, repoJSON(repo))
}

func (s *Server) apiProjectRepoDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.RepoLinks.Delete(id); err != nil {
		s.Logger.Error("delete repo", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiProjectRepoTest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	cfg, err := s.RepoLinks.Config(id)
	if err != nil {
		if repolink.ErrNoRepo(err) {
			writeAPIError(w, http.StatusBadRequest, "no repository linked")
			return
		}
		s.Logger.Error("repo config", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := forge.NewClient().Check(ctx, cfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Connected to " + cfg.RepoSlug() + "."})
}

func linkJSON(l repolink.Link) map[string]any {
	return map[string]any{
		"id": l.ID, "issueId": l.IssueID, "provider": l.Provider, "repo": l.Repo,
		"number": l.Number, "url": l.URL, "title": l.Title, "createdAt": l.CreatedAt,
	}
}

func (s *Server) apiIssueLinksList(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueForNotes(w, r)
	if !ok {
		return
	}
	links, err := s.RepoLinks.Links(issue.ID)
	if err != nil {
		s.Logger.Error("list links", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(links))
	for _, l := range links {
		out = append(out, linkJSON(l))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiIssueLinksCreate(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueForNotes(w, r)
	if !ok {
		return
	}
	issueID := issue.ID
	cfg, err := s.RepoLinks.Config(issue.ProjectID)
	if err != nil {
		if repolink.ErrNoRepo(err) {
			writeAPIError(w, http.StatusBadRequest, "no repository linked to this project")
			return
		}
		s.Logger.Error("repo config", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	var input struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = issue.Title
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		body = fmt.Sprintf("%s: %s\n\nTracked in errgotrack: %s/issues/%d",
			issue.ExceptionType, issue.ExceptionValue, s.PublicURL, issue.ID)
		if strings.TrimSpace(issue.ExceptionType) == "" {
			body = fmt.Sprintf("%s\n\nTracked in errgotrack: %s/issues/%d",
				issue.Title, s.PublicURL, issue.ID)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	created, err := forge.NewClient().CreateIssue(ctx, cfg, title, body)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	link, err := s.RepoLinks.AddLink(issueID, cfg.Provider, cfg.RepoSlug(), created.Number, created.URL, created.Title)
	if err != nil {
		s.Logger.Error("add link", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if link.Title == "" {
		link.Title = title
	}
	writeJSON(w, http.StatusCreated, linkJSON(link))
}

func (s *Server) apiIssueLinksDelete(w http.ResponseWriter, r *http.Request) {
	issue, ok := s.issueForNotes(w, r)
	if !ok {
		return
	}
	issueID := issue.ID
	linkID, err := strconv.ParseInt(r.PathValue("linkId"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.RepoLinks.RemoveLink(issueID, linkID); err != nil {
		s.Logger.Error("remove link", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
