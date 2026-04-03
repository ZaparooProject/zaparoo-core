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

package models_test

import (
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errSentinel = errors.New("sentinel")

func TestClientErr_WrapsError(t *testing.T) {
	wrapped := models.ClientErr(errSentinel)
	require.Error(t, wrapped)
	assert.Equal(t, "sentinel", wrapped.Error())
}

func TestClientErr_UnwrapsToOriginal(t *testing.T) {
	wrapped := models.ClientErr(errSentinel)
	assert.ErrorIs(t, wrapped, errSentinel)
}

func TestClientErr_DetectedByErrorsAs(t *testing.T) {
	wrapped := models.ClientErr(errSentinel)
	var clientErr *models.ClientError
	assert.ErrorAs(t, wrapped, &clientErr)
	assert.Equal(t, errSentinel, clientErr.Err)
}

func TestClientErrf_FormatsMessage(t *testing.T) {
	err := models.ClientErrf("bad input: %s", "foo")
	require.Error(t, err)
	assert.Equal(t, "bad input: foo", err.Error())
}

func TestClientErrf_WrapsErrorWithIs(t *testing.T) {
	err := models.ClientErrf("wrapped: %w", errSentinel)
	assert.ErrorIs(t, err, errSentinel)
}

func TestClientErrf_DetectedByErrorsAs(t *testing.T) {
	err := models.ClientErrf("wrapped: %w", errSentinel)
	var clientErr *models.ClientError
	assert.ErrorAs(t, err, &clientErr)
}
