package config

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig() should be valid, got: %v", err)
	}

	if cfg.ProxyListenAddr != ":8080" {
		t.Errorf("expected proxy listen addr :8080, got %s", cfg.ProxyListenAddr)
	}
	if n := len(cfg.Backends); n != 3 {
		t.Errorf("expected 3 backends, got %d", n)
	}
	if cfg.VirtualNodes != 100 {
		t.Errorf("expected 100 virtual nodes, got %d", cfg.VirtualNodes)
	}
	if cfg.SimulatorTargetRPS != 500 {
		t.Errorf("expected 500 simulator target RPS, got %d", cfg.SimulatorTargetRPS)
	}
}

func TestConfigValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(cfg *Config)
		wantError string
	}{
		{
			name: "empty proxy listen addr",
			modify: func(c *Config) {
				c.ProxyListenAddr = ""
			},
			wantError: "proxy_listen_addr cannot be empty",
		},
		{
			name: "invalid proxy listen addr",
			modify: func(c *Config) {
				c.ProxyListenAddr = "invalid-port"
			},
			wantError: "invalid proxy_listen_addr",
		},
		{
			name: "empty backends",
			modify: func(c *Config) {
				c.Backends = nil
			},
			wantError: "backends list cannot be empty",
		},
		{
			name: "backend empty name",
			modify: func(c *Config) {
				c.Backends[0].Name = ""
			},
			wantError: "backend[0] name cannot be empty",
		},
		{
			name: "backend duplicate name",
			modify: func(c *Config) {
				c.Backends[1].Name = c.Backends[0].Name
			},
			wantError: "duplicate backend name",
		},
		{
			name: "backend empty URL",
			modify: func(c *Config) {
				c.Backends[0].URL = ""
			},
			wantError: "backend[0] URL cannot be empty",
		},
		{
			name: "backend invalid URL",
			modify: func(c *Config) {
				c.Backends[0].URL = "://bad-url"
			},
			wantError: "is invalid",
		},
		{
			name: "backend duplicate URL",
			modify: func(c *Config) {
				c.Backends[1].URL = c.Backends[0].URL
			},
			wantError: "duplicate backend URL",
		},
		{
			name: "zero virtual nodes",
			modify: func(c *Config) {
				c.VirtualNodes = 0
			},
			wantError: "virtual_nodes must be positive",
		},
		{
			name: "negative health check interval",
			modify: func(c *Config) {
				c.HealthCheckInterval = -1 * time.Second
			},
			wantError: "health_check_interval must be positive",
		},
		{
			name: "dial timeout greater than interval",
			modify: func(c *Config) {
				c.HealthCheckDialTimeout = 5 * time.Second
				c.HealthCheckInterval = 2 * time.Second
			},
			wantError: "cannot be greater than health_check_interval",
		},
		{
			name: "zero simulator RPS",
			modify: func(c *Config) {
				c.SimulatorTargetRPS = 0
			},
			wantError: "simulator_target_rps must be positive",
		},
		{
			name: "zero client count",
			modify: func(c *Config) {
				c.SimulatedClientCount = 0
			},
			wantError: "simulated_client_count must be positive",
		},
		{
			name: "zero event channel cap",
			modify: func(c *Config) {
				c.TUIEventChannelCap = 0
			},
			wantError: "tui_event_channel_cap must be positive",
		},
		{
			name: "zero TUI refresh interval",
			modify: func(c *Config) {
				c.TUIRefreshInterval = 0
			},
			wantError: "tui_refresh_interval must be positive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.modify(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantError)
			}
		})
	}
}
