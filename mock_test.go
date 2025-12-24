package registryclient

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// MockRegistry is a simple mock registry server for testing
type MockRegistry struct {
	listener      net.Listener
	address       string
	connections   []net.Conn
	mu            sync.Mutex
	responseQueue []string
	t             *testing.T
}

// NewMockRegistry creates a new mock registry server
func NewMockRegistry(t *testing.T) (*MockRegistry, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	mock := &MockRegistry{
		listener:      listener,
		address:       listener.Addr().String(),
		connections:   make([]net.Conn, 0),
		responseQueue: make([]string, 0),
		t:             t,
	}

	go mock.acceptConnections()

	return mock, nil
}

// acceptConnections accepts incoming connections
func (m *MockRegistry) acceptConnections() {
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			return // Listener closed
		}

		m.mu.Lock()
		m.connections = append(m.connections, conn)
		m.mu.Unlock()

		go m.handleConnection(conn)
	}
}

// handleConnection handles a single connection
func (m *MockRegistry) handleConnection(conn net.Conn) {
	scanner := bufio.NewScanner(conn)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "|")

		if len(parts) == 0 {
			continue
		}

		command := parts[0]

		switch command {
		case "REGISTER":
			// REGISTER|serviceName|instanceName|port|metadata
			if len(parts) >= 4 {
				sessionID := "test-session-123"
				response := fmt.Sprintf("ACK|%s\n", sessionID)
				conn.Write([]byte(response))
			}

		case "RECONNECT":
			// RECONNECT|sessionID
			conn.Write([]byte("OK\n"))

		case "PING":
			conn.Write([]byte("PONG\n"))

		case "ROUTE_ADD":
			// ROUTE_ADD|sessionID|domains|path|backendURL|priority
			if len(parts) >= 6 {
				routeID := "route-001"
				response := fmt.Sprintf("ROUTE_OK|%s\n", routeID)
				conn.Write([]byte(response))
			}

		case "ROUTE_LIST":
			conn.Write([]byte("OK|[]\n"))

		case "ROUTE_REMOVE":
			conn.Write([]byte("OK\n"))

		case "ROUTE_UPDATE":
			conn.Write([]byte("OK\n"))

		case "HEADERS_SET":
			conn.Write([]byte("OK\n"))

		case "OPTIONS_SET":
			conn.Write([]byte("OK\n"))

		case "HEALTH_SET":
			conn.Write([]byte("OK\n"))

		case "RATELIMIT_SET":
			conn.Write([]byte("OK\n"))

		case "CIRCUIT_BREAKER_SET":
			conn.Write([]byte("OK\n"))

		case "CONFIG_VALIDATE":
			conn.Write([]byte("OK\n"))

		case "CONFIG_APPLY":
			conn.Write([]byte("OK\n"))

		case "CONFIG_ROLLBACK":
			conn.Write([]byte("OK\n"))

		case "CONFIG_DIFF":
			conn.Write([]byte("OK|{}\n"))

		case "DRAIN_START":
			completion := time.Now().Add(30 * time.Second).Format(time.RFC3339)
			response := fmt.Sprintf("DRAIN_OK|%s\n", completion)
			conn.Write([]byte(response))

		case "DRAIN_STATUS":
			conn.Write([]byte("OK|{\"active\":false}\n"))

		case "DRAIN_CANCEL":
			conn.Write([]byte("OK\n"))

		case "MAINT_ENTER":
			conn.Write([]byte("ACK\n"))
			// Send MAINT_OK confirmation after a brief delay
			go func() {
				time.Sleep(50 * time.Millisecond)
				conn.Write([]byte("MAINT_OK|ALL\n"))
			}()

		case "MAINT_EXIT":
			conn.Write([]byte("ACK\n"))
			// Send MAINT_OK confirmation
			go func() {
				time.Sleep(50 * time.Millisecond)
				conn.Write([]byte("MAINT_OK|ALL\n"))
			}()

		case "MAINT_STATUS":
			conn.Write([]byte("OK|{\"active\":false}\n"))

		case "STATS_GET":
			conn.Write([]byte("OK|[]\n"))

		case "BACKEND_TEST":
			conn.Write([]byte("OK|{\"reachable\":true}\n"))

		case "SESSION_INFO":
			conn.Write([]byte("OK|{\"session_id\":\"test-session-123\"}\n"))

		case "CLIENT_SHUTDOWN":
			conn.Write([]byte("OK\n"))
			return

		default:
			m.t.Logf("Unknown command: %s", command)
		}
	}
}

// Close closes the mock registry
func (m *MockRegistry) Close() {
	m.listener.Close()
	m.mu.Lock()
	for _, conn := range m.connections {
		conn.Close()
	}
	m.mu.Unlock()
}

// GetAddress returns the mock registry address
func (m *MockRegistry) GetAddress() string {
	return m.address
}

// Integration tests using mock registry

func TestInitWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		map[string]interface{}{"version": "1.0.0"},
		true,
	)

	connectedReceived := false
	client.On(EventConnected, func(event Event) {
		connectedReceived = true
	})

	// Init without cleanup to avoid route list/remove calls
	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	if !connectedReceived {
		t.Error("Expected connected event")
	}

	if client.sessionID == "" {
		t.Error("Expected non-empty session ID")
	}

	if client.conn == nil {
		t.Error("Expected non-nil connection")
	}
}

func TestPingWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.Ping()
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestAddRouteWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	routeAddedReceived := false
	var routeData map[string]interface{}

	client.On(EventRouteAdded, func(event Event) {
		routeAddedReceived = true
		routeData = event.Data
	})

	routeID, err := client.AddRoute(
		[]string{"example.com"},
		"/api",
		"http://10.2.2.100:3000",
		10,
	)

	if err != nil {
		t.Errorf("AddRoute failed: %v", err)
	}

	if routeID == "" {
		t.Error("Expected non-empty route ID")
	}

	time.Sleep(100 * time.Millisecond)

	if !routeAddedReceived {
		t.Error("Expected route added event")
	}

	if routeData["route_id"] != routeID {
		t.Errorf("Expected route_id='%s', got '%v'", routeID, routeData["route_id"])
	}
}

func TestListRoutesWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	routes, err := client.ListRoutes()
	if err != nil {
		t.Errorf("ListRoutes failed: %v", err)
	}

	if routes == nil {
		t.Error("Expected non-nil routes")
	}
}

func TestSetHeadersWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.SetHeaders("X-Custom-Header", "CustomValue")
	if err != nil {
		t.Errorf("SetHeaders failed: %v", err)
	}
}

func TestSetOptionsWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.SetOptions("timeout", "30s")
	if err != nil {
		t.Errorf("SetOptions failed: %v", err)
	}
}

func TestSetHealthCheckWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	healthCheckReceived := false
	client.On(EventHealthCheckSet, func(event Event) {
		healthCheckReceived = true
	})

	err = client.SetHealthCheck("route-001", "/health", "30s", "5s")
	if err != nil {
		t.Errorf("SetHealthCheck failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !healthCheckReceived {
		t.Error("Expected health check set event")
	}
}

func TestApplyConfigWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	configAppliedReceived := false
	client.On(EventConfigApplied, func(event Event) {
		configAppliedReceived = true
	})

	err = client.ApplyConfig()
	if err != nil {
		t.Errorf("ApplyConfig failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !configAppliedReceived {
		t.Error("Expected config applied event")
	}
}

func TestShutdownWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	disconnectedReceived := false
	client.On(EventDisconnected, func(event Event) {
		disconnectedReceived = true
	})

	err = client.Shutdown()
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !disconnectedReceived {
		t.Error("Expected disconnected event")
	}
}

func TestRemoveRouteWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.RemoveRoute("route-001")
	if err != nil {
		t.Errorf("RemoveRoute failed: %v", err)
	}
}

func TestUpdateRouteWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.UpdateRoute("route-001", "priority", "20")
	if err != nil {
		t.Errorf("UpdateRoute failed: %v", err)
	}
}

func TestSetRateLimitWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.SetRateLimit("route-001", 100, "1m")
	if err != nil {
		t.Errorf("SetRateLimit failed: %v", err)
	}
}

func TestSetCircuitBreakerWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.SetCircuitBreaker("route-001", 5, "30s", 3)
	if err != nil {
		t.Errorf("SetCircuitBreaker failed: %v", err)
	}
}

func TestValidateConfigWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.ValidateConfig()
	if err != nil {
		t.Errorf("ValidateConfig failed: %v", err)
	}
}

func TestRollbackConfigWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.RollbackConfig()
	if err != nil {
		t.Errorf("RollbackConfig failed: %v", err)
	}
}

func TestConfigDiffWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	diff, err := client.ConfigDiff()
	if err != nil {
		t.Errorf("ConfigDiff failed: %v", err)
	}

	if diff == nil {
		t.Error("Expected non-nil diff")
	}
}

func TestDrainStartWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	completionTime, err := client.DrainStart(30)
	if err != nil {
		t.Errorf("DrainStart failed: %v", err)
	}

	if completionTime.IsZero() {
		t.Error("Expected non-zero completion time")
	}
}

func TestDrainStatusWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	status, err := client.DrainStatus()
	if err != nil {
		t.Errorf("DrainStatus failed: %v", err)
	}

	if status == nil {
		t.Error("Expected non-nil status")
	}
}

func TestDrainCancelWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	err = client.DrainCancel()
	if err != nil {
		t.Errorf("DrainCancel failed: %v", err)
	}
}

func TestMaintenanceStatusWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	status, err := client.MaintenanceStatus()
	if err != nil {
		t.Errorf("MaintenanceStatus failed: %v", err)
	}

	if status == nil {
		t.Error("Expected non-nil status")
	}
}

func TestGetStatsWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	stats, err := client.GetStats()
	if err != nil {
		t.Errorf("GetStats failed: %v", err)
	}

	if stats == nil {
		t.Error("Expected non-nil stats")
	}
}

func TestTestBackendWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	result, err := client.TestBackend("http://10.2.2.100:3000")
	if err != nil {
		t.Errorf("TestBackend failed: %v", err)
	}

	if result == nil {
		t.Error("Expected non-nil result")
	}
}

func TestSessionInfoWithMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	info, err := client.SessionInfo()
	if err != nil {
		t.Errorf("SessionInfo failed: %v", err)
	}

	if info == nil {
		t.Error("Expected non-nil info")
	}
}

// TestMaintenanceEnterWithURLMockRegistry tests entering maintenance with a custom URL
func TestMaintenanceEnterWithURLMockRegistry(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	maintenanceReceived := false
	client.On(EventMaintenanceEntered, func(event Event) {
		maintenanceReceived = true
	})

	err = client.MaintenanceEnterWithURL("ALL", "http://maintenance.example.com")
	if err != nil {
		t.Errorf("MaintenanceEnterWithURL failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if !maintenanceReceived {
		t.Error("Expected maintenance entered event")
	}

	if !client.IsInMaintenanceMode() {
		t.Error("Expected client to be in maintenance mode")
	}
}

// TestMultipleRouteAdditions tests adding multiple routes
func TestMultipleRouteAdditions(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	// Add first route
	routeID1, err := client.AddRoute(
		[]string{"example.com"},
		"/api",
		"http://10.2.2.100:3000",
		10,
	)
	if err != nil {
		t.Errorf("First AddRoute failed: %v", err)
	}

	// Add second route
	routeID2, err := client.AddRoute(
		[]string{"test.com"},
		"/v2",
		"http://10.2.2.101:3001",
		5,
	)
	if err != nil {
		t.Errorf("Second AddRoute failed: %v", err)
	}

	if routeID1 == "" || routeID2 == "" {
		t.Error("Expected non-empty route IDs")
	}

	// Verify stored routes
	if len(client.storedRoutes) != 2 {
		t.Errorf("Expected 2 stored routes, got %d", len(client.storedRoutes))
	}
}

// TestConfigurationStorage tests that configuration is properly stored
func TestConfigurationStorage(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer client.Close()

	// Add route and verify storage
	_, err = client.AddRoute(
		[]string{"example.com"},
		"/api",
		"http://10.2.2.100:3000",
		10,
	)
	if err != nil {
		t.Fatalf("AddRoute failed: %v", err)
	}

	if len(client.storedRoutes) != 1 {
		t.Errorf("Expected 1 stored route, got %d", len(client.storedRoutes))
	}

	// Set option and verify storage
	err = client.SetOptions("timeout", "30s")
	if err != nil {
		t.Fatalf("SetOptions failed: %v", err)
	}

	client.configMu.Lock()
	optionValue := client.storedOptions["timeout"]
	client.configMu.Unlock()

	if optionValue != "30s" {
		t.Errorf("Expected option value '30s', got '%s'", optionValue)
	}

	// Set health check and verify storage
	err = client.SetHealthCheck("route-001", "/health", "30s", "5s")
	if err != nil {
		t.Fatalf("SetHealthCheck failed: %v", err)
	}

	client.configMu.Lock()
	healthCheck := client.storedHealthChecks["route-001"]
	client.configMu.Unlock()

	if healthCheck.Path != "/health" {
		t.Errorf("Expected health check path '/health', got '%s'", healthCheck.Path)
	}
}

// TestInitDefaultsToInitWithCleanupTrue tests that Init() calls InitWithCleanup(true)
func TestInitDefaultsToInitWithCleanupTrue(t *testing.T) {
	// We'll test this by verifying that Init() is callable
	// The actual network testing is done in other tests
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	// Call Init() which should delegate to InitWithCleanup(true)
	// However, since we don't have existing routes, this should succeed without cleanup
	err = client.Init()
	if err != nil {
		t.Errorf("Init failed: %v", err)
	}
	defer client.Close()

	if client.sessionID == "" {
		t.Error("Expected non-empty session ID after Init")
	}
}

// TestReconnectWithExistingSession tests reconnection with an existing session
func TestReconnectWithExistingSession(t *testing.T) {
	mock, err := NewMockRegistry(t)
	if err != nil {
		t.Fatalf("Failed to create mock registry: %v", err)
	}
	defer mock.Close()

	client := NewRegistryClient(
		mock.GetAddress(),
		"test-service",
		"test-instance",
		8080,
		nil,
		false,
	)

	err = client.InitWithCleanup(false)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	_ = client.sessionID // Keep track that we have a session

	// Simulate reconnect by calling reconnect directly
	err = client.reconnect()
	if err != nil {
		t.Errorf("Reconnect failed: %v", err)
	}

	// Session should be restored or a new one created
	if client.sessionID == "" {
		t.Error("Expected session ID after reconnect")
	}

	client.Close()
}


