-- +goose Up
-- Projection of UserDB's hidden folders, so browse and search read
-- visibility from the database they query. Paths are stable identities;
-- BrowseDirs DBIDs belong to the rebuildable browse cache.
CREATE TABLE IF NOT EXISTS HiddenDirectories (
    SystemID TEXT NOT NULL,
    Path     TEXT NOT NULL,
    PRIMARY KEY (SystemID, Path)
) WITHOUT ROWID;

-- +goose Down
DROP TABLE IF EXISTS HiddenDirectories;
