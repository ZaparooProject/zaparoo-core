-- +goose Up
ALTER TABLE Media ADD COLUMN ParentDir TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS BrowseCache (
    DirPath    TEXT PRIMARY KEY NOT NULL,
    ParentPath TEXT NOT NULL DEFAULT '',
    Name       TEXT NOT NULL DEFAULT '',
    FileCount  INT  NOT NULL DEFAULT 0,
    IsVirtual  INT  NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_browsecache_parent ON BrowseCache(ParentPath);
CREATE INDEX IF NOT EXISTS idx_browsecache_virtual ON BrowseCache(IsVirtual);

-- +goose Down
DROP INDEX IF EXISTS idx_browsecache_virtual;
DROP INDEX IF EXISTS idx_browsecache_parent;
DROP TABLE IF EXISTS BrowseCache;
ALTER TABLE Media DROP COLUMN ParentDir;
