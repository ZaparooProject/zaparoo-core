-- +goose Up
-- +goose StatementBegin

-- Where each playlist was left, per profile, so loading it again resumes
-- there. The shared (no profile) state is ProfileID '' rather than NULL so the
-- unique key holds for it too. OrderJSON is the item order a shuffled
-- playlist was playing in, reused only while its set of items is unchanged.
CREATE TABLE PlaylistMemory (
    DBID INTEGER PRIMARY KEY,
    ProfileID TEXT NOT NULL DEFAULT '',
    PlaylistKey TEXT NOT NULL,
    ItemZapScript TEXT NOT NULL,
    ItemIndex INTEGER NOT NULL DEFAULT 0,
    ItemsHash TEXT NOT NULL,
    OrderJSON TEXT NOT NULL DEFAULT '',
    UpdatedAt INTEGER NOT NULL,
    UNIQUE (ProfileID, PlaylistKey)
);
CREATE INDEX playlistmemory_profile_updated_idx ON PlaylistMemory (ProfileID, UpdatedAt);

-- The playlist open in each media slot, restored paused after a restart. It
-- is device state: one row per slot, owned by the profile that loaded it.
CREATE TABLE OpenPlaylists (
    Slot TEXT PRIMARY KEY,
    ProfileID TEXT NOT NULL DEFAULT '',
    Snapshot TEXT NOT NULL,
    UpdatedAt INTEGER NOT NULL
);

-- How far into a long native audio track playback got. Keyed by
-- (SystemID, Path) because a MediaDB row's DBID changes on a rebuild.
CREATE TABLE MediaPositions (
    DBID INTEGER PRIMARY KEY,
    ProfileID TEXT NOT NULL DEFAULT '',
    SystemID TEXT NOT NULL,
    Path TEXT NOT NULL,
    PositionMs INTEGER NOT NULL,
    DurationMs INTEGER NOT NULL,
    UpdatedAt INTEGER NOT NULL,
    UNIQUE (ProfileID, SystemID, Path)
);
CREATE INDEX mediapositions_profile_updated_idx ON MediaPositions (ProfileID, UpdatedAt);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS mediapositions_profile_updated_idx;
DROP TABLE IF EXISTS MediaPositions;
DROP TABLE IF EXISTS OpenPlaylists;
DROP INDEX IF EXISTS playlistmemory_profile_updated_idx;
DROP TABLE IF EXISTS PlaylistMemory;
-- +goose StatementEnd
