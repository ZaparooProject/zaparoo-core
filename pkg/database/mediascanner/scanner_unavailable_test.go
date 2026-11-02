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
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
)

// An unreachable media server was reported to Sentry on every index run, one
// event per scanner, for as long as the user's Kodi host stayed offline.
func TestScannerSourceUnavailable(t *testing.T) {
	t.Parallel()

	assert.True(t, scannerSourceUnavailable(fmt.Errorf("get shows: %w", syscall.ECONNREFUSED)))
	assert.True(t, scannerSourceUnavailable(
		fmt.Errorf("get shows: %w", platforms.ErrScannerSourceUnreachable)))
	assert.False(t, scannerSourceUnavailable(errors.New("error from kodi api: Invalid params")))
	assert.False(t, scannerSourceUnavailable(platforms.ErrScannerUnavailable))
}
