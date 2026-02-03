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

package database

// Composite key builder functions for consistent key generation across the codebase.
// These functions centralize key format to prevent inconsistencies and make format
// changes easier in the future.

// TitleKey builds a composite key for title deduplication: "systemID:slug"
func TitleKey(systemID, slug string) string {
	return systemID + ":" + slug
}

// MediaKey builds a composite key for media deduplication: "systemID:path"
func MediaKey(systemID, path string) string {
	return systemID + ":" + path
}

// TagKey builds a composite key for tag deduplication: "type:value"
func TagKey(tagType, tagValue string) string {
	return tagType + ":" + tagValue
}
