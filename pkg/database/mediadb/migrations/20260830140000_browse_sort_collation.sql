-- +goose Up
-- idx_media_browse_sort orders SortName with the ZAPAROO_TITLE_V1 collation,
-- which this binary's SQLite driver registers per connection rather than
-- storing in the file. A build without that collation cannot prepare any
-- statement against Media at all, because SQLite resolves index definitions
-- first: indexing dies on its first insert with "no such collation sequence"
-- and browse falls back to "no query solution". Nothing recovers from it,
-- because the media database is rebuilt on a schema version it does not
-- understand, and creating an index bumps no version by itself.
--
-- CreateSecondaryIndexes owns this index's real lifecycle, dropping and
-- recreating it around bulk inserts. Creating it here as well moves the schema
-- version past what older builds embed, so going back to one of them raises
-- ErrSchemaAhead and startup rebuilds the media database instead of running
-- against a schema it cannot parse.

-- 20260609120000_media_sortname created this index without a collation, so it
-- has to be replaced rather than created. Queries pin it with INDEXED BY, so
-- dropping it without putting it back would fail browse outright.
DROP INDEX IF EXISTS idx_media_browse_sort;
CREATE INDEX idx_media_browse_sort
    ON Media(ParentDir, IsMissing, SortName COLLATE ZAPAROO_TITLE_V1, DBID);

-- +goose Down
DROP INDEX IF EXISTS idx_media_browse_sort;
CREATE INDEX idx_media_browse_sort ON Media(ParentDir, IsMissing, SortName, DBID);
