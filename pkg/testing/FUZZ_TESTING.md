# Fuzz Testing

Zaparoo Core uses native Go fuzzing (Go 1.18+) for discovering edge cases in parsing and validation functions. Fuzzing complements table-driven tests by automatically generating random inputs to find bugs that manual test cases might miss.

### When to Use Fuzz Testing

Fuzz testing is ideal for:

- **Input parsing**: URI parsing, path parsing, file extensions
- **Validation functions**: Scheme validation, port validation, character checks
- **Security-critical code**: Anything handling untrusted user input
- **Complex state machines**: Functions with many branches and edge cases

### Writing Fuzz Tests

Fuzz tests use the `testing.F` type and follow a simple pattern:

```go
// pkg/helpers/uris_fuzz_test.go
package helpers

import (
    "strings"
    "testing"
    "unicode/utf8"
)

// FuzzParseVirtualPathStr tests ParseVirtualPathStr with random inputs
func FuzzParseVirtualPathStr(f *testing.F) {
    // 1. Seed corpus with known good/bad inputs
    f.Add("steam://123/GameName")
    f.Add("steam://456/Game%20With%20Spaces")
    f.Add("") // Edge case
    f.Add("://") // Malformed

    // 2. Define fuzz target (test function)
    f.Fuzz(func(t *testing.T, virtualPath string) {
        // Call the function - should never panic
        result, err := ParseVirtualPathStr(virtualPath)

        // 3. Test properties (invariants), not specific outputs

        // Property 1: Result should always be valid UTF-8
        if err == nil {
            if !utf8.ValidString(result.Name) {
                t.Errorf("Invalid UTF-8 in Name: %q", result.Name)
            }
        }

        // Property 2: Should reject paths without scheme
        if !strings.Contains(virtualPath, "://") && err == nil {
            t.Errorf("Should reject path without scheme: %q", virtualPath)
        }

        // Property 3: If contains control chars, should handle gracefully
        if containsControlChar(virtualPath) && err == nil {
            t.Errorf("Should reject control characters: %q", virtualPath)
        }
    })
}
```

### Running Fuzz Tests

```bash
# Run all tests (includes fuzz corpus as regression tests) - FAST
task test
go test ./pkg/helpers/

# Manual fuzzing for specific function (runs until failure or Ctrl+C)
go test -fuzz=FuzzParseVirtualPathStr ./pkg/helpers/virtualpath/

# Time-boxed fuzzing (30 seconds)
go test -fuzz=FuzzParseVirtualPathStr -fuzztime=30s ./pkg/helpers/

# Run with more parallel workers (8 cores)
go test -fuzz=FuzzParseVirtualPathStr -parallel=8 ./pkg/helpers/

# Disable minimization for faster fuzzing
go test -fuzz=FuzzParseVirtualPathStr -fuzzminimizetime=0 ./pkg/helpers/
```

### Property-Based Testing

Focus on testing **properties** (invariants that should always hold), not specific outputs:

```go
// Good: Test properties
f.Fuzz(func(t *testing.T, input string) {
    result := DecodeURIIfNeeded(input)

    // Property: Result must be valid UTF-8
    if !utf8.ValidString(result) {
        t.Errorf("Invalid UTF-8: %q", result)
    }

    // Property: Idempotence - decode twice = decode once
    if DecodeURIIfNeeded(result) != result {
        t.Errorf("Not idempotent: %q", input)
    }
})

// Bad: Test specific outputs (use table-driven tests for this)
f.Fuzz(func(t *testing.T, input string) {
    result := DecodeURIIfNeeded(input)
    if input == "steam://123/Game%20Name" && result != "steam://123/Game Name" {
        t.Errorf("Unexpected result")
    }
})
```

### Interpreting Fuzz Output

When fuzzing finds a bug:

```bash
--- FAIL: FuzzParseVirtualPathStr (0.03s)
    --- FAIL: FuzzParseVirtualPathStr (0.00s)
        uris_fuzz_test.go:25: Invalid UTF-8 in result: "\x91"

    Failing input written to testdata/fuzz/FuzzParseVirtualPathStr/abc123...
    To re-run:
    go test -run=FuzzParseVirtualPathStr/abc123...
```

**Steps to handle:**

1. **Fix the bug** in the source code
2. **Keep the corpus entry** (committed to Git) - it becomes a regression test
3. **Re-run tests** to verify the fix

### Corpus Management

- Failing inputs are automatically saved to `testdata/fuzz/FuzzName/`
- These entries run automatically with `task test` (regression protection)
- **Commit corpus entries to Git** - they're valuable regression tests
- Go automatically minimizes inputs to smallest failing case

### Fuzz Testing Best Practices

1. **Fast targets**: Keep fuzz targets under 1ms per iteration
2. **No side effects**: Fuzz functions should be pure (no I/O, no global state)
3. **Test properties**: Focus on "what shouldn't happen" (crashes, invalid state)
4. **Seed from table tests**: Include edge cases from existing test cases
5. **Deterministic**: Same input should always produce same output

### Fuzz vs Table-Driven Tests

| Aspect | Table-Driven Tests | Fuzz Tests |
|--------|-------------------|------------|
| **Purpose** | Test specific known cases | Discover unknown edge cases |
| **Coverage** | Predictable, explicit | Coverage-guided exploration |
| **Speed** | Very fast (milliseconds) | Slower (runs until stopped) |
| **Maintenance** | Manual test case addition | Auto-discovers new cases |
| **Use case** | Regression, known bugs | Security, edge cases |

**Best practice**: Use both! Table-driven tests for known cases, fuzz tests for discovering new ones.

### Example Fuzz Tests

Fuzz test files across the project:

- `pkg/helpers/uris_fuzz_test.go` - URI parsing and decoding
- `pkg/helpers/paths_fuzz_test.go` - Path operations
- `pkg/helpers/virtualpath/virtualpath_fuzz_test.go` - Virtual path parsing
- `pkg/database/mediascanner/findpath_fuzz_test.go` - Media path matching
- `pkg/database/tags/filename_parser_fuzz_test.go` - Filename tag parsing
- `pkg/readers/shared/ndef/parser_fuzz_test.go` - NDEF record parsing
- `pkg/readers/rs232barcode/rs232barcode_fuzz_test.go` - Barcode input parsing

### Additional Resources

- **Go fuzzing tutorial**: https://go.dev/doc/tutorial/fuzz
- **Go fuzzing docs**: https://go.dev/doc/security/fuzz
