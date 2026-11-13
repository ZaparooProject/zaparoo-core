//go:build !windows

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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A refused connection means Core is not running, which is expected and gets
// logged at Warn to stay out of Sentry. The check runs against a real refused
// dial rather than a hand-built error, because what a dial actually returns is
// the thing that has to match.
func TestIsConnectionRefused(t *testing.T) {
	t.Parallel()

	dialErr := refusedDial(t)
	assert.True(t, isConnectionRefused(dialErr), "a real refused dial must be recognised")

	// Wrapped the way the callers wrap it, since that is how it arrives.
	assert.True(t, isConnectionRefused(fmt.Errorf("calling update.status: %w", dialErr)))

	assert.False(t, isConnectionRefused(errors.New("something else")))
	assert.False(t, isConnectionRefused(nil))
}
