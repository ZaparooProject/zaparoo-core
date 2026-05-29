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

package methods

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMediaRequest_RejectsMixedBatchAndTopLevelRef(t *testing.T) {
	t.Parallel()

	_, err := parseMediaRequest(json.RawMessage(`{
		"mediaId": 1,
		"items": [{"mediaId": 2}]
	}`), maxMediaMetaBatchItems)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "items cannot be mixed")
}

func TestParseMediaRequest_RejectsInvalidBatchItemImageTypes(t *testing.T) {
	t.Parallel()

	_, err := parseMediaRequest(json.RawMessage(`{
		"items": [{"mediaId": 2, "imageTypes": [""]}]
	}`), maxMediaImageBatchItems)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "items[0]")
	assert.Contains(t, err.Error(), "imageTypes entries must be non-empty")
}

func TestParseMediaRequest_RejectsTopLevelImageTypesInBatch(t *testing.T) {
	t.Parallel()

	_, err := parseMediaRequest(json.RawMessage(`{
		"imageTypes": [""],
		"items": [{"mediaId": 2}]
	}`), maxMediaImageBatchItems)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "imageTypes entries must be non-empty")
}
