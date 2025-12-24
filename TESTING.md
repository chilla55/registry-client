# Testing Guide - Registry Client V2

## Quick Start

### Run All Tests
```bash
cd registry-client-v2
make test
```

### Run Tests with Coverage
```bash
make test-coverage
```

### Run CI Pipeline Locally
```bash
make ci
```

## Test Structure

### Test Files
- `client_test.go` - Main test suite with mock registry server

### Test Categories

#### 1. Unit Tests
- **TestNewRegistryClient** - Client initialization
- **TestNewRegistryClientWithDebug** - Debug mode
- **TestBuildBackendURL** - URL building
- **TestGetLocalIP** - IP detection

#### 2. Integration Tests
- **TestClientInit** - Connection establishment
- **TestAddRoute** - Route registration
- **TestSetHealthCheck** - Health check configuration
- **TestApplyConfig** - Configuration application

#### 3. Maintenance Mode Tests
- **TestMaintenanceModeEnter** - Enter maintenance
- **TestMaintenanceModeExit** - Exit maintenance
- **TestMaintenanceModeRouteTargeting** - Route-specific maintenance

#### 4. Event System Tests
- **TestEventHandlers** - Single event handler
- **TestMultipleEventHandlers** - Multiple handlers per event
- **TestConcurrentEventEmission** - Thread safety

#### 5. Reliability Tests
- **TestConfigurationPersistence** - State preservation
- **TestShutdown** - Clean shutdown
- **TestConcurrentEventEmission** - Race conditions

## Mock Registry Server

The test suite includes a full mock implementation of the go-proxy registry:

```go
server, _ := NewMockRegistryServer()
defer server.Stop()

client := NewRegistryClient(server.GetAddress(), "test-service", "", 0, nil, false)
client.Init()
```

### Mock Server Features
- ✅ REGISTER command
- ✅ RECONNECT command
- ✅ ROUTE_ADD command
- ✅ HEALTH_SET command
- ✅ OPTIONS_SET command
- ✅ CONFIG_APPLY command
- ✅ MAINT_ENTER command
- ✅ MAINT_EXIT command
- ✅ PING/PONG
- ✅ Session management
- ✅ Concurrent connections

## Running Specific Tests

### Run Single Test
```bash
go test -v -run TestClientInit
```

### Run Test Pattern
```bash
go test -v -run "TestMaintenance.*"
```

### Run with Race Detection
```bash
go test -race ./...
```

### Run with Verbose Output
```bash
go test -v ./...
```

## Coverage Reports

### Generate Coverage Report
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
open coverage.html
```

### View Coverage Summary
```bash
go test -cover ./...
```

### Coverage by Function
```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

## Benchmarking

### Run All Benchmarks
```bash
go test -bench=. -benchmem ./...
```

### Benchmark Specific Function
```bash
go test -bench=BenchmarkEventEmission -benchmem
```

## Code Quality Tools

### Format Code
```bash
make fmt
```

### Check Formatting
```bash
make fmt-check
```

### Run go vet
```bash
make vet
```

### Run staticcheck
```bash
make staticcheck
```

### Run golangci-lint
```bash
make lint
```

## GitHub Actions Workflows

### Registry Client Workflow
**File**: `.github/workflows/registry-client-v2.yml`

**Triggers**:
- Push to main/develop branches
- Pull requests to main/develop
- Changes in `registry-client-v2/` directory

**Jobs**:
1. **test** - Run tests with coverage
2. **build** - Build module and check formatting

**Features**:
- Go 1.21 setup
- Race detection enabled
- Coverage reports uploaded to Codecov
- Artifact uploads

### Services Workflow
**File**: `.github/workflows/services.yml`

**Jobs**:
1. **build-orbat** - Build orbat service
2. **build-petrodactyl** - Build petrodactyl service
3. **build-vaultwarden** - Build vaultwarden service
4. **docker-build** - Build Docker images
5. **lint** - Lint all services

## Continuous Integration

### Local CI Pipeline
```bash
make ci
```

This runs:
1. Clean build artifacts
2. Check formatting
3. Run go vet
4. Run tests with coverage
5. Build module

### Pre-commit Checks
```bash
make check
```

This runs:
1. Format check
2. Go vet
3. Tests

## Writing New Tests

### Example Test Structure
```go
func TestNewFeature(t *testing.T) {
    // Setup
    server, err := NewMockRegistryServer()
    if err != nil {
        t.Fatalf("Failed to create mock server: %v", err)
    }
    defer server.Stop()

    client := NewRegistryClient(server.GetAddress(), "test-service", "", 0, nil, false)
    if err := client.Init(); err != nil {
        t.Fatalf("Init failed: %v", err)
    }
    defer client.Shutdown()

    // Wait for connection
    time.Sleep(100 * time.Millisecond)

    // Test logic
    // ...

    // Assertions
    if /* condition */ {
        t.Error("Expected X but got Y")
    }
}
```

### Testing Best Practices

1. **Always defer cleanup**
   ```go
   defer server.Stop()
   defer client.Shutdown()
   ```

2. **Add timing for async operations**
   ```go
   time.Sleep(100 * time.Millisecond) // Wait for connection
   ```

3. **Use table-driven tests for multiple scenarios**
   ```go
   tests := []struct {
       name     string
       input    string
       expected string
   }{
       {"case1", "input1", "output1"},
       {"case2", "input2", "output2"},
   }
   ```

4. **Test concurrent access with mutexes**
   ```go
   var mu sync.Mutex
   var count int
   
   client.On(EventLog, func(event Event) {
       mu.Lock()
       count++
       mu.Unlock()
   })
   ```

5. **Use subtests for clarity**
   ```go
   t.Run("WithValidInput", func(t *testing.T) {
       // test code
   })
   ```

## Debugging Tests

### Enable Debug Logging
```go
client := NewRegistryClient(addr, "test", "", 0, nil, true) // debug=true
```

### Print Test Output
```bash
go test -v ./...
```

### Run Single Test with Details
```bash
go test -v -run TestClientInit 2>&1 | less
```

## Test Coverage Goals

Current coverage targets:
- **Unit Tests**: >80% coverage
- **Integration Tests**: All critical paths
- **Concurrent Tests**: Race detection enabled
- **Mock Server**: 100% protocol coverage

## CI/CD Integration

### GitHub Actions Status Badges

Add to README.md:
```markdown
![Registry Client Tests](https://github.com/username/repo/workflows/Registry%20Client%20V2%20-%20Build%20and%20Test/badge.svg)
![Services Build](https://github.com/username/repo/workflows/Services%20-%20Build%20and%20Test/badge.svg)
```

### Required Checks
For branch protection, enable:
- ✅ Test Registry Client
- ✅ Build Registry Client
- ✅ Build Orbat Service
- ✅ Build Petrodactyl Service
- ✅ Build Vaultwarden Service
- ✅ Lint Services

## Performance Testing

### Memory Profiling
```bash
go test -memprofile=mem.prof -bench=.
go tool pprof mem.prof
```

### CPU Profiling
```bash
go test -cpuprofile=cpu.prof -bench=.
go tool pprof cpu.prof
```

### Test Execution Time
```bash
go test -v -timeout 30s ./...
```

## Troubleshooting

### Tests Hang
- Check for missing `defer server.Stop()`
- Increase timeout: `time.Sleep()` durations
- Add `-timeout` flag: `go test -timeout 60s`

### Race Conditions
```bash
go test -race ./...
```

### Flaky Tests
- Add more timing buffers
- Check for proper mutex usage
- Verify cleanup in defer statements

### Mock Server Issues
- Ensure `server.Stop()` is called
- Check port conflicts (using random ports)
- Verify proper connection handling

## Documentation

### Generate Documentation
```bash
godoc -http=:6060
# Open http://localhost:6060/pkg/registry-client-v2/
```

### Test Examples as Documentation
Tests serve as usage examples:
- See `TestClientInit` for basic setup
- See `TestAddRoute` for route registration
- See `TestMaintenanceModeEnter` for maintenance mode

## Future Test Additions

Planned test coverage:
- [ ] Reconnection scenarios
- [ ] IP change detection
- [ ] Session expiry handling
- [ ] Network timeout scenarios
- [ ] Configuration replay after reconnect
- [ ] Maintenance mode persistence
- [ ] Multiple concurrent clients
- [ ] Protocol error handling
