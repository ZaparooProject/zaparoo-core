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

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func TestLogClientCommandError(t *testing.T) {
	tests := []struct {
		err           error
		name          string
		expectedLevel string
	}{
		{
			name:          "connection refused logs at warn",
			err:           syscall.ECONNREFUSED,
			expectedLevel: "warn",
		},
		{
			name:          "wrapped connection refused logs at warn",
			err:           fmt.Errorf("dial failed: %w", syscall.ECONNREFUSED),
			expectedLevel: "warn",
		},
		{
			name:          "other error logs at error",
			err:           errors.New("something else broke"),
			expectedLevel: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := zerolog.New(&buf).Level(zerolog.TraceLevel)
			prevLogger := log.Logger
			log.Logger = logger
			t.Cleanup(func() { log.Logger = prevLogger })

			logClientCommandError(tt.err, "error running")

			output := buf.String()
			assert.Contains(t, output, `"level":"`+tt.expectedLevel+`"`)
			assert.Contains(t, output, "error running")
		})
	}
}
