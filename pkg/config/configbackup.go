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

package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

const (
	DefaultBackupRemoteBaseURL  = "https://api.zaparoo.com"
	DefaultBackupRemoteSchedule = "daily"
)

// OfficialAuthHosts are the hosts of the official hosted API
// services. settings.auth.status only answers link probes for these hosts
// (over HTTPS) and the configured backup server; other URLs report
// linked=false without revealing whether a credential exists. The claim
// flow's trusted-domain extension may store credentials under further
// domains — those are found at unlink time by linked_via provenance tags,
// never by this list.
var OfficialAuthHosts = []string{
	"api.zaparoo.com",
	"edge.zaparoo.com",
	"zpr.au",
}

// Backup scopes select what a backup job collects.
const (
	// BackupScopePlatform includes platform files (settings, inputs,
	// saves, savestates) alongside Zaparoo's own data.
	BackupScopePlatform = "platform"
	// BackupScopeZaparoo restricts backups to Zaparoo's own data:
	// user.db, Core config, frontend/TUI config, launchers, mappings.
	BackupScopeZaparoo = "zaparoo"
)

type Backup struct {
	LocalDir string       `toml:"local_dir,omitempty"`
	Scope    string       `toml:"scope,omitempty"`
	Remote   BackupRemote `toml:"remote,omitempty"`
}

type BackupRemote struct {
	BaseURL  string `toml:"base_url,omitempty"`
	Schedule string `toml:"schedule,omitempty"`
	Enabled  bool   `toml:"enabled,omitempty"`
}

func (c *Instance) BackupLocalDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Backup.LocalDir
}

func (c *Instance) SetBackupLocalDir(localDir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Backup.LocalDir = localDir
}

// BackupScope returns the configured backup scope. Only an explicit
// "zaparoo" value narrows the scope; anything else (including unset)
// means the full platform scope.
func (c *Instance) BackupScope() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if strings.EqualFold(c.vals.Backup.Scope, BackupScopeZaparoo) {
		return BackupScopeZaparoo
	}
	return BackupScopePlatform
}

func (c *Instance) SetBackupScope(scope string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Backup.Scope = scope
}

func (c *Instance) BackupRemoteEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Backup.Remote.Enabled
}

func (c *Instance) SetBackupRemoteEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Backup.Remote.Enabled = enabled
}

func (c *Instance) BackupRemoteSchedule() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Backup.Remote.Schedule == "" {
		return DefaultBackupRemoteSchedule
	}
	return c.vals.Backup.Remote.Schedule
}

func (c *Instance) SetBackupRemoteSchedule(schedule string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Backup.Remote.Schedule = schedule
}

func (c *Instance) BackupRemoteBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Backup.Remote.BaseURL == "" {
		return DefaultBackupRemoteBaseURL
	}
	return c.vals.Backup.Remote.BaseURL
}

func (c *Instance) SetBackupRemoteBaseURL(rawURL string) error {
	if err := ValidateBackupRemoteBaseURL(rawURL); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Backup.Remote.BaseURL = normalizeBackupBaseURL(rawURL)
	return nil
}

func ValidateBackupRemoteBaseURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid backup remote base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("backup remote base URL must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("backup remote base URL must include only scheme, host, optional port, and optional path")
	}
	if parsed.Scheme == "https" {
		return nil
	}

	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return errors.New("http backup remote base URL must use localhost or a private IP literal")
	}
	if isAllowedHTTPBackupAddr(addr) {
		return nil
	}
	return errors.New("http backup remote base URL must use localhost or a private IP literal")
}

func normalizeBackupBaseURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String()
}

func isAllowedHTTPBackupAddr(addr netip.Addr) bool {
	if addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return true
	}
	if addr.Is4() {
		privateBlocks := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"}
		for _, block := range privateBlocks {
			prefix := netip.MustParsePrefix(block)
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	if addr.Is6() {
		for _, block := range []string{"fc00::/7", "fe80::/10"} {
			prefix := netip.MustParsePrefix(block)
			if prefix.Contains(addr) {
				return true
			}
		}
	}
	return false
}

func BackupAuthLookupURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := parsed.Host
	if host == "" {
		return rawURL
	}
	return parsed.Scheme + "://" + host
}
