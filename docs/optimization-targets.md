# Optimization Targets

Measurable performance targets for zaparoo-core. Background agents should use these as benchmarks for optimization work.

## Critical Path: Token Scan to Launch

Target: < 100ms from NFC scan to launcher invocation (software path, excluding launcher startup)

Based on human perception research (Miller 1968, Nielsen): < 100ms feels instantaneous to the user. Physical token tap adds 100-300ms of perceived latency attributed to the physical action, giving additional forgiveness.

Components: NDEF parse -> mapping match -> ZapScript parse -> command dispatch -> title resolve -> launch

## Slug Search Cache - Search

Target at 500k entries: < 100ms per search query (single word)
Currently: linear scan with bytes.Contains(), O(n) per query
MiSTer baseline at 500k: 281ms — nearly 3x over target. #1 optimization priority.

Search is on the critical tap path via launch.search ZapScript, so the same 100ms perceptual instant threshold applies.

## Slug Search Cache - Build

Target at 500k titles: < 5s cache construction from DB
MiSTer baseline at 500k: 1.96s — well within target. Build is a startup-only operation.

## Slug Search Cache - Memory

Target at 500k titles: < 30MB heap

Production measurement: 129k titles = 5.2MB (42 bytes/entry). Cache indexes titles, not media paths — 239k media files deduplicate to 129k titles (1.85:1 ratio). Extrapolated: 500k titles ≈ 20MB. To reach 500k titles requires ~925k media files.

Note: the B/op metric in benchmarks includes build-time temporaries (~51MB at 500k) and overstates retained size. The `MB` custom metric measures retained cache size via `cache.Size()`.

## Fuzzy Matching

Target: < 100ms per query (same perceptual instant threshold — fuzzy is on the tap path as a fallback)
Currently: O(n*m) Jaro-Winkler, length pre-filter eliminates ~70-80%

Best case (match found) at production 129k titles: ~44ms — within target. Exceeds 100ms above ~296k titles.
Worst case (no match, full scan) at production 129k titles: ~331ms — needs algorithmic improvement (inverted index or candidate limit) to hit 100ms.

## Media Indexing

Target: Full re-index of 239k media in < 15 minutes on MiSTer (real-world, including filesystem walk and overhead)
Benchmark target: < 21s per 10k in IndexingPipeline (MiSTer). Current: 32s per 10k — needs 1.5x speedup.
Components: fastwalk -> filename parsing -> slugification -> tag extraction -> DB batch insert

Real-world overhead is ~1.8x the benchmark pipeline (filesystem walking, SD card I/O, WAL checkpointing, cache rebuild).

## Memory Budget (Total)

Target idle RSS: < 50MB (MiSTer, 129k titles / 239k media)
Current idle RSS: 101MB — 21% of MiSTer's 492MB RAM. Slug cache is only 5MB; the remaining ~96MB is Go runtime, SQLite page cache, and unreleased post-indexing memory (RSS = peak RSS).

Optimization levers: GOGC/GOMEMLIMIT tuning, SQLite page cache sizing, Go memory scavenger behavior, heap profiling at idle.

## Title Resolution Cache

The resolution cache (`SlugResolutionCache`) only stores the `mediaDBID` and strategy name — not the full result object. A "cache hit" still executes 2 heavy DB queries via `GetMediaByDBID` (a 3-table join + a UNION with 6 table joins) to reconstruct the result. MiSTer baseline shows 12ms per cache hit (190x slower than x86) because DB reads dominate.

Optimization opportunity: cache the full result object to make cache hits a pure memory lookup (~41x ratio instead of ~190x).

## x86-to-MiSTer Multiplier Bands

Derived from baseline comparison across 70+ benchmarks. These enable CI prediction of MiSTer performance from x86 benchmark runs.

MiSTer hardware: DE10-Nano, ARM Cortex-A9 ~800MHz, 492MB RAM, SD card storage. Effectively single-core — the second core runs the FPGA main process at 100%.

| Band | Ratio | What it covers | Spread |
|------|-------|----------------|--------|
| Pure CPU | 41x | Text processing, parsing, slugification | 33-65x |
| Search/match | 35x | Slug search, fuzzy matching, Jaro-Winkler | 24-43x |
| DB reads | 138x | Cache build from DB, query-heavy paths | 132-145x |
| DB writes (in-transaction) | 240x | Batch inserts, transaction cycles, flushes | 183-274x |
| DB writes (with commits/fsync) | 1220x | Full indexing pipeline with fsync to SD card | 1220-1222x |

To derive CI thresholds from MiSTer targets: divide the MiSTer target by the appropriate multiplier. For example, a 100ms MiSTer search target implies a 2.9ms x86 CI threshold (100ms / 35x).

Concurrent benchmarks are not CI-predictable due to core count differences (MiSTer: 1 core, x86: 16 threads).
