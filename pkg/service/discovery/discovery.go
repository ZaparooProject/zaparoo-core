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

package discovery

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/discovery/mdns"
	"github.com/rs/zerolog/log"
)

// ServiceType is the DNS-SD service type for Zaparoo Core.
const ServiceType = "_zaparoo._tcp"

// watchInterval is how often the interface list is re-examined. It covers both
// a network that was not ready when Core started and one that changes later:
// an Ethernet cable plugged in after startup is advertised within a tick,
// without restarting Core.
const watchInterval = 15 * time.Second

// virtualInterfacePrefixes lists common prefixes for virtual/container network
// interfaces that should be excluded from mDNS registration. "veth" also
// covers Windows Hyper-V and WSL adapters, which are named
// "vEthernet (<switch>)".
var virtualInterfacePrefixes = []string{
	"docker", "br-", "veth", "virbr", "lxc", "lxd",
	"cni", "flannel", "cali", "tunl", "wg",
	"vmware", "virtualbox",
}

// defaultTXTRecords carries no data: device ID, version, and platform are
// exposed only via the authenticated API. Broadcasting them on the LAN would
// expose information useful for targeted attacks (version → known
// vulnerabilities, platform → attack surface).
//
// "No data" is one zero-length string, not an empty slice. A TXT record must
// hold at least one character-string (RFC 1035 §3.3.14, RFC 6763 §6.1), and an
// empty slice packs to a TXT record with rdlength 0, which strict resolvers
// reject — Apple's mDNSResponder drops the record outright, and some parsers
// fail the whole response, taking the address records with it. Any change to
// this slice must be pinned by TestDefaultTXTRecordsCarryNoData.
var defaultTXTRecords = []string{""}

// getPreferredInterfaces returns network interfaces suitable for mDNS
// registration. It filters out loopback, down, non-multicast, and virtual
// interfaces. Interfaces with no address worth advertising are dropped later,
// by the responder, which is where addresses are already being inspected.
func getPreferredInterfaces() ([]net.Interface, error) {
	allIfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}

	return filterInterfaces(allIfaces), nil
}

// filterInterfaces filters a list of network interfaces to only include those
// suitable for mDNS: up, non-loopback, multicast-capable, and non-virtual.
func filterInterfaces(ifaces []net.Interface) []net.Interface {
	var preferred []net.Interface
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// mDNS requires multicast
		if iface.Flags&net.FlagMulticast == 0 {
			continue
		}

		if isVirtualInterface(iface.Name) {
			continue
		}

		preferred = append(preferred, iface)
	}

	return preferred
}

// isVirtualInterface checks if an interface name matches known virtual interface prefixes.
func isVirtualInterface(name string) bool {
	lowerName := strings.ToLower(name)
	for _, prefix := range virtualInterfacePrefixes {
		if strings.HasPrefix(lowerName, prefix) {
			return true
		}
	}
	return false
}

// Service manages mDNS service advertising for network discovery.
// It allows mobile apps to discover Zaparoo Core instances without
// manual IP configuration.
type Service struct {
	responder    *mdns.Responder
	cfg          *config.Instance
	cancelFunc   context.CancelFunc
	instanceName string
	mu           syncutil.Mutex
	stopped      bool
}

// New creates a new discovery service.
func New(cfg *config.Instance) *Service {
	return &Service{
		cfg: cfg,
	}
}

// Start begins mDNS service advertising and keeps it in step with the
// machine's network interfaces. Registration failing right now is not an
// error: the watch loop retries for as long as the service runs, because a
// network that is not ready at boot usually becomes ready shortly after.
func (s *Service) Start() error {
	if !s.cfg.DiscoveryEnabled() {
		log.Info().Msg("mDNS discovery disabled by configuration")
		return nil
	}

	instanceName, err := s.resolveInstanceName()
	if err != nil {
		return fmt.Errorf("resolve instance name: %w", err)
	}
	s.instanceName = instanceName

	responder := mdns.New(&mdns.Service{
		Instance: instanceName,
		Type:     ServiceType,
		Host:     hostLabel(instanceName),
		Port:     uint16(s.cfg.APIPort()), //nolint:gosec // a TCP port cannot exceed uint16
		Text:     defaultTXTRecords,
	}, func(format string, args ...any) {
		log.Debug().Msgf("mDNS: "+format, args...)
	})

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.responder = responder
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFunc = cancel
	s.mu.Unlock()

	started := s.tryStart(responder, false)

	go s.watchInterfaces(ctx, responder, started)

	return nil
}

// tryStart attempts registration once, reporting whether it succeeded. quiet
// suppresses the failure log, so a machine that stays offline does not repeat
// the same line every tick for as long as it runs.
func (s *Service) tryStart(responder *mdns.Responder, quiet bool) bool {
	ifaces, err := getPreferredInterfaces()
	if err != nil {
		if !quiet {
			log.Debug().Err(err).Msg("failed to get network interfaces")
		}
		return false
	}
	if len(ifaces) == 0 {
		if !quiet {
			log.Debug().Msg("no suitable network interfaces found for mDNS, will keep watching")
		}
		return false
	}

	if err := responder.Start(ifaces); err != nil {
		if !quiet {
			log.Debug().Err(err).Msg("mDNS registration attempt failed, will keep watching")
		}
		return false
	}

	log.Info().
		Str("instance", s.instanceName).
		Int("port", s.cfg.APIPort()).
		Str("type", ServiceType).
		Strs("interfaces", responder.Interfaces()).
		Msg("mDNS service advertising started")

	return true
}

// watchInterfaces keeps the advertised interface set in step with the machine.
func (s *Service) watchInterfaces(ctx context.Context, responder *mdns.Responder, started bool) {
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()

	reportedFailure := !started

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if !started {
			started = s.tryStart(responder, reportedFailure)
			reportedFailure = !started
			continue
		}

		ifaces, err := getPreferredInterfaces()
		if err != nil {
			log.Debug().Err(err).Msg("failed to get network interfaces")
			continue
		}

		changed, err := responder.SetInterfaces(ifaces)
		if err != nil {
			log.Debug().Err(err).Msg("mDNS interface update failed")
			continue
		}
		if changed {
			log.Info().
				Strs("interfaces", responder.Interfaces()).
				Msg("mDNS interfaces changed, advertising updated")
		}
	}
}

// Stop gracefully shuts down mDNS advertising, sending goodbye packets.
func (s *Service) Stop() {
	s.mu.Lock()
	s.stopped = true
	cancel := s.cancelFunc
	responder := s.responder
	s.cancelFunc = nil
	s.responder = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if responder != nil {
		log.Debug().Msg("stopping mDNS service advertising")
		responder.Stop()
	}
}

// InstanceName returns the resolved mDNS instance name.
// Returns empty string if Start() has not been called.
func (s *Service) InstanceName() string {
	return s.instanceName
}

// resolveInstanceName determines the instance name to advertise.
// Priority: config value > hostname > fallback.
func (s *Service) resolveInstanceName() (string, error) {
	if name := s.cfg.DiscoveryInstanceName(); name != "" {
		return name, nil
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get hostname, using fallback")
		deviceID := s.cfg.DeviceID()
		if len(deviceID) >= 8 {
			return "zaparoo-" + deviceID[:8], nil
		}
		return "zaparoo", nil
	}

	return hostname, nil
}

// hostLabel picks the single DNS label the SRV record targets. The machine's
// own hostname is preferred so that "<host>.local" resolves the way the rest
// of the network already expects; a configured instance name is only a
// display name and may not match. A hostname that is already qualified is cut
// back to its first label, because the responder appends ".local".
func hostLabel(fallback string) string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		name = fallback
	}
	if idx := strings.Index(name, "."); idx > 0 {
		name = name[:idx]
	}
	if name == "" {
		return "zaparoo"
	}
	return name
}
