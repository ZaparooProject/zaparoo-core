-- +goose Up
-- Per-system record of the last successful scan reconcile: a digest of the
-- staged set it consumed and a digest of the stored rows it left behind. When
-- the next scan stages a byte-identical set against unchanged stored rows,
-- reconcile is provably a no-op and is skipped (see sqlReconcileStagedSystem).
-- Rows are written inside the reconcile's own transaction so a rollback
-- discards them together with the work they describe.
CREATE TABLE IF NOT EXISTS ScanSystemFingerprints (
    SystemDBID  INTEGER PRIMARY KEY,
    Fingerprint TEXT    NOT NULL,
    StateDigest TEXT    NOT NULL,
    MediaCount  INTEGER NOT NULL,
    TitleCount  INTEGER NOT NULL,
    ReconcileMs INTEGER NOT NULL,
    FOREIGN KEY (SystemDBID) REFERENCES Systems(DBID) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE IF EXISTS ScanSystemFingerprints;
