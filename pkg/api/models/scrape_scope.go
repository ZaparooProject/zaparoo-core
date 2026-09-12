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

package models

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// MediaScrapeScope selects one indexed item or one filesystem subtree.
// Selection forms are mutually exclusive; systems remains the legacy form.
type MediaScrapeScope struct {
	MediaID *int64           `json:"mediaId,omitempty"`
	File    *MediaScrapePath `json:"file,omitempty"`
	Subtree *MediaScrapePath `json:"subtree,omitempty"`
}

type MediaScrapePath struct {
	System string `json:"system"`
	Path   string `json:"path"`
}

// UnmarshalJSON rejects unknown selectors instead of silently broadening work.
func (s *MediaScrapeScope) UnmarshalJSON(data []byte) error {
	type plain MediaScrapeScope
	var value plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("invalid scrape scope: %w", err)
	}
	*s = MediaScrapeScope(value)
	return nil
}
