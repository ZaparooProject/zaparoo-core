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

package methods

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleScreenshot_Success(t *testing.T) {
	t.Parallel()

	imgData := []byte("fake-png-data")
	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("Screenshot").Return(&platforms.ScreenshotResult{
		Path: "/screenshots/SNES/20260327_120000-Game.png",
		Data: imgData,
	}, nil)

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
	}

	result, err := HandleScreenshot(env)
	require.NoError(t, err)

	resp, ok := result.(models.ScreenshotResponse)
	require.True(t, ok)
	assert.Equal(t, "/screenshots/SNES/20260327_120000-Game.png", resp.Path)
	assert.Equal(t, base64.StdEncoding.EncodeToString(imgData), resp.Data)
	assert.Equal(t, len(imgData), resp.Size)
	pl.AssertCalled(t, "Screenshot")
}

func TestHandleScreenshot_PlatformError(t *testing.T) {
	t.Parallel()

	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	pl.On("Screenshot").Return(nil, platforms.ErrNotSupported)

	st, ns := state.NewState(pl, "test")
	defer st.StopService()
	drainNotifications(t, ns)

	env := requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
	}

	_, err := HandleScreenshot(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}
