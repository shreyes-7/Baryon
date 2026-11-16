package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"
)

// BackendConfig holds the identity and network configuration for a backend server.
type BackendConfig struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Config represents the complete runtime configuration for BARYON.
type Config struct {
	// ProxyListenAddr is the address and port BARYON listens on for incoming HTTP requests (e.g. ":8080").
	ProxyListenAddr string `json:"proxy_listen_addr"`

	// Backends is the list of configured target backends.
	Backends []BackendConfig `json:"backends"`

	// VirtualNodes is the number of virtual node replicas per backend on the consistent hash ring.
	VirtualNodes int `json:"virtual_nodes"`

	// HealthCheckInterval is the period between consecutive TCP health check probes.
	HealthCheckInterval time.Duration `json:"health_check_interval"`

	// HealthCheckDialTimeout is the maximum duration to wait for a TCP health probe connection.
	HealthCheckDialTimeout time.Duration `json:"health_check_dial_timeout"`

	// SimulatorTargetRPS is the target number of requests per second for the traffic simulator.
	SimulatorTargetRPS int `json:"simulator_target_rps"`

	// SimulatedClientCount is the number of distinct client IP addresses used by the simulator.
	SimulatedClientCount int `json:"simulated_client_count"`

	// TUIEventChannelCap is the buffer capacity for the bounded event channel.
	TUIEventChannelCap int `json:"tui_event_channel_cap"`

	// TUIRefreshInterval is the interval at which the TUI repaints and samples throughput.
	TUIRefreshInterval time.Duration `json:"tui_refresh_interval"`

	// ServerReadTimeout is the maximum duration for reading the entire request.
	ServerReadTimeout time.Duration `json:"server_read_timeout"`

	// ServerWriteTimeout is the maximum duration before timing out writes of the response.
	ServerWriteTimeout time.Duration `json:"server_write_timeout"`

	// ServerIdleTimeout is the maximum amount of time to wait for the next request when keep-alives are enabled.
	ServerIdleTimeout time.Duration `json:"server_idle_timeout"`

	// ClientTimeout is the timeout used by the traffic simulator HTTP client.
	ClientTimeout time.Duration `json:"client_timeout"`

	// TrustedProxies is a list of CIDRs or IP addresses trusted for parsing X-Forwarded-For headers.
	TrustedProxies []string `json:"trusted_proxies"`

	// NoTUI disables the interactive Bubble Tea terminal dashboard (useful for headless / script runs).
	NoTUI bool `json:"no_tui"`

	// NoSim disables the traffic simulator on startup (useful for manual curl testing).
	NoSim bool `json:"no_sim"`

	// BoundedLoadFactor specifies the load capacity multiplier for Consistent Hashing with Bounded Loads (default 1.25, i.e. 125% of average active requests).
	// Set to 0 to disable bounded loads.
	BoundedLoadFactor float64 `json:"bounded_load_factor"`

	// MaxActivePerBackend is a static ceiling on concurrent requests per backend (0 = auto based on bounded load factor).
	MaxActivePerBackend int `json:"max_active_per_backend"`

	// RateLimitPerIP is the maximum requests per second allowed from a single client IP (0 = disabled).
	RateLimitPerIP int `json:"rate_limit_per_ip"`

	// RateLimitBurst is the maximum burst requests allowed for a client IP.
	RateLimitBurst int `json:"rate_limit_burst"`
}

// DefaultConfig returns the default production configuration for BARYON.
func DefaultConfig() *Config {
	return &Config{
		ProxyListenAddr: ":8080",
		Backends: []BackendConfig{
			{Name: "backend-1", URL: "http://localhost:8081"},
			{Name: "backend-2", URL: "http://localhost:8082"},
			{Name: "backend-3", URL: "http://localhost:8083"},
		},
		VirtualNodes:           100,
		HealthCheckInterval:    3 * time.Second,
		HealthCheckDialTimeout: 2 * time.Second,
		SimulatorTargetRPS:     500,
		SimulatedClientCount:   10,
		TUIEventChannelCap:     1000,
		TUIRefreshInterval:     100 * time.Millisecond,
		ServerReadTimeout:      10 * time.Second,
		ServerWriteTimeout:     10 * time.Second,
		ServerIdleTimeout:      30 * time.Second,
		ClientTimeout:          5 * time.Second,
		TrustedProxies:         []string{"127.0.0.1", "::1"},
		NoTUI:                  false,
		NoSim:                  false,
		BoundedLoadFactor:      1.25,
		MaxActivePerBackend:    0,
		RateLimitPerIP:         2000,
		RateLimitBurst:         500,
	}
}

// Validate checks whether the configuration is structurally and logically valid.
func (c *Config) Validate() error {
	if c.ProxyListenAddr == "" {
		return errors.New("proxy_listen_addr cannot be empty")
	}
	if _, _, err := net.SplitHostPort(c.ProxyListenAddr); err != nil {
		// If only port is specified like ":8080", SplitHostPort succeeds,
		// but if it's invalid, it returns error.
		return fmt.Errorf("invalid proxy_listen_addr %q: %w", c.ProxyListenAddr, err)
	}

	if len(c.Backends) == 0 {
		return errors.New("backends list cannot be empty")
	}

	seenNames := make(map[string]bool)
	seenURLs := make(map[string]bool)
	for i, b := range c.Backends {
		if b.Name == "" {
			return fmt.Errorf("backend[%d] name cannot be empty", i)
		}
		if seenNames[b.Name] {
			return fmt.Errorf("duplicate backend name: %q", b.Name)
		}
		seenNames[b.Name] = true

		if b.URL == "" {
			return fmt.Errorf("backend[%d] URL cannot be empty", i)
		}
		parsedURL, err := url.Parse(b.URL)
		if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
			return fmt.Errorf("backend[%d] URL %q is invalid", i, b.URL)
		}
		if seenURLs[b.URL] {
			return fmt.Errorf("duplicate backend URL: %q", b.URL)
		}
		seenURLs[b.URL] = true
	}

	if c.VirtualNodes <= 0 {
		return fmt.Errorf("virtual_nodes must be positive, got %d", c.VirtualNodes)
	}

	if c.HealthCheckInterval <= 0 {
		return fmt.Errorf("health_check_interval must be positive, got %v", c.HealthCheckInterval)
	}

	if c.HealthCheckDialTimeout <= 0 {
		return fmt.Errorf("health_check_dial_timeout must be positive, got %v", c.HealthCheckDialTimeout)
	}

	if c.HealthCheckDialTimeout > c.HealthCheckInterval {
		return fmt.Errorf("health_check_dial_timeout (%v) cannot be greater than health_check_interval (%v)",
			c.HealthCheckDialTimeout, c.HealthCheckInterval)
	}

	if c.SimulatorTargetRPS <= 0 {
		return fmt.Errorf("simulator_target_rps must be positive, got %d", c.SimulatorTargetRPS)
	}

	if c.SimulatedClientCount <= 0 {
		return fmt.Errorf("simulated_client_count must be positive, got %d", c.SimulatedClientCount)
	}

	if c.TUIEventChannelCap <= 0 {
		return fmt.Errorf("tui_event_channel_cap must be positive, got %d", c.TUIEventChannelCap)
	}

	if c.TUIRefreshInterval <= 0 {
		return fmt.Errorf("tui_refresh_interval must be positive, got %v", c.TUIRefreshInterval)
	}

	if c.BoundedLoadFactor < 0 {
		return fmt.Errorf("bounded_load_factor cannot be negative, got %f", c.BoundedLoadFactor)
	}

	if c.MaxActivePerBackend < 0 {
		return fmt.Errorf("max_active_per_backend cannot be negative, got %d", c.MaxActivePerBackend)
	}

	if c.RateLimitPerIP < 0 {
		return fmt.Errorf("rate_limit_per_ip cannot be negative, got %d", c.RateLimitPerIP)
	}

	if c.RateLimitBurst < 0 {
		return fmt.Errorf("rate_limit_burst cannot be negative, got %d", c.RateLimitBurst)
	}

	return nil
}

