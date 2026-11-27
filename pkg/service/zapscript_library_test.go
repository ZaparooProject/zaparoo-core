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

package service

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The library a script uses runs the API handlers on the service's own
// platform, database and state, under the device's kiosk lock.
func TestZapScriptLibrary_RunsOnTheServiceItself(t *testing.T) {
	t.Parallel()
	pl := mocks.NewMockPlatform()
	st, _ := state.NewState(pl, "test-boot")
	svc := &ServiceContext{
		Platform: pl,
		Config:   &config.Instance{},
		State:    st,
		DB:       &database.Database{},
	}

	library, ok := zapScriptLibrary(svc).(*methods.ZapScriptLibrary)
	require.True(t, ok)
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "command")
	env := library.Env(ctx)
	assert.Equal(t, ctx, env.Context)
	assert.Same(t, svc.Config, env.Config)
	assert.Same(t, svc.State, env.State)
	assert.Same(t, svc.DB, env.Database)
	assert.Equal(t, pl, env.Platform)
	assert.Same(t, st.Kiosk(), env.Kiosk)
	assert.Same(t, helpers.GlobalLauncherCache, env.LauncherCache)
}
