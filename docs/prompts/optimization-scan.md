Run benchmarks and identify optimization opportunities in the database and search layer.

1. Run `task bench` and capture full output
2. Run `task bench-compare` to compare against stored baseline
3. Identify any regressions >5% in ns/op, B/op, or allocs/op
4. Profile allocation hotspots: focus on functions with highest allocs/op
5. Check for optimization opportunities in priority order:
   - SlugSearchCache: bytes.Contains() linear scan, system filter short-circuiting
   - Fuzzy matching: Jaro-Winkler per-call cost, length pre-filter effectiveness
   - Slug generation: regex allocations, Unicode normalization
   - Indexing pipeline: tag extraction regex, batch transaction overhead
6. For any proposed optimization:
   - Make targeted changes (one optimization per commit)
   - Re-run benchmarks with `go test -bench=BenchmarkAffected -benchmem -count=6`
   - Only commit if >10% improvement with no regressions elsewhere
   - Include before/after benchstat output in commit message
7. Report findings summary with affected files and measured impact

Reference: docs/optimization-targets.md for target thresholds
