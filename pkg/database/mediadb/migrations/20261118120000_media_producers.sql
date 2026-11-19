-- +goose Up
-- MediaProducers records which scan sources reported each media row: the
-- filesystem walk, a launcher scanner, or platform virtual media. When one
-- source fails during an index, reconcile protects only the rows that source
-- reported, so the sources that did succeed still flag what they no longer
-- find. MediaScanners interns the source IDs. ScanStageProducers carries the
-- producers of each staged path through reconcile.

CREATE TABLE MediaScanners (
    DBID      INTEGER PRIMARY KEY,
    ScannerID TEXT NOT NULL UNIQUE
);

CREATE TABLE MediaProducers (
    MediaDBID   INTEGER NOT NULL,
    ScannerDBID INTEGER NOT NULL,
    PRIMARY KEY (MediaDBID, ScannerDBID),
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE,
    FOREIGN KEY (ScannerDBID) REFERENCES MediaScanners(DBID) ON DELETE CASCADE
) WITHOUT ROWID;

CREATE TABLE ScanStageProducers (
    Path      TEXT NOT NULL,
    ScannerID TEXT NOT NULL,
    PRIMARY KEY (Path, ScannerID)
) WITHOUT ROWID;

-- +goose Down
DROP TABLE IF EXISTS ScanStageProducers;
DROP TABLE IF EXISTS MediaProducers;
DROP TABLE IF EXISTS MediaScanners;
