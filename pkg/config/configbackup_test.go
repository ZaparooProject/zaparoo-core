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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := NewConfig(t.TempDir(), BaseDefaults)
	require.NoError(t, err)

	assert.Empty(t, cfg.BackupLocalDir())
	assert.False(t, cfg.BackupRemoteEnabled())
	assert.Equal(t, DefaultBackupRemoteBaseURL, cfg.BackupRemoteBaseURL())
	assert.Equal(t, DefaultBackupRemoteSchedule, cfg.BackupRemoteSchedule())
	assert.Equal(t, BackupScopePlatform, cfg.BackupScope())
}

func TestBackupScope(t *testing.T) {
	t.Parallel()
	cfg, err := NewConfig(t.TempDir(), BaseDefaults)
	require.NoError(t, err)

	cfg.SetBackupScope("zaparoo")
	assert.Equal(t, BackupScopeZaparoo, cfg.BackupScope())

	cfg.SetBackupScope("ZAPAROO")
	assert.Equal(t, BackupScopeZaparoo, cfg.BackupScope())

	cfg.SetBackupScope("platform")
	assert.Equal(t, BackupScopePlatform, cfg.BackupScope())

	// Unknown values fall back to the full platform scope.
	cfg.SetBackupScope("everything")
	assert.Equal(t, BackupScopePlatform, cfg.BackupScope())
}

func TestSetBackupLocalDir(t *testing.T) {
	t.Parallel()
	cfg, err := NewConfig(t.TempDir(), BaseDefaults)
	require.NoError(t, err)

	cfg.SetBackupLocalDir("/media/usb/zaparoo-backups")
	assert.Equal(t, "/media/usb/zaparoo-backups", cfg.BackupLocalDir())
}

func TestValidateBackupRemoteBaseURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "https public", rawURL: "https://example.com"},
		{name: "https path", rawURL: "https://example.com/backups"},
		{name: "localhost http", rawURL: "http://localhost:8787"},
		{name: "loopback http", rawURL: "http://127.0.0.1:8787"},
		{name: "private http", rawURL: "http://192.168.1.5:8787"},
		{name: "ipv6 local http", rawURL: "http://[fc00::1]:8787"},
		{name: "public http", rawURL: "http://example.com", wantErr: true},
		{name: "public ip http", rawURL: "http://8.8.8.8", wantErr: true},
		{name: "query rejected", rawURL: "https://example.com?token=1", wantErr: true},
		{name: "userinfo rejected", rawURL: "https://user@example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateBackupRemoteBaseURL(tt.rawURL)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSetBackupRemoteBaseURLNormalizes(t *testing.T) {
	t.Parallel()
	cfg, err := NewConfig(t.TempDir(), BaseDefaults)
	require.NoError(t, err)

	require.NoError(t, cfg.SetBackupRemoteBaseURL("https://example.com/backups/"))
	assert.Equal(t, "https://example.com/backups", cfg.BackupRemoteBaseURL())
}
