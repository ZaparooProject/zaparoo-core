-- +goose Up
-- Scraped game metadata (text data that benefits from DB storage)
CREATE TABLE ScrapedMetadata (
    DBID           INTEGER PRIMARY KEY,
    MediaTitleDBID INTEGER NOT NULL,
    ScraperSource  TEXT NOT NULL,       -- Which scraper provided this
    Description    TEXT,
    Genre          TEXT,
    Players        TEXT,
    ReleaseDate    TEXT,
    Developer      TEXT,
    Publisher      TEXT,
    Rating         REAL,
    ScrapedAt      INTEGER NOT NULL,    -- Unix timestamp
    FOREIGN KEY (MediaTitleDBID) REFERENCES MediaTitles(DBID)
);

-- Game file hashes for scraper matching
CREATE TABLE GameHashes (
    DBID           INTEGER PRIMARY KEY,
    MediaDBID      INTEGER NOT NULL,    -- References Media.DBID
    CRC32          TEXT,                -- CRC32 hash (8 hex chars)
    MD5            TEXT,                -- MD5 hash (32 hex chars)
    SHA1           TEXT,                -- SHA1 hash (40 hex chars)
    FileSize       INTEGER,             -- File size in bytes
    ComputedAt     INTEGER NOT NULL,    -- Unix timestamp
    FOREIGN KEY (MediaDBID) REFERENCES Media(DBID)
);

-- Create indexes for better performance
CREATE INDEX idx_scraped_metadata_mediatitle ON ScrapedMetadata(MediaTitleDBID);
CREATE INDEX idx_scraped_metadata_source ON ScrapedMetadata(ScraperSource);
CREATE INDEX idx_game_hashes_media ON GameHashes(MediaDBID);
CREATE INDEX idx_game_hashes_md5 ON GameHashes(MD5);
CREATE INDEX idx_game_hashes_crc32 ON GameHashes(CRC32);
CREATE INDEX idx_game_hashes_sha1 ON GameHashes(SHA1);

-- +goose Down
DROP INDEX IF EXISTS idx_game_hashes_sha1;
DROP INDEX IF EXISTS idx_game_hashes_crc32;
DROP INDEX IF EXISTS idx_game_hashes_md5;
DROP INDEX IF EXISTS idx_game_hashes_media;
DROP INDEX IF EXISTS idx_scraped_metadata_source;
DROP INDEX IF EXISTS idx_scraped_metadata_mediatitle;
DROP TABLE IF EXISTS GameHashes;
DROP TABLE IF EXISTS ScrapedMetadata;