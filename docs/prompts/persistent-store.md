# Persistent MediaDB

This document serves as a planning document for agent work around a new feature. Please use and update this document as a living requirements spec.

## Goal

Media indexing truncates existing records for speed. The primary bottleneck of our indexing implementation is that a primary build target is a very low-spec ARM hardware platform with _very slow_ read/write to SD.

Many strides were made to assist this indexing speed by using maps in memory to track DBIDs. This allows checks against existing records to not require DB reads or complex insert conditions. It also unlocked batching which has been very useful.

Still missing is the ability to rescan OVER existing records.

## Code Context References
@pkg\database\mediascanner\indexing_pipeline.go
@pkg\database\mediadb\mediadb.go
@pkg\database\mediascanner\mediascanner.go
@pkg\database\database.go

## Design Decisions

- **Always persistent**: Every re-index is non-destructive. Truncate only for first-ever index (empty DB) or explicit DB reset.
- **Keep flagged, cleanup later**: IsMissing records persist across scans. Orphan cleanup is deferred (future work: manual trigger or scheduled).
- **Hidden from all queries**: IsMissing=1 records filtered from search, browse, random, and slug resolution.
- **Option A for flush handling**: Don't flush MediaIDs during persistent mode. ~6MB memory cost per large system is acceptable even on ARM.

## Initial strategy

- Adding an IsMissing boolean to Media schema and associated struct. Migration should default to 0 for existing rows.
- Add a new MissingMedia map(int64,interface{}) to ScanState
- When indexing, when a System is being initialized for path scanning
  - Pull all known Media for that system into ScanState
  - Copy all Media.DBID to MissingMedia map in ScanState
  - AddMediaPath will be able to check for existence in scan state
    - Whether found or inserted, delete the MissingMedia key by DBID
  When Indexing is almost complete a new Bulk operation to `UPDATE Media SET IsMissing = 1 WHERE DBID IN (...MissingMedia)` should be performed
- IsMissing should then be a good baseline field to add an orphan cleanup script to post-processing