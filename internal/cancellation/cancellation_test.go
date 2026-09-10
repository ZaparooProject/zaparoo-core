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

package cancellation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func TestCancellationOnly(t *testing.T) {
	for _, tc := range []struct {
		err  error
		name string
		only bool
	}{
		{name: "nil"},
		{name: "canceled", err: context.Canceled, only: true},
		{name: "wrapped", err: fmt.Errorf("status: %w", context.Canceled), only: true},
		{
			name: "joined cancellations",
			err:  errors.Join(context.Canceled, fmt.Errorf("wrapped: %w", context.Canceled)), only: true,
		},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "text", err: errors.New("context canceled")},
		{name: "mixed", err: errors.Join(context.Canceled, errors.New("disk failure"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.only, Only(tc.err))
			if tc.err == nil {
				return
			}
			var output bytes.Buffer
			old, level := log.Logger, zerolog.GlobalLevel()
			log.Logger = zerolog.New(&output)
			zerolog.SetGlobalLevel(zerolog.DebugLevel)
			t.Cleanup(func() { log.Logger = old; zerolog.SetGlobalLevel(level) })
			LogFailure(tc.err, "operation stopped")
			if tc.only {
				assert.Contains(t, output.String(), `"level":"debug"`)
				assert.NotContains(t, output.String(), `"level":"error"`)
			} else {
				assert.Contains(t, output.String(), `"level":"error"`)
			}
		})
	}
}
