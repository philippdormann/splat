-- Resolve-in-next-release: a resolved issue pinned to the release it was
-- resolved in. Events from the same release keep it resolved; an event
-- with a different (non-empty) release reopens it. Empty means a plain
-- manual resolve, which reopens on any new occurrence as before.
ALTER TABLE issues ADD COLUMN resolved_in_release TEXT NOT NULL DEFAULT '';
