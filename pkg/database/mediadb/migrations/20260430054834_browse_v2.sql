-- +goose Up
DROP INDEX IF EXISTS idx_media_parentdir_path;
DROP INDEX IF EXISTS idx_media_parentdir_system_path;
DROP INDEX IF EXISTS idx_media_parentdir_browsename;
DROP INDEX IF EXISTS idx_media_parentdir_system_browsename;
DROP INDEX IF EXISTS idx_media_parentdir_browsechar_name;
DROP INDEX IF EXISTS idx_media_parentdir_system_browsechar_name;
DROP INDEX IF EXISTS idx_browsecache_parent_name;
DROP INDEX IF EXISTS idx_browsesystemcache_parent_name;
DROP INDEX IF EXISTS idx_browsecache_parent;
DROP INDEX IF EXISTS idx_browsecache_virtual;
DROP INDEX IF EXISTS idx_browsesystemcache_parent;
DROP INDEX IF EXISTS idx_browsesystemcache_dir;
DROP INDEX IF EXISTS idx_browsesystemcache_virtual;

DROP TABLE IF EXISTS BrowseSystemCache;
DROP TABLE IF EXISTS BrowseCache;

CREATE TABLE IF NOT EXISTS BrowseDirs (
    DBID integer primary key,
    ParentDirDBID integer,
    Path text unique not null,
    Name text not null,
    IsVirtual bool default false,
    foreign key (ParentDirDBID) references BrowseDirs (DBID) on delete cascade
);

CREATE TABLE IF NOT EXISTS BrowseEntries (
    ParentDirDBID integer not null,
    MediaDBID integer primary key,
    SystemDBID integer not null,
    Name text not null,
    NameFirstChar text not null,
    FileName text not null,
    foreign key (ParentDirDBID) references BrowseDirs (DBID) on delete cascade,
    foreign key (MediaDBID) references Media (DBID) on delete cascade,
    foreign key (SystemDBID) references Systems (DBID) on delete cascade
);

CREATE TABLE IF NOT EXISTS BrowseDirCounts (
    ParentDirDBID integer not null,
    ChildDirDBID integer not null,
    SystemDBID integer not null,
    FileCount integer not null,
    primary key (ParentDirDBID, ChildDirDBID, SystemDBID),
    foreign key (ParentDirDBID) references BrowseDirs (DBID) on delete cascade,
    foreign key (ChildDirDBID) references BrowseDirs (DBID) on delete cascade,
    foreign key (SystemDBID) references Systems (DBID) on delete cascade
);

CREATE INDEX IF NOT EXISTS idx_browsedirs_parent_name ON BrowseDirs(ParentDirDBID, Name);
CREATE INDEX IF NOT EXISTS idx_browseentries_parent_system_name ON BrowseEntries(ParentDirDBID, SystemDBID, Name, MediaDBID);
CREATE INDEX IF NOT EXISTS idx_browseentries_parent_system_file ON BrowseEntries(ParentDirDBID, SystemDBID, FileName, MediaDBID);
CREATE INDEX IF NOT EXISTS idx_browsedircounts_parent_system ON BrowseDirCounts(ParentDirDBID, SystemDBID);
CREATE INDEX IF NOT EXISTS idx_browsedircounts_child_system ON BrowseDirCounts(ChildDirDBID, SystemDBID);

INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES ('BrowseIndexVersion', '0');
INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES ('OptimizationStatus', 'pending');
INSERT OR REPLACE INTO DBConfig (Name, Value) VALUES ('OptimizationStep', 'browse_cache');
DELETE FROM DBConfig WHERE Name = 'BrowseSortReady';

-- +goose Down
DROP INDEX IF EXISTS idx_browsedircounts_child_system;
DROP INDEX IF EXISTS idx_browsedircounts_parent_system;
DROP INDEX IF EXISTS idx_browseentries_parent_system_file;
DROP INDEX IF EXISTS idx_browseentries_parent_system_name;
DROP INDEX IF EXISTS idx_browsedirs_parent_name;

DROP TABLE IF EXISTS BrowseDirCounts;
DROP TABLE IF EXISTS BrowseEntries;
DROP TABLE IF EXISTS BrowseDirs;

CREATE TABLE IF NOT EXISTS BrowseCache (
    DirPath text primary key,
    ParentPath text not null,
    Name text not null,
    FileCount integer not null,
    IsVirtual bool default false
);

CREATE INDEX IF NOT EXISTS idx_browsecache_parent ON BrowseCache(ParentPath);
CREATE INDEX IF NOT EXISTS idx_browsecache_virtual ON BrowseCache(IsVirtual);

CREATE TABLE IF NOT EXISTS BrowseSystemCache (
    SystemDBID integer not null,
    DirPath text not null,
    ParentPath text not null,
    Name text not null,
    FileCount integer not null,
    IsVirtual bool default false,
    primary key (SystemDBID, DirPath),
    foreign key (SystemDBID) references Systems (DBID) on delete cascade
);

CREATE INDEX IF NOT EXISTS idx_browsesystemcache_parent ON BrowseSystemCache(SystemDBID, ParentPath);
CREATE INDEX IF NOT EXISTS idx_browsesystemcache_dir ON BrowseSystemCache(DirPath);
CREATE INDEX IF NOT EXISTS idx_browsesystemcache_virtual ON BrowseSystemCache(SystemDBID, IsVirtual);

DELETE FROM DBConfig WHERE Name = 'BrowseIndexVersion';
