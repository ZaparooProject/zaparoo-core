-- +goose Up
-- A schema version bump, deliberately doing no table work.
--
-- idx_media_browse_sort orders SortName with the ZAPAROO_TITLE_V1 collation,
-- which the driver registers per connection rather than storing in the file. A
-- build without that collation cannot prepare any statement against Media at
-- all, because SQLite resolves index definitions first: indexing dies on its
-- first insert with "no such collation sequence" and browse falls back to
-- "no query solution". Recreating the index bumps no version by itself, so
-- nothing triggered the rebuild that already exists for a schema an older
-- build cannot read.
--
-- Moving the version is the whole job. Going back to a build without the
-- collation now raises ErrSchemaAhead and startup rebuilds the media database.
--
-- Replacing the index itself stays in CreateSecondaryIndexes, at the end of an
-- indexing run. Doing it here instead rebuilt the index over every media row
-- while the service was starting and nothing on screen said why: 2m14s on a
-- 229k-item library on MiSTer SD, and libraries get much larger than that.
-- Until that run happens the index is simply the uncollated one; browse stays
-- correct because the collation is written into the queries, and INDEXED BY
-- still plans against it.

INSERT INTO DBConfig (Name, Value) VALUES ('BrowseSortCollation', 'zaparoo_title_v1')
    ON CONFLICT(Name) DO UPDATE SET Value = excluded.Value;

-- +goose Down
-- An explicit downgrade can follow an indexing run that created the collated
-- index. Restore the legacy definition before lowering the schema version so
-- a build without ZAPAROO_TITLE_V1 can prepare statements against Media.
DROP INDEX IF EXISTS idx_media_browse_sort;
CREATE INDEX idx_media_browse_sort ON Media(ParentDir, IsMissing, SortName, DBID);
DELETE FROM DBConfig WHERE Name = 'BrowseSortCollation';
