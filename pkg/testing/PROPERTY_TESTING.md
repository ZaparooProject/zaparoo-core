# Property-Based Testing with Rapid

Zaparoo Core uses [rapid](https://pgregory.net/rapid) (`pgregory.net/rapid`) for property-based testing. Unlike fuzz testing which explores inputs randomly, rapid uses property-based testing to verify that code invariants hold across many generated inputs.

### When to Use Rapid

Property-based testing is ideal for:

- **Mathematical properties**: Commutativity, associativity, idempotence
- **Round-trip operations**: Encode/decode, serialize/deserialize
- **Ordering invariants**: Sorting, comparison functions
- **State machine properties**: Valid state transitions
- **Determinism**: Same input always produces same output

### Writing Property Tests

```go
package mypackage

import (
    "testing"
    "pgregory.net/rapid"
)

// Test that sorting is deterministic
func TestPropertySortDeterministic(t *testing.T) {
    t.Parallel()
    rapid.Check(t, func(t *rapid.T) {
        // Generate random input
        items := rapid.SliceOf(rapid.String()).Draw(t, "items")

        // Run operation twice
        result1 := sortItems(items)
        result2 := sortItems(items)

        // Verify property: determinism
        if !reflect.DeepEqual(result1, result2) {
            t.Fatalf("Non-deterministic: %v vs %v", result1, result2)
        }
    })
}

// Test order independence with custom generator
func TestPropertyCacheKeyOrderIndependent(t *testing.T) {
    t.Parallel()
    rapid.Check(t, func(t *rapid.T) {
        tags := rapid.SliceOfN(tagGen(), 2, 10).Draw(t, "tags")

        // Shuffle the tags
        shuffled := make([]Tag, len(tags))
        copy(shuffled, tags)
        // ... shuffle logic ...

        key1 := generateCacheKey(tags)
        key2 := generateCacheKey(shuffled)

        // Property: order shouldn't affect the key
        if key1 != key2 {
            t.Fatalf("Order affected key: %q vs %q", key1, key2)
        }
    })
}
```

### Custom Generators

Create domain-specific generators for realistic test data:

```go
// systemIDGen generates realistic system IDs
func systemIDGen() *rapid.Generator[string] {
    return rapid.StringMatching(`[a-z0-9_]{1,20}`)
}

// tagFilterGen generates random TagFilter values
func tagFilterGen() *rapid.Generator[database.TagFilter] {
    return rapid.Custom(func(t *rapid.T) database.TagFilter {
        return database.TagFilter{
            Type:     rapid.SampledFrom([]string{"lang", "region"}).Draw(t, "type"),
            Value:    rapid.StringMatching(`[a-z0-9-]{1,15}`).Draw(t, "value"),
            Operator: rapid.SampledFrom(operators).Draw(t, "op"),
        }
    })
}
```

### Running Property Tests

```bash
# Run property tests (they run with regular tests)
go test ./pkg/database/...

# Run specific property test
go test -run TestProperty ./pkg/database/matcher/

# Run with verbose output
go test -v -run TestProperty ./pkg/database/slugs/
```

### Regression Files (.fail)

When rapid finds a failing input, it saves it to `testdata/rapid/TestName/`:

```bash
--- FAIL: TestPropertyCacheKeyOrderIndependent (0.01s)
    cache_test.go:42: [rapid] panic after 8 tests
    To reproduce, specify -rapid.failfile="testdata/rapid/TestPropertyCacheKeyOrderIndependent/..."
```

**Important**: **Commit .fail files to Git** - they serve as regression tests:

1. They contain the exact input that found a real bug
2. Rapid automatically replays them on every test run
3. They ensure the bug stays fixed across the team and CI
4. They're small files (just seeds, not full inputs)

**Shrinking**: When rapid finds a failing input, it automatically "shrinks" the input to find the smallest possible example that still fails. This makes debugging much easier - instead of a complex 50-element slice, you might get a minimal 2-element case that exposes the bug.

### Property Test Best Practices

1. **Use `t.Parallel()`**: Property tests are independent and can run concurrently
2. **Test invariants, not specifics**: Focus on "what should always be true"
3. **Create domain generators**: Realistic data finds more bugs than random strings
4. **Commit .fail files**: They're valuable regression tests
5. **Keep tests fast**: Each iteration should be under 1ms

### Rapid vs Fuzz Testing

| Aspect | Rapid (Property-Based) | Go Fuzzing |
|--------|------------------------|------------|
| **Library** | `pgregory.net/rapid` | Built-in (`testing.F`) |
| **Input generation** | Structured, type-safe | Byte-level mutation |
| **Best for** | Invariants, properties | Crash discovery, security |
| **Test style** | Multiple properties per test | One property per fuzz target |
| **Regression files** | `testdata/rapid/*.fail` | `testdata/fuzz/*` |

**Best practice**: Use rapid for testing invariants and properties; use Go fuzzing for security-critical parsing of untrusted input.

### CI Integration

Property tests run automatically with `go test` - no special configuration needed. Each test runs 100 iterations by default, completing in under 1 second. Unlike fuzz tests, property tests are fast enough for regular CI.

On CI failure, `.fail` files are uploaded as artifacts so developers can reproduce locally:

```bash
# Download the .fail file from CI artifacts, then:
go test -run TestPropertyCacheKeyOrderIndependent -rapid.failfile=path/to/downloaded.fail ./pkg/...
```

### Example Property Tests

Property test files across the project:

- `pkg/config/config_property_test.go` - Configuration properties
- `pkg/database/filters/parser_property_test.go` - Tag filter parsing
- `pkg/database/matcher/fuzzy_property_test.go` - Fuzzy matching properties
- `pkg/database/mediadb/batch_inserter_property_test.go` - Batch insert properties
- `pkg/database/mediadb/slug_cache_property_test.go` - Cache key determinism
- `pkg/database/slugs/slugify_property_test.go` - Slug normalization properties
- `pkg/database/tags/tags_property_test.go` - Tag parsing properties
- `pkg/database/userdb/media_history_property_test.go` - Media history properties
- `pkg/helpers/paths_property_test.go` - Path normalization and comparison
- `pkg/service/playlists/playlists_property_test.go` - Playlist properties
- `pkg/service/playtime/playtime_property_test.go` - Playtime tracking properties
