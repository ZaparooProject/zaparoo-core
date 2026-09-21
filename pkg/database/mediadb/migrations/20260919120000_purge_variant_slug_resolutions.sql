-- +goose Up
-- Title resolution used to reject an exact title whose files were all
-- prereleases, translations or other variants whenever there was more than one
-- of them, and fell through to strategies that match a shorter title. The
-- result was cached, so "RoboCop versus The Terminator" kept resolving to
-- "RoboCop" after the selection rule was corrected: a cache hit returns without
-- running selection at all.
--
-- Clearing the cache once retires those entries. Nothing is lost: the table
-- only memoises resolutions the pipeline can redo, and the next launch of each
-- title repopulates it.

-- zaparoo:allow-unbounded a stale entry returns the wrong title, and only the
-- cache knows which of its rows predate the corrected selection rule, so there
-- is nothing narrower to delete and no later moment at which it is still safe
-- to serve them. The row count is the cache's, not the library's: it holds one
-- row per title actually launched, and the delete is a single unindexed scan of
-- a table that is empty on a new install.
DELETE FROM SlugResolutionCache;

-- +goose Down
-- The cache rebuilds itself on use, so there is nothing to restore.
SELECT 1;
