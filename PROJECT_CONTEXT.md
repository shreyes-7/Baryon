# BARYON --- Project Context & Engineering Specification

**Document purpose:** This file is the authoritative project brief for
Antigravity or another autonomous coding agent. It defines the product
goal, scope, architecture, implementation requirements, interfaces,
execution flow, testing strategy, acceptance criteria, and development
workflow for Baryon.

**Project name:** Baryon\
**Project type:** Go-based Layer 7 HTTP load balancer with consistent
hashing, active backend health monitoring, traffic simulation, and an
interactive terminal dashboard.\
**Primary language:** Go\
**Initial deployment target:** Local development machine; three
simulated backend servers.\
**Primary entry point:** `go run ./cmd/proxy`

------------------------------------------------------------------------

## 1. Product Overview

Baryon is a modular HTTP load balancer written in Go. It accepts HTTP
requests from clients and forwards them to one of several backend HTTP
servers. Backend selection is based on a consistent hashing ring, using
the client IP address as the routing key. This provides sticky routing:
while the ring membership remains unchanged, requests carrying the same
client IP should map to the same backend.

Baryon also monitors backend availability, removes unavailable backends
from the active hash ring, and adds recovered backends back into the
ring. It records request and connection metrics using concurrency-safe
counters and streams operational events to a Bubble Tea terminal user
interface (TUI).

For a compelling demonstration, Baryon starts three local dummy backend
servers and a traffic simulator automatically. The simulator
continuously sends requests through the load balancer. The TUI displays
backend status, traffic distribution, aggregate request statistics,
uptime, and recent routing decisions. A keyboard action can stop a
backend listener to demonstrate failure detection and redistribution.

### 1.1 Core value

Baryon should demonstrate the interaction of several real backend and
distributed-systems concepts in one working application:

-   Layer 7 HTTP reverse proxying.
-   Consistent hashing and virtual nodes.
-   Sticky client routing.
-   Active backend health checks.
-   Dynamic backend membership.
-   Fault detection and recovery.
-   Concurrent request handling in Go.
-   Atomic metrics and safe shared state.
-   Event-driven terminal visualization.
-   Automated testing and benchmarking.

### 1.2 Product boundaries

The first version is a local, self-contained simulation. It is not
intended to be represented as a production-ready, internet-facing load
balancer.

The initial implementation must not require cloud infrastructure, a
database, a browser frontend, Docker, or external services. These may be
added later as optional extensions, but they are not prerequisites for
the core deliverable.

The initial TUI is the user interface. Do not build a React, Next.js, or
other web frontend unless the project scope is explicitly changed.

------------------------------------------------------------------------

## 2. Goals and Non-Goals

### 2.1 Goals

1.  Receive HTTP requests on `localhost:8080`.
2.  Determine a client routing key from a trusted/appropriately parsed
    client IP source.
3.  Select a healthy backend using a consistent hash ring.
4.  Forward requests using Go's `net/http/httputil.ReverseProxy`.
5.  Keep request routing sticky for a client IP while ring membership is
    unchanged.
6.  Use virtual nodes to improve distribution across backends.
7.  Detect backend availability changes using periodic TCP checks.
8.  Remove unavailable backends from the active ring and restore
    recovered backends.
9.  Track total requests and active requests/connections per backend
    safely under concurrency.
10. Run a terminal dashboard that displays system status, traffic
    distribution, backend health, and recent routing events.
11. Generate demo traffic automatically at a configurable target rate.
12. Demonstrate a backend failure using a keyboard control.
13. Provide unit tests, integration tests, race detection, and
    benchmarks.
14. Keep modules small, cohesive, documented, and independently
    testable.

### 2.2 Non-goals for the first version

-   Multi-machine orchestration.
-   Kubernetes integration.
-   Distributed configuration or consensus.
-   Persistent metrics storage.
-   Authentication or user management.
-   TLS certificate management.
-   A public web dashboard.
-   Production-grade DDoS protection or rate limiting.
-   Guaranteed zero request loss during backend failure.
-   Sophisticated retry policies that could duplicate non-idempotent
    requests.
-   Automatic cloud deployment.

------------------------------------------------------------------------

## 3. Functional Requirements

### FR-01: HTTP ingress

-   The proxy listens on `:8080` by default.
-   It accepts ordinary HTTP requests.
-   It selects a backend based on the client IP routing key.
-   It forwards the request and returns the backend response.
-   If there are no healthy backends, it returns HTTP
    `503 Service Unavailable`.
-   Proxy errors should be logged and represented clearly; they must not
    crash the process.

### FR-02: Client IP extraction

-   Prefer a configurable trusted-proxy policy for `X-Forwarded-For`.
-   If the deployment is local and no trusted proxy is configured, use
    `RemoteAddr`.
-   Parse `RemoteAddr` safely with `net.SplitHostPort`.
-   When processing `X-Forwarded-For`, parse the comma-separated chain
    and select the correct client address according to the configured
    trust policy. Do not blindly trust arbitrary client-supplied
    forwarding headers in a production deployment.
-   If extraction fails, use a documented fallback (for example, a
    normalized remote address) and log the condition.
-   Tests must cover IPv4, IPv6, malformed addresses, and
    forwarding-header behavior.

For the local demo, the traffic simulator and integration tests should
be able to set `X-Forwarded-For` explicitly so that different simulated
clients can be routed predictably.

### FR-03: Consistent hashing

-   Implement the ring in `internal/hashing/ring.go`.
-   Use `hash/crc32.ChecksumIEEE` for the initial hash function.
-   Store sorted hash positions and a mapping from hash position to
    backend.
-   Use a default of 100 virtual nodes (replicas) per backend.
-   Use binary search (`sort.Search`) to find the first ring position
    greater than or equal to the key hash; wrap to index zero when no
    position is greater than or equal to the key hash.
-   Provide `AddNode`, `RemoveNode`, and `GetNode` operations.
-   Protect ring state with `sync.RWMutex`.
-   Define deterministic virtual-node key generation so tests are
    repeatable.
-   Handle duplicate hash positions deterministically. Do not silently
    overwrite a different backend mapping without a deliberate collision
    policy.
-   Avoid adding the same backend more than once.
-   Removing a backend must remove all of its virtual nodes.

### FR-04: Sticky routing

-   Use the normalized client IP as the hash key.
-   Repeated requests from the same IP should select the same backend as
    long as active ring membership and hashing configuration remain
    unchanged.
-   The behavior must be covered by unit and integration tests.
-   Sticky routing is a routing affinity mechanism, not a guarantee that
    a user's application session data is stored on that backend.

### FR-05: Backend model and metrics

Each backend should contain:

-   A unique identity/name.
-   Its parsed base URL.
-   Its health state.
-   Its reverse proxy instance or enough information to construct one
    safely.
-   An atomic total-request counter.
-   An atomic active-request counter.
-   Synchronization for health-state reads and writes.

Use `sync/atomic` for high-frequency counters. Do not protect traffic
counters with a mutex.

Recommended conceptual API:

``` go
type Backend struct {
    Name         string
    URL          *url.URL
    ReverseProxy *httputil.ReverseProxy
    // health state and atomic metrics
}

func (b *Backend) SetAlive(alive bool)
func (b *Backend) IsAlive() bool
func (b *Backend) IncRequests()
func (b *Backend) GetStats() (total uint64, active int64)
```

Choose a consistent atomic representation compatible with the Go version
in `go.mod` (for example, `atomic.Uint64` / `atomic.Int64` or
package-level atomic functions). Document whether `ActiveConns` means
active HTTP requests rather than literal TCP socket count.

### FR-06: Reverse proxy

-   Use `httputil.ReverseProxy`.
-   Ensure the selected backend's URL is used correctly when forwarding.
-   Preserve relevant request method, path, query, and headers.
-   Avoid mutating shared request objects in unsafe ways.
-   Use an appropriate `ErrorHandler` to report backend failures.
-   Increment the selected backend's total request counter.
-   Increment the active request counter before forwarding and decrement
    it with `defer`.
-   Release the hash-ring read lock before performing network I/O.
-   Emit a non-blocking routing event to the TUI so a slow dashboard
    cannot stall the proxy.

### FR-07: Health checking

-   Implement in `internal/health/checker.go`.
-   Run in a goroutine with a cancellable `context.Context`.
-   Default check interval: 3 seconds.
-   Default TCP dial timeout: 2 seconds.
-   Check all configured backends, including currently unavailable ones,
    so recovery can be detected.
-   Perform network dialing outside the hash-ring lock.
-   On a healthy-to-unhealthy transition, mark the backend unavailable
    and remove it from the ring.
-   On an unhealthy-to-healthy transition, mark it available and add it
    back to the ring.
-   Emit a node-down or node-up event only on a state transition, not on
    every repeated check.
-   Avoid concurrent duplicate additions/removals.
-   Stop the ticker and exit cleanly when the context is cancelled.

TCP reachability is a basic health signal. It does not prove that the
backend application is functioning correctly. HTTP health endpoints may
be added as a future enhancement.

### FR-08: Traffic simulator

-   Implement in `pkg/simulator/traffic.go`.
-   Generate requests against `http://localhost:8080` by default.
-   Default target rate: 500 requests per second, configurable.
-   Cycle through 10 stable, predefined client IP addresses.
-   Add the selected simulated IP in `X-Forwarded-For`.
-   Use a reusable HTTP client and sensible timeouts.
-   Close response bodies.
-   Record and report simulator errors without leaking goroutines or
    resources.
-   Support cancellation through context.
-   Avoid an uncontrolled tight loop. Use a ticker or rate-limiting
    mechanism.
-   Clarify whether the configured rate is a target request-start rate
    or a guaranteed completed request rate; it should be treated as a
    target, not a throughput guarantee.

### FR-09: Terminal dashboard

Use Bubble Tea for the TUI application model and Lipgloss for styling.

The TUI should show three logical panes/sections:

**Header / overview** - Application name: BARYON. - Proxy status. -
Uptime. - Total requests processed. - Global requests per second (define
the sampling window and calculation).

**Backend / traffic section** - Each backend's name and address. -
Alive/dead status. - Total requests. - Active requests. - Traffic share
as a percentage of total requests. - A bar whose length represents the
backend's share of total observed requests. - Clear visual distinction
between healthy and unhealthy backends.

**Recent routing log** - Keep the latest five routing decisions. - Each
entry should show timestamp, client IP, and selected backend. - Include
node-up/node-down events where appropriate, either in the same event log
or a clearly separated status log.

The TUI must remain responsive if event production exceeds its update
rate. Bound event channels and/or use non-blocking sends. Avoid
unbounded memory growth.

The original design proposes a 60 FPS repaint ticker. Use a reasonable
refresh cadence and avoid unnecessary CPU usage; the UI does not need to
redraw 60 times per second if the displayed information changes less
frequently.

### FR-10: Interactive failure simulation

-   Bind `k` to stop the listener for backend 1 (default
    `localhost:8081`) in the local demo.
-   The action should be coordinated through the demo backend lifecycle
    manager; do not attempt to kill an arbitrary operating-system
    process.
-   The health checker should detect the stopped listener on its next
    check and remove it from the ring.
-   The TUI should display the failure and updated traffic routing.
-   Provide a recovery action or documented mechanism to restart the
    same demo backend and verify that the health checker adds it back.
-   Bind `q` or Ctrl+C to exit cleanly.
-   Keyboard handling must not block proxy request processing.

The supplied baseline explicitly defines `k` as the failure action. A
restart key is a useful implementation addition and should be documented
clearly (for example, `r` to restart backend 1), or recovery may be
demonstrated through another clearly documented control.

### FR-11: Configuration

Create `internal/config/config.go`.

Configuration should centralize, validate, and document at least:

-   Proxy listen address (default `:8080`).
-   Backend names and URLs (default ports `8081`, `8082`, `8083`).
-   Virtual-node count (default `100`).
-   Health-check interval (default `3s`).
-   Health-check dial timeout (default `2s`).
-   Traffic simulator target rate (default `500` requests/second).
-   Simulated client count (default `10`).
-   TUI event channel capacity.
-   TUI refresh interval.
-   HTTP client and server timeouts where applicable.

Keep defaults suitable for a zero-configuration local demo. Validate
invalid ports, empty backend lists, non-positive intervals, and invalid
rates.

------------------------------------------------------------------------

## 4. System Architecture

### 4.1 High-level architecture

``` text
                   Simulated / Real Clients
                              |
                              | HTTP :8080
                              v
                 +--------------------------+
                 |       BARYON PROXY       |
                 |  Client IP extraction    |
                 |  Consistent hash lookup  |
                 |  Reverse proxy           |
                 +------------+-------------+
                              |
                  +-----------+-----------+
                  |           |           |
                  v           v           v
              Backend 1   Backend 2   Backend 3
              :8081       :8082       :8083
                  ^           ^           ^
                  |           |           |
                  +-----------+-----------+
                              |
                    TCP Health Checker
                       every 3 seconds

      Proxy + Health Checker events
                    |
                    v
             Bounded Event Channel
                    |
                    v
          +-------------------------+
          |    BUBBLE TEA TUI       |
          | Status / Metrics        |
          | Traffic Bars             |
          | Recent Routing Events    |
          | Keyboard Controls        |
          +-------------------------+

             Traffic Simulator
                    |
                    +---- HTTP requests to :8080
```

### 4.2 Concurrency model

The application will have independently managed concurrent activities:

1.  Main process and lifecycle orchestration.
2.  HTTP server serving incoming requests concurrently.
3.  Health checker goroutine.
4.  Traffic simulator goroutine(s).
5.  Bubble Tea event/update loop.
6.  Demo backend HTTP servers.

Use contexts for lifecycle cancellation. The main process should
coordinate startup and shutdown, handle errors, and wait for owned
goroutines/servers to exit.

### 4.3 Event flow

Use typed messages/events rather than passing arbitrary strings between
components.

Suggested events:

``` go
type RequestEvent struct {
    Time      time.Time
    ClientIP  string
    BackendID string
}

type NodeStatusEvent struct {
    Time      time.Time
    BackendID string
    Alive     bool
    Error     string
}
```

Bubble Tea messages can wrap these event structures. Keep the event
contract independent of presentation details.

The proxy should not wait for the TUI to consume an event. A
non-blocking send is acceptable for high-frequency routing events.
Health-state transitions are lower frequency and more important;
consider a separate channel or a reliable state-refresh mechanism so
that a dropped UI event cannot leave the dashboard permanently stale.

The TUI should derive the current backend state from authoritative
snapshots or update its model from events while periodically refreshing
metrics. Do not make the TUI the source of truth for proxy or health
state.

------------------------------------------------------------------------

## 5. Consistent Hash Ring: Detailed Design

### 5.1 Data structure

A suitable initial design:

``` go
type HashRing struct {
    mu       sync.RWMutex
    hashKeys []uint32
    hashMap  map[uint32]*proxy.Backend
    replicas int
}
```

The actual implementation may use a slightly different representation if
it improves correctness, but it must preserve the documented behavior.

### 5.2 Adding a backend

1.  Validate the backend and its identity.
2.  Acquire the ring write lock.
3.  Check whether the backend is already registered.
4.  Generate one deterministic key per virtual node, such as
    `backendID#replicaIndex`.
5.  Hash each virtual-node key with CRC32.
6.  Insert the position into the map and sorted key collection.
7.  Handle collisions deterministically.
8.  Release the lock.

### 5.3 Removing a backend

1.  Acquire the ring write lock.
2.  Find and remove every virtual node belonging to that backend.
3.  Remove corresponding hash-map entries.
4.  Keep the sorted key slice consistent.
5.  Remove backend membership from the ring's identity registry.
6.  Release the lock.

Do not identify a backend solely by URL string if a stable unique
backend ID is available.

### 5.4 Looking up a backend

1.  Hash the client routing key.
2.  Acquire the ring read lock.
3.  If the ring is empty, return `nil`.
4.  Use `sort.Search` to find the first position greater than or equal
    to the hash.
5.  Wrap to the first position when necessary.
6.  Read the backend mapping.
7.  Release the lock.
8.  Return the backend.

The lookup should do no network I/O and should hold the read lock only
for the minimal data-structure access.

### 5.5 Distribution and churn expectations

With three backends and 100 virtual nodes each, a reasonably balanced
distribution is expected over a sufficiently large set of random keys,
but exact equal distribution is not guaranteed. The test should use a
statistically sensible tolerance and should be repeatable where
possible.

When a backend is removed, consistent hashing should minimize remapping
compared with naive modulo-based hashing. Tests should verify stickiness
and that only keys assigned to the removed backend need to move, subject
to hash collisions and implementation details.

------------------------------------------------------------------------

## 6. Backend and Proxy Design

### 6.1 Backend lifecycle

Maintain two concepts clearly:

-   **Configured backends:** all known backends, including currently
    unavailable ones.
-   **Active ring members:** only backends currently considered healthy.

The health checker must iterate over configured backends, not only ring
members. Otherwise, a removed backend could never be discovered as
recovered.

### 6.2 Health-state concurrency

Backend health state must be safe to read and write concurrently. A
mutex or atomic boolean is acceptable. Do not read/write a plain boolean
from multiple goroutines without synchronization.

### 6.3 Request path pseudocode

``` text
Handle HTTP request:
    extract normalized client IP
    backend = hashRing.GetNode(clientIP)

    if backend is nil:
        return HTTP 503

    increment backend total requests
    increment backend active requests
    defer decrement active requests

    emit routing event without blocking

    forward request through backend reverse proxy
```

A backend can fail between lookup and forwarding. Handle reverse-proxy
errors gracefully. The initial version may return an appropriate gateway
error for that request. Do not claim that every request survives an
abrupt backend crash.

### 6.4 Request and response behavior

-   Preserve method, path, query parameters, and appropriate headers.
-   Ensure the reverse proxy sets the target backend correctly.
-   Do not forward hop-by-hop headers incorrectly; rely on Go's
    reverse-proxy behavior where possible.
-   Avoid exposing internal stack traces to clients.
-   Log failures with backend identity and useful context.
-   Use timeouts and shutdown behavior deliberately.

------------------------------------------------------------------------

## 7. Health Checker Design

### 7.1 Defaults

-   Interval: 3 seconds.
-   TCP timeout: 2 seconds.
-   Probe target: backend URL host and port.

### 7.2 State machine

``` text
                  TCP probe succeeds
          +--------------------------------+
          |                                v
    +-----------+                    +-----------+
    |  UNKNOWN  |                    |   ALIVE   |
    +-----------+                    +-----------+
          |                                |
          | probe fails                    | probe fails
          v                                v
    +-----------+ <------------------------+
    |   DEAD    |
    +-----------+
          |
          | TCP probe succeeds
          +-------------------------------> ALIVE
```

The implementation should initialize backend state intentionally. On
startup, either perform an initial health check before serving traffic
or define a safe startup policy that prevents known-unhealthy backends
from entering the ring.

### 7.3 Transition handling

-   Alive -\> Dead: remove from ring once; emit node-down event once.
-   Dead -\> Alive: add to ring once; emit node-up event once.
-   Alive -\> Alive: no membership change.
-   Dead -\> Dead: no membership change.

The ring membership and backend health state must not become
inconsistent. Define the order of state and membership changes carefully
and test transitions.

------------------------------------------------------------------------

## 8. TUI Design Specification

### 8.1 Visual identity

The terminal interface should feel like a polished developer operations
dashboard. Use a dark terminal-friendly appearance, restrained accent
colors, readable spacing, and clear status indicators.

Project branding should consistently use **BARYON**. Do not use the old
temporary name "GoProxy" in user-facing titles or README headings.

### 8.2 Layout

``` text
╔══════════════════════════════════════════════════════════════════════╗
║                    BARYON — LOAD BALANCER                           ║
╠══════════════════════════════════════════════════════════════════════╣
║ PROXY: RUNNING       UPTIME: 00:12:45       REQUESTS/SEC: 500         ║
║ TOTAL REQUESTS: 125430                                                ║
╠══════════════════════════════════════════════════════════════════════╣
║ BACKEND STATUS & TRAFFIC                                              ║
║                                                                      ║
║ backend-1 :8081  ONLINE   ███████████████       35%  total: 43800     ║
║ backend-2 :8082  ONLINE   ██████████████        33%  total: 41300     ║
║ backend-3 :8083  ONLINE   █████████████         32%  total: 40330     ║
╠══════════════════════════════════════════════════════════════════════╣
║ RECENT ROUTING EVENTS                                                 ║
║ 23:40:01  192.168.1.10  -> backend-1                                 ║
║ 23:40:02  192.168.1.20  -> backend-3                                 ║
║ 23:40:03  192.168.1.30  -> backend-2                                 ║
║ 23:40:04  192.168.1.10  -> backend-1                                 ║
║ 23:40:05  192.168.1.40  -> backend-3                                 ║
╠══════════════════════════════════════════════════════════════════════╣
║ [K] Kill backend-1   [R] Restart backend-1   [Q] Quit                 ║
╚══════════════════════════════════════════════════════════════════════╝
```

This is a conceptual layout. Adapt it to terminal width and height.
Handle small terminals gracefully, avoid panics, and keep the most
important status visible.

### 8.3 Metrics definitions

-   **Total requests:** sum of backend total-request counters, or a
    proxy-wide counter. Choose one authoritative definition and avoid
    double counting.
-   **Active requests:** currently in-flight requests for a backend.
-   **Traffic share:** backend total requests divided by total backend
    requests over the chosen measurement window. If using lifetime
    totals, label it as lifetime share. A recent-window share is more
    useful for live balancing visualization.
-   **Requests per second:** calculate using counter deltas over a known
    sampling interval. Do not simply display the configured simulator
    rate as if it were measured throughput.
-   **Uptime:** duration since application startup.

------------------------------------------------------------------------

## 9. Traffic Simulator Design

### 9.1 Default demo behavior

On normal local startup:

1.  Start the three dummy backend HTTP servers.
2.  Initialize backend objects and health state.
3.  Create the hash ring.
4.  Run initial health checks or otherwise establish known availability.
5.  Start the proxy listener.
6.  Start the health checker.
7.  Start the TUI.
8.  Start the traffic simulator after the proxy is ready.

The exact startup order may be adjusted to avoid race conditions, but
the simulator must not send requests before the proxy is accepting
traffic.

### 9.2 Dummy backend responses

Each demo backend should return a simple response that identifies
itself, for example:

``` json
{
  "backend": "backend-1",
  "address": "localhost:8081",
  "status": "ok"
}
```

A request routed to a backend should therefore make the selected backend
observable in tests and during manual verification.

### 9.3 Load behavior

The default target is 500 request starts per second, using 10 stable
simulated client IPs. The implementation must avoid unbounded goroutine
creation and must make the load configurable.

If the machine cannot sustain the target, report actual measured
throughput and simulator errors rather than pretending the target was
achieved.

------------------------------------------------------------------------

## 10. Keyboard Controls and Failure Demonstration

### 10.1 Kill action

Pressing `k` should request that the demo lifecycle manager stop backend
1's HTTP listener. It should not terminate the entire process or kill an
unrelated process by PID.

Expected sequence:

1.  TUI receives `k`.
2.  TUI sends a control request to the demo backend manager.
3.  Backend 1 listener is closed.
4.  Health checker detects failed TCP connection, within the next check
    interval plus probe time.
5.  Backend 1 is removed from the ring.
6.  TUI displays backend 1 as offline.
7.  Requests are routed among the remaining healthy backends.

### 10.2 Recovery action

Provide a recovery control, recommended `r`, that restarts backend 1's
listener on its configured address. The health checker should detect the
successful TCP connection and re-add it to the ring.

If restart is not implemented in the first increment, clearly document
the manual recovery mechanism and do not claim a keyboard recovery
feature is complete.

### 10.3 Exit

On `q`, Ctrl+C, or termination signal:

-   Stop generating new simulator traffic.
-   Cancel health checks.
-   Stop accepting new proxy requests.
-   Gracefully shut down the proxy server.
-   Stop demo backend servers.
-   Close channels only when ownership is clear and no sender can still
    use them.
-   Wait for owned goroutines to exit within a bounded timeout.

------------------------------------------------------------------------

## 11. Repository Structure

Use this exact baseline structure unless there is a clear engineering
reason to add a small supporting file. Keep the responsibilities
separated.

``` text
baryon/
├── cmd/
│   └── proxy/
│       └── main.go
│
├── internal/
│   ├── config/
│   │   └── config.go
│   │
│   ├── hashing/
│   │   ├── ring.go
│   │   └── ring_test.go
│   │
│   ├── proxy/
│   │   ├── backend.go
│   │   ├── handler.go
│   │   └── handler_test.go
│   │
│   ├── health/
│   │   ├── checker.go
│   │   └── checker_test.go
│   │
│   └── tui/
│       ├── model.go
│       ├── view.go
│       ├── update.go
│       └── messages.go
│
├── pkg/
│   └── simulator/
│       └── traffic.go
│
├── go.mod
├── go.sum
├── README.md
└── PROJECT_CONTEXT.md
```

Additional files are acceptable when they improve separation, such as a
demo backend manager, event definitions, or lifecycle coordinator. Do
not force unrelated logic into `main.go`.

### File responsibilities

  -----------------------------------------------------------------------
  File                                Responsibility
  ----------------------------------- -----------------------------------
  `cmd/proxy/main.go`                 Dependency wiring, startup,
                                      orchestration, shutdown

  `internal/config/config.go`         Defaults, parsing, validation

  `internal/hashing/ring.go`          Consistent hash ring

  `internal/hashing/ring_test.go`     Hash ring correctness and
                                      distribution

  `internal/proxy/backend.go`         Backend state and metrics

  `internal/proxy/handler.go`         HTTP handler and reverse proxy

  `internal/proxy/handler_test.go`    Proxy integration tests

  `internal/health/checker.go`        Active health checks and membership
                                      transitions

  `internal/health/checker_test.go`   Failure and recovery tests

  `internal/tui/model.go`             Bubble Tea model and state

  `internal/tui/view.go`              Dashboard rendering

  `internal/tui/update.go`            Messages, events, keyboard
                                      interactions

  `internal/tui/messages.go`          Typed TUI messages

  `pkg/simulator/traffic.go`          Configurable load generator
  -----------------------------------------------------------------------

------------------------------------------------------------------------

## 12. Dependencies

Use Go's standard library wherever practical.

### Standard library

-   `net/http` --- HTTP servers and clients.
-   `net/http/httputil` --- reverse proxy.
-   `net/url` --- backend URL parsing.
-   `net` --- TCP health checks.
-   `hash/crc32` --- consistent-hash function.
-   `sort` --- binary search and sorted ring positions.
-   `sync` --- mutexes and wait groups.
-   `sync/atomic` --- concurrent counters.
-   `context` --- lifecycle cancellation.
-   `time` --- tickers, intervals, timestamps.
-   `testing` and `net/http/httptest` --- automated tests and test
    servers.
-   `log` or `log/slog` --- structured/consistent logs.

### External dependencies

-   `github.com/charmbracelet/bubbletea` --- TUI framework.
-   `github.com/charmbracelet/lipgloss` --- terminal styling.

Use compatible, stable dependency versions. Record them in `go.mod` and
`go.sum`. Do not add a dependency for functionality already
well-supported by the standard library without a clear reason.

------------------------------------------------------------------------

## 13. Testing Strategy

Testing is a required part of the deliverable, not an optional final
polish.

### 13.1 Hash ring unit tests

**Distribution test** - Add three backends. - Use 100 virtual nodes per
backend. - Generate at least 10,000 distinct deterministic test keys
(random UUIDs may be seeded). - Count backend assignments. - Verify each
backend receives a reasonably balanced share. - The initial target is
approximately one-third per backend with a 5 percentage-point tolerance,
but adjust only if empirical deterministic results show the test is
statistically brittle. Explain any changed tolerance.

**Stickiness test** - Route the same client key repeatedly. - Assert it
always maps to the same backend while ring membership is unchanged.

**Removal test** - Record mappings for a large set of keys. - Remove one
backend. - Verify no lookup returns the removed backend. - Verify keys
previously assigned to unaffected backends remain there. - Verify keys
assigned to the removed backend are remapped to remaining nodes.

**Addition test** - Add a new backend. - Verify it becomes eligible for
lookup and that the ring remains valid.

**Empty-ring test** - Verify lookup returns `nil` when no nodes exist.

**Duplicate and collision tests** - Verify duplicate backend addition
does not corrupt the ring. - Test or explicitly handle hash-position
collisions.

### 13.2 Proxy integration tests

-   Start three `httptest.NewServer` backend servers.
-   Each backend returns a unique identifier.
-   Build the hash ring and proxy handler.
-   Start a test proxy server.
-   Send requests with explicit `X-Forwarded-For` values under the test
    trust policy.
-   Verify response backend identifiers match the ring's expected
    selection.
-   Verify request path, query, method, and relevant headers are
    forwarded.
-   Verify an empty ring returns 503.
-   Verify a backend failure produces a controlled error response.

### 13.3 Health checker tests

-   Use test listeners or controllable fake backends.
-   Verify healthy backends remain registered.
-   Stop a backend and verify it is marked dead and removed.
-   Restart it and verify it is marked alive and re-added.
-   Verify repeated failed checks do not cause duplicate removal events.
-   Verify cancellation stops the checker promptly.

### 13.4 Simulator tests

-   Verify generated requests target the configured proxy URL.
-   Verify client IPs cycle through the configured set.
-   Verify context cancellation stops generation.
-   Verify response bodies are closed and errors are handled.

### 13.5 TUI tests

Where practical, test: - Event handling. - Metric calculations. -
Traffic percentage calculations. - Recent log bounded length. - Keyboard
command behavior. - Layout behavior for narrow terminal sizes.

### 13.6 Race detection

Run:

``` bash
go test -race ./...
```

Resolve all data races. Do not suppress race reports merely to make the
command pass.

### 13.7 Benchmarks

Add a benchmark for ring lookup and a separate end-to-end HTTP proxy
benchmark.

Example ring lookup benchmark:

``` go
func BenchmarkHashRingGetNode(b *testing.B) {
    // Create ring and backends before the timer.
    // Prepare a stable set of routing keys.
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            // Call ring.GetNode with a prepared key.
        }
    })
}
```

Measure the hash-ring lookup independently from HTTP network latency.
The target is sub-millisecond lookup latency; report measured results
rather than claiming a result in advance.

------------------------------------------------------------------------

## 14. Acceptance Criteria

Baryon is considered ready for its initial demo when all of the
following are true:

### Build and startup

-   [ ] `go build ./...` succeeds.
-   [ ] `go test ./...` succeeds.
-   [ ] `go run ./cmd/proxy` starts the local demo without manual
    backend setup.
-   [ ] Three backend servers are started on the configured ports.
-   [ ] The proxy listens on port 8080.
-   [ ] The dashboard launches and displays the BARYON name.

### Routing

-   [ ] Requests are forwarded to a backend.
-   [ ] The backend identity can be observed in the response.
-   [ ] Same client IP maps consistently while ring membership is
    unchanged.
-   [ ] The empty ring returns HTTP 503.
-   [ ] Backend selection uses the consistent hash ring, not round
    robin.

### Health and recovery

-   [ ] Health checker probes every configured backend.
-   [ ] Unavailable backends are removed from the ring.
-   [ ] Recovered backends are re-added.
-   [ ] State-transition events are visible in the dashboard.
-   [ ] Health checks do not hold ring locks while performing network
    I/O.

### Metrics and dashboard

-   [ ] Total requests are counted correctly.
-   [ ] Active requests increase and decrease correctly.
-   [ ] Traffic shares are calculated from observed metrics.
-   [ ] Requests/sec is measured, not hardcoded to the simulator target.
-   [ ] Latest five routing events are visible.
-   [ ] Dashboard remains responsive under traffic.

### Failure simulation

-   [ ] Pressing `k` stops backend 1 in the demo.
-   [ ] Health checker detects the failure.
-   [ ] New requests no longer select backend 1 after removal.
-   [ ] Remaining backends continue receiving requests.
-   [ ] Recovery action or documented recovery procedure works.

### Engineering quality

-   [ ] `go test -race ./...` passes.
-   [ ] Benchmarks exist and report measured results.
-   [ ] Goroutines and servers shut down cleanly.
-   [ ] No unbounded event or log queues exist.
-   [ ] Configuration and behavior are documented.

------------------------------------------------------------------------

## 15. Development Milestones

Implement Baryon incrementally. Do not attempt to generate the entire
application in one unverified pass.

### Milestone 1 --- Project foundation

-   Initialize Go module.
-   Create the repository structure.
-   Add configuration defaults and validation.
-   Create backend model and dummy backend servers.
-   Verify three backends respond independently.

**Exit condition:** Three identifiable HTTP backend servers can be
started and stopped.

### Milestone 2 --- Consistent hashing

-   Implement ring data structure.
-   Implement virtual nodes.
-   Implement add/remove/lookup.
-   Handle duplicate registration and collisions.
-   Write unit tests.

**Exit condition:** Ring tests pass and repeated keys are sticky.

### Milestone 3 --- HTTP reverse proxy

-   Implement client IP extraction.
-   Implement routing handler.
-   Integrate `httputil.ReverseProxy`.
-   Add atomic metrics.
-   Add 503 behavior for an empty ring.
-   Write integration tests.

**Exit condition:** Requests sent to port 8080 reach the expected
backend.

### Milestone 4 --- Health monitoring

-   Implement cancellable periodic checker.
-   Add backend down/up transitions.
-   Synchronize ring membership.
-   Test failure and recovery.

**Exit condition:** Stopping and restarting a backend changes ring
membership correctly.

### Milestone 5 --- Event architecture

-   Define typed routing and node-status events.
-   Add bounded channels.
-   Ensure proxy event sends are non-blocking.
-   Define authoritative state snapshots.

**Exit condition:** Events can be consumed without blocking the request
path or leaking memory.

### Milestone 6 --- Terminal dashboard

-   Create Bubble Tea model.
-   Build header, backend traffic section, and recent-event section.
-   Add Lipgloss styles.
-   Add responsive layout handling.
-   Add keyboard handling.

**Exit condition:** Dashboard displays live state and remains usable.

### Milestone 7 --- Traffic simulator and demo controls

-   Add configurable target rate.
-   Add 10 stable client IPs.
-   Start traffic after proxy readiness.
-   Add `k` backend failure control.
-   Add `r` recovery control.
-   Verify visible traffic redistribution.

**Exit condition:** A complete demo can run from one command.

### Milestone 8 --- Hardening and documentation

-   Run all tests and race detector.
-   Add benchmarks.
-   Verify shutdown behavior.
-   Improve error handling and configuration.
-   Write README with architecture, features, setup, controls,
    screenshots/terminal captures, and test commands.

**Exit condition:** All acceptance criteria are checked and the
repository is ready to share.

------------------------------------------------------------------------

## 16. How to Run and Verify

The intended default command is:

``` bash
go run ./cmd/proxy
```

Expected behavior: 1. Configuration loads. 2. Three demo backends start.
3. Ring initializes with healthy backends. 4. Proxy starts on port 8080.
5. Health checker starts. 6. TUI starts. 7. Simulator begins sending
requests. 8. Dashboard starts showing traffic.

Manual request example:

``` bash
curl -H "X-Forwarded-For: 192.168.1.50" http://localhost:8080/
```

The response should identify the backend that handled the request.

Repeat the command with the same IP and verify the backend remains the
same while ring membership is unchanged.

Useful verification commands:

``` bash
go test ./...
go test -race ./...
go test ./internal/hashing -bench=. -benchmem
go test ./internal/proxy -bench=. -benchmem
go vet ./...
go build ./...
```

The simulator should be configurable or disableable so that manual tests
can be performed without background traffic distorting results.

------------------------------------------------------------------------

## 17. Future Enhancements (Not Required for Initial Completion)

Potential later improvements:

1.  Docker Compose deployment for independent backend containers.
2.  Config-file or environment-variable backend registration.
3.  HTTP readiness/liveness checks.
4.  Weighted consistent hashing.
5.  Multiple routing-key strategies (IP, cookie, header).
6.  Prometheus metrics endpoint.
7.  Grafana dashboards.
8.  TLS termination.
9.  Rate limiting and circuit breakers.
10. Configurable retry policy for safe/idempotent requests.
11. Backend latency and error-rate metrics.
12. Persistent historical metrics.
13. More than three demo backends.
14. A web-based dashboard, only if explicitly added to scope.

Do not let future enhancements delay the core project.

------------------------------------------------------------------------

## 18. Instructions to Antigravity / Autonomous Coding Agent

Treat this document as the authoritative specification for the Baryon
project.

### Before coding

1.  Inspect the current workspace and existing files.
2.  If a project already exists, preserve useful work and inspect before
    overwriting.
3.  Check the installed Go version.
4.  Confirm compatible Bubble Tea and Lipgloss versions.
5.  Summarize the planned implementation briefly and identify any
    genuine ambiguity.
6.  Do not ask questions for details already specified here.

### While coding

1.  Follow the milestone order.
2.  Build and test after each milestone.
3.  Implement real functionality; do not create placeholder methods that
    appear complete.
4.  Keep the project modular.
5.  Use idiomatic Go and clear naming.
6.  Add tests alongside implementation.
7.  Use contexts and explicit lifecycle ownership for goroutines and
    servers.
8.  Protect shared state correctly.
9.  Use atomic counters for high-frequency metrics.
10. Never hold the hash-ring lock during network I/O.
11. Never block the proxy request path on TUI rendering.
12. Keep event queues bounded.
13. Avoid hardcoded fake metrics or fabricated benchmark results.
14. Do not claim zero dropped requests during backend failure unless
    demonstrated by an explicitly implemented and tested mechanism.
15. Do not silently omit acceptance criteria.

### When finishing each milestone

Report: - Files created or modified. - Functionality implemented. -
Commands executed. - Test/build results. - Known limitations or
unresolved issues. - The next milestone.

### Completion requirements

Do not declare the project complete merely because it compiles. Run the
tests, race detector, vet, and build commands. Demonstrate the local
startup, sticky routing, backend failure detection, traffic
redistribution, and recovery. Clearly distinguish verified behavior from
unverified assumptions.

------------------------------------------------------------------------

## 19. Naming and Branding

The official project name is **Baryon**.

Use: - `Baryon` in prose and documentation. - `BARYON` in terminal
headings and prominent UI labels. - `baryon` as the repository/module
naming preference, subject to the actual Go module path selected by the
developer.

Avoid: - `GoProxy` as the product name. - Generic or inconsistent names
in the README and TUI. - Claiming the project is a cloud-distributed
production service when the initial version runs locally.

Suggested short description:

> Baryon is a Go-based Layer 7 HTTP load balancer that uses consistent
> hashing for sticky client routing, actively monitors backend health,
> dynamically redistributes traffic, and visualizes live cluster
> activity through an interactive terminal dashboard.

------------------------------------------------------------------------

## 20. Final Definition of Done

The first version of Baryon is complete when a developer can clone/open
the repository, run `go run ./cmd/proxy`, observe three backends
receiving simulated traffic, verify that the same client IP remains
sticky, press `k` to take one backend offline, watch health monitoring
remove it from the ring and traffic move to the remaining backends,
restart the backend, observe it rejoin, and run the complete automated
test suite successfully.

**Build the system for correctness first, observability second, and
visual polish third.**
