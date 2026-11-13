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

## Getting Started

### Prerequisites

- **Go 1.22+** (tested on Go 1.27.3 windows/amd64).

### Running Baryon

Launch the complete self-contained application with a single command:

```bash
go run ./cmd/proxy
```

This will automatically:
1. Start three dummy upstream HTTP servers on ports `8081`, `8082`, and `8083`.
2. Initialize the consistent hash ring with 100 virtual nodes per backend.
3. Start the reverse proxy on port `8080`.
4. Launch the active health checker probing every 3 seconds.
5. Launch the Bubble Tea terminal dashboard.
6. Begin streaming 500 requests/second from the traffic simulator.

### Headless / CLI Mode

To run without the terminal dashboard (e.g. for CI pipelines or scripts):

```bash
go run ./cmd/proxy -no-tui
```

### Manual Request Verification

You can verify consistent hashing and sticky routing manually using `curl`:

```bash
# Request with Client IP 192.168.1.50 -> consistently routes to backend-2
curl -H "X-Forwarded-For: 192.168.1.50" http://localhost:8080/

# Output:
# {"backend":"backend-2","address":"localhost:8082","status":"ok","path":"/"}

# Request with Client IP 10.0.0.1 -> consistently routes to backend-3
curl -H "X-Forwarded-For: 10.0.0.1" http://localhost:8080/

# Output:
# {"backend":"backend-3","address":"localhost:8083","status":"ok","path":"/"}
```

---

## Command-Line Configuration Flags

| Flag | Default | Description |
|---|---|---|
| `-listen` | `:8080` | Proxy listen host and port |
| `-rate` | `500` | Simulator target requests per second |
| `-no-tui` | `false` | Run in headless mode without Bubble Tea TUI |
| `-no-sim` | `false` | Disable background traffic simulator |
| `-log-file` | `""` | Optional file destination for structured logs |

---

## Testing & Benchmarks

### Running the Test Suite

```bash
# Run all unit and integration tests
go test -v ./...

# Static analysis and vet
go vet ./...
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
├── README.md
├── TASKS.md                     # Roadmap and progress tracker
└── PROJECT_CONTEXT.md           # Engineering specification
```

---

## License

MIT License. Designed and developed as an autonomous engineering implementation of the BARYON specification.
