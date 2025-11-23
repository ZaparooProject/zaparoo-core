// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"net"
	"net/http"

	"github.com/rs/zerolog/log"
)

// IPFilter manages IP allowlist filtering for both HTTP and WebSocket connections
type IPFilter struct {
	allowedIPs   []string
	allowedNets  []*net.IPNet
	allowedAddrs []net.IP
}

// NewIPFilter creates a new IP filter from a list of allowed IPs/CIDRs.
// Empty list means all IPs are allowed (no filtering).
func NewIPFilter(allowedIPs []string) *IPFilter {
	filter := &IPFilter{
		allowedIPs:   allowedIPs,
		allowedNets:  make([]*net.IPNet, 0),
		allowedAddrs: make([]net.IP, 0),
	}

	// Parse and categorize allowed IPs
	for _, ipStr := range allowedIPs {
		// Clean input if user pasted an address with port (e.g., "192.168.1.1:7497")
		if host, _, err := net.SplitHostPort(ipStr); err == nil {
			ipStr = host
		}

		// Try parsing as CIDR first
		if _, network, err := net.ParseCIDR(ipStr); err == nil {
			filter.allowedNets = append(filter.allowedNets, network)
			continue
		}

		// Try parsing as individual IP
		if ip := net.ParseIP(ipStr); ip != nil {
			filter.allowedAddrs = append(filter.allowedAddrs, ip)
			continue
		}

		// Invalid IP/CIDR - log and skip
		log.Warn().Str("ip", ipStr).Msg("invalid IP or CIDR in allowed_ips, skipping")
	}

	return filter
}

// IsAllowed checks if an IP address is allowed.
// Returns true if the allowlist is empty (no filtering) or if the IP matches an allowed entry.
func (f *IPFilter) IsAllowed(remoteAddr string) bool {
	// Empty allowlist means allow all
	if len(f.allowedIPs) == 0 {
		return true
	}

	// Extract IP from "IP:port" format
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		log.Warn().Str("addr", remoteAddr).Msg("failed to parse IP address")
		return false
	}

	// Check against individual IPs
	for _, allowedIP := range f.allowedAddrs {
		if ip.Equal(allowedIP) {
			return true
		}
	}

	// Check against CIDR networks
	for _, network := range f.allowedNets {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// HTTPIPFilterMiddleware creates an HTTP middleware that filters requests by IP.
// This middleware applies to both regular HTTP requests and WebSocket upgrade requests.
func HTTPIPFilterMiddleware(filter *IPFilter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !filter.IsAllowed(r.RemoteAddr) {
				host, _, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					host = r.RemoteAddr
				}

				// Use Debug level to prevent log flooding from blocked IPs
				log.Debug().
					Str("ip", host).
					Str("path", r.URL.Path).
					Str("method", r.Method).
					Msg("request from blocked IP")

				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
