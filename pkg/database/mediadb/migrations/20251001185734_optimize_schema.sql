-- +goose Up
-- Comprehensive schema optimization migration
-- Consolidates: foreign keys, indexes, WITHOUT ROWID optimization, and cache tables

-- Step 1: Truncate all tables to allow clean schema changes
-- Disable foreign keys to avoid CASCADE overhead during mass deletion
PRAGMA foreign_keys = OFF;

-- Delete in reverse dependency order (children first, parents last)
delete from SupportingMedia;
delete from MediaTitleTags;
delete from MediaTags;
delete from Media;
delete from MediaTitles;
delete from Tags;
delete from TagTypes;
delete from Systems;

-- Re-enable foreign keys for schema recreation
PRAGMA foreign_keys = ON;

-- Step 2: Recreate tables with foreign keys and optimizations

-- 2.1 MediaTitles: Add CASCADE foreign key from Systems
CREATE TABLE MediaTitles_new (
    DBID       INTEGER PRIMARY KEY,
    SystemDBID integer not null,
    Slug       text    not null,
    Name       text    not null,
    FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID) ON DELETE CASCADE
);
DROP TABLE MediaTitles;
ALTER TABLE MediaTitles_new RENAME TO MediaTitles;

-- 2.2 Media: Add CASCADE foreign key from MediaTitles, SystemDBID for duplicate prevention
CREATE TABLE Media_new (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    SystemDBID     integer not null,
    Path           text    not null,
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID) ON DELETE CASCADE,
    UNIQUE(SystemDBID, Path)  -- Prevent duplicate media entries within same system
);
DROP TABLE Media;
ALTER TABLE Media_new RENAME TO Media;

-- 2.3 Tags: Add CASCADE foreign key from TagTypes
CREATE TABLE Tags_new (
    DBID     INTEGER PRIMARY KEY,
    TypeDBID integer not null,
    Tag      text    not null,
    FOREIGN KEY (TypeDBID) REFERENCES TagTypes(DBID) ON DELETE CASCADE
);
DROP TABLE Tags;
ALTER TABLE Tags_new RENAME TO Tags;

-- 2.4 MediaTags: Convert to WITHOUT ROWID with composite primary key
CREATE TABLE MediaTags_new (
    MediaDBID INTEGER NOT NULL,
    TagDBID   INTEGER NOT NULL,
    PRIMARY KEY(MediaDBID, TagDBID),
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT
) WITHOUT ROWID;
DROP TABLE MediaTags;
ALTER TABLE MediaTags_new RENAME TO MediaTags;

-- 2.5 MediaTitleTags: Convert to WITHOUT ROWID with composite primary key
CREATE TABLE MediaTitleTags_new (
    MediaTitleDBID INTEGER NOT NULL,
    TagDBID        INTEGER NOT NULL,
    PRIMARY KEY(MediaTitleDBID, TagDBID),
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT
) WITHOUT ROWID;
DROP TABLE MediaTitleTags;
ALTER TABLE MediaTitleTags_new RENAME TO MediaTitleTags;

-- 2.6 SupportingMedia: Add CASCADE and RESTRICT foreign keys
CREATE TABLE SupportingMedia_new (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    TypeTagDBID    integer not null,
    Path           string  not null,
    ContentType    text    not null,
    Binary         blob,
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT
);
DROP TABLE SupportingMedia;
ALTER TABLE SupportingMedia_new RENAME TO SupportingMedia;

-- Step 3: Create optimized indexes

-- MediaTitles indexes
CREATE INDEX mediatitles_slug_idx ON MediaTitles(Slug);
CREATE INDEX mediatitles_system_slug_idx ON MediaTitles(SystemDBID, Slug);

-- Media indexes
CREATE INDEX media_mediatitle_idx ON Media(MediaTitleDBID);
CREATE INDEX media_system_path_idx ON Media(SystemDBID, Path);  -- Supports duplicate prevention

-- Tags indexes
CREATE INDEX tags_tag_idx ON Tags(Tag);
CREATE INDEX tags_tagtype_idx ON Tags(TypeDBID);

-- MediaTags indexes (reverse index for tag filtering)
CREATE INDEX mediatags_tag_media_idx ON MediaTags(TagDBID, MediaDBID);

-- SupportingMedia indexes
CREATE INDEX supportingmedia_mediatitle_idx ON SupportingMedia(MediaTitleDBID);
CREATE INDEX supportingmedia_typetag_idx ON SupportingMedia(TypeTagDBID);

-- Step 4: Create cache tables

-- 4.1 SystemTagsCache: Pre-computed tag associations per system
CREATE TABLE SystemTagsCache (
    SystemDBID INTEGER NOT NULL,
    TagDBID INTEGER NOT NULL,
    TagType TEXT NOT NULL,
    Tag TEXT NOT NULL,
    PRIMARY KEY (SystemDBID, TagDBID),
    FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE CASCADE
) WITHOUT ROWID;

CREATE INDEX idx_systemtagscache_type_tag ON SystemTagsCache(SystemDBID, TagType, Tag);

-- 4.2 MediaCountCache: Cached counts for random media selection
CREATE TABLE MediaCountCache (
    QueryHash TEXT PRIMARY KEY NOT NULL,
    QueryParams TEXT NOT NULL,
    Count INTEGER NOT NULL,
    MinDBID INTEGER NOT NULL,
    MaxDBID INTEGER NOT NULL,
    LastUpdated INTEGER NOT NULL
);

-- 4.3 SlugResolutionCache: Cached successful slug resolutions to avoid expensive fuzzy matching
CREATE TABLE SlugResolutionCache (
    CacheKey TEXT PRIMARY KEY NOT NULL,
    SystemID TEXT NOT NULL,
    Slug TEXT NOT NULL,
    TagFilters TEXT NOT NULL,  -- JSON-serialized tag filters (sorted for consistency)
    MediaDBID INTEGER NOT NULL,
    Strategy TEXT NOT NULL,    -- Which strategy found the match (for debugging/analytics)
    LastUpdated INTEGER NOT NULL,
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE
);

CREATE INDEX idx_slug_cache_system ON SlugResolutionCache(SystemID);
CREATE INDEX idx_slug_cache_media ON SlugResolutionCache(MediaDBID);

-- +goose Down
-- Restore original schema without optimizations

-- Drop cache tables
DROP INDEX IF EXISTS idx_slug_cache_media;
DROP INDEX IF EXISTS idx_slug_cache_system;
DROP TABLE IF EXISTS SlugResolutionCache;
DROP TABLE IF EXISTS MediaCountCache;
DROP INDEX IF EXISTS idx_systemtagscache_type_tag;
DROP TABLE IF EXISTS SystemTagsCache;

-- Restore MediaTitles without foreign keys
CREATE TABLE MediaTitles_old (
    DBID       INTEGER PRIMARY KEY,
    SystemDBID integer not null,
    Slug       text    not null,
    Name       text    not null
);
DROP TABLE MediaTitles;
ALTER TABLE MediaTitles_old RENAME TO MediaTitles;

-- Restore Media without foreign keys or SystemDBID
CREATE TABLE Media_old (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    Path           text    not null
);
DROP TABLE Media;
ALTER TABLE Media_old RENAME TO Media;

-- Restore Tags without foreign keys
CREATE TABLE Tags_old (
    DBID     INTEGER PRIMARY KEY,
    TypeDBID integer not null,
    Tag      text    not null
);
DROP TABLE Tags;
ALTER TABLE Tags_old RENAME TO Tags;

-- Restore MediaTags with ROWID
CREATE TABLE MediaTags_old (
    DBID      INTEGER PRIMARY KEY,
    MediaDBID integer not null,
    TagDBID   integer not null
);
DROP TABLE MediaTags;
ALTER TABLE MediaTags_old RENAME TO MediaTags;

-- Restore MediaTitleTags with ROWID
CREATE TABLE MediaTitleTags_old (
    DBID           INTEGER PRIMARY KEY,
    TagDBID        integer not null,
    MediaTitleDBID integer not null
);
DROP TABLE MediaTitleTags;
ALTER TABLE MediaTitleTags_old RENAME TO MediaTitleTags;

-- Restore SupportingMedia without foreign keys
CREATE TABLE SupportingMedia_old (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    TypeTagDBID    integer not null,
    Path           string  not null,
    ContentType    text    not null,
    Binary         blob
);
DROP TABLE SupportingMedia;
ALTER TABLE SupportingMedia_old RENAME TO SupportingMedia;

-- Recreate original indexes
CREATE INDEX mediatitles_slug_idx ON MediaTitles(Slug);
CREATE INDEX mediatitles_system_idx ON MediaTitles(SystemDBID);
CREATE INDEX media_mediatitle_idx ON Media(MediaTitleDBID);
CREATE INDEX tags_tag_idx ON Tags(Tag);
CREATE INDEX tags_tagtype_idx ON Tags(TypeDBID);
