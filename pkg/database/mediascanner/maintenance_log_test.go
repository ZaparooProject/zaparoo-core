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

package mediascanner

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

// Global zerolog state is mutated to capture output; must not be parallel.
func TestLogMaintenanceError(t *testing.T) {
	prevGlobalLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(prevGlobalLevel) })

	tests := []struct {
		err           error
		name          string
		expectedLevel string
	}{
		{
			name:          "context cancelled logs at debug",
			err:           context.Canceled,
			expectedLevel: "debug",
		},
		{
			name:          "wrapped context cancelled logs at debug",
			err:           fmt.Errorf("set status: %w", context.Canceled),
			expectedLevel: "debug",
		},
		{
			name:          "other error logs at error",
			err:           errors.New("disk write failed"),
			expectedLevel: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			prevLogger := log.Logger
			log.Logger = zerolog.New(&buf).Level(zerolog.TraceLevel)
			t.Cleanup(func() { log.Logger = prevLogger })

			logMaintenanceError(tt.err, "failed to set indexing status")

			output := buf.String()
			assert.Contains(t, output, `"level":"`+tt.expectedLevel+`"`)
			assert.Contains(t, output, "failed to set indexing status")
		})
	}
}
