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

// matchAllowedIPs reports whether ip matches any entry in the allowed list.
// Entries can be individual IPs or CIDR ranges. Reuses parseAllowedIPs.
func matchAllowedIPs(ip net.IP, allowed []string) bool {
	nets, addrs := parseAllowedIPs(allowed)
	for _, a := range addrs {
		if ip.Equal(a) {
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

// RunIPFilterMiddleware allows remote access to run endpoints when allow_run
// is configured (the handler validates content). Otherwise falls back to the
// standard AllowedIPs check.
func RunIPFilterMiddleware(ipsProvider IPsProvider, hasAllowRun func() bool) func(http.Handler) http.Handler {
	ipFilter := NonWSIPFilterMiddleware(ipsProvider)
	return func(next http.Handler) http.Handler {
		filtered := ipFilter(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsLoopbackAddr(r.RemoteAddr) || hasAllowRun() {
				next.ServeHTTP(w, r)
				return
			}
			filtered.ServeHTTP(w, r)
		})
	}
}

// NonWSIPFilterMiddleware denies non-loopback access unless AllowedIPs is configured.
func NonWSIPFilterMiddleware(ipsProvider IPsProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsLoopbackAddr(r.RemoteAddr) {
				next.ServeHTTP(w, r)
				return
			}
			allowed := ipsProvider()
			if len(allowed) == 0 {
				log.Debug().
					Str("addr", r.RemoteAddr).
					Str("path", r.URL.Path).
					Msg("non-WS remote access denied: AllowedIPs empty")
				http.Error(w, "remote access disabled for this transport", http.StatusForbidden)
				return
			}
			ip := ParseRemoteIP(r.RemoteAddr)
			if ip == nil {
				log.Warn().Str("addr", r.RemoteAddr).Msg("non-WS: failed to parse IP")
				http.Error(w, "invalid remote address", http.StatusForbidden)
				return
			}
			if !matchAllowedIPs(ip, allowed) {
				log.Debug().
					Str("ip", ip.String()).
					Str("path", r.URL.Path).
					Msg("non-WS remote access denied: not in AllowedIPs")
				http.Error(w, "remote access denied", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
