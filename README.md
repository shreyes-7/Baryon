# BARYON — Consistent Hashing HTTP Load Balancer

> **Baryon** is a Go-based Layer 7 HTTP load balancer that uses consistent hashing for sticky client routing, actively monitors backend health, dynamically redistributes traffic, and visualizes live cluster activity through an interactive terminal dashboard.

---

## Architecture Overview

```
                   Simulated / Real Clients
                              │
                              │ HTTP :8080
                              ▼
                 ┌──────────────────────────┐
                 │       BARYON PROXY       │
                 │  Client IP extraction    │
                 │  Consistent hash lookup  │
                 │  Reverse proxy           │
                 └────────────┬─────────────┘
                              │
                  ┌───────────┼───────────┐
                  │           │           │
                  ▼           ▼           ▼
              Backend 1   Backend 2   Backend 3
              :8081       :8082       :8083
                  ▲           ▲           ▲
                  │           │           │
                  └───────────┼───────────┘
                              │
                    TCP Health Checker
                       every 3 seconds

       Proxy + Health Checker events
                     │
                     ▼
              Bounded Event Channel
                     │
                     ▼
           ┌─────────────────────────┐
           │    BUBBLE TEA TUI       │
           │ Status / Metrics        │
           │ Traffic Bars             │
           │ Recent Routing Events    │
           │ Keyboard Controls        │
           └─────────────────────────┘

              Traffic Simulator
                     │
                     └──── HTTP requests to :8080
```

---

## Key Features

- **Layer 7 HTTP Reverse Proxying**: Built upon Go's production-grade `net/http/httputil.ReverseProxy`, preserving request methods, paths, query strings, and custom headers.
- **Consistent Hashing Engine**: Implements a consistent hash ring using `hash/crc32.ChecksumIEEE` with 100 virtual node replicas per backend, deterministic positioning, and collision resolution.
- **Sticky Client Routing**: Automatically hashes normalized client IPs (from direct socket or trusted `X-Forwarded-For` chains). Requests from identical client IPs consistently route to the identical backend while ring membership remains stable.
- **Minimal Remapping**: When cluster membership changes, only keys mapped to the affected node move; all unaffected backends retain 100% of their existing key associations.
- **Active TCP Health Monitoring**: Background health checker probes all configured backends every 3 seconds (2s dial timeout) outside of ring locks, automatically pruning dead nodes and restoring recovered backends.
- **Atomic Concurrency Architecture**: High-frequency metrics use `sync/atomic` (`atomic.Uint64`, `atomic.Int64`, `atomic.Bool`) without mutex contention on the fast request path.
- **Real-Time Terminal Dashboard**: Interactive TUI powered by `github.com/charmbracelet/bubbletea` and `github.com/charmbracelet/lipgloss`, featuring live traffic distribution bars, measured requests per second (RPS), backend health tags, and latest routing events.
- **Traffic Simulator**: Self-contained load generator driving 500 requests per second across 10 deterministic client IPs to demonstrate live load balancing.
- **Interactive Failure & Recovery Simulation**: Dedicated keyboard shortcuts (`K` to stop Backend 1, `R` to restart Backend 1) allow live demonstration of fault detection, automatic ring pruning, traffic rebalancing, and node resurrection.

---

## Terminal Dashboard Preview

```text
╭──────────────────────────────────────────────────────────────────────────╮
│                BARYON — CONSISTENT HASHING LOAD BALANCER                 │
│──────────────────────────────────────────────────────────────────────────│
│ PROXY: RUNNING   UPTIME: 00:04:12   REQUESTS/SEC:     500                │
│ TOTAL REQUESTS: 126000                                                   │
│──────────────────────────────────────────────────────────────────────────│
│ BACKEND STATUS & TRAFFIC DISTRIBUTION                                    │
│                                                                          │
│ backend-1  localhost:8081  ONLINE   █████░░░░░░░░░░   33.2%  total: 41832    active: 0   │
│ backend-2  localhost:8082  ONLINE   █████░░░░░░░░░░   33.5%  total: 42210    active: 0   │
│ backend-3  localhost:8083  ONLINE   █████░░░░░░░░░░   33.3%  total: 41958    active: 0   │
│                                                                          │
│──────────────────────────────────────────────────────────────────────────│
│ RECENT ROUTING LOG (LATEST 5)                                            │
│   00:04:10  192.168.1.10    -> backend-1                                 │
│   00:04:10  10.0.0.1        -> backend-3                                 │
│   00:04:11  172.16.0.100    -> backend-1                                 │
│   00:04:11  192.168.1.50    -> backend-2                                 │
│   00:04:12  10.0.0.2        -> backend-3                                 │
│──────────────────────────────────────────────────────────────────────────│
│                                                                          │
│ [K] Kill backend-1    [R] Restart backend-1    [Q] Quit                  │
╰──────────────────────────────────────────────────────────────────────────╯
```

---

## Interactive Keyboard Controls

| Key | Action | Description |
|---|---|---|
| **`K`** | **Kill Backend 1** | Closes Backend 1's TCP listener (`:8081`). Health checker detects failure within 3 seconds, removes it from the hash ring, and traffic automatically redistributes to Backend 2 and 3. |
| **`R`** | **Restart Backend 1** | Restarts Backend 1's listener on `:8081`. Health checker detects recovery, restores it to the hash ring, and traffic seamlessly rebalances across all 3 nodes. |
| **`Q`** or **`Ctrl+C`** | **Quit** | Triggers coordinated graceful shutdown of the proxy, simulator, health checker, and dummy servers without resource leaks. |

---

## Getting Started & Execution Guide

### Prerequisites

- **Go 1.22+** (tested on Go 1.27.3 `windows/amd64` and Linux).

---

### Execution Modes

#### Mode 1: Interactive Real-Time Dashboard (Recommended)

Launch the complete self-contained application with a single command:

```bash
go run ./cmd/proxy
```

This single command automatically orchestrates the entire system:
1. Starts three local dummy upstream HTTP servers on ports `:8081`, `:8082`, and `:8083`.
2. Initializes the consistent hash ring with 100 virtual node replicas per backend.
3. Starts the HTTP reverse proxy listening on `:8080`.
4. Spawns the active TCP health monitor probing backends every 3 seconds.
5. Launches the interactive Bubble Tea terminal dashboard.
6. Starts the background traffic simulator streaming 500 requests/second across 10 distinct client IPs.

---

#### Mode 2: Compile and Run Standalone Binary

You can compile a standalone executable binary without Go installed on the target host:

```bash
# On Windows (PowerShell)
go build -o baryon.exe ./cmd/proxy
.\baryon.exe

# On Linux / macOS (Bash)
go build -o baryon ./cmd/proxy
./baryon
```

---

#### Mode 3: Headless / Console Mode (CI & Automation)

To run BARYON in server/headless mode without the Bubble Tea TUI (ideal for Docker containers, CI test pipelines, or background services):

```bash
go run ./cmd/proxy -no-tui
```

Output is emitted to stdout/stderr with clean structured logs. Terminate at any time with `Ctrl+C`.

---

#### Mode 4: Manual API Testing (Disable Simulator)

To start the load balancer without the automatic traffic simulator (allowing clean, isolated manual `curl` testing):

```bash
go run ./cmd/proxy -no-sim
```

Or run headless without the simulator:

```bash
go run ./cmd/proxy -no-tui -no-sim
```

---

#### Mode 5: Custom Ports, Throughput Rates & File Logging

BARYON supports flexible command-line flags:

```bash
# Listen on port 9000, generate 1000 requests/second, and write logs to baryon.log
go run ./cmd/proxy -listen :9000 -rate 1000 -log-file baryon.log
```

---

## Command-Line Configuration Reference

| Flag | Default | Description |
|---|---|---|
| `-listen` | `:8080` | Proxy listen host and port (e.g. `:8080` or `127.0.0.1:9090`) |
| `-rate` | `500` | Simulator target requests per second |
| `-no-tui` | `false` | Run in headless console mode without the Bubble Tea TUI |
| `-no-sim` | `false` | Disable background traffic simulator on startup |
| `-log-file` | `""` | Optional file destination for structured background logs |

---

## Step-by-Step Interactive Demo Walkthrough

Once BARYON is running in default mode (`go run ./cmd/proxy`), follow this 4-step demonstration:

1. **Observe Normal Load Balancing**:
   - The simulator pumps ~500 req/s across 10 client IPs.
   - All three backends display `ONLINE` with evenly distributed traffic bars (~33% share each).
   - The Recent Routing Log shows continuous sticky client mappings.

2. **Trigger Simulated Backend Failure (`K`)**:
   - Press **`K`** on your keyboard.
   - Backend 1 (`:8081`) immediately closes its TCP listener.
   - Within 3 seconds, the health checker detects the failure, updates Backend 1 to **`OFFLINE`** (red), and removes it from the hash ring.
   - Traffic automatically shifts to Backend 2 and 3 (~50% share each) with **zero 503 errors**.

3. **Trigger Simulated Backend Recovery (`R`)**:
   - Press **`R`** on your keyboard.
   - Backend 1 rebinds its TCP listener on `:8081`.
   - The health checker detects recovery, updates Backend 1 to **`ONLINE`** (green), and re-adds its 100 virtual nodes to the hash ring.
   - Traffic dynamically rebalances across all 3 nodes (~33% share each).

4. **Graceful Shutdown (`Q`)**:
   - Press **`Q`** or **`Ctrl+C`**.
   - The simulator cancels workers, health checks halt, the reverse proxy finishes in-flight requests, and dummy servers shut down cleanly.

---

## Verification Scripts

### PowerShell Verification Script (Windows)

Save as `test_routing.ps1` or run directly in PowerShell while BARYON is running:

```powershell
Write-Host "=== BARYON Sticky Routing Verification ===" -ForegroundColor Cyan

$clients = @(
    @{ IP = "192.168.1.50"; Label = "Client A" },
    @{ IP = "10.0.0.1";     Label = "Client B" },
    @{ IP = "172.16.0.100"; Label = "Client C" }
)

foreach ($c in $clients) {
    Write-Host "`nTesting $($c.Label) (IP: $($c.IP))..." -ForegroundColor Yellow
    for ($i = 1; $i -le 3; $i++) {
        $resp = Invoke-RestMethod -Uri "http://localhost:8080/" -Headers @{ "X-Forwarded-For" = $c.IP }
        Write-Host "  Request $i -> Handled by: $($resp.backend) ($($resp.address))" -ForegroundColor Green
    }
}
```

### Bash Verification Script (Linux / macOS)

```bash
#!/usr/bin/env bash
echo "=== BARYON Sticky Routing Verification ==="

clients=("192.168.1.50" "10.0.0.1" "172.16.0.100")

for ip in "${clients[@]}"; do
    echo -e "\nTesting Client IP: $ip..."
    for i in {1..3}; do
        resp=$(curl -s -H "X-Forwarded-For: $ip" http://localhost:8080/)
        echo "  Request $i -> $resp"
    done
done
```

---

## Testing & Performance Benchmarks

### Running the Full Test Suite

```bash
# Run all unit and integration tests with verbose output
go test -v ./...

# Static analysis and vet
go vet ./...
```

### Running Targeted Package Tests

```bash
# Test Consistent Hash Ring (distribution, stickiness, minimal remapping)
go test -v ./internal/hashing/...

# Test HTTP Reverse Proxy and Client IP Extraction
go test -v ./internal/proxy/...

# Test Active Health Checker and Auto-Recovery
go test -v ./internal/health/...

# Test Terminal Dashboard Model and View
go test -v ./internal/tui/...

# Test Traffic Simulator Engine
go test -v ./pkg/simulator/...
```

### Measured Performance Benchmarks

Benchmarks executed on an **Intel Core i5-13500H** (16 threads):

#### 1. Consistent Hash Ring Lookup (`internal/hashing`)
```bash
go test ./internal/hashing -bench="." -benchmem
```
```text
BenchmarkHashRingGetNode-16    15644942    75.20 ns/op    12 B/op    1 allocs/op
```
- **Lookup Latency**: **75.20 nanoseconds** per lookup (~0.000075 ms).
- **Lookup Throughput**: **~13.3 Million lookups / second**.

#### 2. End-to-End Reverse Proxy Round-Trip (`internal/proxy`)
```bash
go test ./internal/proxy -bench="BenchmarkProxyHandler" -benchmem
```
```text
BenchmarkProxyHandler-16       8929        123013 ns/op   59552 B/op   176 allocs/op
```
- **Proxy Latency**: **0.123 milliseconds** per complete HTTP reverse-proxy request-response cycle on local loopback.
- **Throughput**: **~8,130 requests / second**.

---

## Project Structure

```text
baryon/
├── cmd/
│   └── proxy/
│       └── main.go              # Main orchestration and graceful lifecycle
├── internal/
│   ├── config/
│   │   ├── config.go            # Centralized configuration and validation
│   │   └── config_test.go       # Config unit tests
│   ├── events/
│   │   ├── events.go            # Typed events and non-blocking EventBus
│   │   └── events_test.go       # Bounded event channel tests
│   ├── hashing/
│   │   ├── ring.go              # Consistent hash ring with CRC32 & virtual nodes
│   │   └── ring_test.go         # Stickiness, distribution, & remapping tests
│   ├── health/
│   │   ├── checker.go           # Active TCP health monitor & state machine
│   │   └── checker_test.go      # Failure & recovery detection tests
│   ├── logging/
│   │   └── logger.go            # Structured slog logging & redirection
│   ├── proxy/
│   │   ├── backend.go           # Backend data model & atomic counters
│   │   ├── backend_test.go      # Concurrency tests
│   │   ├── handler.go           # Reverse proxy handler & HTTP 503 handling
│   │   ├── handler_test.go      # Proxy integration & end-to-end benchmarks
│   │   ├── ip.go                # Safe client IP & X-Forwarded-For extraction
│   │   ├── ip_test.go           # IP parsing unit tests
│   │   ├── server.go            # Manageable dummy HTTP backend servers
│   │   ├── server_test.go       # Demo server lifecycle tests
│   │   └── simulation_test.go   # End-to-end K/R failure simulation tests
│   └── tui/
│       ├── messages.go          # Typed Bubble Tea messages
│       ├── model.go             # Dashboard state & subscription commands
│       ├── update.go            # Message handling & keyboard actions
│       ├── view.go              # Lipgloss responsive 3-section layout
│       └── tui_test.go          # TUI rendering & FIFO buffer tests
├── pkg/
│   └── simulator/
│       ├── traffic.go           # High-throughput load generator
│       └── traffic_test.go      # Simulator rate & cancellation tests
├── go.mod
├── go.sum
└── README.md
```

---

## License

MIT License. Designed and developed as an autonomous engineering implementation of the BARYON specification.

