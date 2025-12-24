# Registry Client V2

[![Go Reference](https://pkg.go.dev/badge/github.com/chilla55/registry-client-v2.svg)](https://pkg.go.dev/github.com/chilla55/registry-client-v2)
[![Go Report Card](https://goreportcard.com/badge/github.com/chilla55/registry-client-v2)](https://goreportcard.com/report/github.com/chilla55/registry-client-v2)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Shared Go module implementing the registry V2 protocol client for service registration and management.

## Features

- 🔌 **Single Persistent TCP Connection** - One connection handles all operations
- 🔄 **Automatic Reconnection** - Exponential backoff with session restoration
- 💾 **Configuration Persistence** - Routes and settings preserved across reconnects
- 🛠️ **Maintenance Mode** - Proxy-confirmed or local fallback after 30s timeout
- 🌐 **IP Change Detection** - Automatically cleans up stale routes
- 📡 **Event-Driven Architecture** - Simple event system for status updates
- 🐛 **Debug Logging** - Optional verbose logging for troubleshooting
- 🔒 **Thread-Safe** - Mutex-protected handlers and configuration

## Installation

```bash
go get github.com/chilla55/registry-client-v2@latest
```

## Quick Start

```go
package main

import (
    "log"
    registryclient "github.com/chilla55/registry-client-v2"
)

func main() {
    // Create client
    client := registryclient.NewRegistryClient(
        "go-proxy:9090",    // Registry address
        "my-service",       // Service name
        "",                 // Instance ID (empty for auto)
        3001,               // Maintenance page port
        map[string]interface{}{
            "version": "1.0.0",
        },
        true,               // Debug mode
    )

    // Subscribe to events
    client.On(registryclient.EventLog, func(event registryclient.Event) {
        level := event.Data["level"]
        message := event.Data["message"]
        log.Printf("[%v] %v", level, message)
    })

    // Initialize connection
    if err := client.Init(); err != nil {
        log.Fatal(err)
    }
    defer client.Shutdown()

    // Register route
    domains := []string{"example.com", "www.example.com"}
    backendURL := client.BuildBackendURL(3000)
    routeID, err := client.AddRoute(domains, "/", backendURL, 10)
    if err != nil {
        log.Fatal(err)
    }

    // Add health check
    err = client.SetHealthCheck(routeID, "/health", "30s", "5s")
    if err != nil {
        log.Fatal(err)
    }

    // Apply configuration
    if err := client.ApplyConfig(); err != nil {
        log.Fatal(err)
    }

    // Keep running
    select {}
}
```

## Events

The client uses an event-driven architecture. Subscribe to events to monitor status:

| Event | Description | Data Fields |
|-------|-------------|-------------|
| `EventLog` | Internal logging | `level`, `message` |
| `EventError` | Critical errors | `message` |
| `EventMaintenanceEntered` | Maintenance mode activated | `target`, `mode` (proxy/local) |
| `EventMaintenanceExited` | Maintenance mode deactivated | `target` |
| `EventIPChanged` | IP address changed | `old_ip`, `new_ip` |

## Maintenance Mode

```go
// Enter maintenance for all routes
maintenanceURL := "http://" + client.GetLocalIP() + ":3001"
client.MaintenanceEnterAll(maintenanceURL)

// Enter maintenance for specific route
client.MaintenanceEnterRoute("/api", maintenanceURL)

// Exit maintenance
client.MaintenanceExit("ALL")
```

Maintenance mode is automatically preserved across reconnections.

## Configuration

The client automatically stores and replays configuration after reconnections:
- Routes
- Health checks
- Options
- Maintenance mode state

## Testing

```bash
# Run tests
go test -v ./...

# Run tests with coverage
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

See [TESTING.md](TESTING.md) for detailed testing guide.

## Documentation

- [API Documentation](https://pkg.go.dev/github.com/chilla55/registry-client-v2)
- [Testing Guide](TESTING.md)
- [Examples](examples/)

## Used By

- [orbat](https://github.com/chilla55/docker-images/tree/main/orbat) - Next.js application with zero-downtime deployments
- [petrodactyl](https://github.com/chilla55/docker-images/tree/main/petrodactyl) - PHP-FPM panel
- [vaultwarden](https://github.com/chilla55/docker-images/tree/main/vaultwarden) - Password manager

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

MIT License - see [LICENSE](LICENSE) file for details

## Support

- 📫 Issues: [GitHub Issues](https://github.com/chilla55/registry-client-v2/issues)
- 💬 Discussions: [GitHub Discussions](https://github.com/chilla55/registry-client-v2/discussions)
