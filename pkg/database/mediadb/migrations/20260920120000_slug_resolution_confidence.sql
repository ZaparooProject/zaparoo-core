-- +goose Up
-- A cached title resolution now keeps the confidence it scored, so a cache hit
-- reports the same confidence as the resolution that wrote it. The score of an
-- existing row is unknown, so the table is rebuilt empty rather than backfilled.
-- Nothing is lost: the table only memoises resolutions the pipeline can redo,
-- and the next launch of each title repopulates it.
--
-- Confidence has no default, so a write that omits the score fails instead of
-- storing one that was never computed.

DROP INDEX IF EXISTS idx_slug_cache_media;
DROP INDEX IF EXISTS idx_slug_cache_system;
DROP TABLE IF EXISTS SlugResolutionCache;

CREATE TABLE SlugResolutionCache (
    CacheKey TEXT PRIMARY KEY NOT NULL,
    SystemID TEXT NOT NULL,
    Slug TEXT NOT NULL,
    TagFilters TEXT NOT NULL,
    MediaDBID INTEGER NOT NULL,
    Strategy TEXT NOT NULL,
    Confidence REAL NOT NULL,
    LastUpdated INTEGER NOT NULL,
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE
);

CREATE INDEX idx_slug_cache_system ON SlugResolutionCache(SystemID);
CREATE INDEX idx_slug_cache_media ON SlugResolutionCache(MediaDBID);

-- +goose Down
DROP INDEX IF EXISTS idx_slug_cache_media;
DROP INDEX IF EXISTS idx_slug_cache_system;
DROP TABLE IF EXISTS SlugResolutionCache;

CREATE TABLE SlugResolutionCache (
    CacheKey TEXT PRIMARY KEY NOT NULL,
    SystemID TEXT NOT NULL,
    Slug TEXT NOT NULL,
    TagFilters TEXT NOT NULL,
    MediaDBID INTEGER NOT NULL,
    Strategy TEXT NOT NULL,
    LastUpdated INTEGER NOT NULL,
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE
);

CREATE INDEX idx_slug_cache_system ON SlugResolutionCache(SystemID);
CREATE INDEX idx_slug_cache_media ON SlugResolutionCache(MediaDBID);
