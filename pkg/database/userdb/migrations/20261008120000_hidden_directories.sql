-- +goose Up
-- A hidden folder is one row, however much media it holds. The path is the
-- stable identity, as it is for MediaUserData.
CREATE TABLE IF NOT EXISTS HiddenDirectories (
    SystemID  TEXT    NOT NULL,
    Path      TEXT    NOT NULL,
    CreatedAt INTEGER NOT NULL,
    PRIMARY KEY (SystemID, Path)
) WITHOUT ROWID;

-- +goose Down
DROP TABLE IF EXISTS HiddenDirectories;
