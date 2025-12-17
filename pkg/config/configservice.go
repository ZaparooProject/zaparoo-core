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

package config

import (
	"regexp"
	"strconv"
)

const DefaultAPIPort = 7497

type Service struct {
	APIPort        *int      `toml:"api_port,omitempty"`
	Discovery      Discovery `toml:"discovery,omitempty"`
	DeviceID       string    `toml:"device_id"`
	APIListen      string    `toml:"api_listen,omitempty"`
	AllowRun       []string  `toml:"allow_run,omitempty,multiline"`
	allowRunRe     []*regexp.Regexp
	AllowedOrigins []string   `toml:"allowed_origins,omitempty"`
	AllowedIPs     []string   `toml:"allowed_ips,omitempty"`
	Publishers     Publishers `toml:"publishers,omitempty"`
}

type Publishers struct {
	MQTT []MQTTPublisher `toml:"mqtt,omitempty"`
}

type MQTTPublisher struct {
	Enabled *bool    `toml:"enabled,omitempty"`
	Broker  string   `toml:"broker"`
	Topic   string   `toml:"topic"`
	Filter  []string `toml:"filter,omitempty,multiline"`
}

type Discovery struct {
	Enabled      *bool  `toml:"enabled,omitempty"`
	InstanceName string `toml:"instance_name,omitempty"`
}

func (c *Instance) APIPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiPortLocked()
}

// apiPortLocked returns the API port. Caller must hold mu (read or write).
func (c *Instance) apiPortLocked() int {
	if c.vals.Service.APIPort == nil {
		return DefaultAPIPort
	}
	return *c.vals.Service.APIPort
}

func (c *Instance) SetAPIPort(port int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Service.APIPort = &port
}

func (c *Instance) AllowedOrigins() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Service.AllowedOrigins
}

func (c *Instance) IsRunAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.Service.AllowRun, c.vals.Service.allowRunRe, s)
}

func (c *Instance) GetMQTTPublishers() []MQTTPublisher {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Service.Publishers.MQTT
}

func (c *Instance) APIListen() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Service.APIListen == "" {
		return ":" + strconv.Itoa(c.apiPortLocked())
	}
	return c.vals.Service.APIListen
}

func (c *Instance) AllowedIPs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Service.AllowedIPs
}

func (c *Instance) DeviceID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Service.DeviceID
}

func (c *Instance) DiscoveryEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Service.Discovery.Enabled == nil {
		return true
	}
	return *c.vals.Service.Discovery.Enabled
}

func (c *Instance) DiscoveryInstanceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Service.Discovery.InstanceName
}
