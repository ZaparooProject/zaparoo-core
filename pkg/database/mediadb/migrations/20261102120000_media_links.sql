-- +goose Up
-- MediaLinks records media rows that are symlink aliases of other media in the
-- same system, such as Arcade Organizer entries indexed by a launcher that
-- opted into scan_duplicates. A linked row carries a copy of its target's
-- scraped data so it browses, searches and shows art like the target.
-- ScanStageLinks carries the scanner's link targets through reconcile.

CREATE TABLE MediaLinks (
    MediaDBID       INTEGER PRIMARY KEY,
    TargetMediaDBID INTEGER NOT NULL,
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TargetMediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE
);
CREATE INDEX medialinks_target_idx ON MediaLinks(TargetMediaDBID);

CREATE TABLE ScanStageLinks (
    Path       TEXT PRIMARY KEY,
    TargetPath TEXT NOT NULL
) WITHOUT ROWID;

-- +goose Down
DROP TABLE IF EXISTS ScanStageLinks;
DROP INDEX IF EXISTS medialinks_target_idx;
DROP TABLE IF EXISTS MediaLinks;
