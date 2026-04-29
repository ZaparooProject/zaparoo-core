// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

const (
	RequestsPerMinute      = 100 // Simple limit - 100 requests per minute per IP
	BurstSize              = 20  // Allow burst of 20 requests
	WebSocketRateLimitWait = 2 * time.Second
)

// IPRateLimiter manages rate limiters per IP address for both HTTP and WebSocket
type IPRateLimiter struct {
	limiters map[string]*rateLimiterEntry
	mu       syncutil.RWMutex
	rate     rate.Limit
	burst    int
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewIPRateLimiter creates a new IP-based rate limiter with the default
// API limits (100 req/min per IP, burst 20).
func NewIPRateLimiter() *IPRateLimiter {
	return NewIPRateLimiterWithLimits(rate.Limit(float64(RequestsPerMinute)/60.0), BurstSize)
}

// NewIPRateLimiterWithLimits creates a new IP-based rate limiter with custom
// rate and burst settings. Used by the pairing endpoints which require a much
// more aggressive limit (1 req/sec per IP) to throttle online PIN guessing.
func NewIPRateLimiterWithLimits(r rate.Limit, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		limiters: make(map[string]*rateLimiterEntry),
		rate:     r,
		burst:    burst,
	}
}

// GetLimiter returns the rate limiter for the given IP
func (rl *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, exists := rl.limiters[ip]
	if !exists {
		limiter := rate.NewLimiter(rl.rate, rl.burst)
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

// StartCleanup starts a goroutine to periodically clean up old rate limiters.
// The cleanup goroutine will stop when the provided context is cancelled.
func (rl *IPRateLimiter) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // Hardcoded cleanup interval
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rl.Cleanup()
			case <-ctx.Done():
				return
			}
		}
	}()
}

// HTTPRateLimitMiddleware creates an HTTP rate limiting middleware
func HTTPRateLimitMiddleware(limiter *IPRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ParseRemoteIP(r.RemoteAddr)
			host := ip.String()
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

// WebSocketRateLimitHandler wraps a WebSocket message handler with rate
// limiting. When the per-IP rate limit is exceeded the connection is closed
// rather than receiving a structured JSON-RPC error: this avoids leaking
// plaintext frames onto encrypted sessions (which would not match the
// {"e":...} envelope and could not be decrypted by the client) and gives
// well-behaved clients an unambiguous "back off and reconnect" signal.
func WebSocketRateLimitHandler(
	limiter *IPRateLimiter,
	handler func(*melody.Session, []byte),
) func(*melody.Session, []byte) {
	return WebSocketRateLimitHandlerWithWait(limiter, WebSocketRateLimitWait, handler)
}

func WebSocketRateLimitHandlerWithWait(
	limiter *IPRateLimiter,
	waitTimeout time.Duration,
	handler func(*melody.Session, []byte),
) func(*melody.Session, []byte) {
	return func(session *melody.Session, msg []byte) {
		ip := ParseRemoteIP(session.Request.RemoteAddr)
		host := ip.String()
		rl := limiter.GetLimiter(host)

		ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
		defer cancel()
		waitTimeoutValue := waitTimeout
		if err := rl.Wait(ctx); err != nil {
			log.Warn().
				Err(err).
				Str("ip", host).
				Int("msg_size", len(msg)).
				Str("wait_timeout", waitTimeoutValue.String()).
				Msg("WebSocket rate limit wait failed, closing connection")

			if err := session.Close(); err != nil {
				log.Debug().Err(err).Msg("failed to close rate-limited session")
			}
			return
		}

		handler(session, msg)
	}
}
