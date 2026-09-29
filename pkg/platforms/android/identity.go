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

package android

import (
	"errors"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
)

// ErrAppIdentity is returned for a virtual path that is not a launchable
// Android app identity.
var ErrAppIdentity = errors.New("invalid android app identity")

// variantPattern bounds the profile-owned variant key. The key is opaque to
// this format: a profile decides what it means, so a new app never needs a new
// path format.
var variantPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// variantSeparator splits the package from its variant. Android package names
// are dot-joined [A-Za-z0-9_] segments, so ':' cannot occur in one and the
// split is unambiguous on the first occurrence.
const variantSeparator = ":"

// AppIdentity is an installed app, optionally one profile-defined variant of
// it. It is identity only: the activity, action and extras that actually start
// the app live in the catalog, so an app that renames an activity does not
// invalidate identities already written to a card.
type AppIdentity struct {
	Package string
	Variant string
	Name    string
}

// AppPath renders the identity as "android://<package>[:<variant>]/<Name>".
func (a AppIdentity) AppPath() string {
	return virtualpath.CreateVirtualPath(shared.SchemeAndroid, a.id(), a.Name)
}

func (a AppIdentity) id() string {
	if a.Variant == "" {
		return a.Package
	}
	return a.Package + variantSeparator + a.Variant
}

// ParseAppPath reads an "android://" virtual path back into an identity.
func ParseAppPath(path string) (AppIdentity, error) {
	result, err := virtualpath.ParseVirtualPathStr(path)
	if err != nil || result.Scheme != shared.SchemeAndroid {
		return AppIdentity{}, ErrAppIdentity
	}
	identity, err := parseAppID(result.ID)
	if err != nil {
		return AppIdentity{}, err
	}
	identity.Name = result.Name
	return identity, nil
}

// parseAppID splits "<package>[:<variant>]" on the first separator.
func parseAppID(id string) (AppIdentity, error) {
	packageName, variant, found := strings.Cut(id, variantSeparator)
	if !dottedNamePattern.MatchString(packageName) {
		return AppIdentity{}, ErrAppIdentity
	}
	if found && !variantPattern.MatchString(variant) {
		return AppIdentity{}, ErrAppIdentity
	}
	return AppIdentity{Package: packageName, Variant: variant}, nil
}
