Investigate and reduce idle memory usage. Priority #4 — investigation-first.

## Target

MiSTer target: < 50MB idle RSS (492MB total RAM, zaparoo shares with FPGA core + Linux)
MiSTer current: 101MB idle RSS (21% of total RAM)
Gap: 2x over target

## The Problem

The slug search cache is only 5.2MB (129k titles, 42 bytes/entry). The remaining ~96MB is:
- Go runtime overhead (GC metadata, goroutine stacks, runtime structures)
- SQLite page cache (default sizing may be too generous for embedded use)
- Unreleased post-indexing memory (RSS = peak RSS; Go's memory scavenger may not return pages to OS)
- Possible heap fragmentation from indexing's allocate-heavy pattern

This is primarily an **investigation task**, not a code optimization. The agent should produce a report with findings and recommendations before making code changes.

## Investigation Steps

### 1. Heap Profile at Idle

On the MiSTer test device (10.0.0.107), after indexing completes and the system is idle:

- Check if pprof is available (it may not be in production builds)
- If not available via pprof, use `runtime.ReadMemStats()` to report: HeapAlloc, HeapSys, HeapIdle, HeapReleased, StackSys, MSpanSys, MCacheSys, GCSys
- Compare HeapAlloc (live objects) vs HeapSys (total heap reserved) vs RSS — the gap between HeapAlloc and RSS is the optimization opportunity

### 2. GOGC / GOMEMLIMIT Tuning

Go 1.19+ supports `GOMEMLIMIT` which caps total Go memory. Combined with GOGC:

- `GOGC=100` (default): GC triggers when heap doubles. After indexing allocates ~50MB of temporaries, the GC target stays high.
- `GOMEMLIMIT=40MiB`: Hard cap that forces more aggressive GC. May increase CPU cost but reduces RSS.
- Test combinations: `GOGC=50 GOMEMLIMIT=40MiB`, `GOGC=100 GOMEMLIMIT=50MiB`, etc.
- Measure: RSS at idle, GC pause times, indexing throughput impact

### 3. SQLite Page Cache

Check the SQLite page cache configuration in the MediaDB and UserDB:

- `PRAGMA cache_size` — default is -2000 (2MB). On MiSTer this may be too large.
- `PRAGMA mmap_size` — if enabled, memory-mapped I/O adds to RSS
- Consider reducing cache_size for MiSTer builds or making it configurable

### 4. Memory Scavenger Behavior

Go returns memory to the OS via the scavenger, but RSS may not drop if:
- Pages are still mapped but not released (`HeapIdle - HeapReleased`)
- The scavenger hasn't run recently (default interval is 2.5 minutes)
- `debug.FreeOSMemory()` after indexing could force immediate release

### 5. Post-Indexing Cleanup

The indexing pipeline allocates large temporary structures (`ScanState` maps, filename buffers). After indexing:
- Are these references dropped so GC can collect them?
- Is `FlushScanStateMaps()` called at the end?
- Could explicit `runtime.GC()` + `debug.FreeOSMemory()` after indexing help?

## Key Files

| File | What |
|------|------|
| `pkg/database/mediadb/mediadb.go` | MediaDB connection setup, SQLite pragmas |
| `pkg/database/userdb/userdb.go` | UserDB connection setup |
| `pkg/database/mediascanner/mediascanner.go` | `NewNamesIndex()` — allocates ScanState, drives indexing |
| `pkg/database/mediascanner/indexing_pipeline.go` | `FlushScanStateMaps()` — clears temporary maps |
| `pkg/database/mediadb/slug_search_cache.go` | Cache struct and `Size()` method |
| `cmd/mister/main.go` | MiSTer entry point — where GOGC/GOMEMLIMIT could be set |

## Benchmarks

Memory benchmarks measure cache size, not total RSS:

```bash
# Cache memory footprint
go test -run='^$' -bench='BenchmarkSlugSearchCacheMemory' -benchmem ./pkg/database/mediadb/

# Peak memory during indexing
go test -run='^$' -bench='BenchmarkGetPathFragments_PeakMemory' -benchmem ./pkg/database/mediascanner/
```

For RSS measurement, use the MiSTer device:
```bash
# After zaparoo is running and idle
ssh root@10.0.0.107 'ps aux | grep zaparoo | grep -v grep'
# Or use task get-logs to check memory stats in application logs
task get-logs -- 10.0.0.107:7497
```

## Expected Output

A report containing:
1. Heap profile breakdown (what's using the 96MB that isn't slug cache)
2. GOGC/GOMEMLIMIT recommendations with measured impact
3. SQLite page cache current size and recommendation
4. Whether post-indexing cleanup is effective
5. Specific code changes recommended (if any), with predicted RSS reduction

## Success Criteria

- Identify where the 96MB gap comes from (heap profile breakdown)
- Propose concrete tuning parameters (GOGC, GOMEMLIMIT, cache_size)
- If code changes proposed: < 50MB idle RSS on MiSTer after applying them
- No performance regressions in indexing or search benchmarks
