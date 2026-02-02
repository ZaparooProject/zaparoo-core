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
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/grandcat/zeroconf"
	"github.com/rs/zerolog/log"
)

// ServiceType is the DNS-SD service type for Zaparoo Core.
const ServiceType = "_zaparoo._tcp"

// retryInterval is how often to retry mDNS registration when network is unavailable.
const retryInterval = 30 * time.Second

// maxRetryDuration is the maximum time to keep retrying mDNS registration.
const maxRetryDuration = 5 * time.Minute

// Service manages mDNS service advertising for network discovery.
// It allows mobile apps to discover Zaparoo Core instances without
// manual IP configuration.
type Service struct {
	server       *zeroconf.Server
	cfg          *config.Instance
	cancelFunc   context.CancelFunc
	platformID   string
	instanceName string
	stopped      bool
	mu           syncutil.Mutex
}

// New creates a new discovery service.
func New(cfg *config.Instance, platformID string) *Service {
	return &Service{
		cfg:        cfg,
		platformID: platformID,
	}
}

// Start begins mDNS service advertising. If initial registration fails due to
// network unavailability, it starts a background retry loop. Returns an error
// only for permanent failures (e.g., disabled by config).
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

	// Try initial registration
	if s.tryRegister() {
		return nil
	}

	// Initial registration failed - start background retry
	log.Info().
		Dur("retryInterval", retryInterval).
		Dur("maxDuration", maxRetryDuration).
		Msg("mDNS registration failed, starting background retry (network may not be ready)")

	ctx, cancel := context.WithTimeout(context.Background(), maxRetryDuration)
	s.mu.Lock()
	s.cancelFunc = cancel
	s.mu.Unlock()

	go s.retryLoop(ctx)

	return nil
}

// tryRegister attempts to register the mDNS service. Returns true on success.
func (s *Service) tryRegister() bool {
	port := s.cfg.APIPort()

	txtRecords := []string{
		"id=" + s.cfg.DeviceID(),
		"version=" + config.AppVersion,
		"platform=" + s.platformID,
	}

	server, err := zeroconf.Register(
		s.instanceName,
		ServiceType,
		"local.",
		port,
		txtRecords,
		nil, // all network interfaces
	)
	if err != nil {
		log.Debug().Err(err).Msg("mDNS registration attempt failed")
		return false
	}

	s.mu.Lock()
	// Check if Stop() was called while we were registering. If so, shut down
	// the newly created server immediately to avoid a resource leak.
	if s.stopped {
		s.mu.Unlock()
		server.Shutdown()
		return false
	}
	s.server = server
	s.mu.Unlock()

	log.Info().
		Str("instance", s.instanceName).
		Int("port", port).
		Str("type", ServiceType).
		Msg("mDNS service advertising started")

	return true
}

// retryLoop periodically retries mDNS registration until successful or context expires.
func (s *Service) retryLoop(ctx context.Context) {
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if s.tryRegister() {
				log.Info().Msg("mDNS registration succeeded after retry")
				return
			}
		case <-ctx.Done():
			log.Warn().Msg("mDNS registration retry timed out, discovery will not be available")
			return
		}
	}
}

// Stop gracefully shuts down mDNS advertising, sending goodbye packets.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stopped = true

	// Cancel any running retry loop
	if s.cancelFunc != nil {
		s.cancelFunc()
		s.cancelFunc = nil
	}

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
			return "zaparoo-" + deviceID[:8], nil
		}
		return "zaparoo", nil
	}

	return hostname, nil
}
