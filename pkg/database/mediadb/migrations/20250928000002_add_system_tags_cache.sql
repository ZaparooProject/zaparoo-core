-- +goose Up
-- Create SystemTagsCache table for fast tag lookups by system
-- This table pre-computes tag associations per system to avoid expensive joins
-- during tag filtering in the UI
CREATE TABLE IF NOT EXISTS SystemTagsCache (
    SystemDBID INTEGER NOT NULL,
    TagDBID INTEGER NOT NULL,
    TagType TEXT NOT NULL,
    Tag TEXT NOT NULL,
    PRIMARY KEY (SystemDBID, TagDBID),
    FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE CASCADE
) WITHOUT ROWID;

-- Index for fast system-based tag lookups
CREATE INDEX IF NOT EXISTS idx_systemtagscache_system ON SystemTagsCache(SystemDBID);

-- Index for tag type ordering within systems
CREATE INDEX IF NOT EXISTS idx_systemtagscache_type_tag ON SystemTagsCache(SystemDBID, TagType, Tag);

-- +goose Down
DROP INDEX IF EXISTS idx_systemtagscache_type_tag;
DROP INDEX IF EXISTS idx_systemtagscache_system;
DROP TABLE IF EXISTS SystemTagsCache;