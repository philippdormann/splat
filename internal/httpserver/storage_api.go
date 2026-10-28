package httpserver

import (
	"net/http"
	"strconv"
	"time"

	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/storage"
)

// --- storage overview (Rustrak-style) ---

func (s *Server) apiStorageSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.Storage.Summary(r.Context())
	if err != nil {
		s.Logger.Error("storage summary", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bytes": sum.Bytes, "events_rows": sum.EventsRows,
		"transactions_rows": sum.TransactionsRows, "logs_rows": sum.LogsRows,
		"source_maps": map[string]any{"file_count": sum.SourceMapFiles, "total_bytes": sum.SourceMapBytes},
		"frees_estimate": sum.FreesEstimate,
	})
}

func (s *Server) apiStorageProjects(w http.ResponseWriter, r *http.Request) {
	projs, err := s.Storage.Projects(r.Context())
	if err != nil {
		s.Logger.Error("storage projects", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, projs)
}

func (s *Server) apiStorageAge(w http.ResponseWriter, r *http.Request) {
	age, err := s.Storage.Age(r.Context())
	if err != nil {
		s.Logger.Error("storage age", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, age)
}

func (s *Server) apiStorageRetentionGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Storage.RetentionResponse())
}

func (s *Server) apiStorageRetentionSave(w http.ResponseWriter, r *http.Request) {
	var input storage.RetentionSettings
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.Storage.SaveRetention(input); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Next run within a minute: poke the scheduler timestamp.
	next := time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	_, _ = s.DB.Exec(`INSERT INTO scheduler_state (key, next_run_at) VALUES ('storage_cleanup', ?)
		ON CONFLICT(key) DO UPDATE SET next_run_at = excluded.next_run_at`, next)
	s.audit(r, audit.SettingsSaved, "storage", "", `{"op":"retention"}`)
	writeJSON(w, http.StatusOK, s.Storage.RetentionResponse())
}

func (s *Server) apiStorageRetentionPreview(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Settings  storage.RetentionSettings `json:"settings"`
		ProjectID *int64                    `json:"projectId"`
	}
	// Empty body previews the saved settings.
	if r.ContentLength != 0 {
		if !decodeJSON(r, &input) {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
	}
	rs := input.Settings
	if r.ContentLength == 0 {
		rs = s.Storage.GetRetention()
	}
	if input.ProjectID == nil {
		if q := r.URL.Query().Get("project_id"); q != "" {
			if id, err := strconv.ParseInt(q, 10, 64); err == nil {
				input.ProjectID = &id
			}
		}
	}
	writeJSON(w, http.StatusOK, s.Storage.Preview(r.Context(), rs, input.ProjectID))
}

func (s *Server) apiStorageRetentionRun(w http.ResponseWriter, r *http.Request) {
	rs := s.Storage.GetRetention()
	if !storage.DeletesAnything(rs) {
		writeAPIError(w, http.StatusBadRequest, "everything is kept forever: there is nothing to clean up")
		return
	}
	st, err := s.Storage.StartRetention(s.DB, s.Cleanup, rs, nil)
	if err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, audit.SettingsSaved, "storage", "", `{"op":"retention_run"}`)
	writeJSON(w, http.StatusAccepted, st)
}

func (s *Server) apiStorageCleanupStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Cleanup.Status())
}

func (s *Server) apiStorageCompaction(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Storage.Compaction(r.Context()))
}

func (s *Server) apiStorageCompact(w http.ResponseWriter, r *http.Request) {
	st := s.Cleanup.Status()
	if st.State == "running" {
		writeAPIError(w, http.StatusConflict, "a cleanup is already running")
		return
	}
	go func() {
		started := time.Now().UTC().Format(time.RFC3339Nano)
		s.Cleanup.SetRunning("compaction", &started)
		err := s.Storage.Vacuum(r.Context())
		finished := time.Now().UTC().Format(time.RFC3339Nano)
		_ = finished
		if err != nil {
			msg := err.Error()
			s.Cleanup.SetDone("error", &started, nil, &msg)
			return
		}
		s.Cleanup.SetDone("done", &started, map[string]int64{}, nil)
	}()
	s.audit(r, audit.SettingsSaved, "storage", "", `{"op":"compact"}`)
	writeJSON(w, http.StatusAccepted, s.Cleanup.Status())
}

func (s *Server) apiStorageGCSourceMaps(w http.ResponseWriter, r *http.Request) {
	// Orphaned source maps: rows whose project no longer exists (FK
	// normally prevents this; GC is a no-op safety net reporting counts).
	writeJSON(w, http.StatusOK, map[string]any{"removed": 0})
}
