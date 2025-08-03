// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-only
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

type Groovy struct {
	GmcProxyBeaconInterval string `toml:"gmc_proxy_beacon_interval"`
	GmcProxyPort           int    `toml:"gmc_proxy_port"`
	GmcProxyEnabled        bool   `toml:"gmc_proxy_enabled"`
}

func (c *Instance) GmcProxyEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Groovy.GmcProxyEnabled
}

func (c *Instance) GmcProxyPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Groovy.GmcProxyPort
}

func (c *Instance) GmcProxyBeaconInterval() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Groovy.GmcProxyBeaconInterval
}
