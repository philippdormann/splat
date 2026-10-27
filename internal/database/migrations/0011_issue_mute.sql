-- Muted issues: like ignored (never auto-reopen, never notify) but with an
-- optional expiry. muted_until is RFC3339Nano or '' for indefinite; when a
-- new event arrives after the timestamp, the issue flips back to
-- unresolved instead of staying muted.
ALTER TABLE issues ADD COLUMN muted_until TEXT NOT NULL DEFAULT '';
