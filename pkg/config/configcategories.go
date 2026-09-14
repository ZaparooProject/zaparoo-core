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
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
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

// CategoryResolver is an immutable snapshot of configured category names and
// ordinary-system memberships.
type CategoryResolver struct {
	bySystem map[string][]string
	custom   []string
}

func validateSystemCategories(categories []SystemsCategory) error {
	seenNames := make([]string, 0, len(categories))
	for i := range categories {
		category := &categories[i]
		if err := validateSystemCategoryName(category.Name); err != nil {
			return fmt.Errorf("systems category %d: %w", i, err)
		}
		if canonicalBuiltInCategory(category.Name) != "" {
			return fmt.Errorf("systems category %d: name %q conflicts with a built-in category", i, category.Name)
		}
		if containsFold(seenNames, category.Name) {
			return fmt.Errorf("systems category %d: duplicate name %q", i, category.Name)
		}
		seenNames = append(seenNames, category.Name)

		seenSystems := make([]string, 0, len(category.Systems))
		for _, systemID := range category.Systems {
			system, err := systemdefs.LookupSystem(systemID)
			if err != nil {
				return fmt.Errorf("systems category %q: invalid system %q: %w", category.Name, systemID, err)
			}
			if containsFold(seenSystems, system.ID) {
				return fmt.Errorf("systems category %q: duplicate system %q", category.Name, systemID)
			}
			seenSystems = append(seenSystems, system.ID)
		}
	}
	return nil
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
	if strings.ContainsAny(name, `/\`) {
		return errors.New("name must not contain path separators")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("name must not contain control characters")
		}
	}
	return nil
}

func newCategoryResolver(categories []SystemsCategory) CategoryResolver {
	resolver := CategoryResolver{
		bySystem: make(map[string][]string),
		custom:   make([]string, 0, len(categories)),
	}
	for i := range categories {
		category := &categories[i]
		resolver.custom = append(resolver.custom, category.Name)
		for _, configuredID := range category.Systems {
			system, err := systemdefs.LookupSystem(configuredID)
			if err != nil {
				continue
			}
			resolver.bySystem[system.ID] = append(resolver.bySystem[system.ID], category.Name)
		}
	}
	return resolver
}

// SystemCategoryResolver returns a read-only snapshot for one API or validation operation.
func (c *Instance) SystemCategoryResolver() CategoryResolver {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return newCategoryResolver(c.vals.Systems.Category)
}

// Canonical resolves a built-in or configured category name case-insensitively.
func (r CategoryResolver) Canonical(name string) (string, bool) {
	if builtIn := canonicalBuiltInCategory(name); builtIn != "" {
		return builtIn, true
	}
	for _, custom := range r.custom {
		if strings.EqualFold(custom, name) {
			return custom, true
		}
	}
	return "", false
}

// ForSystem combines a system's primary category with configured custom memberships.
func (r CategoryResolver) ForSystem(systemID, primary string) []string {
	categories := make([]string, 0, 1+len(r.bySystem[systemID]))
	if primary != "" {
		categories = append(categories, primary)
	}

	canonicalID := systemID
	if system, err := systemdefs.LookupSystem(systemID); err == nil {
		canonicalID = system.ID
	}
	for _, category := range r.bySystem[canonicalID] {
		categories = appendCanonicalCategory(categories, r, category)
	}
	return categories
}

// Combine canonicalizes a primary category and ordered additional memberships.
func (r CategoryResolver) Combine(primary string, additional []string) []string {
	categories := make([]string, 0, 1+len(additional))
	if primary != "" {
		categories = append(categories, primary)
	}
	for _, category := range additional {
		categories = appendCanonicalCategory(categories, r, category)
	}
	return categories
}

func appendCanonicalCategory(categories []string, resolver CategoryResolver, category string) []string {
	if category == "" {
		return categories
	}
	canonical, ok := resolver.Canonical(category)
	if !ok {
		canonical = category
	}
	if !containsFold(categories, canonical) {
		categories = append(categories, canonical)
	}
	return categories
}

func canonicalBuiltInCategory(name string) string {
	for _, category := range builtInSystemCategories {
		if strings.EqualFold(category, name) {
			return category
		}
	}
	return ""
}

func containsFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}
