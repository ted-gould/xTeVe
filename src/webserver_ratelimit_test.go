package src

import (
	"testing"
	"time"
	"github.com/stretchr/testify/assert"
)

func TestCheckLoginRateLimit_SustainedSlowRequests(t *testing.T) {
	// Reset the rate limiter for the test
	loginRateLimiter.Lock()
	loginRateLimiter.attempts = make(map[string]int)
	loginRateLimiter.windowStart = make(map[string]time.Time)
	loginRateLimiter.Unlock()

	ip := "192.0.2.1"
	start := time.Now()

	// Make 15 requests, spaced 4 minutes apart.
	// Total duration: 15 * 4 = 60 minutes.
	// Total requests: 15.
	// Rate: 0.25 req/min = 1.25 req/5min.
	// Limit is 10 req/5min.
	// This traffic should be ALLOWED.

	for i := 0; i < 15; i++ {
		now := start.Add(time.Duration(i) * 4 * time.Minute)
		allowed := checkLoginRateLimitWithTime(ip, now)

		// With buggy logic, request 11 (index 10) will fail because attempts will be 11
		// and time.Since(last) = 4min < 5min, so it won't reset.
		if !allowed {
			t.Logf("Request %d at %v was denied", i+1, now.Sub(start))
		}
		assert.True(t, allowed, "Request %d should be allowed", i+1)
	}
}
