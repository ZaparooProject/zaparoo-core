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

package helpers

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/require"
)

// AssertValidActiveMedia validates that an ActiveMedia struct has all required fields properly set.
// This helper catches common bugs like missing timestamps or empty required fields.
// Use this in tests that receive ActiveMedia from production code to ensure data integrity.
func AssertValidActiveMedia(t *testing.T, media *models.ActiveMedia) {
	t.Helper()

	require.NotNil(t, media, "ActiveMedia should not be nil")

	// Critical: Started must not be zero value (would cause huge PlayTime calculations)
	require.False(t, media.Started.IsZero(),
		"ActiveMedia.Started must be set (was zero time - would cause incorrect PlayTime)")

	// Required fields must not be empty
	require.NotEmpty(t, media.SystemID, "ActiveMedia.SystemID is required")
	require.NotEmpty(t, media.SystemName, "ActiveMedia.SystemName is required")
	require.NotEmpty(t, media.Path, "ActiveMedia.Path is required")
	require.NotEmpty(t, media.Name, "ActiveMedia.Name is required")
	// Note: LauncherID can be empty when detecting already-running games
}

// AssertValidActiveMediaOrNil validates ActiveMedia if non-nil, otherwise passes.
// Use this when ActiveMedia can legitimately be nil (e.g., no media playing).
func AssertValidActiveMediaOrNil(t *testing.T, media *models.ActiveMedia) {
	t.Helper()

	if media != nil {
		AssertValidActiveMedia(t, media)
	}
}
