# Registry Client V2

Shared Go module for service registry communication with go-proxy.

## Features

- **Single Persistent TCP Connection** - All communications use one maintained connection
- **Event-Driven Architecture** - Zero coupling between services and proxy
- **Automatic Reconnection** - Exponential backoff with session restoration
- **Infinite DNS Retry** - Handles temporary DNS failures during container startup with infinite automatic retry
- **Granular Maintenance Mode** - Control maintenance at service, route, or domain level
- **IP Change Detection** - Automatic cleanup and re-registration on IP changes
- **Configuration Persistence** - Routes, health checks, and options survive reconnections

## Usage

```go
import registryclient "registry-client-v2"

// Create client
client := registryclient.NewRegistryClient(
    "go-proxy:7000",
    "myservice",
    "instance-1",
    3001, // maintenance port
    metadata,
)

// Register event handlers
client.On(registryclient.EventConnected, func(e registryclient.Event) {
    log.Printf("Connected: %v", e.Data)
})

client.On(registryclient.EventMaintenanceProxyEntered, func(e registryclient.Event) {
    log.Printf("Maintenance mode active")
})

// Initialize and start
client.Init()
go client.StartKeepalive()

// Add routes
client.AddRoute([]string{"example.com"}, "/", "http://backend:3000/", 100)
client.SetHealthCheck(routeID, "/health", "10s", "5s")
client.ApplyConfig()

// Maintenance mode - granular control
client.MaintenanceEnterAll()                  // Entire service
client.MaintenanceEnter("/admin")            // Specific route
client.MaintenanceEnter("admin.example.com") // Specific domain

// Exit maintenance
client.MaintenanceExitAll()
client.MaintenanceExitRoute("/admin")
```

## Events

### Connection Events
- `CONNECTED` - Connected to proxy
- `DISCONNECTED` - Disconnected from proxy
- `RETRYING` - Attempting reconnection
- `EXTENDED_RETRY` - In extended retry mode
- `RECONNECTED` - Successfully reconnected

### Route Management
- `ROUTE_ADDED` - Route added
- `ROUTE_REMOVED` - Route removed
- `HEALTH_CHECK_SET` - Health check configured
- `CONFIG_APPLIED` - Configuration applied

### Maintenance Mode
- `MAINT_REQUESTED` - Maintenance requested
- `MAINT_PROXY_ENTERED` - Proxy confirmed entry (30s timeout)
- `MAINT_LOCAL_ENTERED` - Local fallback (no proxy)
- `MAINT_EXIT_REQUESTED` - Exit requested
- `MAINT_PROXY_EXITED` - Proxy confirmed exit
- `MAINT_LOCAL_EXITED` - Local exit

### Utility
- `CONFIG_VALIDATED` - Config validation success
- `CONFIG_INVALID` - Config validation failed
- `LOG` - General logging
- `ERROR` - Error occurred
- `IP_CHANGED` - Container IP changed

## Architecture

- Single TCP connection for all operations
- Thread-safe with mutex protection
- Automatic session restoration via RECONNECT command
- Configuration replay after reconnection
- Maintenance mode state preservation
- 30-second timeout for proxy confirmation with local fallback
