package src

import (
	"net/http"
	"testing"
)

func TestGetClientIP_ProxyLogic(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		xForwardedFor  string
        xRealIP        string
		expectedIP     string
	}{
		{
			name:           "Public IP - No Header",
			remoteAddr:     "8.8.8.8:1234",
			expectedIP:     "8.8.8.8",
		},
		{
			name:           "Public IP - Ignore Headers",
			remoteAddr:     "8.8.8.8:1234",
			xForwardedFor:  "1.2.3.4",
            xRealIP:        "5.6.7.8",
			expectedIP:     "8.8.8.8", // Should NOT trust header from public source
		},
		{
			name:           "Private IP - Trust X-Real-IP",
			remoteAddr:     "127.0.0.1:1234",
            xRealIP:        "5.6.7.8",
			xForwardedFor:  "1.2.3.4",
			expectedIP:     "5.6.7.8", // X-Real-IP takes precedence
		},
		{
			name:           "Private IP - Trust XFF Last IP (Spoofing Prevention)",
			remoteAddr:     "192.168.1.5:1234",
			xForwardedFor:  "fake_ip, real_ip",
			expectedIP:     "real_ip", // Should take the last one (added by the trusted proxy)
		},
        {
			name:           "Private IP - XFF Single IP",
			remoteAddr:     "10.0.0.5:1234",
			xForwardedFor:  "5.6.7.8",
			expectedIP:     "5.6.7.8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
            if tt.xRealIP != "" {
                req.Header.Set("X-Real-IP", tt.xRealIP)
            }

			got := getClientIP(req)
			if got != tt.expectedIP {
				t.Errorf("getClientIP() = %v, want %v", got, tt.expectedIP)
			}
		})
	}
}
