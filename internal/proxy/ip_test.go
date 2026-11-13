package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		xff            string
		trustedProxies []string
		expectedIP     string
	}{
		{
			name:           "standard IPv4 RemoteAddr without headers",
			remoteAddr:     "192.168.1.100:54321",
			xff:            "",
			trustedProxies: []string{"127.0.0.1"},
			expectedIP:     "192.168.1.100",
		},
		{
			name:           "standard IPv6 RemoteAddr without headers",
			remoteAddr:     "[2001:db8::1]:12345",
			xff:            "",
			trustedProxies: []string{"127.0.0.1"},
			expectedIP:     "2001:db8::1",
		},
		{
			name:           "XFF single client with trusted proxy",
			remoteAddr:     "127.0.0.1:50000",
			xff:            "203.0.113.195",
			trustedProxies: []string{"127.0.0.1"},
			expectedIP:     "203.0.113.195",
		},
		{
			name:           "XFF multiple clients with whitespace",
			remoteAddr:     "127.0.0.1:50000",
			xff:            " 198.51.100.2 , 10.0.0.1, 127.0.0.1 ",
			trustedProxies: []string{"127.0.0.1"},
			expectedIP:     "198.51.100.2",
		},
		{
			name:           "XFF untrusted proxy ignores XFF",
			remoteAddr:     "192.168.1.50:50000",
			xff:            "203.0.113.195",
			trustedProxies: []string{"127.0.0.1"},
			expectedIP:     "192.168.1.50",
		},
		{
			name:           "XFF trusted CIDR block",
			remoteAddr:     "10.0.0.15:50000",
			xff:            "172.16.5.99",
			trustedProxies: []string{"10.0.0.0/8"},
			expectedIP:     "172.16.5.99",
		},
		{
			name:           "malformed RemoteAddr fallback",
			remoteAddr:     "",
			xff:            "",
			trustedProxies: nil,
			expectedIP:     "127.0.0.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/test", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			actualIP := ExtractClientIP(req, tc.trustedProxies)
			if actualIP != tc.expectedIP {
				t.Errorf("expected IP %s, got %s", tc.expectedIP, actualIP)
			}
		})
	}
}

