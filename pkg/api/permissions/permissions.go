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
// A remote request with no paired identity (plaintext WebSocket while
// service.encryption is off) is treated as admin: it predates the
// permission system and restricting it would break unpaired clients.
// Setting service.encryption = true requires every remote client to be
// paired, which is what makes member restrictions enforceable.
//
// Capabilities listed in authenticatedCapabilities are the exception:
// they need a request from the device itself or from a paired client,
// whatever role the request resolves to.
package permissions

import "slices"

// Role is a paired client's permission level.
type Role string

const (
	// RoleAdmin grants every capability.
	RoleAdmin Role = "admin"
	// RoleMember grants day-to-day use (browse, launch, switch profile
	// with PIN) but none of the capabilities that could weaken another
	// person's limits.
	RoleMember Role = "member"
)

// ValidRole reports whether s is a recognized role name.
func ValidRole(s string) bool {
	return s == string(RoleAdmin) || s == string(RoleMember)
}

// Capability names a privileged operation a handler can require. The
// guiding rule for what needs a capability: anything that can weaken
// playtime limits is admin.
type Capability string

const (
	// CapProfilesManage covers creating, updating, and deleting device
	// profiles, and reading profile switch IDs (bearer credentials that
	// authorize PIN-free switching).
	CapProfilesManage Capability = "profiles.manage"
	// CapSettingsWrite covers device settings changes, which include
	// disabling playtime limits and the require-profile launch gate.
	CapSettingsWrite Capability = "settings.write"
	// CapUpdateApply covers replacing the running binary and restarting
	// the service. It is the one capability that is not about weakening
	// someone's limits: an update decides what code the device runs from
	// then on, and it stops whatever is playing to do it. Checking for an
	// update needs no capability. It is also in
	// authenticatedCapabilities, so an unpaired remote client cannot use
	// it even though such a request resolves to admin.
	CapUpdateApply Capability = "update.apply"
)

// authenticatedCapabilities lists the capabilities that need a request from
// the device itself or from a paired client. An unpaired remote request
// resolves to admin, and replacing the running binary is too much to hand to
// a client on the network that has not paired.
//
//nolint:gochecknoglobals // immutable capability table
var authenticatedCapabilities = map[Capability]bool{
	CapUpdateApply: true,
}

// roleCapabilities maps each role to its granted capabilities.
//
//nolint:gochecknoglobals // immutable capability table
var roleCapabilities = map[Role]map[Capability]bool{
	RoleAdmin: {
		CapProfilesManage: true,
		CapSettingsWrite:  true,
		CapUpdateApply:    true,
	},
	RoleMember: {},
}

// Grant describes the authority of a single request.
type Grant struct {
	// Role is the paired client's stored role, or "" when the request
	// carries no paired identity.
	Role Role
	// SessionRole is a voluntary downgrade declared by the client for
	// this session. Empty means no downgrade. Reserved for kiosk mode.
	SessionRole Role
	// IsLocal is true for loopback connections.
	IsLocal bool
}

// EffectiveRole resolves the request's role: local connections and
// unpaired remote requests are admin (see the package doc for why), a
// paired identity uses its stored role (unknown values degrade to
// member), and a voluntary session downgrade to member always wins.
func (g Grant) EffectiveRole() Role {
	role := RoleAdmin
	if !g.IsLocal && g.Role != "" {
		if g.Role == RoleAdmin {
			role = RoleAdmin
		} else {
			// Member, or an unrecognized role: least privilege.
			role = RoleMember
		}
	}
	if g.SessionRole == RoleMember {
		role = RoleMember
	}
	return role
}

// Authenticated reports whether the request came from the device itself or
// from a paired client. This is not the same question as EffectiveRole: an
// unpaired remote request resolves to admin but is not authenticated.
func (g Grant) Authenticated() bool {
	return g.IsLocal || g.Role != ""
}

// Has reports whether the request may perform the given capability.
func (g Grant) Has(capability Capability) bool {
	if authenticatedCapabilities[capability] && !g.Authenticated() {
		return false
	}
	return roleCapabilities[g.EffectiveRole()][capability]
}

// Capabilities returns the effective grant's enabled capabilities in stable
// lexical order. The returned slice is always non-nil.
func (g Grant) Capabilities() []Capability {
	roleGrants := roleCapabilities[g.EffectiveRole()]
	capabilities := make([]Capability, 0, len(roleGrants))
	for capability, enabled := range roleGrants {
		if enabled && g.Has(capability) {
			capabilities = append(capabilities, capability)
		}
	}
	slices.Sort(capabilities)
	return capabilities
}
