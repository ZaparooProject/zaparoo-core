Optimize the media indexing pipeline. Priority #2 — 1.3x over target.

## Target

MiSTer target: < 21s per 10k files in `BenchmarkMediaScanner_StageAndReconcile_FreshDB`
MiSTer baseline: 27s per 10k files
Real-world target: Full 239k re-index in < 15 minutes (currently about an hour for a first index of 252k files, 9 minutes for an update with nothing changed)
Gap: 1.3x over benchmark target

Real-world overhead is ~1.8x the benchmark pipeline (filesystem walking, SD card I/O, WAL checkpointing, cache rebuild).

## The Problem

The indexing pipeline (`NewNamesIndex()` in `pkg/database/mediascanner/mediascanner.go`) processes files through multiple stages: fastwalk, path fragmentation, slugification, tag extraction, and database inserts with 10k-file transaction batches. On MiSTer (ARM Cortex-A9, SD card storage), DB writes with fsync dominate.

## Pipeline Stages

Per file, inside `AddMediaPath()` at `pkg/database/mediascanner/indexing_pipeline.go:88-290`:

1. **GetPathFragments()** (indexing_pipeline.go:787-866) — Path normalization, `tags.ParseTitleFromFilename()`, `slugs.Slugify()`, `tags.ParseFilenameToCanonicalTags()` (~20 compiled regexes)
2. **System lookup/insert** — Map lookup in `ss.SystemIDs`, DB insert if missing
3. **Title lookup/insert** — Map lookup via `database.TitleKey()`, `mediadb.GenerateSlugWithMetadata()` for fuzzy prefilter, DB insert with metadata
4. **Media + tag insertion** — Media record insert, extension tag, filename tag parsing via `ParseFilenameToCanonicalTags()`, MediaTag associations

Every 10k files: `CommitTransaction()` + `FlushScanStateMaps()` (clears TitleIDs/MediaIDs maps).

## Key Files

| File | What | Multiplier Band |
|------|------|-----------------|
| `pkg/database/mediascanner/mediascanner.go` | `NewNamesIndex()` orchestrator, transaction batching, fastwalk | Mixed |
| `pkg/database/mediascanner/indexing_pipeline.go` | `AddMediaPath()`, `GetPathFragments()`, `FlushScanStateMaps()`, `SeedCanonicalTags()` | DB writes: 240x |
| `pkg/database/tags/filename_parser.go` | `ParseFilenameToCanonicalTags()`, ~20 compiled regex patterns | CPU: 66x |
| `pkg/database/slugs/slugify.go` | `Slugify()`, 9-stage Unicode normalization pipeline | CPU: 66x |
| `pkg/database/slugs/normalization.go` | Unicode normalization stages | CPU: 66x |
| `pkg/database/slugs/media_parsing.go` | Media-type-specific parsing dispatchers | CPU: 66x |
| `pkg/database/mediascanner/mediascanner_bench_test.go` | All indexing benchmarks | - |

## Key Hotspots

1. **DB transaction commits + fsync** (378x for bulk writes, 1778x for a single-row commit on MiSTer SD card) — Each 10k-file batch triggers a commit with fsync. This is the dominant cost on MiSTer.
2. **Slugification** (66x CPU multiplier) — 9-stage Unicode pipeline per title: width normalization, NFKD decomposition, article stripping, character filtering. Called once per unique title.
3. **Regex tag parsing** (66x CPU multiplier) — 20 compiled patterns per filename in `ParseFilenameToCanonicalTags()`. Called once per file.
4. **Map operations** — `FlushScanStateMaps()` clears TitleIDs/MediaIDs every 10k files. Map clear is O(n) in Go.
5. **GenerateSlugWithMetadata()** — Called per unique title during insert. Generates SlugLength, SlugWordCount, TokenSignature, SecondarySlug.

## Constraints

- **Do NOT change**: Resume/selective indexing logic, `maxSystemsPerTransaction` limit, `PopulateScanState*` variants, `SeedCanonicalTags()` — these are correctness-critical
- Transaction batching at 10k files is a tuning parameter — can be adjusted
- The pipeline must remain single-threaded for DB writes (SQLite limitation with `SetMaxOpenConns(1)`)
- CPU-bound stages (parsing, slugification) could potentially be parallelized or pipelined ahead of DB writes

## Benchmarks

The benchmarks open a real SQLite file under `TMPDIR` with the production settings. On x86 that is fast storage, so they measure CPU and SQL cost; the MiSTer cost of a write is set by its `sync` mount (see "Storage on MiSTer" in `docs/optimization-targets.md`) and has to be measured on the device with `task mister:bench`.

| Benchmark | What it measures |
|-----------|-----------------|
| `BenchmarkGetPathFragments_Batch` | Parsing and slugification only, no database |
| `BenchmarkMediaScanner_StageAndReconcile_FreshDB` | Staging and reconciling a system into an empty database |
| `BenchmarkMediaScanner_Reconcile_ExistingRows` | Rescanning a system whose rows already exist |
| `BenchmarkMediaScanner_Reconcile_FixedScanGrowingDB` | A fixed-size scan against a growing database |
| `BenchmarkMediaScanner_GrowingRun_AnalyzeCadence` | Planner statistics cadence over a long run |
| `BenchmarkSQLiteWritePattern` | Page size and checkpoint mode against the storage under `TMPDIR` |

## Optimization Ideas

- **Increase batch size**: Larger batches mean fewer fsync operations. Test 20k or 50k file batches — fewer commits = less SD card I/O
- **Pipeline parsing ahead of DB writes**: `GetPathFragments()` is CPU-bound and independent of DB state. Could process filenames in a goroutine pool, feeding results to the single DB writer
- **Memoize slugification**: Identical titles generate identical slugs. If multiple media files map to the same title, cache the slug result
- **Reduce regex passes**: 20 patterns per filename may have redundant matches. Profile which patterns actually match and consider short-circuit ordering
- **Batch SQL statements**: Prepare INSERT statements once per transaction batch instead of per-file
- **Optimize GenerateSlugWithMetadata()**: Called per unique title — profile whether TokenSignature generation (sort.Strings) is a hotspot

## Benchmarks

```bash
# All indexing benchmarks
go test -run='^$' -bench='Benchmark(MediaScanner_|GetPathFragments_Batch)' \
  -benchmem -count=6 -timeout=30m ./pkg/database/mediascanner/

# Slugification (called during indexing)
go test -run='^$' -bench='BenchmarkSlugify' -benchmem -count=6 ./pkg/database/slugs/

# Tag parsing (called during indexing)
go test -run='^$' -bench='Benchmark' -benchmem -count=6 ./pkg/database/tags/

# Full comparison
go test -run='^$' -bench='Benchmark' -benchmem -count=6 -timeout=30m ./pkg/database/mediascanner/ \
  | grep -E '^(Benchmark|goos:|goarch:|pkg:|cpu:)' > /tmp/bench-current.txt
benchstat testdata/benchmarks/baseline.txt /tmp/bench-current.txt
```

## Success Criteria

- `BenchmarkMediaScanner_StageAndReconcile_FreshDB/10k`: measurable improvement (> 10%) on x86
- `BenchmarkMediaScanner_Reconcile_ExistingRows`: measurable improvement (> 10%) on x86
- No regressions in `BenchmarkGetPathFragments` (per-file parsing)
- All existing tests pass
- `task lint-fix` passes clean
- Predicted MiSTer improvement brings 10k pipeline closer to 21s target
