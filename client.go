package registryclient

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// EventType represents different registry events
type EventType string

const (
	// Connection events
	EventConnected     EventType = "CONNECTED"
	EventDisconnected  EventType = "DISCONNECTED"
	EventRetrying      EventType = "RETRYING"
	EventExtendedRetry EventType = "EXTENDED_RETRY"
	EventReconnected   EventType = "RECONNECTED"

	// Route management events
	EventRouteAdded     EventType = "ROUTE_ADDED"
	EventRouteRemoved   EventType = "ROUTE_REMOVED"
	EventHealthCheckSet EventType = "HEALTH_CHECK_SET"
	EventConfigApplied  EventType = "CONFIG_APPLIED"

	// Maintenance mode events (simplified - main doesn't need to know proxy vs local)
	EventMaintenanceEntered EventType = "MAINT_ENTERED" // Maintenance mode active
	EventMaintenanceExited  EventType = "MAINT_EXITED"  // Exited maintenance mode

	// Utility events
	EventLog       EventType = "LOG"        // All logging goes through this
	EventError     EventType = "ERROR"      // Errors
	EventIPChanged EventType = "IP_CHANGED" // Container IP address changed
)

// Event represents a registry event with associated data
type Event struct {
	Type      EventType
	Data      map[string]interface{}
	Timestamp time.Time
}

// EventHandler is a function that handles registry events
type EventHandler func(event Event)

// RouteConfig stores route configuration for re-registration
type RouteConfig struct {
	Domains    []string
	Path       string
	BackendURL string
	Priority   int
}

// HealthCheckConfig stores health check configuration
type HealthCheckConfig struct {
	RouteID  string
	Path     string
	Interval string
	Timeout  string
}

// RegistryClientV2 handles v2 protocol communication with the registry
//
// Architecture:
//   - Single persistent TCP connection (conn) used for ALL communications
//   - Connection established during Init() and maintained via StartKeepalive()
//   - Automatic reconnection with exponential backoff on failure
//   - All methods (AddRoute, MaintenanceEnter, etc.) use the same connection
//   - Thread-safe with mutex protection for concurrent access
//
// Communication Pattern:
//   - Main.go interacts ONLY via events - zero direct proxy coupling
//   - Registry client handles all protocol details and error recovery
//   - Supports granular maintenance mode (ALL, specific routes, domains)
type RegistryClientV2 struct {
	conn      net.Conn // Single persistent TCP connection for all communications
	sessionID string   // Session ID from registration
	scanner   *bufio.Scanner
	localIP   string // The container's IP on the web-net network

	// Event handling
	handlers map[EventType][]EventHandler
	mu       sync.RWMutex

	// Retry mechanism
	registryAddr    string
	serviceName     string
	instanceName    string
	maintenancePort int
	metadata        map[string]interface{}
	failureCount    int
	inExtendedRetry bool
	done            chan struct{}

	// Configuration storage for re-registration
	storedRoutes       []RouteConfig
	storedHealthChecks map[string]HealthCheckConfig // keyed by routeID
	storedOptions      map[string]string            // key-value pairs
	routeIDs           map[string]string            // map route config to route ID
	configMu           sync.Mutex                   // protects stored configuration

	// Maintenance mode state
	inMaintenanceMode  bool
	maintenanceTarget  string
	maintenancePageURL string
	lastIP             string // Last known IP to detect changes

	// Logging
	debug     bool   // Enable verbose logging
	logPrefix string // Prefix for log messages
}

// getContainerIP detects the container's IP address on the web-net network (10.2.2.0/24)
func getContainerIP() string {
	// Try to get IP from network interfaces
	// We specifically want the IP on web-net (10.2.2.0/24)
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		fmt.Printf("[client] Warning: failed to get interface addresses: %v\n", err)
		return ""
	}

	// Look for IP in 10.2.2.0/24 subnet (web-net)
	_, webNet, _ := net.ParseCIDR("10.2.2.0/24")

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil && webNet != nil && webNet.Contains(ipnet.IP) {
				return ipnet.IP.String()
			}
		}
	}

	// Fallback: return first non-loopback IPv4
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				fmt.Printf("[client] Warning: using IP outside web-net: %s\n", ipnet.IP.String())
				return ipnet.IP.String()
			}
		}
	}

	fmt.Printf("[client] Warning: no non-loopback IP found\n")
	return ""
}

// NewRegistryClient creates a new registry client without connecting
// Call Init() to establish the connection
// Set debug=true to enable verbose internal logging
func NewRegistryClient(registryAddr string, serviceName string, instanceName string, maintenancePort int, metadata map[string]interface{}, debug bool) *RegistryClientV2 {
	return &RegistryClientV2{
		registryAddr:       registryAddr,
		serviceName:        serviceName,
		instanceName:       instanceName,
		maintenancePort:    maintenancePort,
		metadata:           metadata,
		handlers:           make(map[EventType][]EventHandler),
		done:               make(chan struct{}),
		storedRoutes:       make([]RouteConfig, 0),
		storedHealthChecks: make(map[string]HealthCheckConfig),
		storedOptions:      make(map[string]string),
		routeIDs:           make(map[string]string),
		debug:              debug,
		logPrefix:          fmt.Sprintf("[%s]", serviceName),
	}
}

// Init establishes connection to the registry and registers the service
// Set cleanupOldRoutes to true to remove routes from previous sessions
func (c *RegistryClientV2) Init() error {
	return c.InitWithCleanup(true)
}

// InitWithCleanup establishes connection with optional route cleanup
func (c *RegistryClientV2) InitWithCleanup(cleanupOldRoutes bool) error {
	// Detect container IP first
	localIP := getContainerIP()
	if localIP == "" {
		return fmt.Errorf("failed to detect container IP address")
	}
	fmt.Printf("[client] Detected container IP: %s\n", localIP)
	c.localIP = localIP
	c.lastIP = localIP // Track initial IP for change detection

	// Use IP as instance name if not provided
	if c.instanceName == "" {
		c.instanceName = localIP
	}

	if err := c.connect(); err != nil {
		return err
	}

	// Cleanup old routes if requested
	if cleanupOldRoutes {
		if err := c.CleanupOldRoutes(); err != nil {
			fmt.Printf("[client] Warning: failed to cleanup old routes: %v\n", err)
			// Don't fail Init if cleanup fails
		}
	}

	return nil
}

// connect performs the actual TCP connection and registration
func (c *RegistryClientV2) connect() error {
	conn, err := net.Dial("tcp", c.registryAddr)
	if err != nil {
		return err
	}

	// Enable TCP keepalive on client side
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	scanner := bufio.NewScanner(conn)

	// Register
	metadataJSON, _ := json.Marshal(c.metadata)
	registerCmd := fmt.Sprintf("REGISTER|%s|%s|%d|%s\n", c.serviceName, c.instanceName, c.maintenancePort, string(metadataJSON))
	conn.Write([]byte(registerCmd))

	if !scanner.Scan() {
		conn.Close()
		return fmt.Errorf("no response from registry")
	}

	response := scanner.Text()
	parts := strings.Split(response, "|")
	if len(parts) < 2 || parts[0] != "ACK" {
		conn.Close()
		return fmt.Errorf("registration failed: %s", response)
	}

	sessionID := parts[1]
	fmt.Printf("[client] Registered with session ID: %s\n", sessionID)

	c.conn = conn
	c.sessionID = sessionID
	c.scanner = scanner

	// Emit connected event
	c.emit(Event{
		Type:      EventConnected,
		Data:      map[string]interface{}{"session_id": sessionID, "local_ip": c.localIP},
		Timestamp: time.Now(),
	})

	return nil
}

// log logs a message via event system if debug is enabled
func (c *RegistryClientV2) log(level string, format string, args ...interface{}) {
	if !c.debug && level == "debug" {
		return // Skip debug logs if debug mode is off
	}

	message := fmt.Sprintf(format, args...)
	c.emit(Event{
		Type: EventLog,
		Data: map[string]interface{}{
			"level":   level,
			"message": message,
		},
		Timestamp: time.Now(),
	})
}

// logDebug logs debug-level messages (only if debug=true)
func (c *RegistryClientV2) logDebug(format string, args ...interface{}) {
	c.log("debug", format, args...)
}

// logInfo logs informational messages
func (c *RegistryClientV2) logInfo(format string, args ...interface{}) {
	c.log("info", format, args...)
}

// logWarn logs warning messages
func (c *RegistryClientV2) logWarn(format string, args ...interface{}) {
	c.log("warn", format, args...)
}

// logError logs error messages and emits error event
func (c *RegistryClientV2) logError(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	c.log("error", format, args...)
	c.emit(Event{
		Type: EventError,
		Data: map[string]interface{}{
			"message": message,
		},
		Timestamp: time.Now(),
	})
}

// logEvent is deprecated - kept for compatibility
func (c *RegistryClientV2) logEvent(level string, message string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["level"] = level
	data["message"] = message

	c.emit(Event{
		Type:      EventLog,
		Data:      data,
		Timestamp: time.Now(),
	})
}

// errorEvent emits an error event for main.go to handle
func (c *RegistryClientV2) errorEvent(message string, err error, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["message"] = message
	if err != nil {
		data["error"] = err.Error()
	}

	c.emit(Event{
		Type:      EventError,
		Data:      data,
		Timestamp: time.Now(),
	})
}

// CleanupOldRoutes removes all routes from previous sessions
// This is useful when restarting to avoid stale route configurations
func (c *RegistryClientV2) CleanupOldRoutes() error {
	fmt.Printf("[client] Checking for old routes to cleanup...\n")

	// List all current routes
	routes, err := c.ListRoutes()
	if err != nil {
		return fmt.Errorf("failed to list routes: %w", err)
	}

	if len(routes) == 0 {
		fmt.Printf("[client] No old routes found\n")
		return nil
	}

	fmt.Printf("[client] Found %d routes from previous session(s)\n", len(routes))

	// Remove all old routes
	for _, route := range routes {
		if routeMap, ok := route.(map[string]interface{}); ok {
			if routeID, ok := routeMap["id"].(string); ok {
				fmt.Printf("[client] Removing old route: %s\n", routeID)
				if err := c.RemoveRoute(routeID); err != nil {
					fmt.Printf("[client] Warning: failed to remove route %s: %v\n", routeID, err)
					// Continue removing other routes
				}
			}
		}
	}

	// Apply the removal
	if err := c.ApplyConfig(); err != nil {
		return fmt.Errorf("failed to apply route cleanup: %w", err)
	}

	fmt.Printf("[client] Old routes cleaned up successfully\n")
	return nil
}

// AddRoute stages and applies a new route
func (c *RegistryClientV2) AddRoute(domains []string, path string, backendURL string, priority int) (string, error) {
	// Store route configuration for re-registration
	c.configMu.Lock()
	routeConfig := RouteConfig{
		Domains:    domains,
		Path:       path,
		BackendURL: backendURL,
		Priority:   priority,
	}
	c.storedRoutes = append(c.storedRoutes, routeConfig)
	configKey := fmt.Sprintf("%v|%s|%s", domains, path, backendURL)
	c.configMu.Unlock()

	// Send to registry
	domainStr := strings.Join(domains, ",")
	cmd := fmt.Sprintf("ROUTE_ADD|%s|%s|%s|%s|%d\n", c.sessionID, domainStr, path, backendURL, priority)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return "", fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if parts[0] == "ERROR" {
		return "", fmt.Errorf("add route failed: %s", response)
	}

	if parts[0] != "ROUTE_OK" {
		return "", fmt.Errorf("unexpected response: %s", response)
	}

	routeID := ""
	if len(parts) > 1 {
		routeID = parts[1]
	}

	// Store routeID mapping
	c.configMu.Lock()
	c.routeIDs[configKey] = routeID
	c.configMu.Unlock()

	// Emit route added event
	c.emit(Event{
		Type: EventRouteAdded,
		Data: map[string]interface{}{
			"route_id":    routeID,
			"domains":     domains,
			"path":        path,
			"backend_url": backendURL,
			"priority":    priority,
		},
		Timestamp: time.Now(),
	})

	return routeID, nil
}

// AddRouteBulk adds multiple routes in bulk
func (c *RegistryClientV2) AddRouteBulk(routes []map[string]interface{}) ([]map[string]string, error) {
	routesJSON, _ := json.Marshal(routes)
	cmd := fmt.Sprintf("ROUTE_ADD_BULK|%s|%s\n", c.sessionID, string(routesJSON))
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if parts[0] == "ERROR" {
		return nil, fmt.Errorf("bulk add failed: %s", response)
	}

	var results []map[string]string
	if len(parts) > 1 {
		json.Unmarshal([]byte(parts[1]), &results)
	}

	return results, nil
}

// ListRoutes gets all routes (active and staged)
func (c *RegistryClientV2) ListRoutes() ([]interface{}, error) {
	cmd := fmt.Sprintf("ROUTE_LIST|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if parts[0] == "ERROR" {
		return nil, fmt.Errorf("list routes failed: %s", response)
	}

	var routes []interface{}
	if len(parts) > 1 {
		json.Unmarshal([]byte(parts[1]), &routes)
	}

	return routes, nil
}

// RemoveRoute removes a route
func (c *RegistryClientV2) RemoveRoute(routeID string) error {
	cmd := fmt.Sprintf("ROUTE_REMOVE|%s|%s\n", c.sessionID, routeID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("remove route failed: %s", response)
	}

	return nil
}

// UpdateRoute updates a route field
func (c *RegistryClientV2) UpdateRoute(routeID string, field string, value string) error {
	cmd := fmt.Sprintf("ROUTE_UPDATE|%s|%s|%s|%s\n", c.sessionID, routeID, field, value)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("update route failed: %s", response)
	}

	return nil
}

// SetHeaders sets response headers
func (c *RegistryClientV2) SetHeaders(headerName string, headerValue string) error {
	cmd := fmt.Sprintf("HEADERS_SET|%s|ALL|%s|%s\n", c.sessionID, headerName, headerValue)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("set headers failed: %s", response)
	}

	return nil
}

// SetOptions sets configuration options
func (c *RegistryClientV2) SetOptions(key string, value string) error {
	// Store option for re-registration
	c.configMu.Lock()
	c.storedOptions[key] = value
	c.configMu.Unlock()

	cmd := fmt.Sprintf("OPTIONS_SET|%s|ALL|%s|%s\n", c.sessionID, key, value)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("set options failed: %s", response)
	}

	return nil
}

// SetHealthCheck configures health checks for a route
func (c *RegistryClientV2) SetHealthCheck(routeID string, path string, interval string, timeout string) error {
	// Store health check configuration
	c.configMu.Lock()
	c.storedHealthChecks[routeID] = HealthCheckConfig{
		RouteID:  routeID,
		Path:     path,
		Interval: interval,
		Timeout:  timeout,
	}
	c.configMu.Unlock()

	cmd := fmt.Sprintf("HEALTH_SET|%s|%s|%s|%s|%s\n", c.sessionID, routeID, path, interval, timeout)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("set health check failed: %s", response)
	}

	// Emit health check set event
	c.emit(Event{
		Type: EventHealthCheckSet,
		Data: map[string]interface{}{
			"route_id": routeID,
			"path":     path,
			"interval": interval,
			"timeout":  timeout,
		},
		Timestamp: time.Now(),
	})

	return nil
}

// SetRateLimit configures rate limiting for a route
func (c *RegistryClientV2) SetRateLimit(routeID string, requests int, window string) error {
	cmd := fmt.Sprintf("RATELIMIT_SET|%s|%s|%d|%s\n", c.sessionID, routeID, requests, window)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("set rate limit failed: %s", response)
	}

	return nil
}

// SetCircuitBreaker configures circuit breaker for a route
func (c *RegistryClientV2) SetCircuitBreaker(routeID string, threshold int, timeout string, halfOpenRequests int) error {
	cmd := fmt.Sprintf("CIRCUIT_BREAKER_SET|%s|%s|%d|%s|%d\n", c.sessionID, routeID, threshold, timeout, halfOpenRequests)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("set circuit breaker failed: %s", response)
	}

	return nil
}

// ValidateConfig validates the staged configuration
func (c *RegistryClientV2) ValidateConfig() error {
	cmd := fmt.Sprintf("CONFIG_VALIDATE|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("validation failed: %s", response)
	}

	return nil
}

// ApplyConfig applies all staged configuration changes
func (c *RegistryClientV2) ApplyConfig() error {
	cmd := fmt.Sprintf("CONFIG_APPLY|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("apply config failed: %s", response)
	}

	fmt.Printf("[client] Configuration applied\n")

	// Emit config applied event
	c.emit(Event{
		Type:      EventConfigApplied,
		Data:      map[string]interface{}{},
		Timestamp: time.Now(),
	})

	return nil
}

// RollbackConfig discards all staged changes
func (c *RegistryClientV2) RollbackConfig() error {
	cmd := fmt.Sprintf("CONFIG_ROLLBACK|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("rollback config failed: %s", response)
	}

	return nil
}

// ConfigDiff shows differences between staged and active
func (c *RegistryClientV2) ConfigDiff() (map[string]interface{}, error) {
	cmd := fmt.Sprintf("CONFIG_DIFF|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if parts[0] == "ERROR" {
		return nil, fmt.Errorf("config diff failed: %s", response)
	}

	var diff map[string]interface{}
	if len(parts) > 1 {
		json.Unmarshal([]byte(parts[1]), &diff)
	}

	return diff, nil
}

// DrainStart initiates graceful drain
func (c *RegistryClientV2) DrainStart(durationSeconds int) (time.Time, error) {
	cmd := fmt.Sprintf("DRAIN_START|%s|%d\n", c.sessionID, durationSeconds)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return time.Time{}, fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if parts[0] == "ERROR" {
		return time.Time{}, fmt.Errorf("drain start failed: %s", response)
	}

	// Response format: DRAIN_OK|completion_time
	var completionTime time.Time
	if parts[0] == "DRAIN_OK" && len(parts) > 1 {
		completionTime, _ = time.Parse(time.RFC3339, parts[1])
	}

	return completionTime, nil
}

// DrainStatus gets the current drain status
func (c *RegistryClientV2) DrainStatus() (map[string]interface{}, error) {
	cmd := fmt.Sprintf("DRAIN_STATUS|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if parts[0] == "ERROR" {
		return nil, fmt.Errorf("drain status failed: %s", response)
	}

	var status map[string]interface{}
	if len(parts) > 1 {
		json.Unmarshal([]byte(parts[1]), &status)
	}

	return status, nil
}

// DrainCancel cancels the ongoing drain
func (c *RegistryClientV2) DrainCancel() error {
	cmd := fmt.Sprintf("DRAIN_CANCEL|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("drain cancel failed: %s", response)
	}

	return nil
}

// MaintenanceEnter puts service in maintenance mode for specific target
// target can be "ALL" to affect all routes, or a specific route like "/admin"
// Examples:
//   - MaintenanceEnter("ALL") - puts entire service in maintenance
//   - MaintenanceEnter("/admin") - puts only /admin route in maintenance
//   - MaintenanceEnter("domain.com") - puts specific domain in maintenance
func (c *RegistryClientV2) MaintenanceEnter(target string) error {
	return c.MaintenanceEnterWithURL(target, "")
}

// MaintenanceEnterAll is a convenience method to put the entire service in maintenance mode
func (c *RegistryClientV2) MaintenanceEnterAll() error {
	return c.MaintenanceEnterWithURL("ALL", "")
}

// MaintenanceEnterRoute puts a specific route in maintenance mode
func (c *RegistryClientV2) MaintenanceEnterRoute(route string) error {
	return c.MaintenanceEnterWithURL(route, "")
}

// MaintenanceEnterWithURL puts service in maintenance mode with custom page URL
// Waits for proxy confirmation with 30s timeout. If proxy is unreachable, enters local maintenance mode.
// The target parameter specifies what to put in maintenance:
//   - "ALL" - entire service
//   - "/path" - specific route path
//   - "domain.com" - specific domain
//
// All communication uses the persistent TCP connection established during Init()
func (c *RegistryClientV2) MaintenanceEnterWithURL(target string, maintenancePageURL string) error {
	c.logInfo("Entering maintenance mode: target=%s", target)

	// Track maintenance mode state immediately
	c.configMu.Lock()
	c.inMaintenanceMode = true
	c.maintenanceTarget = target
	c.maintenancePageURL = maintenancePageURL
	c.configMu.Unlock()

	// Try to send command to proxy
	cmd := fmt.Sprintf("MAINT_ENTER|%s|%s|%s\n", c.sessionID, target, maintenancePageURL)
	if c.conn == nil {
		c.logWarn("No proxy connection - maintenance mode active locally only")
		c.emit(Event{
			Type: EventMaintenanceEntered,
			Data: map[string]interface{}{
				"target": target,
				"mode":   "local",
			},
			Timestamp: time.Now(),
		})
		return nil
	}

	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		c.logWarn("Failed to communicate with proxy - maintenance mode active locally only: %v", err)
		c.emit(Event{
			Type: EventMaintenanceEntered,
			Data: map[string]interface{}{
				"target":               target,
				"maintenance_page_url": maintenancePageURL,
				"reason":               "proxy_communication_failed",
				"error":                err.Error(),
			},
			Timestamp: time.Now(),
		})
		return nil
	}

	// Set 30s timeout for proxy response
	waitTimeout := 30 * time.Second
	deadline := time.Now().Add(waitTimeout)

	// Set read deadline on the underlying connection
	if conn, ok := c.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = conn.SetReadDeadline(deadline)
		defer conn.SetReadDeadline(time.Time{})
	}

	// Try to read ACK response
	if !c.scanner.Scan() {
		c.logWarn("No ACK from proxy within 30s - maintenance mode active locally only")
		c.emit(Event{
			Type: EventMaintenanceEntered,
			Data: map[string]interface{}{
				"target": target,
				"mode":   "local",
			},
			Timestamp: time.Now(),
		})
		return nil
	}

	response := c.scanner.Text()
	if response != "ACK" {
		if strings.HasPrefix(response, "ERROR") {
			c.logWarn("Proxy rejected maintenance - maintenance mode active locally only: %s", response)
			c.emit(Event{
				Type: EventMaintenanceEntered,
				Data: map[string]interface{}{
					"target": target,
					"mode":   "local",
				},
				Timestamp: time.Now(),
			})
			return nil
		}
	}

	// Wait for MAINT_OK confirmation from proxy
	for c.scanner.Scan() {
		event := c.scanner.Text()
		// go-proxy sends "MAINT_OK|target" so use prefix check
		if strings.HasPrefix(event, "MAINT_OK") {
			c.logInfo("Maintenance mode confirmed by proxy")
			c.emit(Event{
				Type: EventMaintenanceEntered,
				Data: map[string]interface{}{
					"target": target,
					"mode":   "proxy",
				},
				Timestamp: time.Now(),
			})
			return nil
		}
		if strings.HasPrefix(event, "ERROR") {
			c.logWarn("Proxy error during maintenance: %s - maintenance active locally only", event)
			c.emit(Event{
				Type: EventMaintenanceEntered,
				Data: map[string]interface{}{
					"target": target,
					"mode":   "local",
				},
				Timestamp: time.Now(),
			})
			return nil
		}
	}

	// Timeout - maintenance mode active locally
	c.logWarn("Proxy confirmation timeout - maintenance mode active locally only")
	c.emit(Event{
		Type: EventMaintenanceEntered,
		Data: map[string]interface{}{
			"target": target,
			"mode":   "local",
		},
		Timestamp: time.Now(),
	})

	return nil
}

// MaintenanceExit exits maintenance mode for specific target
// target can be "ALL" to exit maintenance for all routes, or a specific route like "/admin"
// Examples:
//   - MaintenanceExit("ALL") - exits maintenance for entire service
//   - MaintenanceExit("/admin") - exits maintenance only for /admin route
//   - MaintenanceExit("domain.com") - exits maintenance for specific domain
//
// All communication uses the persistent TCP connection established during Init()
func (c *RegistryClientV2) MaintenanceExit(target string) error {
	c.logInfo("Exiting maintenance mode: target=%s", target)

	// Update maintenance mode state
	c.configMu.Lock()
	c.inMaintenanceMode = false
	c.maintenanceTarget = ""
	c.maintenancePageURL = ""
	c.configMu.Unlock()

	// Try to send command to proxy
	if c.conn == nil {
		c.logInfo("Exited maintenance mode (no proxy connection)")
		c.emit(Event{
			Type:      EventMaintenanceExited,
			Data:      map[string]interface{}{"target": target},
			Timestamp: time.Now(),
		})
		return nil
	}

	cmd := fmt.Sprintf("MAINT_EXIT|%s|%s\n", c.sessionID, target)
	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		c.logWarn("Failed to communicate with proxy for maintenance exit: %v", err)
		c.emit(Event{
			Type:      EventMaintenanceExited,
			Data:      map[string]interface{}{"target": target},
			Timestamp: time.Now(),
		})
		return nil
	}

	// Update maintenance mode state
	c.configMu.Lock()
	c.inMaintenanceMode = false
	c.maintenanceTarget = ""
	c.maintenancePageURL = ""
	c.configMu.Unlock()

	if !c.scanner.Scan() {
		c.errorEvent("No response from proxy for maintenance exit", nil, nil)
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if response != "ACK" {
		if strings.HasPrefix(response, "ERROR") {
			c.errorEvent("Proxy rejected maintenance exit", fmt.Errorf(response), nil)
			return fmt.Errorf("maintenance exit failed: %s", response)
		}
	}

	// Wait for MAINT_OK confirmation. Set timeout
	waitTimeout := 10 * time.Second
	if conn, ok := c.conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = conn.SetReadDeadline(time.Now().Add(waitTimeout))
		defer conn.SetReadDeadline(time.Time{})
	}

	for c.scanner.Scan() {
		event := c.scanner.Text()
		// go-proxy sends "MAINT_OK|target" so use prefix check
		if strings.HasPrefix(event, "MAINT_OK") {
			c.logEvent("info", "Proxy confirmed maintenance exit", map[string]interface{}{
				"target": target,
			})
			// Emit maintenance proxy exited event
			c.emit(Event{
				Type:      EventMaintenanceExited,
				Data:      map[string]interface{}{"target": target, "event": event},
				Timestamp: time.Now(),
			})
			return nil
		}
		if strings.HasPrefix(event, "ERROR") {
			c.errorEvent("Proxy error during maintenance exit", fmt.Errorf(event), nil)
			return fmt.Errorf("maintenance exit failed: %s", event)
		}
	}

	c.logEvent("warn", "No confirmation from proxy for maintenance exit", nil)
	return fmt.Errorf("no MAINT_OK received")
}

// MaintenanceExitAll is a convenience method to exit maintenance mode for the entire service
func (c *RegistryClientV2) MaintenanceExitAll() error {
	return c.MaintenanceExit("ALL")
}

// MaintenanceExitRoute exits maintenance mode for a specific route
func (c *RegistryClientV2) MaintenanceExitRoute(route string) error {
	return c.MaintenanceExit(route)
}

// MaintenanceStatus gets maintenance status
// Uses the persistent TCP connection for the query
func (c *RegistryClientV2) MaintenanceStatus() (map[string]interface{}, error) {
	cmd := fmt.Sprintf("MAINT_STATUS|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if parts[0] == "ERROR" {
		return nil, fmt.Errorf("maintenance status failed: %s", response)
	}

	var status map[string]interface{}
	if len(parts) > 1 {
		json.Unmarshal([]byte(parts[1]), &status)
	}

	return status, nil
}

// GetStats retrieves statistics for the service
func (c *RegistryClientV2) GetStats() ([]interface{}, error) {
	cmd := fmt.Sprintf("STATS_GET|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if parts[0] == "ERROR" {
		return nil, fmt.Errorf("get stats failed: %s", response)
	}

	var stats []interface{}
	if len(parts) > 1 {
		json.Unmarshal([]byte(parts[1]), &stats)
	}

	return stats, nil
}

// TestBackend tests if a backend is reachable
func (c *RegistryClientV2) TestBackend(backendURL string) (map[string]interface{}, error) {
	cmd := fmt.Sprintf("BACKEND_TEST|%s|%s\n", c.sessionID, backendURL)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid response")
	}

	var result map[string]interface{}
	if len(parts) > 1 {
		json.Unmarshal([]byte(parts[1]), &result)
	}

	return result, nil
}

// SessionInfo gets session information
func (c *RegistryClientV2) SessionInfo() (map[string]interface{}, error) {
	cmd := fmt.Sprintf("SESSION_INFO|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return nil, fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	parts := strings.Split(response, "|")
	if parts[0] == "ERROR" {
		return nil, fmt.Errorf("session info failed: %s", response)
	}

	var info map[string]interface{}
	if len(parts) > 1 {
		json.Unmarshal([]byte(parts[1]), &info)
	}

	return info, nil
}

// Ping keeps the connection alive
func (c *RegistryClientV2) Ping() error {
	cmd := fmt.Sprintf("PING|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if response != "PONG" {
		return fmt.Errorf("unexpected response to ping: %s", response)
	}

	return nil
}

// Shutdown gracefully shuts down the service
func (c *RegistryClientV2) Shutdown() error {
	cmd := fmt.Sprintf("CLIENT_SHUTDOWN|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response from registry")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("shutdown failed: %s", response)
	}

	c.conn.Close()

	// Emit disconnected event
	c.emit(Event{
		Type:      EventDisconnected,
		Data:      map[string]interface{}{"reason": "shutdown"},
		Timestamp: time.Now(),
	})

	return nil
}

// Close closes the connection and stops the keepalive loop
func (c *RegistryClientV2) Close() {
	close(c.done)
	if c.conn != nil {
		c.conn.Close()
	}
	c.emit(Event{
		Type:      EventDisconnected,
		Data:      map[string]interface{}{"reason": "close"},
		Timestamp: time.Now(),
	})
}

// On registers an event handler for a specific event type
func (c *RegistryClientV2) On(eventType EventType, handler EventHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[eventType] = append(c.handlers[eventType], handler)
}

// Off removes all handlers for a specific event type
func (c *RegistryClientV2) Off(eventType EventType) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.handlers, eventType)
}

// emit triggers all handlers for a specific event
func (c *RegistryClientV2) emit(event Event) {
	c.mu.RLock()
	handlers := c.handlers[event.Type]
	c.mu.RUnlock()

	for _, handler := range handlers {
		// Run handlers in goroutines to avoid blocking
		go handler(event)
	}
}

// GetLocalIP returns the detected container IP address
func (c *RegistryClientV2) GetLocalIP() string {
	return c.localIP
}

// BuildBackendURL builds a backend URL using the container's IP and specified port
func (c *RegistryClientV2) BuildBackendURL(port string) string {
	return fmt.Sprintf("http://%s:%s", c.localIP, port)
}

// BuildMaintenanceURL builds a maintenance URL using the container's IP and specified port
func (c *RegistryClientV2) BuildMaintenanceURL(port string) string {
	return fmt.Sprintf("http://%s:%s/", c.localIP, port)
}

// StartKeepalive starts the keepalive loop with automatic retry mechanism
func (c *RegistryClientV2) StartKeepalive() {
	normalInterval := 30 * time.Second
	extendedRetryInterval := 60 * time.Second
	maxQuickRetries := 5

	ticker := time.NewTicker(normalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := c.Ping()
			if err != nil {
				c.failureCount++
				fmt.Printf("[client] Keepalive ping failed (attempt %d): %v\n", c.failureCount, err)

				// Emit retry event
				c.emit(Event{
					Type: EventRetrying,
					Data: map[string]interface{}{
						"attempt": c.failureCount,
						"error":   err.Error(),
					},
					Timestamp: time.Now(),
				})

				// Try to reconnect with backoff strategy
				go c.reconnectWithBackoff(maxQuickRetries, &ticker, normalInterval, extendedRetryInterval)
			} else {
				// Successful ping - reset failure count
				if c.failureCount > 0 {
					c.failureCount = 0
					if c.inExtendedRetry {
						fmt.Printf("[client] Connection stable, returning to normal ping interval\n")
						c.inExtendedRetry = false
						ticker.Reset(normalInterval)
					}
				}
			}
		case <-c.done:
			return
		}
	}
}

// reconnectWithBackoff attempts to reconnect with exponential backoff
func (c *RegistryClientV2) reconnectWithBackoff(maxQuickRetries int, ticker **time.Ticker, normalInterval, extendedRetryInterval time.Duration) {
	attempt := c.failureCount
	var retryDelay time.Duration

	// First 5 attempts: exponential backoff (5s, 10s, 15s, 20s, 25s)
	if attempt <= maxQuickRetries {
		retryDelay = time.Duration(attempt*5) * time.Second
	} else {
		// After 5 failures: switch to 1-minute retries
		if !c.inExtendedRetry {
			fmt.Printf("[client] Switching to extended retry mode (1-minute intervals)\n")
			c.inExtendedRetry = true
			(*ticker).Reset(extendedRetryInterval)

			// Emit extended retry event
			c.emit(Event{
				Type:      EventExtendedRetry,
				Data:      map[string]interface{}{"interval": extendedRetryInterval.String()},
				Timestamp: time.Now(),
			})
		}
		retryDelay = 5 * time.Second
	}

	time.Sleep(retryDelay)
	fmt.Printf("[client] Attempting reconnection (attempt %d, after %v delay)...\n", attempt, retryDelay)

	if err := c.reconnect(); err != nil {
		fmt.Printf("[client] Reconnection attempt %d failed: %v\n", attempt, err)
	} else {
		// Connection successful - reset to normal behavior
		c.failureCount = 0
		if c.inExtendedRetry {
			fmt.Printf("[client] Connection restored! Returning to normal ping interval\n")
			c.inExtendedRetry = false
			(*ticker).Reset(normalInterval)
		}
		fmt.Printf("[client] Successfully reconnected to registry\n")

		// Emit reconnected event
		c.emit(Event{
			Type:      EventReconnected,
			Data:      map[string]interface{}{"attempt": attempt, "session_id": c.sessionID},
			Timestamp: time.Now(),
		})
	}
}

// reconnect attempts to reconnect to the registry
func (c *RegistryClientV2) reconnect() error {
	// Close old connection if it exists
	if c.conn != nil {
		c.conn.Close()
	}

	// Detect container IP
	localIP := getContainerIP()
	if localIP == "" {
		return fmt.Errorf("failed to detect container IP address")
	}
	c.localIP = localIP

	// Establish new TCP connection
	conn, err := net.Dial("tcp", c.registryAddr)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}

	// Enable TCP keepalive
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
	}

	scanner := bufio.NewScanner(conn)

	// Try to reconnect to existing session first
	if c.sessionID != "" {
		reconnectCmd := fmt.Sprintf("RECONNECT|%s\n", c.sessionID)
		conn.Write([]byte(reconnectCmd))

		if scanner.Scan() {
			response := scanner.Text()
			if response == "OK" {
				// Session restored successfully, routes reactivated
				fmt.Printf("[client] Session %s restored, routes reactivated\n", c.sessionID)
				c.conn = conn
				c.scanner = scanner

				// Emit connected event
				c.emit(Event{
					Type:      EventConnected,
					Data:      map[string]interface{}{"session_id": c.sessionID, "local_ip": c.localIP, "reconnected": true},
					Timestamp: time.Now(),
				})
				return nil
			} else if response == "REREGISTER" {
				// Session expired, need to re-register
				fmt.Printf("[client] Session expired, re-registering...\n")
				// Fall through to registration
			}
		}
	}

	// Session not found or expired - do full registration
	metadataJSON, _ := json.Marshal(c.metadata)
	registerCmd := fmt.Sprintf("REGISTER|%s|%s|%d|%s\n", c.serviceName, c.instanceName, c.maintenancePort, string(metadataJSON))
	conn.Write([]byte(registerCmd))

	if !scanner.Scan() {
		conn.Close()
		return fmt.Errorf("no response from registry")
	}

	response := scanner.Text()
	parts := strings.Split(response, "|")
	if len(parts) < 2 || parts[0] != "ACK" {
		conn.Close()
		return fmt.Errorf("registration failed: %s", response)
	}

	sessionID := parts[1]
	fmt.Printf("[client] Re-registered with new session ID: %s\n", sessionID)

	c.conn = conn
	c.sessionID = sessionID
	c.scanner = scanner

	// Replay stored configuration after re-registration
	if err := c.replayConfiguration(); err != nil {
		fmt.Printf("[client] Warning: failed to replay configuration: %v\n", err)
		// Don't fail reconnect, just log the warning
	}

	// Check for IP changes and cleanup stale routes if needed
	if err := c.checkIPChange(); err != nil {
		fmt.Printf("[client] Warning: IP change check failed: %v\n", err)
	}

	// Restore maintenance mode if it was active
	if err := c.restoreMaintenanceModeIfNeeded(); err != nil {
		fmt.Printf("[client] Warning: failed to restore maintenance mode: %v\n", err)
	}

	// Emit connected event
	c.emit(Event{
		Type:      EventConnected,
		Data:      map[string]interface{}{"session_id": sessionID, "local_ip": c.localIP, "reregistered": true},
		Timestamp: time.Now(),
	})

	return nil
}

// replayConfiguration re-applies all stored routes, health checks, and options after re-registration
func (c *RegistryClientV2) replayConfiguration() error {
	c.configMu.Lock()
	defer c.configMu.Unlock()

	fmt.Printf("[client] Replaying stored configuration...\n")

	// Clear old route IDs
	c.routeIDs = make(map[string]string)

	// Re-add all routes
	for _, route := range c.storedRoutes {
		domainStr := strings.Join(route.Domains, ",")
		cmd := fmt.Sprintf("ROUTE_ADD|%s|%s|%s|%s|%d\n", c.sessionID, domainStr, route.Path, route.BackendURL, route.Priority)
		c.conn.Write([]byte(cmd))

		if !c.scanner.Scan() {
			return fmt.Errorf("no response for route add")
		}

		response := c.scanner.Text()
		parts := strings.Split(response, "|")
		if parts[0] != "ROUTE_OK" {
			return fmt.Errorf("route add failed: %s", response)
		}

		routeID := ""
		if len(parts) > 1 {
			routeID = parts[1]
		}

		configKey := fmt.Sprintf("%v|%s|%s", route.Domains, route.Path, route.BackendURL)
		c.routeIDs[configKey] = routeID
		fmt.Printf("[client] Re-added route: %v%s -> %s (ID: %s)\n", route.Domains, route.Path, route.BackendURL, routeID)
	}

	// Re-apply health checks
	// Note: After re-registration, we have new route IDs. The old health checks
	// are stored by old route IDs, but we need to apply them to new route IDs.
	// Since we re-added routes in the same order, we'll try to apply each health check
	// to all new routes and use the first successful one.
	newHealthChecks := make(map[string]HealthCheckConfig)
	for _, hc := range c.storedHealthChecks {
		applied := false
		for _, newRouteID := range c.routeIDs {
			cmd := fmt.Sprintf("HEALTH_SET|%s|%s|%s|%s|%s\n", c.sessionID, newRouteID, hc.Path, hc.Interval, hc.Timeout)
			c.conn.Write([]byte(cmd))

			if !c.scanner.Scan() {
				return fmt.Errorf("no response for health check")
			}

			response := c.scanner.Text()
			if !strings.HasPrefix(response, "ERROR") {
				newHealthChecks[newRouteID] = hc
				fmt.Printf("[client] Re-applied health check for route %s\n", newRouteID)
				applied = true
				break // Only apply once
			}
		}
		if !applied {
			fmt.Printf("[client] Warning: Could not re-apply health check %+v\n", hc)
		}
	}
	c.storedHealthChecks = newHealthChecks

	// Re-apply options
	for key, value := range c.storedOptions {
		cmd := fmt.Sprintf("OPTIONS_SET|%s|ALL|%s|%s\n", c.sessionID, key, value)
		c.conn.Write([]byte(cmd))

		if !c.scanner.Scan() {
			return fmt.Errorf("no response for options set")
		}

		response := c.scanner.Text()
		if strings.HasPrefix(response, "ERROR") {
			return fmt.Errorf("options set failed: %s", response)
		}
		fmt.Printf("[client] Re-applied option: %s=%s\n", key, value)
	}

	// Apply configuration
	cmd := fmt.Sprintf("CONFIG_APPLY|%s\n", c.sessionID)
	c.conn.Write([]byte(cmd))

	if !c.scanner.Scan() {
		return fmt.Errorf("no response for config apply")
	}

	response := c.scanner.Text()
	if strings.HasPrefix(response, "ERROR") {
		return fmt.Errorf("config apply failed: %s", response)
	}

	fmt.Printf("[client] Configuration replayed and applied successfully\n")
	return nil
}

// IsInMaintenanceMode returns whether the service is currently in maintenance mode
func (c *RegistryClientV2) IsInMaintenanceMode() bool {
	c.configMu.Lock()
	defer c.configMu.Unlock()
	return c.inMaintenanceMode
}

// checkIPChange detects if the container IP has changed and cleans up stale routes
func (c *RegistryClientV2) checkIPChange() error {
	currentIP := getContainerIP()
	if currentIP == "" {
		return fmt.Errorf("failed to detect current IP")
	}

	c.configMu.Lock()
	lastIP := c.lastIP
	c.configMu.Unlock()

	if currentIP != lastIP {
		c.logEvent("info", "IP address changed - cleaning up stale routes", map[string]interface{}{
			"old_ip": lastIP,
			"new_ip": currentIP,
		})

		// Emit IP changed event
		c.emit(Event{
			Type: EventIPChanged,
			Data: map[string]interface{}{
				"old_ip": lastIP,
				"new_ip": currentIP,
			},
			Timestamp: time.Now(),
		})

		// Update local IP
		c.localIP = currentIP
		c.configMu.Lock()
		c.lastIP = currentIP
		c.configMu.Unlock()

		// Cleanup old routes with the old IP
		if err := c.CleanupOldRoutes(); err != nil {
			c.errorEvent("Failed to cleanup old routes after IP change", err, map[string]interface{}{
				"old_ip": lastIP,
				"new_ip": currentIP,
			})
		}

		// Re-register with new IP
		return c.Init()
	}

	return nil
}

// restoreMaintenanceModeIfNeeded re-enters maintenance mode after reconnection if it was active
func (c *RegistryClientV2) restoreMaintenanceModeIfNeeded() error {
	c.configMu.Lock()
	inMaintenance := c.inMaintenanceMode
	target := c.maintenanceTarget
	pageURL := c.maintenancePageURL
	c.configMu.Unlock()

	if inMaintenance {
		fmt.Printf("[client] Restoring maintenance mode: target=%s\n", target)
		return c.MaintenanceEnterWithURL(target, pageURL)
	}

	return nil
}

// Close stops the keepalive loop and closes the connection
