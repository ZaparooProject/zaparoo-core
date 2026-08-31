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

package mediaslot_test

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty defaults primary", raw: "", want: mediaslot.Primary},
		{name: "primary", raw: "primary", want: mediaslot.Primary},
		{name: "background", raw: "background", want: mediaslot.Background},
		{name: "trims case", raw: " Background ", want: mediaslot.Background},
		{name: "bg alias", raw: "bg", want: mediaslot.Background},
		{name: "bg alias uppercase", raw: " BG ", want: mediaslot.Background},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := mediaslot.Normalize(tt.raw)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalize_Invalid(t *testing.T) {
	t.Parallel()

	_, err := mediaslot.Normalize("tertiary")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported media slot")
}
