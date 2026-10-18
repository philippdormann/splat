-- The Sentry "contexts" object (browser, os, device, runtime, trace, culture,
-- and SDK/framework-specific contexts like a "react" context) was being
-- silently discarded during ingestion - this column stores it as-is so the
-- issue detail page can render it the way Sentry/GlitchTip do.
ALTER TABLE events ADD COLUMN contexts_json TEXT NOT NULL DEFAULT '{}';
