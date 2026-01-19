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
	"net"
	"net/http"

	"github.com/rs/zerolog/log"
)

// ParseRemoteIP extracts and parses the IP address from a RemoteAddr string (IP:port format).
func ParseRemoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	return net.ParseIP(host)
}

// IsLoopbackAddr checks if a RemoteAddr string represents a loopback address.
func IsLoopbackAddr(remoteAddr string) bool {
	ip := ParseRemoteIP(remoteAddr)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// IPsProvider is a function that returns the current list of allowed IPs/CIDRs.
// This allows the IP filter to dynamically fetch the allowlist on each request,
// supporting hot-reload of configuration.
type IPsProvider func() []string

// IPFilter manages IP allowlist filtering for both HTTP and WebSocket connections.
// It uses a provider function to fetch the allowlist dynamically.
type IPFilter struct {
	getAllowedIPs IPsProvider
}

// NewIPFilter creates a new IP filter with an IPs provider function.
// The provider is called on each request to get the current allowlist,
// allowing configuration changes to take effect without server restart.
func NewIPFilter(ipsProvider IPsProvider) *IPFilter {
	return &IPFilter{
		getAllowedIPs: ipsProvider,
	}
}

// parseAllowedIPs parses a list of IP strings into nets and individual IPs.
func parseAllowedIPs(allowedIPs []string) (nets []*net.IPNet, addrs []net.IP) {
	for _, ipStr := range allowedIPs {
		if host, _, err := net.SplitHostPort(ipStr); err == nil {
			ipStr = host
		}

		if _, network, err := net.ParseCIDR(ipStr); err == nil {
			nets = append(nets, network)
			continue
		}

		if ip := net.ParseIP(ipStr); ip != nil {
			addrs = append(addrs, ip)
			continue
		}

		log.Warn().Str("ip", ipStr).Msg("invalid IP or CIDR in allowed_ips, skipping")
	}
	return nets, addrs
}

// IsAllowed checks if an IP address is allowed.
// Returns true if the allowlist is empty (no filtering) or if the IP matches an allowed entry.
func (f *IPFilter) IsAllowed(remoteAddr string) bool {
	allowedIPs := f.getAllowedIPs()
	if len(allowedIPs) == 0 {
		return true
	}

	ip := ParseRemoteIP(remoteAddr)
	if ip == nil {
		log.Warn().Str("addr", remoteAddr).Msg("failed to parse IP address")
		return false
	}

	nets, addrs := parseAllowedIPs(allowedIPs)

	for _, allowedIP := range addrs {
		if ip.Equal(allowedIP) {
			return true
		}
	}

	for _, network := range nets {
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
				ip := ParseRemoteIP(r.RemoteAddr)
				log.Debug().
					Str("ip", ip.String()).
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
