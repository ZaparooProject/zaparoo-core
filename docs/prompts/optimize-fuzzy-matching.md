Optimize fuzzy matching performance. Priority #2 — worst-case full scan exceeds target.

## Target

MiSTer target: < 100ms per query (on the tap path as search fallback)
MiSTer baseline (best case, match found): ~44ms at 129k titles — within target
MiSTer baseline (worst case, no match): ~331ms at 129k titles — 3x over target
x86 CI threshold: < 2.9ms (100ms / 35x search multiplier)
Gap: Worst case is ~3x over target. Exceeds 100ms above ~296k titles even in best case.

## The Problem

`FindFuzzyMatches()` in `pkg/database/matcher/fuzzy.go` compares every candidate against the query using Jaro-Winkler similarity (via `edlib`). The algorithm is O(n*m) where n is the number of candidates and m is the string comparison cost.

A length pre-filter eliminates ~70-80% of candidates by rejecting entries whose slug length differs too much from the query. But the remaining 20-30% still get full Jaro-Winkler comparison. When no match is found (worst case), every non-filtered candidate is compared and all scores fall below threshold — full scan with no early exit.

## Key Files

| File | What |
|------|------|
| `pkg/database/matcher/fuzzy.go` | `FindFuzzyMatches()`, Jaro-Winkler scoring, length pre-filter |
| `pkg/database/matcher/fuzzy_test.go` | Fuzzy matching benchmarks and tests |
| `pkg/database/mediadb/slug_metadata.go` | `SlugMetadata` struct — precomputed SlugLength, SlugWordCount, TokenSignature |
| `pkg/database/mediadb/slug_search_cache.go` | Cache that provides the candidate list |

## What to Optimize

The `FindFuzzyMatches()` function. The goal is to reduce the number of Jaro-Winkler comparisons in the worst case (no match found) without sacrificing match quality.

## Constraints

- Match quality must not degrade — if the current implementation finds a match, the optimized version must find the same match (or a better one)
- The function is called as a fallback when exact/prefix search fails, so it's on the tap path
- The `SlugMetadata` struct already stores precomputed fields (SlugLength, SlugWordCount, TokenSignature) — use these for filtering
- Must work with the existing `SlugSearchCache` data structures

## Algorithmic Ideas

- **Candidate limiting**: Stop after scanning the first K candidates that pass the length filter. If no match found in K candidates, return empty. Trades recall for speed — acceptable if K is large enough (e.g., 10k candidates)
- **Token signature pre-filter**: `TokenSignature` is a sorted list of word tokens from the slug. Check token overlap before running Jaro-Winkler — if no tokens match, skip the expensive comparison
- **BK-tree**: Build a tree indexed by edit distance. Query traverses only nodes within the distance threshold. O(log n) per query instead of O(n)
- **Inverted token index**: Map tokens to candidate indices. Query tokens look up posting lists, union them, and only run Jaro-Winkler on candidates that share at least one token
- **Word-count bucketing**: Group candidates by SlugWordCount. Only compare candidates with similar word count to the query
- **Early termination**: If a high-confidence match is found (score > 0.95), stop searching

## Benchmarks

```bash
# Primary benchmarks
go test -run='^$' -bench='BenchmarkFindFuzzyMatches' -benchmem -count=6 ./pkg/database/matcher/

# Full comparison
go test -run='^$' -bench='Benchmark' -benchmem -count=6 ./pkg/database/matcher/ \
  | grep -E '^(Benchmark|goos:|goarch:|pkg:|cpu:)' > /tmp/bench-current.txt
benchstat testdata/benchmarks/baseline.txt /tmp/bench-current.txt
```

## Success Criteria

- Worst case (no match) at 129k titles: < 2.9ms on x86 (currently ~9.5ms)
- Best case (match found) must not regress
- Match quality unchanged — same results for same inputs
- All existing tests pass
- `task lint-fix` passes clean
