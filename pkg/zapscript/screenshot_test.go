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

package zapscript

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCmdScreenshot_Success(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("Screenshot").Return(&platforms.ScreenshotResult{
		Path: "/screenshots/SNES/20260327_120000-Game.png",
		Data: []byte("fake-png-data"),
	}, nil)

	result, err := cmdScreenshot(pl, platforms.CmdEnv{})
	require.NoError(t, err)
	assert.Equal(t, platforms.CmdResult{}, result)
	pl.AssertCalled(t, "Screenshot")
}

func TestCmdScreenshot_PlatformError(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("Screenshot").Return(nil, platforms.ErrNotSupported)

	_, err := cmdScreenshot(pl, platforms.CmdEnv{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
