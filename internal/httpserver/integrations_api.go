package httpserver

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/notifications"
	"github.com/philippdormann/splat/internal/webhooks"
)

func integrationJSON(in notifications.Integration) map[string]any {
	creds := map[string]any{}
	for k, v := range in.Credentials {
		creds[k] = v
	}
	// Mask secrets in responses; blank/• on write keeps the stored value.
	switch in.Provider {
	case "telegram":
		if tok, _ := creds["bot_token"].(string); tok != "" {
			creds["bot_token"] = maskToken(tok)
		}
	case "smtp":
		if pw, _ := creds["password"].(string); pw != "" {
			creds["password"] = maskToken(pw)
		}
	case "generic":
		if sec, _ := creds["secret"].(string); sec != "" {
			creds["secret"] = maskToken(sec)
		}
	}
	return map[string]any{
		"id": in.ID, "name": in.Name, "provider": in.Provider,
		"enabled": in.IsEnabled, "credentials": creds,
		"failureCount": in.FailureCount, "lastFailure": in.LastFailure,
	}
}

// mergeSecret keeps the stored secret when the client sends blank or a
// masked placeholder (mirrors the old bot-token convention).
func mergeSecret(input, stored string) string {
	if input == "" || strings.Contains(input, "•") {
		return stored
	}
	return input
}

func mergeCredentials(provider string, input map[string]any, stored map[string]any) map[string]any {
	if input == nil {
		input = map[string]any{}
	}
	out := map[string]any{}
	for k, v := range input {
		out[k] = v
	}
	for k, v := range stored {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	switch provider {
	case "telegram":
		inStr, _ := input["bot_token"].(string)
		stStr, _ := stored["bot_token"].(string)
		out["bot_token"] = mergeSecret(inStr, stStr)
	case "smtp":
		inStr, _ := input["password"].(string)
		stStr, _ := stored["password"].(string)
		out["password"] = mergeSecret(inStr, stStr)
	case "generic":
		inStr, _ := input["secret"].(string)
		stStr, _ := stored["secret"].(string)
		out["secret"] = mergeSecret(inStr, stStr)
	}
	return out
}

func validateIntegrationURL(provider string, creds map[string]any, override notifications.RoutingOverride) error {
	candidates := []string{}
	if override.URL != "" {
		candidates = append(candidates, override.URL)
	}
	if u, _ := creds["url"].(string); u != "" {
		candidates = append(candidates, u)
	}
	if provider == "slack" || provider == "discord" || provider == "teams" || provider == "sheets" || provider == "generic" {
		for _, u := range candidates {
			if err := webhooks.ValidateURL(u); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) apiIntegrationsList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Notifications.Integrations()
	if err != nil {
		s.Logger.Error("list integrations", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, in := range list {
		out = append(out, integrationJSON(in))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiIntegrationsCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name        string         `json:"name"`
		Provider    string         `json:"provider"`
		Enabled     bool           `json:"enabled"`
		Credentials map[string]any `json:"credentials"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := validateIntegrationURL(input.Provider, input.Credentials, notifications.RoutingOverride{}); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Not saved: "+err.Error())
		return
	}
	in, err := s.Notifications.CreateIntegration(notifications.Integration{
		Name: input.Name, Provider: input.Provider, Credentials: input.Credentials, IsEnabled: input.Enabled,
	})
	if err != nil {
		s.Logger.Error("create integration", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.SettingsSaved, "integration", strconv.FormatInt(in.ID, 10), `{"op":"create"}`)
	writeJSON(w, http.StatusCreated, integrationJSON(in))
}

func (s *Server) apiIntegrationsUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		Name        string         `json:"name"`
		Provider    string         `json:"provider"`
		Enabled     bool           `json:"enabled"`
		Credentials map[string]any `json:"credentials"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	stored, err := s.Notifications.GetIntegration(id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	provider := input.Provider
	if provider == "" {
		provider = stored.Provider
	}
	merged := mergeCredentials(provider, input.Credentials, stored.Credentials)
	if err := validateIntegrationURL(provider, merged, notifications.RoutingOverride{}); err != nil {
		writeAPIError(w, http.StatusBadRequest, "Not saved: "+err.Error())
		return
	}
	in, err := s.Notifications.UpdateIntegration(notifications.Integration{
		ID: id, Name: input.Name, Provider: provider, Credentials: merged, IsEnabled: input.Enabled,
	})
	if err != nil {
		s.Logger.Error("update integration", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.SettingsSaved, "integration", strconv.FormatInt(id, 10), `{"op":"update"}`)
	writeJSON(w, http.StatusOK, integrationJSON(in))
}

func (s *Server) apiIntegrationsDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Notifications.DeleteIntegration(id); err != nil {
		s.Logger.Error("delete integration", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.SettingsSaved, "integration", strconv.FormatInt(id, 10), `{"op":"delete"}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiIntegrationsTest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		RoutingOverride notifications.RoutingOverride `json:"routingOverride"`
	}
	_ = decodeJSON(r, &input)
	err = s.Notifications.SendTestIntegration(r.Context(), id, input.RoutingOverride)
	msg := "Test notification sent."
	ok := true
	if err != nil {
		msg = "Failed to send: " + err.Error()
		ok = false
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "message": msg})
}

// --- alert rules (integrations version) ---

func ruleJSON(r notifications.Rule) map[string]any {
	var projectID any
	if r.ProjectID.Valid {
		projectID = r.ProjectID.Int64
	}
	links := make([]map[string]any, 0, len(r.Integrations))
	for _, l := range r.Integrations {
		links = append(links, map[string]any{
			"integrationId": l.IntegrationID, "provider": l.Provider,
			"integrationName": l.IntegrationName, "isEnabled": l.IsEnabled,
			"routingOverride": l.Override,
		})
	}
	return map[string]any{
		"id": r.ID, "name": r.Name, "projectId": projectID,
		"environment": r.Environment, "notifyNewIssue": r.NotifyNewIssue,
		"notifyThreshold": r.NotifyThreshold, "notifyReopened": r.NotifyReopened,
		"cooldownSeconds": r.CooldownSeconds, "integrations": links,
		// Deprecated: old channel names, derived for backwards compat.
		"channels": ruleChannelsCompat(r),
	}
}

func ruleChannelsCompat(r notifications.Rule) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, l := range r.Integrations {
		if l.Provider == "smtp" {
			if !seen["email"] {
				seen["email"] = true
				out = append(out, "email")
			}
			continue
		}
		if !seen[l.Provider] {
			seen[l.Provider] = true
			out = append(out, l.Provider)
		}
	}
	return out
}

type ruleIntegrationInput struct {
	IntegrationID   int64                          `json:"integrationId"`
	RoutingOverride notifications.RoutingOverride `json:"routingOverride"`
}

type ruleInput struct {
	Name            string                 `json:"name"`
	ProjectID       *int64                 `json:"projectId"`
	Environment     string                 `json:"environment"`
	NotifyNewIssue  bool                   `json:"notifyNewIssue"`
	NotifyThreshold int64                  `json:"notifyThreshold"`
	NotifyReopened  bool                   `json:"notifyReopened"`
	CooldownSeconds int                    `json:"cooldownSeconds"`
	Integrations    []ruleIntegrationInput `json:"integrations"`
	// Deprecated: accepted for backwards compat, ignored when integrations present.
	Channels []string `json:"channels"`
}

func (s *Server) apiRulesList(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Notifications.Rules()
	if err != nil {
		s.Logger.Error("list rules", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if pid := r.URL.Query().Get("project_id"); pid != "" {
		filtered := []notifications.Rule{}
		for _, rule := range rules {
			if rule.ProjectID.Valid && strconv.FormatInt(rule.ProjectID.Int64, 10) == pid {
				filtered = append(filtered, rule)
			} else if !rule.ProjectID.Valid && pid == "null" {
				filtered = append(filtered, rule)
			}
		}
		rules = filtered
	}
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, ruleJSON(rule))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiRulesCreate(w http.ResponseWriter, r *http.Request) {
	var input ruleInput
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	for _, l := range input.Integrations {
		in, err := s.Notifications.GetIntegration(l.IntegrationID)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "integration not found")
			return
		}
		if err := validateIntegrationURL(in.Provider, in.Credentials, l.RoutingOverride); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Not saved: "+err.Error())
			return
		}
	}
	rule, err := s.Notifications.CreateRule(input.toRule(0))
	if err != nil {
		s.Logger.Error("create rule", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.RuleChanged, "rule", strconv.FormatInt(rule.ID, 10), `{"op":"create"}`)
	writeJSON(w, http.StatusCreated, ruleJSON(rule))
}

func (s *Server) apiRulesUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input ruleInput
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	for _, l := range input.Integrations {
		in, err := s.Notifications.GetIntegration(l.IntegrationID)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "integration not found")
			return
		}
		if err := validateIntegrationURL(in.Provider, in.Credentials, l.RoutingOverride); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Not saved: "+err.Error())
			return
		}
	}
	rule, err := s.Notifications.UpdateRule(input.toRule(id))
	if err != nil {
		s.Logger.Error("update rule", "error", err)
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.RuleChanged, "rule", strconv.FormatInt(id, 10), `{"op":"update"}`)
	writeJSON(w, http.StatusOK, ruleJSON(rule))
}

func (s *Server) apiRulesDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Notifications.DeleteRule(id); err != nil {
		s.Logger.Error("delete rule", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.RuleChanged, "rule", strconv.FormatInt(id, 10), `{"op":"delete"}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (i ruleInput) toRule(id int64) notifications.Rule {
	r := notifications.Rule{
		ID: id, Name: i.Name, Environment: i.Environment,
		NotifyNewIssue: i.NotifyNewIssue, NotifyThreshold: i.NotifyThreshold,
		NotifyReopened: i.NotifyReopened, CooldownSeconds: i.CooldownSeconds,
	}
	if i.ProjectID != nil {
		r.ProjectID = sql.NullInt64{Int64: *i.ProjectID, Valid: true}
	}
	for _, l := range i.Integrations {
		r.Integrations = append(r.Integrations, notifications.RuleIntegration{
			IntegrationID: l.IntegrationID, Override: l.RoutingOverride,
		})
	}
	return r
}
