Autonomous optimization loop for zaparoo-core. Read targets, pick the highest-priority gap, make a single change, measure, and keep or discard based on evidence.

## Setup

1. Read `docs/optimization-targets.md` to understand current targets, baselines, and x86-to-MiSTer multiplier bands
2. Read `CLAUDE.md` for project rules and background agent constraints
3. Run `task bench-compare` to see current performance vs baseline
4. Identify which optimization target has the largest gap between current and target

## Priority Order

If multiple targets are behind, work on the highest priority first:

1. **Slug search** — 3x over target at 500k. See `docs/prompts/optimize-slug-search.md`
2. **Fuzzy matching** — worst case 3x over target. See `docs/prompts/optimize-fuzzy-matching.md`
3. **Media indexing** — 1.5x over target. See `docs/prompts/optimize-indexing.md`
4. **Memory** — 2x over target. See `docs/prompts/optimize-memory.md`

Read the target-specific prompt for the area you're optimizing. It contains the exact files, functions, benchmarks, and constraints.

## Experiment Loop

For each optimization attempt:

### 1. Read the Code
Read the specific source files referenced in the target prompt. Understand the current implementation before proposing changes.

### 2. Propose a Single Change
One optimization per experiment. Keep changes small and focused. Don't combine multiple ideas — test them individually so you can attribute improvements accurately.

### 3. Measure BEFORE

Before making any changes, run benchmarks to capture your local baseline. This is critical — you must compare your own before vs after, not against the committed baseline file (which may have been generated on different hardware).

```bash
go test -run='^$' -bench='BenchmarkAffected' -benchmem -count=6 -timeout=30m ./pkg/specific/ \
  | grep -E '^(Benchmark|goos:|goarch:|pkg:|cpu:)' > /tmp/bench-before.txt
```

### 4. Implement the Change

Make your single, focused optimization.

### 5. Measure AFTER

Run the same benchmarks again:

```bash
go test -run='^$' -bench='BenchmarkAffected' -benchmem -count=6 -timeout=30m ./pkg/specific/ \
  | grep -E '^(Benchmark|goos:|goarch:|pkg:|cpu:)' > /tmp/bench-after.txt
```

Compare your before vs after:

```bash
benchstat /tmp/bench-before.txt /tmp/bench-after.txt
```

### 6. Decide: Keep or Discard

**Keep if ALL of**:
- Improvement >= 10% in the target metric (ns/op for latency, B/op for memory)
- Statistical significance: p < 0.05 in benchstat output
- No regressions > 5% in other benchmarks for the same package

**Discard if ANY of**:
- Improvement < 10%
- p >= 0.05 (not statistically significant)
- Causes regressions in other benchmarks
- Increases code complexity without proportional gain

If discarding: revert the change (`git checkout -- .`) and try a different approach.

### 7. Verify

If keeping the change:
- Run `task lint-fix` — must pass clean
- Run `task test` — all tests must pass
- Run the full package benchmarks (not just the targeted one) to check for regressions

### 8. Commit

Commit each optimization separately with a clear message:
- Format: `perf(<component>): <what changed>`
- Include the benchstat comparison in the commit message body
- Include predicted MiSTer impact using the multiplier band

**You must commit your work in the worktree.** The maintainer will review your commits and cherry-pick changes individually during their review process. Uncommitted work is much harder to review.

## Scope Constraints

- Only modify source files in the component being optimized
- Never change benchmark code — benchmarks measure, they don't get optimized
- Never change test assertions to make tests pass — fix the code, not the tests
- Never add `nolint` directives without justification
- One optimization per commit
- Always run in a worktree or branch — never modify main directly

## MiSTer Performance Prediction

After measuring x86 improvement, predict MiSTer impact using the multiplier bands:

| Band | Ratio | Applies to |
|------|-------|------------|
| Pure CPU | 41x | Text processing, parsing, slugification |
| Search/match | 35x | Slug search, fuzzy matching |
| DB reads | 138x | Cache build, query-heavy paths |
| DB writes (in-transaction) | 240x | Batch inserts, transaction cycles |
| DB writes (with fsync) | 1220x | Full indexing pipeline on SD card |

Example: If x86 slug search improves from 8ms to 2ms, predicted MiSTer improvement is 280ms to 70ms (using 35x search band).

Concurrent benchmarks are NOT predictable across platforms due to core count differences (MiSTer: 1 core, x86: 16 threads).

## Review Workflow

After you commit, the maintainer will:
1. Run benchmarks against the baseline to get official numbers on their machine
2. Review each commit one at a time, staging approved changes
3. Commit with benchstat output in the PR description

This means your commits should be clean and self-contained — one optimization per commit, with a clear message explaining what changed and why.

## Baseline Management

The baseline (`testdata/benchmarks/baseline.txt`) is a fixed reference point. It does NOT get regenerated after every optimization. It gets regenerated at milestones (releases, large accumulations of changes, or when the delta is so large that comparisons become meaningless).

## Reference

- Optimization targets and baselines: `docs/optimization-targets.md`
- x86 baseline: `testdata/benchmarks/baseline.txt`
- MiSTer baseline: `testdata/benchmarks/baseline-mister.txt`
- Target-specific prompts: `docs/prompts/optimize-*.md`
