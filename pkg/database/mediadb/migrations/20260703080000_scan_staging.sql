-- +goose Up
-- Scanner staging tables: the indexing pipeline streams scan results here and
-- reconciles them against Media/MediaTitles/MediaTags with set-based SQL, so
-- indexing memory no longer scales with the number of rows in the database.
-- Contents are scratch data scoped to a single system's scan: cleared before
-- staging a system and again after its reconcile. Rows surviving a crash are
-- harmless and removed at the next scan.

CREATE TABLE IF NOT EXISTS ScanStage (
    Path          TEXT PRIMARY KEY,
    ParentDir     TEXT NOT NULL,
    Slug          TEXT NOT NULL,
    TitleName     TEXT NOT NULL,
    SortName      TEXT NOT NULL,
    SlugLength    INTEGER NOT NULL,
    SlugWordCount INTEGER NOT NULL,
    SecondarySlug TEXT
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS scanstage_slug_idx ON ScanStage(Slug);

-- Tag is the stored (padded) form via tags.PadTagValue so joins against
-- Tags.Tag are exact.
CREATE TABLE IF NOT EXISTS ScanStageTags (
    Path    TEXT NOT NULL,
    TagType TEXT NOT NULL,
    Tag     TEXT NOT NULL,
    PRIMARY KEY (Path, TagType, Tag)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS scanstagetags_type_tag_path_idx ON ScanStageTags(TagType, Tag, Path);

-- Title DBIDs whose disambiguation inputs changed during the current system's
-- reconcile; read out for RecomputeTitleDisambiguation, then cleared.
CREATE TABLE IF NOT EXISTS ScanTouchedTitles (
    TitleDBID INTEGER PRIMARY KEY
) WITHOUT ROWID;

-- Runtime indexes used by scan reconcile on large media databases. Keep these
-- in this branch migration because dev/test databases are being recreated.
CREATE INDEX IF NOT EXISTS media_missing_idx ON Media(IsMissing);
CREATE INDEX IF NOT EXISTS media_system_present_path_idx ON Media(SystemDBID, Path) WHERE IsMissing = 0;
CREATE INDEX IF NOT EXISTS media_title_present_idx ON Media(MediaTitleDBID, DBID) WHERE IsMissing = 0;
CREATE INDEX IF NOT EXISTS idx_browsedircounts_system ON BrowseDirCounts(SystemDBID);

-- +goose Down
DROP INDEX IF EXISTS idx_browsedircounts_system;
DROP INDEX IF EXISTS media_title_present_idx;
DROP INDEX IF EXISTS media_system_present_path_idx;
DROP INDEX IF EXISTS media_missing_idx;
DROP TABLE IF EXISTS ScanTouchedTitles;
DROP INDEX IF EXISTS scanstagetags_type_tag_path_idx;
DROP TABLE IF EXISTS ScanStageTags;
DROP INDEX IF EXISTS scanstage_slug_idx;
DROP TABLE IF EXISTS ScanStage;
