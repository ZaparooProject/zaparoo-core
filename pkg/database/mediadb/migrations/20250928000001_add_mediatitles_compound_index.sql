-- +goose Up
-- Add compound index to optimize media search with filtering and sorting
-- This index allows efficient filtering on SystemDBID and Slug with sorting by DBID
-- The DBID column enables efficient cursor-based pagination
CREATE INDEX IF NOT EXISTS mediatitles_search_compound_idx ON MediaTitles(SystemDBID, Slug, DBID);

-- +goose Down
DROP INDEX IF EXISTS mediatitles_search_compound_idx;