-- +goose Up
-- MediaSources persists optional local metadata provenance emitted by launcher
-- scanners for virtual media. ScanStageSources carries the same sparse data
-- through the normal set-based indexing reconcile.

CREATE TABLE MediaSources (
    MediaDBID  INTEGER PRIMARY KEY,
    SourcePath TEXT NOT NULL,
    SourceKey  TEXT NOT NULL,
    SourceRoot TEXT NOT NULL,
    SourceKind TEXT NOT NULL CHECK (SourceKind IN ('file', 'directory')),
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID) ON DELETE CASCADE
);
CREATE INDEX mediasources_key_idx ON MediaSources(SourceKey);

CREATE TABLE ScanStageSources (
    Path       TEXT PRIMARY KEY,
    SourcePath TEXT NOT NULL,
    SourceKey  TEXT NOT NULL,
    SourceRoot TEXT NOT NULL,
    SourceKind TEXT NOT NULL CHECK (SourceKind IN ('file', 'directory'))
) WITHOUT ROWID;

-- +goose Down
DROP TABLE IF EXISTS ScanStageSources;
DROP INDEX IF EXISTS mediasources_key_idx;
DROP TABLE IF EXISTS MediaSources;
