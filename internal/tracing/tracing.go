// Package tracing links a trace_id already captured in an event's "trace"
// context out to whatever external tracing backend (Jaeger, Tempo,
// Honeycomb, ...) the operator runs. errgotrack does not ingest or store
// spans itself - this is correlation, not APM.
package tracing

import (
	"github.com/philippdormann/splat/internal/database"
	"strings"
)

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

func (s *Store) GetURLTemplate() (string, error) {
	var tmpl string
	err := s.db.QueryRow(`SELECT url_template FROM tracing_config WHERE id = 1`).Scan(&tmpl)
	return tmpl, err
}

func (s *Store) SetURLTemplate(tmpl string) error {
	_, err := s.db.Exec(`UPDATE tracing_config SET url_template = ? WHERE id = 1`, tmpl)
	return err
}

// BuildURL substitutes a trace id into the configured template's
// "{trace_id}" placeholder. Returns "" if no template is configured or no
// trace id was given, so callers can treat an empty result as "no link".
func BuildURL(tmpl, traceID string) string {
	if tmpl == "" || traceID == "" {
		return ""
	}
	return strings.ReplaceAll(tmpl, "{trace_id}", traceID)
}
