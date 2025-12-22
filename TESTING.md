# Testing Guide for Zaparoo Core

This guide provides comprehensive documentation for testing practices in the Zaparoo Core project. It covers the testing infrastructure, patterns, and best practices to help developers write effective tests with comprehensive coverage.

## Table of Contents

- [Overview](#overview)
- [Testing Infrastructure](#testing-infrastructure)
- [Quick Start Guide](#quick-start-guide)
- [Testing Patterns](#testing-patterns)
- [Mock Usage](#mock-usage)
- [Database Testing](#database-testing)
- [Filesystem Testing](#filesystem-testing)
- [API Testing](#api-testing)
- [Service Layer Testing](#service-layer-testing)
- [Fuzz Testing](#fuzz-testing)
- [Best Practices](#best-practices)
- [Running Tests](#running-tests)
- [Troubleshooting](#troubleshooting)

## Overview

Zaparoo Core uses a comprehensive testing infrastructure built around:

- **testify/mock**: For interface mocking and assertions
- **sqlmock**: For database testing without SQLite dependencies
- **afero**: For filesystem abstraction and in-memory testing
- **httptest**: For WebSocket and HTTP API testing
- **Custom testing utilities**: Located in `pkg/testing/`

### Testing Philosophy

- **Comprehensive Coverage**: All new features and bug fixes require tests
- **Fast, Isolated Tests**: Use mocks for external dependencies
- **No Hardware Dependencies**: All hardware interactions are mocked
- **Behavior Testing**: Focus on what the code does, not how it does it
- **Fast Feedback**: Tests should complete in under 5 seconds

## Testing Infrastructure

The testing infrastructure is organized under `pkg/testing/`:

```
pkg/testing/
├── README.md           # Quick reference guide to all testing utilities ⭐
├── mocks/              # Interface mocks
│   ├── reader.go       # Reader interface mock
│   ├── platform.go     # Platform interface mock
│   └── websocket.go    # WebSocket mocks
├── helpers/            # Testing utilities
│   ├── db.go          # Database testing helpers
│   ├── fs.go          # Filesystem testing helpers
│   └── api.go         # API testing helpers
├── fixtures/           # Test data
│   ├── tokens.go      # Sample tokens: SampleTokens(), NewNFCToken(), NewTokenCollection()
│   ├── media.go       # Sample media: SampleMedia(), NewRetroGame(), NewMediaCollection()
│   ├── playlists.go   # Sample playlists: SamplePlaylists()
│   ├── kodi.go        # Kodi test fixtures
│   └── database.go    # Database fixtures and history entries
├── sqlmock/            # SQL mock utilities (testsqlmock.NewSQLMock())
└── examples/           # Example tests and patterns
    ├── mock_usage_example_test.go
    ├── database_example_test.go
    ├── filesystem_example_test.go
    ├── api_example_test.go
    ├── service_token_processing_test.go
    ├── service_zapscript_test.go
    └── service_state_management_test.go
```

**New to testing in Zaparoo?** Start with `pkg/testing/README.md` for a quick reference guide to all available helpers and examples.

## Quick Start Guide

### 1. Basic Test Structure

```go
package mypackage

import (
    "testing"
    
    "github.com/ZaparooProject/zaparoo-core/pkg/testing/fixtures"
    "github.com/ZaparooProject/zaparoo-core/pkg/testing/helpers"
    "github.com/ZaparooProject/zaparoo-core/pkg/testing/mocks"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestMyFunction(t *testing.T) {
    // Setup
    mockPlatform := mocks.NewMockPlatform()
    mockPlatform.SetupBasicMock()
    
    // Test
    result := MyFunction(mockPlatform)
    
    // Verify
    assert.NotNil(t, result)
    mockPlatform.AssertExpectations(t)
}
```

### 2. Using Fixtures

```go
func TestWithFixtures(t *testing.T) {
    // Get sample data
    tokens := fixtures.SampleTokens()
    media := fixtures.SampleMedia()
    systems := fixtures.SampleSystems()
    
    // Use in tests
    assert.Len(t, tokens, 3)
    assert.Equal(t, "Super Mario Bros", media[0].Name)
}
```

### 3. Database Testing

```go
func TestDatabaseOperations(t *testing.T) {
    // Setup mock database
    mockUserDB := helpers.NewMockUserDBI()
    mockUserDB.On("AddHistory", fixtures.HistoryEntryMatcher()).Return(nil)
    
    // Test your function
    err := MyDatabaseFunction(mockUserDB)
    
    // Verify
    require.NoError(t, err)
    mockUserDB.AssertExpectations(t)
}
```

## Testing Patterns

### Table-Driven Tests

Use table-driven tests for testing multiple scenarios:

```go
func TestTokenValidation(t *testing.T) {
    tests := []struct {
        name      string
        token     tokens.Token
        expectErr bool
        errMsg    string
    }{
        {
            name: "Valid NFC token",
            token: fixtures.SampleTokens()[0],
            expectErr: false,
        },
        {
            name: "Invalid token",
            token: tokens.Token{UID: ""},
            expectErr: true,
            errMsg: "empty UID",
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateToken(tt.token)
            if tt.expectErr {
                assert.Error(t, err)
                assert.Contains(t, err.Error(), tt.errMsg)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### Subtests for Organization

```go
func TestComplexWorkflow(t *testing.T) {
    // Setup common to all subtests
    platform := mocks.NewMockPlatform()
    platform.SetupBasicMock()
    
    t.Run("Success case", func(t *testing.T) {
        // Test successful workflow
    })
    
    t.Run("Error case", func(t *testing.T) {
        // Test error handling
    })
    
    t.Run("Edge case", func(t *testing.T) {
        // Test edge cases
    })
}
```

### Concurrent Testing

```go
func TestConcurrentOperations(t *testing.T) {
    const numGoroutines = 10
    var wg sync.WaitGroup
    
    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            // Perform concurrent operation
            result := MyConcurrentFunction(id)
            assert.NotNil(t, result)
        }(i)
    }
    
    wg.Wait()
}
```

## Mock Usage

### Platform Mock

```go
func TestPlatformIntegration(t *testing.T) {
    // Create platform mock
    platform := mocks.NewMockPlatform()
    platform.SetupBasicMock()
    
    // Set specific expectations
    platform.On("LaunchMedia", fixtures.MediaMatcher(), fixtures.SystemMatcher()).Return(nil)
    platform.On("SendKeyboard", "RETURN").Return(nil)
    
    // Use in your code
    MyFunction(platform)
    
    // Verify expectations
    platform.AssertExpectations(t)
    
    // Check recorded actions
    launched := platform.GetLaunchedMedia()
    assert.Len(t, launched, 1)
    
    keyPresses := platform.GetKeyboardPresses()
    assert.Contains(t, keyPresses, "RETURN")
}
```

### Reader Mock

```go
func TestReaderOperations(t *testing.T) {
    // Create reader mock
    reader := mocks.NewMockReader()
    reader.SetupBasicMock()

    // Create a scan channel and simulate token detection
    scanChan := make(chan readers.Scan, 1)
    token := fixtures.SampleTokens()[0]
    reader.SimulateTokenScan(scanChan, token, "mock://test")

    // Receive the scan
    scan := <-scanChan
    require.NoError(t, scan.Error)
    assert.Equal(t, token.UID, scan.Token.UID)

    // For write testing, set up expectations
    reader.On("Write", "test-data").Return(token, nil)
    result, err := reader.Write("test-data")
    require.NoError(t, err)
    assert.Equal(t, token.UID, result.UID)
    reader.AssertExpectations(t)
}
```

### WebSocket Mock

```go
func TestWebSocketCommunication(t *testing.T) {
    // Create mock session
    session := mocks.NewMockMelodySession()
    session.SetupBasicMock()
    
    // Test message sending
    message := []byte(`{"method":"ping"}`)
    err := session.Write(message)
    require.NoError(t, err)
    
    // Verify message was sent
    sent := session.GetSentMessages()
    assert.Len(t, sent, 1)
    assert.Equal(t, message, sent[0])
}
```

## Database Testing

### Using Database Mocks

```go
func TestUserOperations(t *testing.T) {
    // Setup database mocks
    userDB := helpers.NewMockUserDBI()
    mediaDB := helpers.NewMockMediaDBI()
    
    db := &database.Database{
        UserDB:  userDB,
        MediaDB: mediaDB,
    }
    
    // Set expectations
    userDB.On("AddHistory", fixtures.HistoryEntryMatcher()).Return(nil)
    mediaDB.On("GetMediaByText", "Game Name").Return(fixtures.SampleMedia()[0], nil)
    
    // Test your function
    err := ProcessToken(token, db)
    
    // Verify
    require.NoError(t, err)
    userDB.AssertExpectations(t)
    mediaDB.AssertExpectations(t)
}
```

### SQLMock for Raw SQL

```go
import (
    testsqlmock "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/sqlmock"
)

func TestRawSQL(t *testing.T) {
    // Create sqlmock
    db, mock, err := testsqlmock.NewSQLMock()
    require.NoError(t, err)
    defer db.Close()

    // Set expectations
    mock.ExpectQuery("SELECT \\* FROM users WHERE id = \\?").
        WithArgs(1).
        WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).
            AddRow(1, "Test User"))

    // Test your function
    user, err := GetUserByID(db, 1)

    // Verify
    require.NoError(t, err)
    assert.Equal(t, "Test User", user.Name)
    assert.NoError(t, mock.ExpectationsWereMet())
}
```

## Filesystem Testing

### Using In-Memory Filesystem

```go
func TestFileOperations(t *testing.T) {
    // Create in-memory filesystem
    fs := helpers.NewMemoryFS()

    // Write files directly
    err := fs.WriteFile("/config/test.json", []byte(`{"setting": "value"}`), 0o644)
    require.NoError(t, err)

    // Or create a config file with a map
    err = fs.CreateConfigFile("/config/app.json", map[string]any{
        "setting": "value",
        "another": "setting",
    })
    require.NoError(t, err)

    // Read and verify
    content, err := fs.ReadFile("/config/test.json")
    require.NoError(t, err)
    assert.Contains(t, string(content), "value")
}
```

### Complex Directory Structures

```go
func TestMediaScanning(t *testing.T) {
    // Create in-memory filesystem with media directories
    fs := helpers.NewMemoryFS()

    // Create sample media directory structure
    err := fs.CreateMediaDirectory("/media/roms")
    require.NoError(t, err)

    // Or create custom structure
    err = fs.CreateDirectoryStructure(map[string]any{
        "media": map[string]any{
            "games": map[string]any{
                "nes":  map[string]any{"mario.nes": "game-data"},
                "snes": map[string]any{"zelda.sfc": "game-data"},
            },
        },
    })
    require.NoError(t, err)

    // Test your function
    assert.True(t, fs.FileExists("/media/games/nes/mario.nes"))
}
```

## API Testing

### WebSocket Testing

```go
func TestWebSocketAPI(t *testing.T) {
    // Create test server
    server := helpers.NewWebSocketTestServer(t, myHandler)
    defer server.Close()
    
    // Connect client
    conn, err := server.CreateWebSocketClient()
    require.NoError(t, err)
    defer conn.Close()
    
    // Send request
    response, err := helpers.SendJSONRPCRequest(conn, "ping", nil)
    require.NoError(t, err)
    
    // Verify response
    helpers.AssertJSONRPCSuccess(t, response)
    assert.Equal(t, "pong", response.Result)
}
```

### HTTP API Testing

```go
func TestHTTPAPI(t *testing.T) {
    // Create HTTP test helper
    helper := helpers.NewHTTPTestHelper(myHandler)
    defer helper.Close()
    
    // Send request
    resp, err := helper.PostJSONRPC("test_method", nil)
    require.NoError(t, err)
    defer resp.Body.Close()
    
    // Verify response
    assert.Equal(t, http.StatusOK, resp.StatusCode)
}
```

## Service Layer Testing

### Token Processing

```go
func TestTokenProcessing(t *testing.T) {
    // Setup complete environment
    platform := mocks.NewMockPlatform()
    platform.SetupBasicMock()

    db := &database.Database{
        UserDB:  helpers.NewMockUserDBI(),
        MediaDB: helpers.NewMockMediaDBI(),
    }

    // Set expectations for complete workflow
    db.UserDB.(*helpers.MockUserDBI).On("AddHistory", fixtures.HistoryEntryMatcher()).Return(nil)
    db.MediaDB.(*helpers.MockMediaDBI).On("GetMediaByText", "Game").Return(fixtures.SampleMedia()[0], nil)
    platform.On("LaunchMedia", fixtures.MediaMatcher(), fixtures.SystemMatcher()).Return(nil)

    // Test complete workflow
    token := fixtures.SampleTokens()[0]
    err := ProcessTokenWorkflow(token, platform, db)

    // Verify
    require.NoError(t, err)
    launched := platform.GetLaunchedMedia()
    assert.Len(t, launched, 1)
}
```

## Time-Based Testing

Zaparoo Core uses the `clockwork` package for testing time-dependent code. This enables fast, deterministic tests without `time.Sleep()` calls.

### Why Use Clockwork?

**Problems with real time:**
- Tests are slow (waiting for real timeouts/tickers)
- Tests are flaky (timing-dependent race conditions)
- Tests are non-deterministic (can't control exact timing)

**Benefits of fake clocks:**
- Tests run instantly (advance time programmatically)
- Tests are deterministic (exact control over time)
- Tests are reliable (no timing races)

### Basic Setup

Production code should accept a `clockwork.Clock` interface:

```go
import "github.com/jonboulle/clockwork"

type MyService struct {
    clock clockwork.Clock
}

func NewMyService() *MyService {
    return &MyService{
        clock: clockwork.NewRealClock(), // Real clock in production
    }
}

func (s *MyService) DoSomethingPeriodically(ctx context.Context) {
    ticker := s.clock.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.Chan():
            // Do periodic work
            s.performUpdate()
        case <-ctx.Done():
            return
        }
    }
}
```

### Testing with Fake Clock

```go
func TestPeriodicUpdates(t *testing.T) {
    t.Parallel()

    // Create fake clock for testing
    fakeClock := clockwork.NewFakeClock()

    service := &MyService{
        clock: fakeClock, // Inject fake clock for testing
    }

    // Setup mocks
    mockDB := helpers.NewMockUserDBI()
    mockDB.On("UpdateTime", mock.Anything).Return(nil)

    // Start service in goroutine
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    done := make(chan bool)
    go func() {
        service.DoSomethingPeriodically(ctx)
        done <- true
    }()

    // Wait for goroutine to reach the select statement
    err := fakeClock.BlockUntilContext(ctx, 1)
    require.NoError(t, err)

    // Advance time by 1 minute - triggers the ticker instantly
    fakeClock.Advance(1 * time.Minute)

    // Give goroutine brief moment to process
    time.Sleep(10 * time.Millisecond)

    // Verify update was called
    mockDB.AssertExpectations(t)

    // Cleanup
    cancel()
    <-done
}
```

### Common Patterns

#### 1. Testing Time Progression

```go
func TestTimeBasedLogic(t *testing.T) {
    t.Parallel()

    fakeClock := clockwork.NewFakeClock()
    startTime := fakeClock.Now()

    service := &MyService{
        clock: fakeClock,
        startTime: startTime,
    }

    // Advance time by 5 minutes
    fakeClock.Advance(5 * time.Minute)

    // Service should calculate elapsed time as exactly 5 minutes
    elapsed := service.GetElapsedTime() // Uses clock.Since(startTime)
    assert.Equal(t, 5*time.Minute, elapsed)
}
```

#### 2. Testing Tickers

```go
func TestTickerBehavior(t *testing.T) {
    t.Parallel()

    fakeClock := clockwork.NewFakeClock()
    service := &MyService{clock: fakeClock}

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    callCount := 0
    mockDB := helpers.NewMockUserDBI()
    mockDB.On("DoWork").Run(func(args mock.Arguments) {
        callCount++
    }).Return(nil)

    // Start background worker
    go service.PeriodicWork(ctx, mockDB)

    // Wait for goroutine to start
    err := fakeClock.BlockUntilContext(ctx, 1)
    require.NoError(t, err)

    // First tick
    fakeClock.Advance(1 * time.Minute)
    time.Sleep(10 * time.Millisecond)
    assert.Equal(t, 1, callCount)

    // Second tick
    fakeClock.Advance(1 * time.Minute)
    time.Sleep(10 * time.Millisecond)
    assert.Equal(t, 2, callCount)

    cancel()
}
```

#### 3. Testing Timeouts

```go
func TestOperationTimeout(t *testing.T) {
    t.Parallel()

    fakeClock := clockwork.NewFakeClock()

    // Start operation
    resultChan := make(chan error)
    go func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        resultChan <- LongRunningOperation(ctx, fakeClock)
    }()

    // Advance past timeout
    fakeClock.Advance(10 * time.Second)

    // Verify timeout occurred
    err := <-resultChan
    assert.Equal(t, context.DeadlineExceeded, err)
}
```

#### 4. Testing Time Calculations

```go
func TestPlayTimeCalculation(t *testing.T) {
    t.Parallel()

    fakeClock := clockwork.NewFakeClock()

    // Record start time
    startTime := fakeClock.Now()
    tracker := &MediaHistoryTracker{
        clock: fakeClock,
        startTime: startTime,
    }

    // Simulate 10 minutes of play
    fakeClock.Advance(10 * time.Minute)

    // Calculate play time
    endTime := fakeClock.Now()
    playTime := int(endTime.Sub(startTime).Seconds())

    // Should be exactly 600 seconds (10 minutes)
    assert.Equal(t, 600, playTime)
}
```

### Best Practices

#### DO ✅

```go
// Inject clock as dependency
type MyService struct {
    clock clockwork.Clock
}

// Use clock methods instead of time package
func (s *MyService) GetCurrentTime() time.Time {
    return s.clock.Now()  // NOT time.Now()
}

func (s *MyService) StartTicker() clockwork.Ticker {
    return s.clock.NewTicker(1 * time.Minute)  // NOT time.NewTicker()
}

// Use BlockUntilContext for goroutine synchronization
err := fakeClock.BlockUntilContext(ctx, 1)
require.NoError(t, err)

// Advance time deterministically
fakeClock.Advance(5 * time.Minute)
```

#### DON'T ❌

```go
// Don't use time package directly in production code
func (s *MyService) GetCurrentTime() time.Time {
    return time.Now()  // ❌ Not testable!
}

// Don't use real tickers
func (s *MyService) StartTicker() *time.Ticker {
    return time.NewTicker(1 * time.Minute)  // ❌ Not testable!
}

// Don't use long sleeps in tests
time.Sleep(1 * time.Minute)  // ❌ Makes tests slow!

// Don't use deprecated BlockUntil
fakeClock.BlockUntil(1)  // ❌ Use BlockUntilContext instead
```

### Real-World Example

See `pkg/service/media_history_tracker_test.go` for a complete example of testing time-based goroutines:

```go
func TestMediaHistoryTracker_UpdatePlayTime(t *testing.T) {
    t.Parallel()

    // Setup with fake clock
    fakeClock := clockwork.NewFakeClock()
    startTime := fakeClock.Now()

    tracker := &mediaHistoryTracker{
        clock:                 fakeClock,
        currentHistoryDBID:    42,
        currentMediaStartTime: startTime,
    }

    // Setup mock expectations
    mockUserDB.On("UpdateMediaHistoryTime", int64(42), 120).Return(nil)

    // Start goroutine
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    done := make(chan bool)
    go func() {
        tracker.updatePlayTime(ctx)
        done <- true
    }()

    // Wait for goroutine to reach select
    err := fakeClock.BlockUntilContext(ctx, 1)
    require.NoError(t, err)

    // Advance time by 2 minutes to trigger ticker
    fakeClock.Advance(2 * time.Minute)

    // Brief sleep for goroutine to process
    time.Sleep(10 * time.Millisecond)

    // Cleanup
    cancel()
    <-done

    // Verify update was called with correct play time (120 seconds)
    mockUserDB.AssertExpectations(t)
}
```

### Additional Resources

- **Clockwork documentation**: https://github.com/jonboulle/clockwork
- **Example tests**: `pkg/database/mediadb/*_test.go` (extensive clockwork usage)
- **Service layer example**: `pkg/service/media_history_tracker_test.go`

## Fuzz Testing

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
go test -fuzz=FuzzParseVirtualPathStr ./pkg/helpers/

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
// ✅ Good: Test properties
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

// ❌ Bad: Test specific outputs (use table-driven tests for this)
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

See `pkg/helpers/uris_fuzz_test.go` and `pkg/helpers/paths_fuzz_test.go` for complete examples:

- `FuzzParseVirtualPathStr` - Virtual path parsing
- `FuzzDecodeURIIfNeeded` - URI decoding
- `FuzzIsValidExtension` - Extension validation
- `FuzzFilenameFromPath` - Filename extraction
- `FuzzGetPathExt` - Path extension extraction

### Additional Resources

- **Go fuzzing tutorial**: https://go.dev/doc/tutorial/fuzz
- **Go fuzzing docs**: https://go.dev/doc/security/fuzz
- **Example fuzz tests**: `pkg/helpers/*_fuzz_test.go`

## Property-Based Testing with Rapid

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

### Example Property Tests

See these files for complete examples:

- `pkg/database/slugs/slugify_property_test.go` - Slug normalization properties
- `pkg/database/mediadb/slug_cache_property_test.go` - Cache key determinism
- `pkg/database/matcher/fuzzy_property_test.go` - Fuzzy matching properties
- `pkg/database/filters/parser_property_test.go` - Tag filter parsing

## Best Practices

### Test Organization

1. **One concept per test**: Each test should verify one behavior
2. **Clear test names**: Use descriptive names that explain the scenario
3. **Arrange-Act-Assert**: Structure tests with clear setup, action, and verification
4. **Use subtests**: Group related tests under a parent test function

### Mock Best Practices

1. **Mock at interface boundaries**: Mock external dependencies, not internal logic
2. **Use behavior verification**: Test what the code does, not how it does it
3. **Setup basic mocks**: Use `SetupBasicMock()` for common expectations
4. **Verify expectations**: Always call `AssertExpectations(t)`

### Error Testing

1. **Test both success and failure paths**: Every error condition should be tested
2. **Use specific error assertions**: Check error messages and types
3. **Test error propagation**: Verify errors are handled correctly up the call stack

### Concurrent Testing

1. **Use proper synchronization**: Always use sync.WaitGroup or channels
2. **Test race conditions**: Use proper timing controls (see Time-Based Testing)
3. **Verify thread safety**: Ensure concurrent access doesn't corrupt state

### Time-Based Testing

1. **Always use clockwork**: Never use `time.Sleep()` or `time.NewTicker()` in production code that needs testing
2. **Inject clocks**: Pass `clockwork.Clock` as a dependency to enable fake clocks in tests
3. **Use FakeClock in tests**: Control time progression deterministically with `Advance()`
4. **Minimize sleeps**: Only use very short sleeps (10ms) when absolutely necessary for goroutine synchronization
5. **Use BlockUntilContext**: Wait for goroutines to reach blocking points instead of sleeping

## Running Tests

### Basic Test Execution

```bash
# Run all tests
task test

# Run tests with race detection
go test -race ./...

# Run specific package tests
go test ./pkg/service/tokens/

# Run with coverage
go test -cover ./...

# Run specific test
go test -run TestTokenProcessing ./pkg/service/
```

### Test Filtering

```bash
# Run only unit tests (exclude integration tests)
go test -short ./...

# Run tests matching pattern
go test -run ".*Token.*" ./...

# Run tests in verbose mode
go test -v ./...
```

### Continuous Testing

```bash
# Use entr or similar for continuous testing
find . -name "*.go" | entr -r task test
```

## Troubleshooting

### Common Issues

**Tests timeout or hang**
- Check for missing `defer` statements on resources
- Ensure goroutines are properly terminated
- Use context with timeout for long-running operations

**Mock expectations not met**
- Verify method signatures match exactly
- Check parameter matchers are appropriate
- Ensure `AssertExpectations(t)` is called

**Race condition failures**
- Add proper synchronization
- Use atomic operations for counters
- Consider using channels for coordination

**Filesystem tests fail**
- Ensure proper cleanup of afero filesystem
- Check file paths use forward slashes
- Verify permissions are set correctly

### Debugging Tests

```go
// Add debugging output
t.Logf("Debug: value = %v", value)

// Use testify's debug functions
assert.Equal(t, expected, actual, "Debug message: %v", debugInfo)

// Print mock call history
for _, call := range mock.Calls {
    t.Logf("Mock call: %s with args: %v", call.Method, call.Arguments)
}
```

### Performance Testing

```go
func BenchmarkMyFunction(b *testing.B) {
    // Setup
    platform := mocks.NewMockPlatform()
    platform.SetupBasicMock()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        MyFunction(platform)
    }
}
```

## Contributing to Tests

### Adding New Mocks

1. Implement the interface mock in `pkg/testing/mocks/`
2. Add `SetupBasicMock()` method for common expectations
3. Add helper methods for verification and state inspection
4. Create example tests showing usage patterns

### Adding New Fixtures

1. Add fixture data in `pkg/testing/fixtures/`
2. Provide both individual items and collections
3. Include helper functions for creating variations
4. Document the fixture data structure

### Adding New Test Helpers

1. Add helper functions in appropriate `pkg/testing/helpers/` file
2. Focus on reusable testing patterns
3. Include comprehensive documentation
4. Add example usage in `pkg/testing/examples/`

---

For more specific examples, see the test files in `pkg/testing/examples/`. Each example demonstrates a complete testing pattern with detailed comments.