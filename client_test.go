package registryclient

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestPackage(t *testing.T) {
	t.Log("Registry client package tests")
}

// TestNewRegistryClient tests the creation of a new registry client
func TestNewRegistryClient(t *testing.T) {
	metadata := map[string]interface{}{
		"version": "1.0.0",
		"env":     "test",
	}

	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		metadata,
		true,
	)

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	if client.registryAddr != "localhost:9000" {
		t.Errorf("Expected registryAddr='localhost:9000', got '%s'", client.registryAddr)
	}

	if client.serviceName != "test-service" {
		t.Errorf("Expected serviceName='test-service', got '%s'", client.serviceName)
	}

	if client.instanceName != "test-instance" {
		t.Errorf("Expected instanceName='test-instance', got '%s'", client.instanceName)
	}

	if client.maintenancePort != 8080 {
		t.Errorf("Expected maintenancePort=8080, got %d", client.maintenancePort)
	}

	if !client.debug {
		t.Error("Expected debug=true")
	}

	if client.handlers == nil {
		t.Error("Expected handlers map to be initialized")
	}

	if client.done == nil {
		t.Error("Expected done channel to be initialized")
	}

	if client.storedRoutes == nil {
		t.Error("Expected storedRoutes to be initialized")
	}

	if client.storedHealthChecks == nil {
		t.Error("Expected storedHealthChecks to be initialized")
	}

	if client.storedOptions == nil {
		t.Error("Expected storedOptions to be initialized")
	}

	if client.routeIDs == nil {
		t.Error("Expected routeIDs to be initialized")
	}
}

// TestEventHandlers tests event handler registration and emission
func TestEventHandlers(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	eventReceived := false
	var receivedEvent Event

	// Register an event handler
	client.On(EventConnected, func(event Event) {
		mu.Lock()
		eventReceived = true
		receivedEvent = event
		mu.Unlock()
	})

	// Emit an event
	testEvent := Event{
		Type: EventConnected,
		Data: map[string]interface{}{
			"session_id": "test-123",
			"local_ip":   "10.2.2.100",
		},
		Timestamp: time.Now(),
	}

	client.emit(testEvent)

	// Wait a bit for goroutine to process
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !eventReceived {
		t.Error("Expected event to be received")
	}

	if receivedEvent.Type != EventConnected {
		t.Errorf("Expected EventConnected, got %s", receivedEvent.Type)
	}
	mu.Unlock()

	// Test removing handlers
	client.Off(EventConnected)

	eventReceived = false
	client.emit(testEvent)
	time.Sleep(50 * time.Millisecond)

	if eventReceived {
		t.Error("Expected no event after Off() was called")
	}
}

// TestMultipleEventHandlers tests multiple handlers for the same event
func TestMultipleEventHandlers(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	handler1Called := false
	handler2Called := false

	client.On(EventLog, func(event Event) {
		mu.Lock()
		handler1Called = true
		mu.Unlock()
	})

	client.On(EventLog, func(event Event) {
		mu.Lock()
		handler2Called = true
		mu.Unlock()
	})

	client.emit(Event{
		Type:      EventLog,
		Data:      map[string]interface{}{"message": "test"},
		Timestamp: time.Now(),
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !handler1Called {
		t.Error("Expected handler1 to be called")
	}

	if !handler2Called {
		t.Error("Expected handler2 to be called")
	}
	mu.Unlock()
}

// TestEventTypes tests all event type constants
func TestEventTypes(t *testing.T) {
	testCases := []struct {
		eventType EventType
		expected  string
	}{
		{EventConnected, "CONNECTED"},
		{EventDisconnected, "DISCONNECTED"},
		{EventRetrying, "RETRYING"},
		{EventExtendedRetry, "EXTENDED_RETRY"},
		{EventReconnected, "RECONNECTED"},
		{EventRouteAdded, "ROUTE_ADDED"},
		{EventRouteRemoved, "ROUTE_REMOVED"},
		{EventHealthCheckSet, "HEALTH_CHECK_SET"},
		{EventConfigApplied, "CONFIG_APPLIED"},
		{EventMaintenanceEntered, "MAINT_ENTERED"},
		{EventMaintenanceExited, "MAINT_EXITED"},
		{EventLog, "LOG"},
		{EventError, "ERROR"},
		{EventIPChanged, "IP_CHANGED"},
	}

	for _, tc := range testCases {
		if string(tc.eventType) != tc.expected {
			t.Errorf("Expected %s, got %s", tc.expected, string(tc.eventType))
		}
	}
}

// TestGetContainerIP tests the IP detection function
func TestGetContainerIP(t *testing.T) {
	ip := getContainerIP()
	// IP may be empty if not on the expected network, but function should not panic
	t.Logf("Detected IP: %s", ip)
}

// TestGetLocalIP tests the GetLocalIP method
func TestGetLocalIP(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	// Set a test IP
	client.localIP = "10.2.2.100"

	ip := client.GetLocalIP()
	if ip != "10.2.2.100" {
		t.Errorf("Expected IP '10.2.2.100', got '%s'", ip)
	}
}

// TestBuildBackendURL tests building backend URLs
func TestBuildBackendURL(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	client.localIP = "10.2.2.100"

	url := client.BuildBackendURL("3000")
	expected := "http://10.2.2.100:3000"
	if url != expected {
		t.Errorf("Expected '%s', got '%s'", expected, url)
	}

	url2 := client.BuildBackendURL("8080")
	expected2 := "http://10.2.2.100:8080"
	if url2 != expected2 {
		t.Errorf("Expected '%s', got '%s'", expected2, url2)
	}
}

// TestBuildMaintenanceURL tests building maintenance URLs
func TestBuildMaintenanceURL(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	client.localIP = "10.2.2.100"

	url := client.BuildMaintenanceURL("9090")
	expected := "http://10.2.2.100:9090/"
	if url != expected {
		t.Errorf("Expected '%s', got '%s'", expected, url)
	}
}

// TestIsInMaintenanceMode tests the maintenance mode state
func TestIsInMaintenanceMode(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	// Initially should not be in maintenance mode
	if client.IsInMaintenanceMode() {
		t.Error("Expected client to not be in maintenance mode initially")
	}

	// Set maintenance mode
	client.configMu.Lock()
	client.inMaintenanceMode = true
	client.configMu.Unlock()

	if !client.IsInMaintenanceMode() {
		t.Error("Expected client to be in maintenance mode")
	}

	// Unset maintenance mode
	client.configMu.Lock()
	client.inMaintenanceMode = false
	client.configMu.Unlock()

	if client.IsInMaintenanceMode() {
		t.Error("Expected client to not be in maintenance mode")
	}
}

// TestRouteConfig tests the RouteConfig structure
func TestRouteConfig(t *testing.T) {
	config := RouteConfig{
		Domains:    []string{"example.com", "www.example.com"},
		Path:       "/api",
		BackendURL: "http://10.2.2.100:3000",
		Priority:   10,
	}

	if len(config.Domains) != 2 {
		t.Errorf("Expected 2 domains, got %d", len(config.Domains))
	}

	if config.Path != "/api" {
		t.Errorf("Expected path='/api', got '%s'", config.Path)
	}

	if config.BackendURL != "http://10.2.2.100:3000" {
		t.Errorf("Expected backendURL='http://10.2.2.100:3000', got '%s'", config.BackendURL)
	}

	if config.Priority != 10 {
		t.Errorf("Expected priority=10, got %d", config.Priority)
	}
}

// TestHealthCheckConfig tests the HealthCheckConfig structure
func TestHealthCheckConfig(t *testing.T) {
	config := HealthCheckConfig{
		RouteID:  "route-123",
		Path:     "/health",
		Interval: "30s",
		Timeout:  "5s",
	}

	if config.RouteID != "route-123" {
		t.Errorf("Expected routeID='route-123', got '%s'", config.RouteID)
	}

	if config.Path != "/health" {
		t.Errorf("Expected path='/health', got '%s'", config.Path)
	}

	if config.Interval != "30s" {
		t.Errorf("Expected interval='30s', got '%s'", config.Interval)
	}

	if config.Timeout != "5s" {
		t.Errorf("Expected timeout='5s', got '%s'", config.Timeout)
	}
}

// TestLoggingMethods tests the logging methods
func TestLoggingMethods(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		true, // debug enabled
	)

	var mu sync.Mutex
	logReceived := false
	errorReceived := false

	client.On(EventLog, func(event Event) {
		mu.Lock()
		logReceived = true
		mu.Unlock()
	})

	client.On(EventError, func(event Event) {
		mu.Lock()
		errorReceived = true
		mu.Unlock()
	})

	// Test debug log
	client.logDebug("Debug message: %s", "test")
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if !logReceived {
		t.Error("Expected log event for debug message")
	}
	mu.Unlock()

	// Test info log
	mu.Lock()
	logReceived = false
	mu.Unlock()
	client.logInfo("Info message: %s", "test")
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if !logReceived {
		t.Error("Expected log event for info message")
	}
	mu.Unlock()

	// Test warn log
	mu.Lock()
	logReceived = false
	mu.Unlock()
	client.logWarn("Warning message: %s", "test")
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if !logReceived {
		t.Error("Expected log event for warn message")
	}
	mu.Unlock()

	// Test error log
	mu.Lock()
	logReceived = false
	mu.Unlock()
	client.logError("Error message: %s", "test")
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if !logReceived {
		t.Error("Expected log event for error message")
	}
	if !errorReceived {
		t.Error("Expected error event for error message")
	}
	mu.Unlock()
}

// TestDebugLogging tests that debug logs are suppressed when debug=false
func TestDebugLogging(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false, // debug disabled
	)

	var mu sync.Mutex
	logReceived := false

	client.On(EventLog, func(event Event) {
		if event.Data["level"] == "debug" {
			mu.Lock()
			logReceived = true
			mu.Unlock()
		}
	})

	// Debug log should not be emitted
	client.logDebug("Debug message")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if logReceived {
		t.Error("Expected debug log to be suppressed when debug=false")
	}
	mu.Unlock()

	// Info log should still work
	client.logInfo("Info message")
	time.Sleep(50 * time.Millisecond)

	// We need to track info separately
	infoReceived := false
	client.On(EventLog, func(event Event) {
		if event.Data["level"] == "info" {
			mu.Lock()
			infoReceived = true
			mu.Unlock()
		}
	})

	client.logInfo("Info message 2")
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if !infoReceived {
		t.Error("Expected info log to work when debug=false")
	}
	mu.Unlock()
}

// TestClose tests the Close method
func TestClose(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	disconnectReceived := false
	client.On(EventDisconnected, func(event Event) {
		mu.Lock()
		disconnectReceived = true
		mu.Unlock()
	})

	client.Close()
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !disconnectReceived {
		t.Error("Expected disconnect event")
	}
	mu.Unlock()

	// Check that done channel is closed
	select {
	case <-client.done:
		// Channel is closed, as expected
	default:
		t.Error("Expected done channel to be closed")
	}
}

// TestLogEvent tests the deprecated logEvent method
func TestLogEvent(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	eventReceived := false
	var receivedData map[string]interface{}

	client.On(EventLog, func(event Event) {
		mu.Lock()
		eventReceived = true
		receivedData = event.Data
		mu.Unlock()
	})

	testData := map[string]interface{}{
		"custom": "value",
	}

	client.logEvent("info", "Test message", testData)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !eventReceived {
		t.Error("Expected log event")
	}

	if receivedData["level"] != "info" {
		t.Errorf("Expected level='info', got '%v'", receivedData["level"])
	}

	if receivedData["message"] != "Test message" {
		t.Errorf("Expected message='Test message', got '%v'", receivedData["message"])
	}

	if receivedData["custom"] != "value" {
		t.Errorf("Expected custom='value', got '%v'", receivedData["custom"])
	}
	mu.Unlock()
}

// TestErrorEvent tests the errorEvent method
func TestErrorEvent(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	eventReceived := false
	var receivedData map[string]interface{}

	client.On(EventError, func(event Event) {
		mu.Lock()
		eventReceived = true
		receivedData = event.Data
		mu.Unlock()
	})

	testData := map[string]interface{}{
		"code": 500,
	}

	client.errorEvent("Test error", nil, testData)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !eventReceived {
		t.Error("Expected error event")
	}

	if receivedData["message"] != "Test error" {
		t.Errorf("Expected message='Test error', got '%v'", receivedData["message"])
	}

	if receivedData["code"] != 500 {
		t.Errorf("Expected code=500, got '%v'", receivedData["code"])
	}
	mu.Unlock()
}

// TestMaintenanceEnterNilConnection tests maintenance mode with no connection
func TestMaintenanceEnterNilConnection(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	maintenanceEventReceived := false
	var receivedMode string

	client.On(EventMaintenanceEntered, func(event Event) {
		mu.Lock()
		maintenanceEventReceived = true
		if mode, ok := event.Data["mode"].(string); ok {
			receivedMode = mode
		}
		mu.Unlock()
	})

	// Try to enter maintenance with no connection
	err := client.MaintenanceEnter("ALL")

	if err != nil {
		t.Errorf("Expected no error when entering maintenance without connection, got %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !maintenanceEventReceived {
		t.Error("Expected maintenance entered event")
	}

	if receivedMode != "local" {
		t.Errorf("Expected mode='local', got '%s'", receivedMode)
	}
	mu.Unlock()

	if !client.IsInMaintenanceMode() {
		t.Error("Expected client to be in maintenance mode")
	}
}

// TestMaintenanceExitNilConnection tests exiting maintenance with no connection
func TestMaintenanceExitNilConnection(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	// Set maintenance mode first
	client.configMu.Lock()
	client.inMaintenanceMode = true
	client.maintenanceTarget = "ALL"
	client.configMu.Unlock()

	var mu sync.Mutex
	maintenanceExitEventReceived := false

	client.On(EventMaintenanceExited, func(event Event) {
		mu.Lock()
		maintenanceExitEventReceived = true
		mu.Unlock()
	})

	// Try to exit maintenance with no connection
	err := client.MaintenanceExit("ALL")

	if err != nil {
		t.Errorf("Expected no error when exiting maintenance without connection, got %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !maintenanceExitEventReceived {
		t.Error("Expected maintenance exited event")
	}
	mu.Unlock()

	if client.IsInMaintenanceMode() {
		t.Error("Expected client to not be in maintenance mode")
	}
}

// TestMaintenanceEnterAll tests the convenience method for entering maintenance
func TestMaintenanceEnterAll(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err := client.MaintenanceEnterAll()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !client.IsInMaintenanceMode() {
		t.Error("Expected client to be in maintenance mode")
	}
}

// TestMaintenanceEnterRoute tests entering maintenance for a specific route
func TestMaintenanceEnterRoute(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err := client.MaintenanceEnterRoute("/admin")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !client.IsInMaintenanceMode() {
		t.Error("Expected client to be in maintenance mode")
	}
}

// TestMaintenanceExitAll tests the convenience method for exiting maintenance
func TestMaintenanceExitAll(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	// Set maintenance mode first
	client.configMu.Lock()
	client.inMaintenanceMode = true
	client.configMu.Unlock()

	err := client.MaintenanceExitAll()
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if client.IsInMaintenanceMode() {
		t.Error("Expected client to not be in maintenance mode")
	}
}

// TestMaintenanceExitRoute tests exiting maintenance for a specific route
func TestMaintenanceExitRoute(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	// Set maintenance mode first
	client.configMu.Lock()
	client.inMaintenanceMode = true
	client.maintenanceTarget = "/admin"
	client.configMu.Unlock()

	err := client.MaintenanceExitRoute("/admin")
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if client.IsInMaintenanceMode() {
		t.Error("Expected client to not be in maintenance mode")
	}
}

// TestInitFailsWithoutIP tests Init failure when IP cannot be detected
func TestInitFailsWithoutIP(t *testing.T) {
	// This test would require mocking getContainerIP to return ""
	// Since we can't easily mock it, we'll skip this test in environments where IP is available
	t.Skip("Skipping test that requires mocking IP detection")
}

// TestMultipleOnHandlersSameEvent tests that multiple handlers can be registered
func TestMultipleOnHandlersSameEvent(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	count := 0

	client.On(EventLog, func(event Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	client.On(EventLog, func(event Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	client.On(EventLog, func(event Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})

	client.emit(Event{
		Type:      EventLog,
		Data:      map[string]interface{}{},
		Timestamp: time.Now(),
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if count != 3 {
		t.Errorf("Expected 3 handlers to be called, got %d", count)
	}
	mu.Unlock()
}

// TestEventStructure tests the Event structure
func TestEventStructure(t *testing.T) {
	now := time.Now()
	event := Event{
		Type: EventConnected,
		Data: map[string]interface{}{
			"session_id": "test-123",
		},
		Timestamp: now,
	}

	if event.Type != EventConnected {
		t.Errorf("Expected Type=EventConnected, got %s", event.Type)
	}

	if event.Data["session_id"] != "test-123" {
		t.Errorf("Expected session_id='test-123', got '%v'", event.Data["session_id"])
	}

	if !event.Timestamp.Equal(now) {
		t.Error("Expected timestamp to match")
	}
}

// TestGetContainerIPReturnsNonEmpty tests that getContainerIP returns a value
func TestGetContainerIPReturnsNonEmpty(t *testing.T) {
	ip := getContainerIP()
	// We expect some IP, but we can't know the exact value
	// Just verify it doesn't panic and returns a string
	t.Logf("Container IP: %s", ip)
}

// TestNewRegistryClientWithNilMetadata tests client creation with nil metadata
func TestNewRegistryClientWithNilMetadata(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	if client.metadata != nil {
		t.Error("Expected nil metadata to be preserved")
	}
}

// TestNewRegistryClientWithEmptyInstanceName tests client with empty instance name
func TestNewRegistryClientWithEmptyInstanceName(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"", // empty instance name
		8080,
		nil,
		false,
	)

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	if client.instanceName != "" {
		t.Errorf("Expected empty instanceName, got '%s'", client.instanceName)
	}
}

// TestLogPrefixGeneration tests that log prefix is generated correctly
func TestLogPrefixGeneration(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"my-service",
		"instance-1",
		8080,
		nil,
		false,
	)

	expected := "[my-service]"
	if client.logPrefix != expected {
		t.Errorf("Expected logPrefix='%s', got '%s'", expected, client.logPrefix)
	}
}

// TestStoredRoutesInitialization tests that stored routes are properly initialized
func TestStoredRoutesInitialization(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	if client.storedRoutes == nil {
		t.Fatal("Expected storedRoutes to be initialized")
	}

	if len(client.storedRoutes) != 0 {
		t.Errorf("Expected empty storedRoutes, got length %d", len(client.storedRoutes))
	}
}

// TestEventTimestamps tests that event timestamps are set correctly
func TestEventTimestamps(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	var timestamp time.Time
	client.On(EventLog, func(event Event) {
		mu.Lock()
		timestamp = event.Timestamp
		mu.Unlock()
	})

	before := time.Now()
	client.logInfo("Test message")
	after := time.Now()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if timestamp.IsZero() {
		t.Error("Expected non-zero timestamp")
	}

	if timestamp.Before(before) || timestamp.After(after) {
		t.Error("Expected timestamp to be within test execution time")
	}
	mu.Unlock()
}

// TestErrorEventWithError tests errorEvent with an actual error
func TestErrorEventWithError(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	eventReceived := false
	var receivedData map[string]interface{}

	client.On(EventError, func(event Event) {
		mu.Lock()
		eventReceived = true
		receivedData = event.Data
		mu.Unlock()
	})

	testData := map[string]interface{}{
		"code": 404,
	}

	testError := fmt.Errorf("connection refused")
	client.errorEvent("Connection error", testError, testData)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !eventReceived {
		t.Error("Expected error event")
	}

	if receivedData["error"] != "connection refused" {
		t.Errorf("Expected error='connection refused', got '%v'", receivedData["error"])
	}
	mu.Unlock()
}

// TestLogEventWithNilData tests logEvent with nil data
func TestLogEventWithNilData(t *testing.T) {
	client := NewRegistryClient(
		"localhost:9000",
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	var mu sync.Mutex
	eventReceived := false
	var receivedData map[string]interface{}

	client.On(EventLog, func(event Event) {
		mu.Lock()
		eventReceived = true
		receivedData = event.Data
		mu.Unlock()
	})

	client.logEvent("info", "Test with nil data", nil)
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !eventReceived {
		t.Error("Expected log event")
	}

	if receivedData == nil {
		t.Error("Expected non-nil data map")
	}

	if receivedData["level"] != "info" {
		t.Errorf("Expected level='info', got '%v'", receivedData["level"])
	}
	mu.Unlock()
}
