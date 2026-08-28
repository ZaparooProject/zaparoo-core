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

package permissions

import (
	"testing"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrant_EffectiveRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		want  Role
		grant Grant
	}{
		{name: "local is admin", grant: Grant{IsLocal: true}, want: RoleAdmin},
		{name: "local ignores stored member role", grant: Grant{IsLocal: true, Role: RoleMember}, want: RoleAdmin},
		{name: "paired admin", grant: Grant{Role: RoleAdmin}, want: RoleAdmin},
		{name: "paired member", grant: Grant{Role: RoleMember}, want: RoleMember},
		{name: "unknown role degrades to member", grant: Grant{Role: "superuser"}, want: RoleMember},
		{name: "unpaired remote is legacy", grant: Grant{}, want: RoleLegacy},
		{name: "API key is admin", grant: Grant{APIKeyAuthenticated: true}, want: RoleAdmin},
		{
			name:  "session downgrade wins over local",
			grant: Grant{IsLocal: true, SessionRole: RoleMember},
			want:  RoleMember,
		},
		{
			name:  "session downgrade wins over admin",
			grant: Grant{Role: RoleAdmin, SessionRole: RoleMember},
			want:  RoleMember,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.grant.EffectiveRole())
		})
	}
}

func TestGrant_Has(t *testing.T) {
	t.Parallel()

	admin := Grant{Role: RoleAdmin}
	member := Grant{Role: RoleMember}

	assert.True(t, admin.Has(CapProfilesManage))
	assert.True(t, admin.Has(CapSettingsWrite))
	assert.True(t, admin.Has(CapUpdateApply))
	assert.False(t, member.Has(CapProfilesManage))
	assert.False(t, member.Has(CapSettingsWrite))
	assert.True(t, member.Has(CapScreenshot))
	assert.True(t, member.Has(CapInput))
	assert.False(t, member.Has(CapUpdateApply))
}

func TestGrant_AuthenticationAndAccess(t *testing.T) {
	t.Parallel()

	//nolint:govet // Test table field order favors readability.
	tests := []struct {
		name          string
		grant         Grant
		access        Access
		authenticated bool
	}{
		{name: "localhost", grant: Grant{IsLocal: true}, access: AccessLocalhost, authenticated: true},
		{name: "paired member", grant: Grant{Role: RoleMember}, access: AccessMember, authenticated: true},
		{name: "paired admin", grant: Grant{Role: RoleAdmin}, access: AccessAdmin, authenticated: true},
		{name: "API key admin", grant: Grant{APIKeyAuthenticated: true}, access: AccessAdmin, authenticated: true},
		{name: "legacy", grant: Grant{}, access: AccessLegacy, authenticated: false},
		{name: "internal remote", grant: Grant{Role: RoleRemote}, access: AccessRemote, authenticated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.access, tt.grant.Access())
			assert.Equal(t, tt.authenticated, tt.grant.Authenticated())
		})
	}

	assert.True(t, Grant{IsLocal: true}.Has(CapUpdateApply))
	assert.True(t, Grant{Role: RoleAdmin}.Has(CapUpdateApply))
	assert.True(t, Grant{APIKeyAuthenticated: true}.Has(CapUpdateApply))
	assert.False(t, Grant{}.Has(CapUpdateApply))
	assert.False(t, Grant{Role: RoleMember}.Has(CapUpdateApply))
	assert.False(t, Grant{IsLocal: true, SessionRole: RoleMember}.Has(CapUpdateApply))
}

func TestGrant_Capabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		grant Grant
		want  []Capability
	}{
		{
			name:  "paired admin is sorted",
			grant: Grant{Role: RoleAdmin},
			want:  []Capability{CapInput, CapProfilesManage, CapScreenshot, CapSettingsWrite, CapUpdateApply},
		},
		{
			name:  "paired member has day-to-day capabilities",
			grant: Grant{Role: RoleMember},
			want:  []Capability{CapInput, CapScreenshot},
		},
		{
			name:  "legacy risky platform is empty",
			grant: Grant{PlatformID: platformids.Linux},
			want:  []Capability{},
		},
		{
			name:  "legacy MiSTer preserves capabilities",
			grant: Grant{PlatformID: platformids.Mister},
			want:  []Capability{CapInput, CapProfilesManage, CapScreenshot, CapSettingsWrite},
		},
		{
			name:  "local member gets local capabilities",
			grant: Grant{Role: RoleMember, IsLocal: true},
			want:  []Capability{CapInput, CapProfilesManage, CapScreenshot, CapSettingsWrite, CapUpdateApply},
		},
		{
			name:  "unknown role degrades to member",
			grant: Grant{Role: "superuser"},
			want:  []Capability{CapInput, CapScreenshot},
		},
		{
			name:  "session downgrade uses member capabilities",
			grant: Grant{Role: RoleAdmin, SessionRole: RoleMember},
			want:  []Capability{CapInput, CapScreenshot},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.grant.Capabilities()
			assert.NotNil(t, got)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLegacyPlatformPolicy(t *testing.T) {
	t.Parallel()

	for _, platformID := range []string{
		platformids.Mister,
		platformids.Mistex,
		platformids.Batocera,
		platformids.LibreELEC,
		platformids.ReplayOS,
	} {
		assert.True(t, LegacyEnabled(platformID), platformID)
	}
	for _, platformID := range []string{
		platformids.Linux,
		platformids.SteamOS,
		platformids.Bazzite,
		platformids.ChimeraOS,
		platformids.Windows,
		platformids.Mac,
		platformids.ZapOS,
		platformids.Recalbox,
		platformids.RetroPie,
		"future-platform",
	} {
		assert.False(t, LegacyEnabled(platformID), platformID)
	}

	assert.False(t, Grant{PlatformID: platformids.LibreELEC}.Has(CapInput))
	assert.False(t, Grant{PlatformID: platformids.LibreELEC}.Has(CapScreenshot))
	assert.True(t, Grant{PlatformID: platformids.ReplayOS}.Has(CapScreenshot))
}

// TestLegacyZapScriptCapabilityMatrix pins the API-layer invariant that makes
// command-level API checks unnecessary: every released legacy platform with a
// functional sensitive ZapScript command retains that command's capability.
func TestLegacyZapScriptCapabilityMatrix(t *testing.T) {
	t.Parallel()

	for _, platformID := range []string{
		platformids.Mister,
		platformids.Mistex,
		platformids.Batocera,
		platformids.ReplayOS,
	} {
		assert.True(t, Grant{PlatformID: platformID}.Has(CapInput), platformID)
	}
	for _, platformID := range []string{
		platformids.Mister,
		platformids.ReplayOS,
	} {
		assert.True(t, Grant{PlatformID: platformID}.Has(CapScreenshot), platformID)
	}
}

func TestValidRole(t *testing.T) {
	t.Parallel()

	assert.True(t, ValidRole("admin"))
	assert.True(t, ValidRole("member"))
	assert.False(t, ValidRole(""))
	assert.False(t, ValidRole("root"))
	assert.False(t, ValidRole("legacy"))
	// RoleRemote is assigned only by the device's own remote-operations
	// dispatcher; a client must never be able to pair as it.
	assert.False(t, ValidRole("remote"))
}

// TestGrant_RoleRemoteNeverGainsCapabilities guards the property remote
// dispatch depends on: RoleRemote is a real, distinct effective role — not
// an alias for RoleMember — and it grants nothing today or after RoleMember
// gains capabilities in the future.
func TestGrant_RoleRemoteNeverGainsCapabilities(t *testing.T) {
	t.Parallel()

	grant := Grant{Role: RoleRemote}
	require.Equal(t, RoleRemote, grant.EffectiveRole())
	assert.NotEqual(t, RoleMember, grant.EffectiveRole())
	assert.False(t, grant.Has(CapProfilesManage))
	assert.False(t, grant.Has(CapSettingsWrite))
	assert.False(t, grant.Has(CapUpdateApply))
	assert.Empty(t, grant.Capabilities())

	// Local never applies to remote-operation requests, but if it somehow
	// did, locality still wins — remote dispatch must never set IsLocal.
	assert.Equal(t, RoleAdmin, Grant{Role: RoleRemote, IsLocal: true}.EffectiveRole())
}
