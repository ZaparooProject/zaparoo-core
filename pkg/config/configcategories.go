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
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"golang.org/x/text/unicode/norm"
)

const maxSystemCategoryNameRunes = 100

var builtInSystemCategories = [...]string{
	"Arcade",
	"Computer",
	"Console",
	"Handheld",
	"Media",
	"Other",
	"Software",
}

// CategoryResolver is an immutable snapshot of the valid configured category
// names and ordinary-system memberships.
type CategoryResolver struct {
	bySystem   map[string][]string
	custom     []string
	customKeys []string
}

// newCategoryResolver builds a resolver from the valid declarations and
// returns one problem for each entry it ignored. Invalid entries never fail the
// config load, so a typo cannot stop Core from starting, and the declarations
// stay in the config as written so a save does not discard them.
func newCategoryResolver(categories []SystemsCategory) (CategoryResolver, []error) {
	resolver := CategoryResolver{bySystem: make(map[string][]string)}
	var problems []error
	for i := range categories {
		category := &categories[i]
		if err := validateSystemCategoryName(category.Name); err != nil {
			problems = append(problems, fmt.Errorf("systems category %d ignored: %w", i, err))
			continue
		}
		key := categoryKey(category.Name)
		if canonicalBuiltInCategory(key) != "" {
			problems = append(problems,
				fmt.Errorf("systems category %q ignored: name conflicts with a built-in category", category.Name))
			continue
		}
		if slices.ContainsFunc(resolver.customKeys, func(existing string) bool {
			return strings.EqualFold(existing, key)
		}) {
			problems = append(problems, fmt.Errorf("systems category %q ignored: duplicate name", category.Name))
			continue
		}
		resolver.custom = append(resolver.custom, category.Name)
		resolver.customKeys = append(resolver.customKeys, key)

		members := make([]string, 0, len(category.Systems))
		for _, configuredID := range category.Systems {
			system, err := systemdefs.LookupSystem(configuredID)
			if err != nil {
				problems = append(problems,
					fmt.Errorf("systems category %q: ignoring unknown system %q", category.Name, configuredID))
				continue
			}
			if slices.Contains(members, system.ID) {
				problems = append(problems,
					fmt.Errorf("systems category %q: ignoring duplicate system %q", category.Name, configuredID))
				continue
			}
			members = append(members, system.ID)
			resolver.bySystem[system.ID] = append(resolver.bySystem[system.ID], category.Name)
		}
	}
	return resolver, problems
}

func validateSystemCategoryName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if !utf8.ValidString(name) {
		return errors.New("name must be valid UTF-8")
	}
	if strings.TrimSpace(name) != name {
		return errors.New("name must not have surrounding whitespace")
	}
	if utf8.RuneCountInString(name) > maxSystemCategoryNameRunes {
		return fmt.Errorf("name must not exceed %d characters", maxSystemCategoryNameRunes)
	}
	if strings.ContainsAny(categoryKey(name), `/\`) {
		return errors.New("name must not contain path separators")
	}
	visible := false
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("name must not contain control characters")
		}
		// Zero-width joiners are part of emoji sequences and several scripts;
		// every other format character is invisible or reorders the text.
		if unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d' {
			return errors.New("name must not contain invisible formatting characters")
		}
		if unicode.In(r, unicode.L, unicode.N, unicode.P, unicode.S) {
			visible = true
		}
	}
	if !visible {
		return errors.New("name must contain a visible character")
	}
	return nil
}

// SystemCategoryResolver returns the resolver built by the last successful load.
func (c *Instance) SystemCategoryResolver() CategoryResolver {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loaded.categoryResolver
}

// Canonical resolves a built-in or configured category name case-insensitively.
func (r CategoryResolver) Canonical(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	key := categoryKey(name)
	if builtIn := canonicalBuiltInCategory(key); builtIn != "" {
		return builtIn, true
	}
	for i, customKey := range r.customKeys {
		if strings.EqualFold(customKey, key) {
			return r.custom[i], true
		}
	}
	return "", false
}

// ForSystem returns an ordinary system's full category membership: its primary
// category first, then configured memberships. systemID must be a canonical
// system ID.
func (r CategoryResolver) ForSystem(systemID, primary string) []string {
	custom := r.bySystem[systemID]
	categories := make([]string, 0, 1+len(custom))
	if primary != "" {
		categories = append(categories, primary)
	}
	// Custom names are unique and never match a built-in, so no deduplication.
	return append(categories, custom...)
}

// VirtualSystem resolves a virtual system's primary and additional categories
// against the current declarations, returning the primary category and the
// full membership with the primary first. An undeclared primary falls back to
// Other and undeclared additional memberships are dropped, so removing a
// category never removes the system or breaks tokens that launch it.
func (r CategoryResolver) VirtualSystem(primary string, additional []string) (resolved string, categories []string) {
	resolved, ok := r.Canonical(primary)
	if !ok {
		resolved = defaultVirtualSystemCategory
	}
	categories = make([]string, 0, 1+len(additional))
	categories = append(categories, resolved)
	for _, category := range additional {
		canonical, found := r.Canonical(category)
		if found && !slices.Contains(categories, canonical) {
			categories = append(categories, canonical)
		}
	}
	return resolved, categories
}

// undeclared returns the configured category references that do not resolve.
func (r CategoryResolver) undeclared(references ...string) []string {
	var missing []string
	for _, reference := range references {
		if _, ok := r.Canonical(reference); !ok && reference != "" {
			missing = append(missing, reference)
		}
	}
	return missing
}

// categoryKey is the comparison form of a category name. Compatibility
// normalization makes full-width and other lookalike spellings compare equal.
func categoryKey(name string) string {
	return norm.NFKC.String(name)
}

func canonicalBuiltInCategory(key string) string {
	for _, category := range builtInSystemCategories {
		if strings.EqualFold(category, key) {
			return category
		}
	}
	return ""
}
