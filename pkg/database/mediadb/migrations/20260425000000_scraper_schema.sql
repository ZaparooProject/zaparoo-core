-- +goose Up

-- Add IsExclusive flag to TagTypes.
-- IsExclusive = 1 means the type is single-value per entity: scrapers perform
-- delete-then-insert. IsExclusive = 0 means values accumulate: INSERT OR IGNORE.
-- This is intent metadata, not a schema constraint.
ALTER TABLE TagTypes ADD COLUMN IsExclusive INTEGER NOT NULL DEFAULT 0;

UPDATE TagTypes SET IsExclusive = 1 WHERE Type IN (
    'developer', 'publisher', 'year', 'rating',
    'rev', 'disc', 'disctotal',
    'players', 'extension',
    'media', 'arcadeboard',
    'season', 'episode', 'track', 'volume', 'issue',
    'unfinished', 'copyright'
);

-- Replace SupportingMedia with MediaTitleProperties.
-- Text replaces Path; UNIQUE(MediaTitleDBID, TypeTagDBID) enforces one property
-- of each type per title.
CREATE TABLE MediaTitleProperties (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    TypeTagDBID    integer not null,
    Text           text    not null DEFAULT '',
    ContentType    text    not null,
    Binary         blob,
    UNIQUE(MediaTitleDBID, TypeTagDBID),
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID)    REFERENCES Tags(DBID)        ON DELETE RESTRICT
);

-- Migrate existing rows; Path becomes Text.
-- INSERT OR IGNORE respects the new unique constraint.
INSERT OR IGNORE INTO MediaTitleProperties
    (DBID, MediaTitleDBID, TypeTagDBID, Text, ContentType, Binary)
SELECT DBID, MediaTitleDBID, TypeTagDBID, Path, ContentType, Binary
FROM SupportingMedia;

DROP TABLE SupportingMedia;

CREATE INDEX mediatitleproperties_title_idx   ON MediaTitleProperties(MediaTitleDBID);
CREATE INDEX mediatitleproperties_typetag_idx ON MediaTitleProperties(TypeTagDBID);

-- New ROM-level properties table for region-specific artwork, per-ROM clips, etc.
CREATE TABLE MediaProperties (
    DBID        INTEGER PRIMARY KEY,
    MediaDBID   integer not null,
    TypeTagDBID integer not null,
    Text        text    not null DEFAULT '',
    ContentType text    not null,
    Binary      blob,
    UNIQUE(MediaDBID, TypeTagDBID),
    FOREIGN KEY (MediaDBID)   REFERENCES Media(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID) REFERENCES Tags(DBID)  ON DELETE RESTRICT
);

CREATE INDEX mediaproperties_media_idx   ON MediaProperties(MediaDBID);
CREATE INDEX mediaproperties_typetag_idx ON MediaProperties(TypeTagDBID);

-- +goose Down
-- NOTE: MediaProperties rows are not recoverable after this down migration.

CREATE TABLE SupportingMedia (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID integer not null,
    TypeTagDBID    integer not null,
    Path           string  not null,
    ContentType    text    not null,
    Binary         blob,
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID)    REFERENCES Tags(DBID)        ON DELETE RESTRICT
);

INSERT INTO SupportingMedia
    (DBID, MediaTitleDBID, TypeTagDBID, Path, ContentType, Binary)
SELECT DBID, MediaTitleDBID, TypeTagDBID, Text, ContentType, Binary
FROM MediaTitleProperties;

DROP TABLE MediaProperties;
DROP TABLE MediaTitleProperties;

ALTER TABLE TagTypes DROP COLUMN IsExclusive;
