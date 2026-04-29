-- +goose Up
CREATE TABLE IF NOT EXISTS BrowseSystemCache (
    SystemDBID INTEGER NOT NULL,
    DirPath    TEXT    NOT NULL,
    ParentPath TEXT    NOT NULL DEFAULT '',
    Name       TEXT    NOT NULL DEFAULT '',
    FileCount  INT     NOT NULL DEFAULT 0,
    IsVirtual  INT     NOT NULL DEFAULT 0,
    PRIMARY KEY (SystemDBID, DirPath),
    FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_browsesystemcache_parent ON BrowseSystemCache(SystemDBID, ParentPath);
CREATE INDEX IF NOT EXISTS idx_browsesystemcache_dir ON BrowseSystemCache(DirPath);
CREATE INDEX IF NOT EXISTS idx_browsesystemcache_virtual ON BrowseSystemCache(SystemDBID, IsVirtual);
CREATE INDEX IF NOT EXISTS idx_media_parentdir_system ON Media(ParentDir, SystemDBID);

-- Existing databases need a browse-cache rebuild to populate BrowseSystemCache.
INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES ('OptimizationStatus', 'pending');
INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES ('OptimizationStep', 'browse_cache');

-- +goose Down
DROP INDEX IF EXISTS idx_media_parentdir_system;
DROP INDEX IF EXISTS idx_browsesystemcache_virtual;
DROP INDEX IF EXISTS idx_browsesystemcache_dir;
DROP INDEX IF EXISTS idx_browsesystemcache_parent;
DROP TABLE IF EXISTS BrowseSystemCache;
