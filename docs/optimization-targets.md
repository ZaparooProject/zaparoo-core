# Optimization Targets

Measurable performance targets for zaparoo-core. Background agents should use these as benchmarks for optimization work.

## Critical Path: Token Scan to Launch

Target: < 50ms from NFC scan to launcher invocation (software path, excluding launcher startup)

Components: NDEF parse -> mapping match -> ZapScript parse -> command dispatch -> title resolve -> launch

## Slug Search Cache - Search

Target at 500k entries: < 100ms per search query (single word)
Target at 1M entries: < 250ms per search query (single word)
Currently: linear scan with bytes.Contains(), O(n) per query

## Slug Search Cache - Build

Target at 500k entries: < 5s cache construction from DB
Target at 1M entries: < 15s cache construction from DB

## Slug Search Cache - Memory

Target at 500k entries: < 30MB heap
Target at 1M entries: < 60MB heap

## Fuzzy Matching

Target at 500k candidates: < 500ms per query
Currently: O(n*m) Jaro-Winkler, length pre-filter eliminates ~70-80%

## Media Indexing

Target: Index 500k files in < 60s
Components: fastwalk -> filename parsing -> slugification -> tag extraction -> DB batch insert

## Memory Budget (Total)

Idle with 500k collection: < 100MB total heap
During re-index of 500k: < 200MB peak heap
