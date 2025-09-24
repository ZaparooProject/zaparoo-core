-- +goose Up
-- Comprehensive foreign key migration for all tables
-- SQLite doesn't support adding foreign keys to existing tables,
-- so we need to recreate them with proper constraints

-- 1. MediaTitles: CASCADE from Systems
CREATE TABLE MediaTitles_new (
    DBID       INTEGER PRIMARY KEY,
    SystemDBID integer not null,
    Slug       text    not null,
    Name       text    not null,
    FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID) ON DELETE CASCADE
);
INSERT INTO MediaTitles_new SELECT * FROM MediaTitles;
DROP TABLE MediaTitles;
ALTER TABLE MediaTitles_new RENAME TO MediaTitles;

-- 2. Media: CASCADE from MediaTitles
CREATE TABLE Media_new (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    Path           text    not null,
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE
);
INSERT INTO Media_new SELECT * FROM Media;
DROP TABLE Media;
ALTER TABLE Media_new RENAME TO Media;

-- 3. Tags: CASCADE from TagTypes
CREATE TABLE Tags_new (
    DBID     INTEGER PRIMARY KEY,
    TypeDBID integer not null,
    Tag      text    not null,
    FOREIGN KEY (TypeDBID) REFERENCES TagTypes(DBID) ON DELETE CASCADE
);
INSERT INTO Tags_new SELECT * FROM Tags;
DROP TABLE Tags;
ALTER TABLE Tags_new RENAME TO Tags;

-- 4. MediaTags: CASCADE from Media, RESTRICT from Tags
CREATE TABLE MediaTags_new (
    DBID      INTEGER PRIMARY KEY,
    MediaDBID integer not null,
    TagDBID   integer not null,
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT,
    UNIQUE(MediaDBID, TagDBID)
);
-- Deduplicate MediaTags during migration (keep first occurrence of duplicates)
INSERT INTO MediaTags_new (DBID, MediaDBID, TagDBID)
SELECT MIN(DBID), MediaDBID, TagDBID
FROM MediaTags
GROUP BY MediaDBID, TagDBID;
DROP TABLE MediaTags;
ALTER TABLE MediaTags_new RENAME TO MediaTags;

-- 5. MediaTitleTags: CASCADE from MediaTitles, RESTRICT from Tags
CREATE TABLE MediaTitleTags_new (
    DBID           INTEGER PRIMARY KEY,
    TagDBID        integer not null,
    MediaTitleDBID integer not null,
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT,
    UNIQUE(MediaTitleDBID, TagDBID)
);
-- Deduplicate MediaTitleTags during migration (keep first occurrence of duplicates)
INSERT INTO MediaTitleTags_new (DBID, TagDBID, MediaTitleDBID)
SELECT MIN(DBID), TagDBID, MediaTitleDBID
FROM MediaTitleTags
GROUP BY MediaTitleDBID, TagDBID;
DROP TABLE MediaTitleTags;
ALTER TABLE MediaTitleTags_new RENAME TO MediaTitleTags;

-- 6. SupportingMedia: CASCADE from MediaTitles, RESTRICT from Tags
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
INSERT INTO SupportingMedia_new SELECT * FROM SupportingMedia;
DROP TABLE SupportingMedia;
ALTER TABLE SupportingMedia_new RENAME TO SupportingMedia;

-- Recreate all indexes for optimal performance
CREATE INDEX mediatitles_slug_idx ON MediaTitles(Slug);
CREATE INDEX mediatitles_system_idx ON MediaTitles(SystemDBID);
CREATE INDEX media_mediatitle_idx ON Media(MediaTitleDBID);
CREATE INDEX tags_tag_idx ON Tags(Tag);
CREATE INDEX tags_tagtype_idx ON Tags(TypeDBID);
CREATE INDEX mediatags_media_idx ON MediaTags(MediaDBID);
CREATE INDEX mediatags_tag_idx ON MediaTags(TagDBID);
CREATE INDEX mediatitletags_mediatitle_idx ON MediaTitleTags(MediaTitleDBID);
CREATE INDEX mediatitletags_tag_idx ON MediaTitleTags(TagDBID);
CREATE INDEX supportingmedia_mediatitle_idx ON SupportingMedia(MediaTitleDBID);
CREATE INDEX supportingmedia_typetag_idx ON SupportingMedia(TypeTagDBID);

-- +goose Down
-- Restore tables without foreign keys and constraints
CREATE TABLE MediaTitles_old (
    DBID       INTEGER PRIMARY KEY,
    SystemDBID integer not null,
    Slug       text    not null,
    Name       text    not null
);
INSERT INTO MediaTitles_old SELECT * FROM MediaTitles;
DROP TABLE MediaTitles;
ALTER TABLE MediaTitles_old RENAME TO MediaTitles;

CREATE TABLE Media_old (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    Path           text    not null
);
INSERT INTO Media_old SELECT * FROM Media;
DROP TABLE Media;
ALTER TABLE Media_old RENAME TO Media;

CREATE TABLE Tags_old (
    DBID     INTEGER PRIMARY KEY,
    TypeDBID integer not null,
    Tag      text    not null
);
INSERT INTO Tags_old SELECT * FROM Tags;
DROP TABLE Tags;
ALTER TABLE Tags_old RENAME TO Tags;

CREATE TABLE MediaTags_old (
    DBID      INTEGER PRIMARY KEY,
    MediaDBID integer not null,
    TagDBID   integer not null
);
INSERT INTO MediaTags_old SELECT * FROM MediaTags;
DROP TABLE MediaTags;
ALTER TABLE MediaTags_old RENAME TO MediaTags;

CREATE TABLE MediaTitleTags_old (
    DBID           INTEGER PRIMARY KEY,
    TagDBID        integer not null,
    MediaTitleDBID integer not null
);
INSERT INTO MediaTitleTags_old SELECT * FROM MediaTitleTags;
DROP TABLE MediaTitleTags;
ALTER TABLE MediaTitleTags_old RENAME TO MediaTitleTags;

CREATE TABLE SupportingMedia_old (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    TypeTagDBID    integer not null,
    Path           string  not null,
    ContentType    text    not null,
    Binary         blob
);
INSERT INTO SupportingMedia_old SELECT * FROM SupportingMedia;
DROP TABLE SupportingMedia;
ALTER TABLE SupportingMedia_old RENAME TO SupportingMedia;

-- Recreate indexes
CREATE INDEX mediatitles_slug_idx ON MediaTitles(Slug);
CREATE INDEX mediatitles_system_idx ON MediaTitles(SystemDBID);
CREATE INDEX media_mediatitle_idx ON Media(MediaTitleDBID);
CREATE INDEX tags_tag_idx ON Tags(Tag);
CREATE INDEX tags_tagtype_idx ON Tags(TypeDBID);
CREATE INDEX mediatags_media_idx ON MediaTags(MediaDBID);
CREATE INDEX mediatags_tag_idx ON MediaTags(TagDBID);
CREATE INDEX mediatitletags_mediatitle_idx ON MediaTitleTags(MediaTitleDBID);
CREATE INDEX mediatitletags_tag_idx ON MediaTitleTags(TagDBID);
CREATE INDEX supportingmedia_mediatitle_idx ON SupportingMedia(MediaTitleDBID);
CREATE INDEX supportingmedia_typetag_idx ON SupportingMedia(TypeTagDBID);
