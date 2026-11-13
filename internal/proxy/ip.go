package proxy

import (
	"net"
	"net/http"
	"strings"
)

// ExtractClientIP extracts the originating client IP from the HTTP request.
// It respects trusted proxy configurations for X-Forwarded-For headers,
// falling back safely to RemoteAddr if forwarding headers are missing or untrusted.
func ExtractClientIP(r *http.Request, trustedProxies []string) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}
	remoteHost = strings.TrimSpace(remoteHost)

	// If no remote host could be determined, fallback to localhost
	if remoteHost == "" {
		remoteHost = "127.0.0.1"
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remoteHost
	}

	// If trustedProxies is configured, only trust XFF if remoteHost is trusted
	if len(trustedProxies) > 0 && !isTrusted(remoteHost, trustedProxies) {
		return remoteHost
	}

	// Parse comma-separated X-Forwarded-For chain
	parts := strings.Split(xff, ",")
	for i := 0; i < len(parts); i++ {
		candidate := strings.TrimSpace(parts[i])
		if host, _, err := net.SplitHostPort(candidate); err == nil {
			candidate = host
		}
		if ip := net.ParseIP(candidate); ip != nil {
			return ip.String()
		}
	}

	return remoteHost
}

func isTrusted(ipStr string, trustedList []string) bool {
	parsedIP := net.ParseIP(ipStr)
	for _, trusted := range trustedList {
		trusted = strings.TrimSpace(trusted)
		// Try CIDR match
		if _, ipNet, err := net.ParseCIDR(trusted); err == nil {
			if parsedIP != nil && ipNet.Contains(parsedIP) {
				return true
			}
			continue
		}
		// Direct IP or hostname match
		if trusted == ipStr {
			return true
		}
		if parsedTrusted := net.ParseIP(trusted); parsedTrusted != nil && parsedIP != nil {
			if parsedTrusted.Equal(parsedIP) {
				return true
			}
		}
	}
	return false
}

