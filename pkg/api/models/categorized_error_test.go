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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCategorizedErrorHidesCauseFromMessage(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("file not found")
	cause := fmt.Errorf("%w: /media/fat/games/secret.sfc", sentinel)
	err := CategorizedErr(ErrorCategoryMediaNotFound, "media not found", cause)

	assert.Equal(t, "media not found", err.Error())
	assert.NotContains(t, err.Error(), "/media/fat")
	require.ErrorIs(t, err, sentinel)

	var catErr *CategorizedError
	require.ErrorAs(t, err, &catErr)
	assert.Equal(t, ErrorCategoryMediaNotFound, catErr.Category)
	assert.Equal(t, cause, catErr.Err)
}
