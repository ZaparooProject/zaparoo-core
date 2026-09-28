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
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostManagedUpdates(t *testing.T) {
	t.Parallel()
	platform := mocks.NewMockPlatform()
	platform.On("Settings").Return(platforms.Settings{DisableSelfUpdate: true})
	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)
	cfg.SetUpdateInstall(true)
	env := requests.RequestEnv{
		Context: t.Context(), Platform: platform, Config: cfg, IsLocal: true,
		Params: json.RawMessage(`{"force":true}`),
	}
	check, err := HandleUpdateCheck(env, func(context.Context, updater.Options) (*updater.Result, error) {
		t.Fatal("host-managed check must not invoke updater")
		return &updater.Result{}, nil
	})
	require.NoError(t, err)
	status, err := HandleUpdateStatus(env, func(context.Context, updater.Options) *updater.Result {
		t.Fatal("host-managed status must not access updater state")
		return nil
	})
	require.NoError(t, err)
	for _, result := range []any{check, status} {
		response, ok := result.(models.UpdateCheckResponse)
		require.True(t, ok)
		assert.Equal(t, updater.EligibilityManaged, response.Eligibility)
		assert.False(t, response.AutoInstall)
		assert.False(t, response.UpdateAvailable)
	}
	_, err = HandleUpdateApply(env, func(context.Context, updater.Options) (string, error) {
		t.Fatal("force must not bypass host-managed update policy")
		return "", nil
	}, func() { t.Fatal("host-managed update must not restart Core") })
	require.ErrorContains(t, err, "managed by the host")
}
