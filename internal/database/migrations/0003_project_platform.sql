-- Lets a project remember which framework it's for, so its setup page shows
-- the right SDK snippet (Django/Flask/FastAPI/Node/Next.js/Spring Boot/
-- Quarkus/...) instead of one hardcoded example. Purely a UI convenience -
-- ingestion itself is framework-agnostic.
ALTER TABLE projects ADD COLUMN platform TEXT NOT NULL DEFAULT 'python-django';
