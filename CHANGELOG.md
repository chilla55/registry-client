# Changelog

## [Unreleased]

### Changed
- Unified retry logic: eliminated redundant `reconnectWithBackoff()` method
- All connection retry logic now centralized in `dialWithRetry()`
- Simplified keepalive mechanism - uses mutex to prevent concurrent reconnection attempts

### Fixed
- DNS resolution failures during container startup now handled with infinite automatic retry
- Added exponential backoff for DNS "no such host" errors matching keepalive behavior
  - First 5 attempts: 5s, 10s, 15s, 20s, 25s delays
  - After 5 attempts: 1-minute intervals indefinitely
- Init() and reconnect() now use same infinite retry logic
- Resolves intermittent startup failures when both containers restart simultaneously

### Technical Details
- `dialWithRetry()` method handles both DNS resolution and connection failures with unified retry logic
- Detects DNS errors: "no such host", "Temporary failure in name resolution", "Name or service not known"
- Infinite retry behavior eliminates failure points during initialization
- Maintains attempt count continuity across reconnection cycles
- Emits `EventRetrying` and `EventExtendedRetry` events for monitoring
- Provides clear logging to distinguish DNS failures from connection failures
- Added `reconnecting` mutex flag to prevent race conditions in concurrent keepalive failures
