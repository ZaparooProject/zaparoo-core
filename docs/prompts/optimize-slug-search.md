Optimize slug search cache substring matching. Priority #1 — furthest from target.

## Target

MiSTer target: < 100ms per search query at 500k entries
MiSTer baseline: 281ms (single-word query at 500k)
x86 CI threshold: < 2.9ms (100ms / 35x search multiplier)
Gap: ~3x over target

## The Problem

`SlugSearchCache.Search()` in `pkg/database/mediadb/slug_search_cache.go:163-207` does a linear scan over ALL entries using `bytes.Contains()`. Every search query touches every entry — O(n) per query regardless of the search term.

The cache stores slugs as a flat `[]byte` blob (`slugData`) with offset arrays. Each entry also has an optional secondary slug (`secSlugData`). The search uses an AND-of-ORs pattern: `variantGroups` is a list of groups where each group contains byte variants for one query word. All groups must match (AND), and within a group any variant can match (OR).

The system filter (`systemDBIDs`) provides early exit for non-matching systems, but the slug comparison is still the bottleneck.

## Key Files

| File | What |
|------|------|
| `pkg/database/mediadb/slug_search_cache.go` | Cache struct, `Search()`, `buildSlugSearchCache()`, entry accessors |
| `pkg/database/mediadb/slug_search_cache_test.go` | All search benchmarks and tests |
| `pkg/database/mediadb/slug_metadata.go` | `GenerateSlugWithMetadata()` — builds metadata during indexing |
| `pkg/database/slugs/slugify.go` | How slugs are generated (determines what's in the cache) |

## What to Optimize

The `Search()` method (lines 163-207). The goal is to replace the linear `bytes.Contains()` scan with a sub-linear data structure.

**The cache build (`buildSlugSearchCache`) can be slower** if it enables faster search. The cache is built once at startup and searched many times. Trading build time and memory for search speed is the right tradeoff.

## Constraints

- Cache is read-only after build — no locks needed for concurrent reads
- `Search()` must return the same results as the current implementation (all matching `titleDBIDs`)
- The AND-of-ORs `variantGroups` pattern must be preserved — callers depend on it
- System filter must still work
- `ExactSlugMatch`, `PrefixSlugMatch`, `ExactSlugMatchAny`, `RandomEntry` are separate methods — don't break them. They're already fast enough.
- Memory budget: cache at 500k is currently ~20MB. An index can use additional memory but should stay reasonable (< 50MB total for 500k entries including the index)

## Algorithmic Ideas

- **Inverted index**: Map each unique word (or n-gram) to a list of entry indices. Query words look up their posting lists and intersect them. Turns AND-of-ORs into set intersections.
- **Trigram index**: Map each 3-character sequence to entry indices. Substring search becomes: find trigrams in query, intersect posting lists, verify candidates with `bytes.Contains()`. Well-suited for substring matching.
- **Suffix array**: Sorted array of all suffixes with binary search. Good for substring queries but complex to build for multiple entries.
- **Hybrid**: Use an inverted word index for whole-word queries (most common case) with trigram fallback for partial matches.

The current `variantGroups` structure suggests callers already split queries into words — an inverted word index may be the most natural fit.

## Benchmarks

```bash
# Primary benchmark — this is what needs to improve
go test -run='^$' -bench='BenchmarkSlugSearchCacheSearch' -benchmem -count=6 ./pkg/database/mediadb/

# Check build time doesn't regress catastrophically
go test -run='^$' -bench='BenchmarkSlugSearchCacheBuild' -benchmem -count=6 ./pkg/database/mediadb/

# Check memory footprint
go test -run='^$' -bench='BenchmarkSlugSearchCacheMemory' -benchmem ./pkg/database/mediadb/

# Full comparison
go test -run='^$' -bench='BenchmarkSlugSearchCache' -benchmem -count=6 ./pkg/database/mediadb/ \
  | grep -E '^(Benchmark|goos:|goarch:|pkg:|cpu:)' > /tmp/bench-current.txt
benchstat testdata/benchmarks/baseline.txt /tmp/bench-current.txt
```

## Success Criteria

- Search at 500k: < 2.9ms on x86 (currently ~8ms)
- No regression in build time > 50% (build is a startup-only cost, some increase is acceptable)
- Memory increase < 2x (< 50MB total at 500k)
- All existing tests pass
- `task lint-fix` passes clean
