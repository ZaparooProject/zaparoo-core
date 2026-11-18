-- +goose Up
-- MediaVersionKeys holds the identities a media row asserts beyond its title:
-- an arcade parent set, the folder an alternate sits in, and the like. Titles
-- of one system whose rows share a key are versions of the same game even when
-- their names differ. A key belongs to the media row that asserts it and to
-- the source that wrote it, so a source can replace exactly its own keys.
--
-- MediaTitles.VersionGroup is the result: every title of a group carries the
-- same value, and a title that is linked to nothing keeps NULL and is its own
-- group, so an unlinked library stores and indexes nothing extra.

CREATE TABLE MediaVersionKeys (
    SystemDBID INTEGER NOT NULL,
    KeyType    TEXT    NOT NULL,
    Key        TEXT    NOT NULL,
    MediaDBID  INTEGER NOT NULL,
    Source     TEXT    NOT NULL,
    PRIMARY KEY (SystemDBID, KeyType, Key, MediaDBID, Source),
    FOREIGN KEY (MediaDBID)  REFERENCES Media(DBID)   ON DELETE CASCADE,
    FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID) ON DELETE CASCADE
) WITHOUT ROWID;
CREATE INDEX mediaversionkeys_media_idx ON MediaVersionKeys(MediaDBID, Source);

-- The index over VersionGroup is built by the first regroup, at the end of an
-- index or scrape run, not here: building it walks every title, and nothing
-- reads it before a regroup has given some title a group.
ALTER TABLE MediaTitles ADD COLUMN VersionGroup INTEGER;

-- MediaTitles.VersionCount is how many versions the title's game has, stored
-- by the regroup so a browse reads it instead of counting. NULL is one
-- version. Systems.VersionCountsState says whether a regroup has counted the
-- system's titles: 0 never, so their counts are unknown; 1 counted and current;
-- 2 counted, but the system's media or groups changed since, so the next
-- regroup counts it again. Stale counts are still read until then.
ALTER TABLE MediaTitles ADD COLUMN VersionCount INTEGER;
ALTER TABLE Systems ADD COLUMN VersionCountsState INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE Systems DROP COLUMN VersionCountsState;
ALTER TABLE MediaTitles DROP COLUMN VersionCount;
DROP INDEX IF EXISTS mediatitles_version_group_idx;
ALTER TABLE MediaTitles DROP COLUMN VersionGroup;
DROP INDEX IF EXISTS mediaversionkeys_media_idx;
DROP TABLE IF EXISTS MediaVersionKeys;
