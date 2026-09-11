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

package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/stretchr/testify/assert"
)

func TestIsValidationClientError(t *testing.T) {
	t.Parallel()
	fieldErr := &validation.Error{Fields: []validation.FieldError{{Message: "name is required"}}}

	tests := map[string]struct {
		err  error
		want bool
	}{
		"invalid params": {err: models.ClientErr(validation.ErrInvalidParams), want: true},
		"missing params wrapped": {
			err:  models.ClientErrf("request rejected: %w", validation.ErrMissingParams),
			want: true,
		},
		"field validation": {err: models.ClientErrf("invalid params: %w", fieldErr), want: true},
		"unrelated client error": {
			err:  models.ClientErrf("indexing already in progress"),
			want: false,
		},
		"unrelated wrapped error": {
			err:  fmt.Errorf("storage failure: %w", errors.New("write failed")),
			want: false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isValidationClientError(tt.err))
		})
	}
}
