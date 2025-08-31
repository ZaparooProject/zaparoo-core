# Testing Guide for Zaparoo Core

This guide provides comprehensive documentation for Test-Driven Development (TDD) practices in the Zaparoo Core project. It covers the testing infrastructure, patterns, and best practices to help developers write effective tests.

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

- **Unit Tests First**: Fast, isolated tests using mocks
- **No Hardware Dependencies**: All hardware interactions are mocked
- **Behavior Testing**: Focus on what the code does, not how it does it
- **Fast Feedback**: Tests should complete in under 5 seconds

## Testing Infrastructure

The testing infrastructure is organized under `pkg/testing/`:

```
pkg/testing/
├── mocks/              # Interface mocks
│   ├── reader.go       # Reader interface mock
│   ├── platform.go     # Platform interface mock
│   └── websocket.go    # WebSocket mocks
├── helpers/            # Testing utilities
│   ├── db.go          # Database testing helpers
│   ├── fs.go          # Filesystem testing helpers
│   └── api.go         # API testing helpers
├── fixtures/           # Test data
│   ├── tokens.go      # Sample token data
│   ├── media.go       # Sample media data
│   └── database.go    # Database fixtures
└── examples/           # Example tests and patterns
    ├── mock_usage_example_test.go
    ├── database_example_test.go
    ├── filesystem_example_test.go
    ├── api_example_test.go
    ├── service_token_processing_test.go
    ├── service_zapscript_test.go
    └── service_state_management_test.go
```

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
    
    // Simulate token detection
    token := fixtures.SampleTokens()[0]
    reader.SimulateTokenScan(token)
    
    // Verify Write operations
    writes := reader.GetWriteHistory()
    assert.Len(t, writes, 1)
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
func TestRawSQL(t *testing.T) {
    // Create sqlmock
    db, mock, err := helpers.NewSQLMock()
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
    // Setup in-memory filesystem
    fs := helpers.SetupMemoryFilesystem()
    
    // Create test directories and files
    helpers.CreateTestConfigFile(fs, "test.yaml", `
setting: value
another: setting
`)
    
    // Test your function
    config, err := LoadConfig(fs, "test.yaml")
    
    // Verify
    require.NoError(t, err)
    assert.Equal(t, "value", config.Setting)
}
```

### Complex Directory Structures

```go
func TestMediaScanning(t *testing.T) {
    // Setup complex directory structure
    fs := helpers.SetupComplexMediaDirectories()
    
    // Test media scanning
    media, err := ScanMediaDirectories(fs, "/media")
    
    // Verify
    require.NoError(t, err)
    assert.Greater(t, len(media), 0)
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
2. **Test race conditions**: Use short sleeps to increase chance of races
3. **Verify thread safety**: Ensure concurrent access doesn't corrupt state

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