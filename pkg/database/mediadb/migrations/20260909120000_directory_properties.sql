-- +goose Up
-- File-backed properties for indexed directories. Paths are stable identities;
-- BrowseDirs DBIDs belong to the rebuildable browse cache and cannot be used as
-- foreign keys here.
CREATE TABLE DirectoryProperties (
    SystemDBID  INTEGER NOT NULL,
    Path        TEXT NOT NULL,
    TypeTagDBID INTEGER NOT NULL,
    Text        TEXT NOT NULL,
    PRIMARY KEY (SystemDBID, Path, TypeTagDBID),
    FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID) ON DELETE CASCADE,
    FOREIGN KEY (TypeTagDBID) REFERENCES Tags(DBID) ON DELETE RESTRICT
) WITHOUT ROWID;

CREATE INDEX directoryproperties_path_system_idx
    ON DirectoryProperties(Path, SystemDBID, TypeTagDBID);
CREATE INDEX directoryproperties_typetag_idx
    ON DirectoryProperties(TypeTagDBID);

-- +goose Down
DROP INDEX IF EXISTS directoryproperties_typetag_idx;
DROP INDEX IF EXISTS directoryproperties_path_system_idx;
DROP TABLE IF EXISTS DirectoryProperties;
