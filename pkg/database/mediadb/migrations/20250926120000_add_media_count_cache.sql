-- +goose Up
-- Add MediaCountCache table for caching counts used in random media selection
CREATE TABLE MediaCountCache (
    QueryHash TEXT PRIMARY KEY NOT NULL,     -- SHA256 hash of normalized query params
    QueryParams TEXT NOT NULL,               -- JSON representation of query for debugging
    Count INTEGER NOT NULL,                  -- The cached count result
    MinDBID INTEGER NOT NULL,                -- Minimum DBID for the query result set
    MaxDBID INTEGER NOT NULL,                -- Maximum DBID for the query result set
    LastUpdated INTEGER NOT NULL             -- Unix timestamp when cache was updated
);

-- Index for potential cleanup operations based on age
CREATE INDEX idx_media_count_cache_updated ON MediaCountCache(LastUpdated);

-- +goose Down
DROP INDEX IF EXISTS idx_media_count_cache_updated;
DROP TABLE IF EXISTS MediaCountCache;