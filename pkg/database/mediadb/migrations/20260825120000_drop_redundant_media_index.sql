-- +goose Up
-- media_system_path_idx duplicated the index SQLite already maintains for
-- Media's UNIQUE(SystemDBID, Path) constraint (sqlite_autoindex_Media_1): same
-- columns, same order. Both were kept up to date on every Media insert and
-- update, doubling b-tree maintenance on the indexing pipeline's hottest write
-- path for no query benefit. Queries that need this access path now pin it with
-- INDEXED BY sqlite_autoindex_Media_1, which produces an identical plan.
-- Dropping it here so databases created before this migration also shed it;
-- CreateSecondaryIndexes no longer recreates it after an index run.

DROP INDEX IF EXISTS media_system_path_idx;

-- +goose Down
CREATE INDEX IF NOT EXISTS media_system_path_idx ON Media(SystemDBID, Path);
