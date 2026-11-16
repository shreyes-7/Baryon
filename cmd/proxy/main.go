package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"baryon/internal/config"
	"baryon/internal/events"
	"baryon/internal/hashing"
	"baryon/internal/health"
	"baryon/internal/logging"
	"baryon/internal/proxy"
	"baryon/internal/tui"
	"baryon/pkg/simulator"
)

func main() {
	cfg := config.DefaultConfig()

	// Command-line flag parsing
	listenAddr := flag.String("listen", cfg.ProxyListenAddr, "Proxy listen address (default :8080)")
	targetRPS := flag.Int("rate", cfg.SimulatorTargetRPS, "Simulator target requests per second (default 500)")
	noTUI := flag.Bool("no-tui", false, "Disable interactive terminal dashboard (runs in console/headless mode)")
	noSim := flag.Bool("no-sim", false, "Disable traffic simulator on startup")
	logFile := flag.String("log-file", "", "Optional log file destination for background logs")
	boundedFactor := flag.Float64("bounded-factor", cfg.BoundedLoadFactor, "Bounded load factor (default 1.25, max load = factor * avg load)")
	maxActive := flag.Int("max-active", cfg.MaxActivePerBackend, "Max active requests per backend (0 for dynamic avg*factor)")
	rateLimit := flag.Int("rate-limit", cfg.RateLimitPerIP, "Max requests/sec per client IP (0 to disable rate limiting)")
	rateBurst := flag.Int("rate-burst", cfg.RateLimitBurst, "Token bucket burst capacity per client IP")
	flag.Parse()

	cfg.ProxyListenAddr = *listenAddr
	cfg.SimulatorTargetRPS = *targetRPS
	cfg.NoTUI = *noTUI
	cfg.NoSim = *noSim
	cfg.BoundedLoadFactor = *boundedFactor
	cfg.MaxActivePerBackend = *maxActive
	cfg.RateLimitPerIP = *rateLimit
	cfg.RateLimitBurst = *rateBurst

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "BARYON Configuration error: %v\n", err)
		os.Exit(1)
	}

	// Logging configuration
	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			defer f.Close()
			logging.SetOutput(f, slog.LevelInfo)
		}
	} else if !cfg.NoTUI {
		// When TUI is enabled and no log file specified, discard logs so they do not corrupt the TUI screen
		logging.SetOutput(io.Discard, slog.LevelInfo)
	}

	logger := logging.Logger()
	logger.Info("Starting BARYON Consistent Hashing Load Balancer",
		"listen_addr", cfg.ProxyListenAddr,
		"backends", len(cfg.Backends),
		"virtual_nodes", cfg.VirtualNodes,
	)

	// 1. Start Demo Upstream Backend Servers
	demoBackends := make([]struct {
		Name string
		Addr string
	}, 0, len(cfg.Backends))

	for _, b := range cfg.Backends {
		u, err := url.Parse(b.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse backend URL %s: %v\n", b.URL, err)
			os.Exit(1)
		}
		host := u.Host
		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(host, "80")
		}
		demoBackends = append(demoBackends, struct {
			Name string
			Addr string
		}{
			Name: b.Name,
			Addr: host,
		})
	}

	demoManager := proxy.NewDemoManager(demoBackends)
	if err := demoManager.StartAll(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start demo backend servers: %v\n", err)
		os.Exit(1)
	}
	defer demoManager.StopAll()

	// 2. Instantiate Backend Models and Reverse Proxies
	backends := make([]*proxy.Backend, 0, len(cfg.Backends))
	for _, b := range cfg.Backends {
		u, _ := url.Parse(b.URL)
		backend := proxy.NewBackend(b.Name, u)
		proxy.SetupBackendReverseProxy(backend, nil)
		backends = append(backends, backend)
	}

	// 3. Initialize Consistent Hash Ring
	ring := hashing.NewHashRing(cfg.VirtualNodes)
	for _, b := range backends {
		if err := ring.AddNode(b); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to register backend %s on hash ring: %v\n", b.Name, err)
			os.Exit(1)
		}
	}

	// 4. Initialize Event Bus
	eventBus := events.NewEventBus(cfg.TUIEventChannelCap)
	defer eventBus.Close()

	// 5. Initialize Reverse Proxy HTTP Server
	proxyHandler := proxy.NewProxyHandler(ring, eventBus, cfg.TrustedProxies)
	proxyHandler.SetBoundedLoad(cfg.BoundedLoadFactor, cfg.MaxActivePerBackend)
	if cfg.RateLimitPerIP > 0 {
		proxyHandler.SetRateLimiter(cfg.RateLimitPerIP, cfg.RateLimitBurst)
	}

	proxyServer := &http.Server{
		Addr:         cfg.ProxyListenAddr,
		Handler:      proxyHandler,
		ReadTimeout:  cfg.ServerReadTimeout,
		WriteTimeout: cfg.ServerWriteTimeout,
		IdleTimeout:  cfg.ServerIdleTimeout,
	}

	proxyLn, err := net.Listen("tcp", cfg.ProxyListenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to listen on proxy address %s: %v\n", cfg.ProxyListenAddr, err)
		os.Exit(1)
	}

	go func() {
		if err := proxyServer.Serve(proxyLn); err != nil && err != http.ErrServerClosed {
			logger.Error("Proxy server failed", "error", err)
		}
	}()

	// 6. Start Active Health Monitoring
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	healthChecker := health.NewHealthChecker(backends, ring, eventBus, health.CheckerConfig{
		Interval:    cfg.HealthCheckInterval,
		DialTimeout: cfg.HealthCheckDialTimeout,
	})
	healthChecker.Start(appCtx)
	defer healthChecker.Stop()

	// 7. Start Traffic Simulator (if enabled)
	var trafficSim *simulator.TrafficSimulator
	if !cfg.NoSim {
		targetURL := fmt.Sprintf("http://localhost%s", cfg.ProxyListenAddr)
		if strings.HasPrefix(cfg.ProxyListenAddr, "127.0.0.1") || strings.HasPrefix(cfg.ProxyListenAddr, "localhost") {
			targetURL = fmt.Sprintf("http://%s", cfg.ProxyListenAddr)
		}
		trafficSim = simulator.New(simulator.Config{
			TargetURL:  targetURL,
			TargetRPS:  cfg.SimulatorTargetRPS,
			ClientIPs:  simulator.DefaultClientIPs,
			Timeout:    cfg.ClientTimeout,
			MaxWorkers: 50,
		})
		trafficSim.Start(appCtx)
		defer trafficSim.Stop()
	}

	// 8. Launch UI or Headless Loop
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	if !cfg.NoTUI {
		model := tui.NewModel(backends, eventBus, cfg.TUIRefreshInterval)
		model.OnKillBackend = func(name string) error {
			return demoManager.StopBackend(name)
		}
		model.OnRestartBackend = func(name string) error {
			return demoManager.RestartBackend(name)
		}
		model.OnQuit = func() {
			cancelApp()
		}

		p := tea.NewProgram(model, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI dashboard: %v\n", err)
		}
		cancelApp()
	} else {
		fmt.Printf("BARYON running in headless mode on %s...\n", cfg.ProxyListenAddr)
		fmt.Println("Press Ctrl+C to terminate.")
		select {
		case <-sigChan:
			fmt.Println("\nShutdown signal received. Exiting...")
			cancelApp()
		case <-appCtx.Done():
		}
	}

	// 9. Coordinated Graceful Shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()

	if trafficSim != nil {
		trafficSim.Stop()
	}
	healthChecker.Stop()
	_ = proxyServer.Shutdown(shutdownCtx)
	_ = demoManager.StopAll()

	logger.Info("BARYON stopped cleanly")
}

