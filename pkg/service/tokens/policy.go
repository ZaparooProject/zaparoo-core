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

package tokens

import (
	"sort"
	"strings"
)

// CommandPolicy bounds which ZapScript commands a token may run. The zero
// value is unrestricted, which is every token that entered the system through
// a channel with no allowlist of its own: a reader scan, the run API, a hook.
//
// A channel that does have one sets it, and from then on the bound travels
// with the token rather than being re-derived. That is what lets a bounded
// token resolve an indirection safely: a ZapLink body, and the items of a
// playlist that body opens, run inside the same bound the original command
// had, even though each one re-enters as a token of a different source.
type CommandPolicy struct {
	allowed map[string]bool
}

// NewCommandPolicy returns a policy permitting exactly names. Passing no names
// returns the unrestricted zero value rather than a policy that forbids
// everything, so a caller cannot accidentally bound a token to nothing.
func NewCommandPolicy(names ...string) CommandPolicy {
	if len(names) == 0 {
		return CommandPolicy{}
	}
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			continue
		}
		allowed[normalized] = true
	}
	if len(allowed) == 0 {
		return CommandPolicy{}
	}
	return CommandPolicy{allowed: allowed}
}

// Unrestricted reports whether the policy bounds nothing.
func (p CommandPolicy) Unrestricted() bool {
	return len(p.allowed) == 0
}

// Allows reports whether name may run under this policy. Command names are
// case-insensitive, matching how the parser normalizes them.
func (p CommandPolicy) Allows(name string) bool {
	if p.Unrestricted() {
		return true
	}
	return p.allowed[strings.ToLower(strings.TrimSpace(name))]
}

// Names returns the permitted command names in sorted order, for logs and
// error messages. It returns nil when the policy is unrestricted.
func (p CommandPolicy) Names() []string {
	if p.Unrestricted() {
		return nil
	}
	names := make([]string, 0, len(p.allowed))
	for name := range p.allowed {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
