package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

const (
	RequestsPerMinute = 100 // Simple limit - 100 requests per minute per IP
	BurstSize         = 20  // Allow burst of 20 requests
)

// IPRateLimiter manages rate limiters per IP address for both HTTP and WebSocket
type IPRateLimiter struct {
	limiters map[string]*rateLimiterEntry
	mu       sync.RWMutex
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewIPRateLimiter creates a new IP-based rate limiter with hardcoded limits
func NewIPRateLimiter() *IPRateLimiter {
	return &IPRateLimiter{
		limiters: make(map[string]*rateLimiterEntry),
	}
}

// GetLimiter returns the rate limiter for the given IP
func (rl *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, exists := rl.limiters[ip]
	if !exists {
		// Create new limiter with hardcoded constants
		limiter := rate.NewLimiter(rate.Limit(float64(RequestsPerMinute)/60.0), BurstSize)
		entry = &rateLimiterEntry{
			limiter:  limiter,
			lastSeen: time.Now(),
		}
		rl.limiters[ip] = entry
	} else {
		entry.lastSeen = time.Now()
	}

	return entry.limiter
}

// Cleanup removes old entries that haven't been seen recently
func (rl *IPRateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	maxAge := 10 * time.Minute // Hardcoded cleanup age
	now := time.Now()
	for ip, entry := range rl.limiters {
		if now.Sub(entry.lastSeen) > maxAge {
			delete(rl.limiters, ip)
			log.Debug().Str("ip", ip).Msg("removed stale rate limiter")
		}
	}
}

// StartCleanup starts a goroutine to periodically clean up old rate limiters
func (rl *IPRateLimiter) StartCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // Hardcoded cleanup interval
		defer ticker.Stop()

		for range ticker.C {
			rl.Cleanup()
		}
	}()
}

// HTTPRateLimitMiddleware creates an HTTP rate limiting middleware
func HTTPRateLimitMiddleware(limiter *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			rl := limiter.GetLimiter(host)

			if !rl.Allow() {
				log.Warn().
					Str("ip", host).
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("HTTP rate limit exceeded")

				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// WebSocketRateLimitHandler wraps a WebSocket message handler with rate limiting
func WebSocketRateLimitHandler(limiter *IPRateLimiter, handler func(*melody.Session, []byte)) func(*melody.Session, []byte) {
	return func(session *melody.Session, msg []byte) {
		host, _, err := net.SplitHostPort(session.Request.RemoteAddr)
		if err != nil {
			host = session.Request.RemoteAddr
		}
		rl := limiter.GetLimiter(host)

		if !rl.Allow() {
			log.Warn().
				Str("ip", host).
				Int("msg_size", len(msg)).
				Msg("WebSocket rate limit exceeded")

			errorMsg := `{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"Rate limit exceeded"}}`
			if err := session.Write([]byte(errorMsg)); err != nil {
				log.Error().Err(err).Msg("failed to send rate limit error")
			}
			return
		}

		handler(session, msg)
	}
}
