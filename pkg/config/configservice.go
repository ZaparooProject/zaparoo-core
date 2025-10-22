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
)

type Service struct {
	DeviceID       string   `toml:"device_id"`
	AllowRun       []string `toml:"allow_run,omitempty,multiline"`
	allowRunRe     []*regexp.Regexp
	AllowedOrigins []string   `toml:"allowed_origins,omitempty"`
	Publishers     Publishers `toml:"publishers,omitempty"`
	APIPort        int        `toml:"api_port"`
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

func (c *Instance) APIPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Service.APIPort
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
