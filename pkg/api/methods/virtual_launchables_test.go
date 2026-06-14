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
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type apiLaunchablePlatform struct {
	*mocks.MockPlatform
	defs []launchables.Launchable
}

func (p *apiLaunchablePlatform) Launchables(*config.Instance) []launchables.Launchable {
	return p.defs
}

func TestHandleSystems_IncludesVirtualSystems(t *testing.T) {
	id := uuid.MustParse("01890f4a-33e8-4d44-d3a8-56824d352000")
	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("IndexedSystems").Return([]string{}, nil)
	mockPlatform := &apiLaunchablePlatform{
		MockPlatform: mocks.NewMockPlatform(),
		defs: []launchables.Launchable{
			launchables.VirtualSystem{
				ID:       id,
				Name:     "Chess",
				Category: "Other",
				Launch: func(
					*config.Instance,
					string,
					*platforms.LaunchOptions,
				) (*os.Process, error) {
					return &os.Process{}, nil
				},
			},
		},
	}

	result, err := HandleSystems(requests.RequestEnv{
		Platform: mockPlatform,
		Config:   &config.Instance{},
		Database: &database.Database{MediaDB: mockMediaDB},
	})

	require.NoError(t, err)
	response, ok := result.(models.SystemsResponse)
	require.True(t, ok)
	require.Len(t, response.Systems, 1)
	assert.Equal(t, launchables.EncodeID(id), response.Systems[0].ID)
	assert.Equal(t, "Chess", response.Systems[0].Name)
	assert.Equal(t, "Other", response.Systems[0].Category)
	assert.Equal(t, "zaparoo://"+launchables.EncodeID(id)+"/Chess", response.Systems[0].ZapScript)
	mockMediaDB.AssertExpectations(t)
}
