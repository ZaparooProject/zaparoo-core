# Testing Helpers & Examples

This directory provides comprehensive testing utilities for Zaparoo Core, enabling hardware-independent testing with zero dependencies on physical readers, databases, or filesystems.

## Quick Reference

### Mock Components

#### Platform Mocking
- `mocks.NewMockPlatform()` - Mock platform for cross-platform testing
- `platform.SetupBasicMock()` - Standard mock configuration

#### Reader Mocking
- `mocks.NewMockReader()` - Mock card reader for testing token scanning
- `reader.On("ReadTokens", ...)` - Set up token reading expectations

#### WebSocket Testing
- `helpers.NewWebSocketTestServer(t, handler)` - Test WebSocket server
- `server.NewClient(t)` - WebSocket test client

### Database Testing

#### Mock Database Interfaces
- `helpers.NewMockUserDBI()` - In-memory user database mock
- `helpers.NewMockMediaDBI()` - In-memory media database mock

#### Database Fixtures & Helpers  
- `fixtures.GetTestTokens()` - Pre-defined test token data
- `fixtures.GetTestMedia()` - Pre-defined media entries
- `fixtures.GetTestPlaylists()` - Pre-defined playlist data
- `helpers.HistoryEntryMatcher()` - Flexible matching for history entries

### Filesystem Testing

#### In-Memory Filesystem
- `helpers.NewMemoryFS()` - In-memory filesystem using afero
- `fs.WriteFile(path, content)` - Create test files
- `fs.MkdirAll(path)` - Create test directories
- `fs.ReadFile(path)` - Read test files

#### Configuration Helpers
- `helpers.NewTestConfig(fs, configDir)` - Test configuration with random port
- `helpers.NewTestConfigWithPort(fs, configDir, port)` - Test configuration with specific port

### API Testing

#### WebSocket & JSON-RPC
- `helpers.NewWebSocketTestServer(t, messageHandler)` - WebSocket server for testing
- `client.SendMessage(data)` - Send test messages
- `client.ReceiveMessage()` - Receive and validate responses

#### HTTP Testing
- Standard Go `net/http/httptest` integration
- Context-aware HTTP client helpers

## Complete Examples

The `examples/` directory contains working examples demonstrating all testing patterns:

### Core Testing Patterns
- **`api_example_test.go`** - WebSocket communication, JSON-RPC testing, HTTP endpoints
- **`database_example_test.go`** - Database operations, mock expectations, transaction testing  
- **`filesystem_example_test.go`** - File operations, configuration management, in-memory filesystem
- **`mock_usage_example_test.go`** - Platform mocking, reader mocking, expectation setup

### Service Layer Testing
- **`service_token_processing_test.go`** - End-to-end token processing workflows
- **`service_state_management_test.go`** - Application state management testing
- **`service_zapscript_test.go`** - ZapScript execution and custom launcher testing

## Common Patterns

### Basic Test Setup
```go
func TestYourFeature(t *testing.T) {
    t.Parallel()
    
    // Setup mocks
    platform := mocks.NewMockPlatform()
    platform.SetupBasicMock()
    
    // Setup database
    db := &database.Database{
        UserDB:  helpers.NewMockUserDBI(),
        MediaDB: helpers.NewMockMediaDBI(),
    }
    
    // Setup state
    st, notifications := state.NewState(platform)
    defer st.StopService()
    
    // Your test logic here
}
```

### API Server Testing
```go
func TestAPIEndpoint(t *testing.T) {
    // Setup test environment
    platform := mocks.NewMockPlatform()
    platform.SetupBasicMock()
    
    fs := helpers.NewMemoryFS()
    cfg, err := helpers.NewTestConfigWithPort(fs, t.TempDir(), 0)
    require.NoError(t, err)
    
    // Start server
    go api.Start(platform, cfg, st, tokenQueue, db, notifications)
    
    // Test API calls...
}
```

### Token Processing Testing
```go
func TestTokenProcessing(t *testing.T) {
    // Setup mock reader
    reader := mocks.NewMockReader()
    reader.On("ReadTokens", mock.Anything).Return([]tokens.Token{
        {UID: "test-uid", Data: "test-data"},
    }, nil)
    
    // Test processing...
}
```

### Database Operation Testing
```go
func TestDatabaseOperations(t *testing.T) {
    userDB := helpers.NewMockUserDBI()
    
    // Set expectations
    userDB.On("AddHistory", helpers.HistoryEntryMatcher()).Return(nil)
    
    // Test your function
    err := YourFunction(userDB)
    require.NoError(t, err)
    
    // Verify expectations
    userDB.AssertExpectations(t)
}
```

## Test Data & Fixtures

The `fixtures/` directory provides pre-built test data:
- **Token fixtures** - Various NFC/RFID test tokens
- **Media fixtures** - Game entries, ROMs, metadata
- **Playlist fixtures** - M3U files, game collections
- **Database fixtures** - Pre-populated database states

## Integration with TDD Guard

All tests are monitored by TDD Guard for strict test-driven development:
- Use `task test` instead of `go test` for TDD integration
- Write failing tests first, then implement features
- TDD Guard ensures code changes are driven by test failures

## Best Practices

1. **Always use `t.Parallel()`** for independent tests
2. **Use unique ports** for concurrent API server tests (8000 + testID pattern)
3. **Clean up resources** with `defer` statements
4. **Use meaningful test data** from fixtures rather than hardcoded values
5. **Test behavior, not implementation** - focus on what the code does, not how
6. **Leverage mocks for hardware dependencies** - no real readers or databases needed