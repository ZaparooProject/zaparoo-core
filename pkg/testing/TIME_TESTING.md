# Time-Based Testing

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

#### DO

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

#### DON'T

```go
// Don't use time package directly in production code
func (s *MyService) GetCurrentTime() time.Time {
    return time.Now()  // Not testable!
}

// Don't use real tickers
func (s *MyService) StartTicker() *time.Ticker {
    return time.NewTicker(1 * time.Minute)  // Not testable!
}

// Don't use long sleeps in tests
time.Sleep(1 * time.Minute)  // Makes tests slow!

// Don't use deprecated BlockUntil
fakeClock.BlockUntil(1)  // Use BlockUntilContext instead
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
- **Example tests**: `pkg/database/mediadb/wal_checkpoint_test.go`, `concurrent_operations_test.go`, `optimization_test.go`
- **Service layer example**: `pkg/service/media_history_tracker_test.go`
