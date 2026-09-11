-- +goose Up
-- Title resolution now promotes a match to its directory's container launch
-- target, so a disc folder resolves to its cue sheet or playlist rather than to
-- whichever companion file the slug search happened to pick. Entries cached
-- before that change still name the companion, and a cache hit returns without
-- consulting the container rule -- deliberately, because that lookup would
-- otherwise run on every launch of a disc image forever.
--
-- Clearing the cache once retires those entries without taxing the hot path.
-- Nothing is lost: the table only memoises resolutions the pipeline can redo,
-- and the next launch of each title repopulates it with a promoted ID.

DELETE FROM SlugResolutionCache;

-- +goose Down
-- The cache rebuilds itself on use, so there is nothing to restore.
SELECT 1;
