-- +goose Up
-- Optimize search performance with composite indexes

-- Add composite index for system-specific searches (SystemDBID + Slug)
-- This dramatically improves performance for searches within specific systems
CREATE INDEX mediatitles_system_slug_idx ON MediaTitles(SystemDBID, Slug);

-- Remove redundant mediatitles_system_idx since it's covered by the composite index
-- Keep mediatitles_slug_idx for cross-system searches
DROP INDEX IF EXISTS mediatitles_system_idx;

-- +goose Down
-- Restore original single-column indexes
CREATE INDEX mediatitles_system_idx ON MediaTitles(SystemDBID);
DROP INDEX IF EXISTS mediatitles_system_slug_idx;