# Optimization Targets

Measurable performance targets for zaparoo-core. Background agents should use these as benchmarks for optimization work.

## Critical Path: Token Scan to Launch

Target: < 100ms from NFC scan to launcher invocation (software path, excluding launcher startup)

Based on human perception research (Miller 1968, Nielsen): < 100ms feels instantaneous to the user. Physical token tap adds 100-300ms of perceived latency attributed to the physical action, giving additional forgiveness.

Components: NDEF parse -> mapping match -> ZapScript parse -> command dispatch -> title resolve -> launch

## Slug Search Cache - Search

Target at 500k entries: < 100ms per search query (single word)
MiSTer baseline at 500k: 36ms — within target (`BenchmarkSlugSearchCacheSearch_500k`).

Search is on the critical tap path via launch.search ZapScript, so the same 100ms perceptual instant threshold applies.

## Slug Search Cache - Build

Target at 500k titles: < 5s cache construction from DB
MiSTer baseline at 500k: 14.4s — nearly 3x over target (`BenchmarkSlugSearchCacheBuild_500k`). The build now also produces the index that made search fast. Build is a startup-only operation, and a normal start loads the persisted cache instead.

## Slug Search Cache - Memory

Target at 500k titles: < 30MB heap

Production measurement: 129k titles = 5.2MB (42 bytes/entry). Cache indexes titles, not media paths — 239k media files deduplicate to 129k titles (1.85:1 ratio). Extrapolated: 500k titles ≈ 20MB. To reach 500k titles requires ~925k media files.

Note: the B/op metric in benchmarks includes build-time temporaries (~51MB at 500k) and overstates retained size. The `MB` custom metric measures retained cache size via `cache.Size()`.

## Fuzzy Matching

Target: < 100ms per query (same perceptual instant threshold — fuzzy is on the tap path as a fallback)
Currently: O(n*m) Jaro-Winkler, length pre-filter eliminates ~70-80%

Best case (match found) at production 129k titles: ~44ms — within target. Exceeds 100ms above ~296k titles.
Worst case (no match, full scan) at production 129k titles: ~331ms — needs algorithmic improvement (inverted index or candidate limit) to hit 100ms.

## Storage on MiSTer

MiSTer mounts its SD card `exfat rw,sync,dirsync`. Every write call is flushed to the card before it returns, so the cost of database work is the number of write calls, not the number of bytes or the `synchronous` setting:

- A 4 KiB write costs 2-4 ms whether it appends or overwrites. 2 MiB written as 512 separate 4 KiB writes took 1.15-2.0 s; the same 2 MiB as two 1 MiB writes took 0.19 s.
- SQLite issues one write per page, two per page for a WAL frame, and one more at checkpoint. A page changed inside a WAL transaction is three flushes.
- `synchronous=FULL` and `NORMAL` measure the same on this mount (16.0 ms against 16.1 ms for a one-row UserDB commit), so UserDB keeps `FULL` at no cost.
- Page size is the lever for bulk loads. Loading the same 12,000 rows spent 14.3 s in commits and checkpoints with 4 KiB pages, 4.8 s with 16 KiB and 3.2 s with 64 KiB (`BenchmarkSQLiteWritePattern`). Read speed with the real 252,000-file library was the same from 4 KiB to 64 KiB.
- A larger page costs a small write more. One tag link committed alone takes 27 ms with 4 KiB pages, 38 ms with 16 KiB and 45 ms with 64 KiB (`BenchmarkSQLiteSmallCommit`). 16 KiB is the trade: bulk loads three times cheaper, a favorite edit or cache write about 11 ms dearer.
- A database file keeps the page size it was created with. A full index of one created with another size replaces the file first, as a fresh-start rebuild does; a selective index never does.
- Anything the indexer does not need to keep must stay out of the main database file. Scan staging rows written there went through the WAL and a checkpoint like real data.

Measure on the SD card, not on `/tmp`: `/tmp` is tmpfs, and a database benchmark run there never pays for a write. `task mister:bench-run` sets `TMPDIR` to the card for that reason. Baselines taken before November 2026 ran on tmpfs with SQLite compiled without optimization.

## Media Indexing

Target: Full re-index of 239k media in < 15 minutes on MiSTer (real-world, including filesystem walk and overhead)
Benchmark target: < 21s per 10k in `BenchmarkMediaScanner_StageAndReconcile_FreshDB` (MiSTer, on the SD card). Current: 27s per 10k — needs 1.3x speedup.
Components: fastwalk -> filename parsing -> slugification -> tag extraction -> DB batch insert

Real-world overhead is ~1.8x the benchmark pipeline (filesystem walking, SD card I/O, WAL checkpointing, cache rebuild).

Measured November 2026 on the test device (252,261 files, 165 systems):

| Run | Before | After |
|-----|--------|-------|
| Update with nothing changed | 67.6 min, 3.6 GB written | 8.0 min, 53 MB written |
| Scrape that follows an unchanged update | 38 min | 1.1 min |
| First index into a new database | not measured | 58-61 min including optimization |
| First scrape after a first index | not measured | 29 min |
| Four systems from empty (55,220 files) | 15.1 min, 1,457 MB written | 8 min, about 420 MB written |
| One system from empty (GBA, 6,115 files) | 141 s, 177 MB written | 66 s, 54 MB written |

An unchanged update is now the directory walk, the indexing throttle and matching files to launchers: a system whose file list matches its stored scan marker is not staged or reconciled at all. About 300 s of the 480 s is reading directory entries from the card. Systems that share a folder each walk it again (54 s on this library).

The marker carries a parsing-rules version and the name catalog versions, not the Core version, so an upgrade only reconciles everything again when what a file is stored as has changed. A first index still stages and reconciles every row.

## Memory Budget (Total)

Target idle RSS: < 50MB (MiSTer, 129k titles / 239k media)
Current idle RSS: 101MB — 21% of MiSTer's 492MB RAM. Slug cache is only 5MB; the remaining ~96MB is Go runtime, SQLite page cache, and unreleased post-indexing memory (RSS = peak RSS).

Optimization levers: GOGC/GOMEMLIMIT tuning, SQLite page cache sizing, Go memory scavenger behavior, heap profiling at idle.

## Title Resolution Cache

The resolution cache (`SlugResolutionCache`) only stores the `mediaDBID` and strategy name — not the full result object. A "cache hit" still executes 2 heavy DB queries via `GetMediaByDBID` (a 3-table join + a UNION with 6 table joins) to reconstruct the result. MiSTer baseline shows 7.7ms per cache hit (120x slower than x86) because DB reads dominate.

Optimization opportunity: cache the full result object to make cache hits a pure memory lookup (~66x ratio instead of ~120x).

## x86-to-MiSTer Multiplier Bands

Derived from the 212 benchmarks present in both baselines (`testdata/benchmarks/baseline.txt` and `baseline-mister.txt`, both regenerated November 2026 from the same commit, the MiSTer run on the SD card). These enable CI prediction of MiSTer performance from x86 benchmark runs.

MiSTer hardware: DE10-Nano, ARM Cortex-A9 ~800MHz, 492MB RAM, SD card storage. Effectively single-core — the second core runs the FPGA main process at 100%.

| Band | Ratio | What it covers | Spread |
|------|-------|----------------|--------|
| Pure CPU | 66x | Text processing, parsing, slugification | 48-92x |
| Search/match | 47x | Slug search, fuzzy matching, Jaro-Winkler | 29-83x |
| DB reads | 144x | Cache build from DB, history pages, query-heavy paths | 87-204x |
| DB writes (bulk) | 378x | Batch inserts, transaction cycles, bulk loads with commits | 144-709x |
| DB writes (single-row commit) | 1778x | One small transaction flushed to the SD card | 1110-2069x |

The MiSTer baseline covers a subset of the MediaDB and media scanner benchmarks: the full set takes more than ten hours on the SD card.

To derive CI thresholds from MiSTer targets: divide the MiSTer target by the appropriate multiplier. For example, a 100ms MiSTer search target implies a 2.1ms x86 CI threshold (100ms / 47x).

Concurrent benchmarks are not CI-predictable due to core count differences (MiSTer: 1 core, x86: 16 threads).
