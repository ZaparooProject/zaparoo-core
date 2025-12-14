//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package procscanner

import "strings"

// CommMatcher matches processes by their comm name (case-insensitive).
type CommMatcher struct {
	names map[string]bool
}

// NewCommMatcher creates a matcher that matches any of the given process names.
func NewCommMatcher(names []string) *CommMatcher {
	m := &CommMatcher{
		names: make(map[string]bool, len(names)),
	}
	for _, name := range names {
		m.names[strings.ToLower(name)] = true
	}
	return m
}

// Match returns true if the process comm matches any registered name.
func (m *CommMatcher) Match(proc ProcessInfo) bool {
	return m.names[strings.ToLower(proc.Comm)]
}

// ExactCommMatcher matches processes by exact comm name (case-sensitive).
type ExactCommMatcher struct {
	name string
}

// NewExactCommMatcher creates a matcher for an exact process name.
func NewExactCommMatcher(name string) *ExactCommMatcher {
	return &ExactCommMatcher{name: name}
}

// Match returns true if the process comm exactly matches.
func (m *ExactCommMatcher) Match(proc ProcessInfo) bool {
	return proc.Comm == m.name
}

// CmdlineContainsMatcher matches processes whose cmdline contains a substring.
type CmdlineContainsMatcher struct {
	substring string
}

// NewCmdlineContainsMatcher creates a matcher that checks if cmdline contains a substring.
func NewCmdlineContainsMatcher(substring string) *CmdlineContainsMatcher {
	return &CmdlineContainsMatcher{substring: substring}
}

// Match returns true if the process cmdline contains the substring.
func (m *CmdlineContainsMatcher) Match(proc ProcessInfo) bool {
	return strings.Contains(proc.Cmdline, m.substring)
}

// AndMatcher combines multiple matchers with AND logic.
type AndMatcher struct {
	matchers []Matcher
}

// NewAndMatcher creates a matcher that requires all sub-matchers to match.
func NewAndMatcher(matchers ...Matcher) *AndMatcher {
	return &AndMatcher{matchers: matchers}
}

// Match returns true if all sub-matchers match.
func (m *AndMatcher) Match(proc ProcessInfo) bool {
	for _, matcher := range m.matchers {
		if !matcher.Match(proc) {
			return false
		}
	}
	return true
}

// OrMatcher combines multiple matchers with OR logic.
type OrMatcher struct {
	matchers []Matcher
}

// NewOrMatcher creates a matcher that requires any sub-matcher to match.
func NewOrMatcher(matchers ...Matcher) *OrMatcher {
	return &OrMatcher{matchers: matchers}
}

// Match returns true if any sub-matcher matches.
func (m *OrMatcher) Match(proc ProcessInfo) bool {
	for _, matcher := range m.matchers {
		if matcher.Match(proc) {
			return true
		}
	}
	return false
}
