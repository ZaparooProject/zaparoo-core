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

package discovery

import (
	"fmt"
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/grandcat/zeroconf"
	"github.com/rs/zerolog/log"
)

// ServiceType is the DNS-SD service type for Zaparoo Core.
const ServiceType = "_zaparoo._tcp"

// Service manages mDNS service advertising for network discovery.
// It allows mobile apps to discover Zaparoo Core instances without
// manual IP configuration.
type Service struct {
	server       *zeroconf.Server
	cfg          *config.Instance
	platformID   string
	instanceName string // resolved instance name after Start()
}

// New creates a new discovery service.
func New(cfg *config.Instance, platformID string) *Service {
	return &Service{
		cfg:        cfg,
		platformID: platformID,
	}
}

// Start begins mDNS service advertising. Returns an error if advertising
// fails to start, but callers should treat this as non-fatal.
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

	port := s.cfg.APIPort()

	txtRecords := []string{
		fmt.Sprintf("id=%s", s.cfg.DeviceID()),
		fmt.Sprintf("version=%s", config.AppVersion),
		fmt.Sprintf("platform=%s", s.platformID),
	}

	server, err := zeroconf.Register(
		instanceName,
		ServiceType,
		"local.",
		port,
		txtRecords,
		nil, // all network interfaces
	)
	if err != nil {
		return fmt.Errorf("start mDNS advertising: %w", err)
	}

	s.server = server
	log.Info().
		Str("instance", instanceName).
		Int("port", port).
		Str("type", ServiceType).
		Msg("mDNS service advertising started")

	return nil
}

// Stop gracefully shuts down mDNS advertising, sending goodbye packets.
func (s *Service) Stop() {
	if s.server != nil {
		log.Debug().Msg("stopping mDNS service advertising")
		s.server.Shutdown()
		s.server = nil
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
			return fmt.Sprintf("zaparoo-%s", deviceID[:8]), nil
		}
		return "zaparoo", nil
	}

	return hostname, nil
}
