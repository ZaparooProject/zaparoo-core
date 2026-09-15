-- +goose Up
-- Answers from resolving identity fingerprints for Library sync. The ordinal
-- is Zaparoo Online's private bitmap position for the fingerprint's release,
-- or 0 with a rejection code. Only the ordinal is kept: it is transport, not
-- identity. The cache is disposable with the rest of this database; losing it
-- only costs resolving the library again.
CREATE TABLE LibraryOrdinalCache (
    Fingerprint    BLOB PRIMARY KEY,
    Ordinal        INTEGER NOT NULL,
    Code           TEXT NOT NULL DEFAULT '',
    ResolvedAt     INTEGER NOT NULL,
    SeenGeneration INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;

-- +goose Down
DROP TABLE IF EXISTS LibraryOrdinalCache;
