package httpserver

import (
	"net/http"
	"strconv"

	"github.com/philippdormann/splat/internal/audit"
)

// --- ingestion quotas (spike protection) ---
//
// Usage counts stored error events over trailing hour/day windows.
// Per-project overrides (nil = inherit, 0 = unlimited) and instance
// defaults live in the quotas store; over-quota ingestion gets 429.

func quotaJSON(projectID int64, hourly, daily int, oh, od *int, usedH, usedD int) map[string]any {
	out := map[string]any{
		"projectId": projectID,
		"hourly":    hourly, "daily": daily,
		"usedHour": usedH, "usedDay": usedD,
		"breachedHour": hourly > 0 && usedH >= hourly,
		"breachedDay":  daily > 0 && usedD >= daily,
	}
	if oh != nil {
		out["hourlyOverride"] = *oh
	} else {
		out["hourlyOverride"] = nil
	}
	if od != nil {
		out["dailyOverride"] = *od
	} else {
		out["dailyOverride"] = nil
	}
	return out
}

func (s *Server) apiProjectQuotaGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Projects.Get(id); err != nil || !s.canAccessProject(r, id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	hourly, daily, err := s.Quotas.Effective(id)
	if err != nil {
		s.Logger.Error("quota effective", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	oh, od, err := s.Quotas.Overrides(id)
	if err != nil {
		s.Logger.Error("quota overrides", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	usedH, usedD, err := s.Quotas.Usage(id)
	if err != nil {
		s.Logger.Error("quota usage", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, quotaJSON(id, hourly, daily, oh, od, usedH, usedD))
}

func (s *Server) apiProjectQuotaSave(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Projects.Get(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	// null = inherit the instance default; 0 = unlimited; >= 0 required.
	var input struct {
		Hourly *int `json:"hourly"`
		Daily  *int `json:"daily"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.Quotas.SetOverrides(id, input.Hourly, input.Daily); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.QuotaChanged, "project", strconv.FormatInt(id, 10), "{}")
	s.apiProjectQuotaGet(w, r)
}

func (s *Server) apiQuotasDefaultsGet(w http.ResponseWriter, r *http.Request) {
	hourly, daily := s.Quotas.Defaults()
	writeJSON(w, http.StatusOK, map[string]any{"defaultHourly": hourly, "defaultDaily": daily})
}

func (s *Server) apiQuotasDefaultsSave(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DefaultHourly int `json:"defaultHourly"`
		DefaultDaily  int `json:"defaultDaily"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.Quotas.SetDefaults(input.DefaultHourly, input.DefaultDaily); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.QuotaChanged, "instance", "", "{}")
	s.apiQuotasDefaultsGet(w, r)
}
