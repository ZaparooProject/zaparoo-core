-- +goose Up
-- Optimize join tables using WITHOUT ROWID for better performance and storage efficiency

-- 1. MediaTags: Convert to WITHOUT ROWID with composite primary key
CREATE TABLE MediaTags_new (
    MediaDBID INTEGER NOT NULL,
    TagDBID   INTEGER NOT NULL,
    PRIMARY KEY(MediaDBID, TagDBID),
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT
) WITHOUT ROWID;

-- Copy existing data (excluding DBID since it's no longer needed)
INSERT INTO MediaTags_new (MediaDBID, TagDBID)
SELECT MediaDBID, TagDBID FROM MediaTags;

-- Replace the old table
DROP TABLE MediaTags;
ALTER TABLE MediaTags_new RENAME TO MediaTags;

-- 2. MediaTitleTags: Convert to WITHOUT ROWID with composite primary key
CREATE TABLE MediaTitleTags_new (
    MediaTitleDBID INTEGER NOT NULL,
    TagDBID        INTEGER NOT NULL,
    PRIMARY KEY(MediaTitleDBID, TagDBID),
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT
) WITHOUT ROWID;

-- Copy existing data (excluding DBID since it's no longer needed)
INSERT INTO MediaTitleTags_new (MediaTitleDBID, TagDBID)
SELECT MediaTitleDBID, TagDBID FROM MediaTitleTags;

-- Replace the old table
DROP TABLE MediaTitleTags;
ALTER TABLE MediaTitleTags_new RENAME TO MediaTitleTags;

-- Note: Indexes are no longer needed since the composite primary key provides the indexing

-- +goose Down
-- Restore original ROWID-based tables

-- 1. Restore MediaTags with ROWID
CREATE TABLE MediaTags_old (
    DBID      INTEGER PRIMARY KEY,
    MediaDBID integer not null,
    TagDBID   integer not null,
    UNIQUE(MediaDBID, TagDBID),
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT
);

INSERT INTO MediaTags_old (MediaDBID, TagDBID)
SELECT MediaDBID, TagDBID FROM MediaTags;

DROP TABLE MediaTags;
ALTER TABLE MediaTags_old RENAME TO MediaTags;

-- 2. Restore MediaTitleTags with ROWID
CREATE TABLE MediaTitleTags_old (
    DBID           INTEGER PRIMARY KEY,
    TagDBID        integer not null,
    MediaTitleDBID integer not null,
    UNIQUE(MediaTitleDBID, TagDBID),
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT
);

INSERT INTO MediaTitleTags_old (MediaTitleDBID, TagDBID)
SELECT MediaTitleDBID, TagDBID FROM MediaTitleTags;

DROP TABLE MediaTitleTags;
ALTER TABLE MediaTitleTags_old RENAME TO MediaTitleTags;

-- Recreate the original indexes
CREATE INDEX mediatags_media_idx ON MediaTags(MediaDBID);
CREATE INDEX mediatags_tag_idx ON MediaTags(TagDBID);
CREATE INDEX mediatitletags_mediatitle_idx ON MediaTitleTags(MediaTitleDBID);
CREATE INDEX mediatitletags_tag_idx ON MediaTitleTags(TagDBID);