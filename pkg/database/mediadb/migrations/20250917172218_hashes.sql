-- +goose Up
CREATE TABLE MediaHashes (
    DBID           INTEGER PRIMARY KEY,
    SystemID       TEXT NOT NULL,       -- System identifier (e.g., "NES", "Genesis")
    MediaPath      TEXT NOT NULL,       -- Media file path for natural key linking
    ComputedAt     INTEGER NOT NULL,    -- Unix timestamp
    FileSize       INTEGER,             -- File size in bytes
    CRC32          TEXT,                -- CRC32 hash (8 hex chars)
    MD5            TEXT,                -- MD5 hash (32 hex chars)
    SHA1           TEXT                 -- SHA1 hash (40 hex chars)
);

CREATE INDEX idx_media_hashes_natural_key ON MediaHashes(SystemID, MediaPath);
CREATE INDEX idx_media_hashes_md5 ON MediaHashes(MD5);
CREATE INDEX idx_media_hashes_crc32 ON MediaHashes(CRC32);
CREATE INDEX idx_media_hashes_sha1 ON MediaHashes(SHA1);

-- +goose Down
DROP INDEX IF EXISTS idx_media_hashes_sha1;
DROP INDEX IF EXISTS idx_media_hashes_crc32;
DROP INDEX IF EXISTS idx_media_hashes_md5;
DROP INDEX IF EXISTS idx_media_hashes_natural_key;
DROP TABLE IF EXISTS MediaHashes;