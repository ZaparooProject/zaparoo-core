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

// Package permissions defines client roles and the capability lookup that
// gates privileged API methods.
//
// Three orthogonal properties describe a request's authority:
//
//   - Locality: a connection from the device itself (loopback). Local
//     means "physically at the device" — anyone with OS access owns the
//     whole system anyway, so local connections default to admin.
//   - Role: the identity of a paired client, chosen at pairing approval.
//   - Session role: a voluntary downgrade a client declares for its own
//     connection (e.g. a kiosk frontend restricting the UI it exposes).
//     Reserved — nothing sets it yet, but the check honors it so kiosk
//     support can land without touching handlers.
//
// Handlers never compare roles directly; they require a capability, and
// roles map to capability sets. Finer-grained roles later are new map
// entries, not handler changes.
//
// A remote request with no authenticated identity is legacy. Legacy access is
// admitted only on explicitly grandfathered appliance platforms and never
// inherits admin authority.
package permissions

import (
	"slices"

	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
)

// Role is a paired client's permission level.
type Role string

const (
	// RoleAdmin grants every capability.
	RoleAdmin Role = "admin"
	// RoleMember grants day-to-day use (browse, launch, switch profile
	// with PIN) but none of the capabilities that could weaken another
	// person's limits.
	RoleMember Role = "member"
	// RoleLegacy represents an unauthenticated compatibility client. It is
	// derived from request context and can never be selected during pairing.
	RoleLegacy Role = "legacy"
	// RoleRemote is assigned only by the device's own remote-operations
	// dispatcher, never by pairing (it is deliberately excluded from
	// ValidRole). It grants nothing today and never inherits whatever
	// RoleMember grows into later — the capability an allowlisted remote
	// operation runs with must always be visible in roleCapabilities, not
	// borrowed from a role that can change independently.
	RoleRemote Role = "remote"
	// RoleKiosk is what every caller except a paired admin client is held
	// to while the device's kiosk lock is enforcing. It is set per request
	// from the lock state, never stored and never chosen at pairing. It
	// holds no capability; which methods it may call is a separate,
	// default-deny list in the API server.
	RoleKiosk Role = "kiosk"
)

// ValidRole reports whether s is a recognized pairing role name. RoleLegacy
// and RoleRemote are intentionally excluded because neither is pairable.
func ValidRole(s string) bool {
	return s == string(RoleAdmin) || s == string(RoleMember)
}

// Capability names an operation whose availability varies by authority or
// legacy platform policy.
type Capability string

const (
	// CapProfilesManage covers creating, updating, and deleting device
	// profiles, and reading profile switch IDs (bearer credentials that
	// authorize PIN-free switching).
	CapProfilesManage Capability = "profiles.manage"
	// CapSettingsWrite covers device settings changes, which include
	// disabling playtime limits and the require-profile launch gate.
	CapSettingsWrite Capability = "settings.write"
	// CapScreenshot covers capturing and returning the device display.
	CapScreenshot Capability = "screenshot"
	// CapInput covers injecting keyboard and gamepad input.
	CapInput Capability = "input"
	// CapPlaytimeExtend covers granting extra playtime to the session
	// currently being limited. It is separate from CapSettingsWrite: a
	// grant weakens one session's limit without changing configuration,
	// and a client trusted to hand out extra time is not necessarily
	// trusted to rewrite the device's settings.
	CapPlaytimeExtend Capability = "playtime.extend"
	// CapUpdateApply covers replacing the running binary and restarting
	// the service. It is the one capability that is not about weakening
	// someone's limits: an update decides what code the device runs from
	// then on, and it stops whatever is playing to do it. Checking for an
	// update needs no capability. Legacy and member roles do not receive it.
	CapUpdateApply Capability = "update.apply"
	// CapDevicePower covers rebooting, shutting down and suspending the
	// device. It stops whatever is running for everyone using the device,
	// so legacy and member roles do not receive it.
	CapDevicePower Capability = "device.power"
	// CapDeviceBluetooth covers scanning for, pairing and forgetting
	// Bluetooth devices. A paired device can be a keyboard that types into
	// the machine, so legacy and member roles do not receive it.
	CapDeviceBluetooth Capability = "device.bluetooth"
)

// roleCapabilities maps authenticated and internal roles to capabilities.
//
//nolint:gochecknoglobals // immutable capability table
var roleCapabilities = map[Role]map[Capability]bool{
	RoleAdmin: {
		CapProfilesManage:  true,
		CapSettingsWrite:   true,
		CapScreenshot:      true,
		CapInput:           true,
		CapPlaytimeExtend:  true,
		CapUpdateApply:     true,
		CapDevicePower:     true,
		CapDeviceBluetooth: true,
	},
	RoleMember: {
		CapScreenshot: true,
		CapInput:      true,
	},
	RoleLegacy: {},
	RoleRemote: {},
	RoleKiosk:  {},
}

// legacyCapabilities freezes compatibility grants for released appliance
// platforms. Missing platforms fail closed.
//
//nolint:gochecknoglobals // immutable compatibility policy
var legacyCapabilities = map[string]map[Capability]bool{
	platformids.Mister: {
		CapProfilesManage: true,
		CapSettingsWrite:  true,
		CapScreenshot:     true,
		CapInput:          true,
	},
	platformids.Mistex: {
		CapProfilesManage: true,
		CapSettingsWrite:  true,
		CapInput:          true,
	},
	platformids.Batocera: {
		CapProfilesManage: true,
		CapSettingsWrite:  true,
		CapInput:          true,
	},
	platformids.LibreELEC: {
		CapProfilesManage: true,
		CapSettingsWrite:  true,
	},
	platformids.ReplayOS: {
		CapProfilesManage: true,
		CapSettingsWrite:  true,
		CapScreenshot:     true,
		CapInput:          true,
	},
}

// LegacyEnabled reports whether platformID admits unauthenticated legacy API
// clients at all.
func LegacyEnabled(platformID string) bool {
	_, ok := legacyCapabilities[platformID]
	return ok
}

// Access is the effective public authority of a request.
type Access string

const (
	AccessLocalhost Access = "localhost"
	AccessMember    Access = "member"
	AccessAdmin     Access = "admin"
	AccessLegacy    Access = "legacy"
	// AccessRemote is internal-only and is never returned for a user client.
	AccessRemote Access = "remote"
)

// Grant describes the authority of a single request.
type Grant struct {
	// Role is the paired client's stored role, RoleRemote for internal remote
	// operations, or empty when no role-bearing identity exists.
	Role Role
	// SessionRole caps the request below what its identity would grant.
	// Empty means no cap. RoleKiosk is applied while the kiosk lock is
	// enforcing; RoleMember is a downgrade a client may declare for itself.
	SessionRole Role
	// PlatformID selects the audited legacy compatibility policy.
	PlatformID string
	// IsLocal is true for loopback connections.
	IsLocal bool
	// APIKeyAuthenticated is true after successful static API-key validation.
	APIKeyAuthenticated bool
}

// EffectiveRole resolves the authority-bearing role. Localhost and API-key
// authentication are admin, paired unknown roles degrade to member, and an
// identityless remote request is legacy.
func (g Grant) EffectiveRole() Role {
	var role Role
	switch {
	case g.IsLocal || g.APIKeyAuthenticated:
		role = RoleAdmin
	case g.Role == "":
		role = RoleLegacy
	case g.Role == RoleAdmin:
		role = RoleAdmin
	case g.Role == RoleRemote:
		role = RoleRemote
	default:
		role = RoleMember
	}
	if g.SessionRole == RoleMember && role == RoleAdmin {
		role = RoleMember
	}
	// The kiosk cap reaches localhost and API keys too: on a locked device
	// the person at the controls is not assumed to be its owner. A legacy
	// request stays legacy, so the legacy gate still gets to refuse it.
	if g.SessionRole == RoleKiosk && role != RoleLegacy {
		role = RoleKiosk
	}
	return role
}

// Capped reports whether the kiosk lock is holding this request down.
func (g Grant) Capped() bool {
	return g.EffectiveRole() == RoleKiosk
}

// Access returns the request's effective public authority. AccessRemote is
// reserved for the internal Online dispatcher.
func (g Grant) Access() Access {
	switch g.EffectiveRole() {
	case RoleAdmin:
		if g.IsLocal {
			return AccessLocalhost
		}
		return AccessAdmin
	case RoleMember, RoleKiosk:
		// A capped caller reads as a member with no capabilities, a value
		// every released client already understands.
		return AccessMember
	case RoleRemote:
		return AccessRemote
	default:
		return AccessLegacy
	}
}

// Authenticated reports whether the request came from localhost, a valid API
// key, a paired client, or the internal remote dispatcher.
func (g Grant) Authenticated() bool {
	return g.IsLocal || g.APIKeyAuthenticated || g.Role != ""
}

// Has reports whether the request may perform the given capability.
func (g Grant) Has(capability Capability) bool {
	role := g.EffectiveRole()
	if role == RoleLegacy {
		return legacyCapabilities[g.PlatformID][capability]
	}
	return roleCapabilities[role][capability]
}

// Capabilities returns the effective grant's enabled capabilities in stable
// lexical order. The returned slice is always non-nil.
func (g Grant) Capabilities() []Capability {
	role := g.EffectiveRole()
	roleGrants := roleCapabilities[role]
	if role == RoleLegacy {
		roleGrants = legacyCapabilities[g.PlatformID]
	}
	capabilities := make([]Capability, 0, len(roleGrants))
	for capability, enabled := range roleGrants {
		if enabled && g.Has(capability) {
			capabilities = append(capabilities, capability)
		}
	}
	slices.Sort(capabilities)
	return capabilities
}
