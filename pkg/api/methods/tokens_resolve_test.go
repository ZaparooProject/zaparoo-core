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
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type tokensResolveFixture struct {
	userDB   *testhelpers.MockUserDBI
	mediaDB  *testhelpers.MockMediaDBI
	platform *mocks.MockPlatform
	st       *state.State
	env      requests.RequestEnv
}

func newTokensResolveFixture(t *testing.T, mappings ...database.Mapping) *tokensResolveFixture {
	t.Helper()
	f := &tokensResolveFixture{
		userDB:   testhelpers.NewMockUserDBI(),
		mediaDB:  testhelpers.NewMockMediaDBI(),
		platform: mocks.NewMockPlatform(),
	}
	f.userDB.On("GetEnabledMappings").Return(mappings, nil)
	f.platform.On("LookupMapping", mock.Anything).Return("", false).Maybe()
	st, ns := state.NewState(f.platform, "boot")
	t.Cleanup(func() {
		for {
			select {
			case <-ns:
			default:
				return
			}
		}
	})
	f.st = st
	f.env = requests.RequestEnv{
		Context:  context.Background(),
		Platform: f.platform,
		State:    st,
		Database: &database.Database{UserDB: f.userDB, MediaDB: f.mediaDB},
	}
	return f
}

func (f *tokensResolveFixture) scan(t *tokens.Token) {
	t.ScanTime = time.Unix(1700000000, 0)
	f.st.SetActiveCard(*t)
}

func (f *tokensResolveFixture) resolve(t *testing.T, params string) models.TokensResolveResponse {
	t.Helper()
	if params != "" {
		f.env.Params = json.RawMessage(params)
	}
	result, err := HandleTokensResolve(f.env)
	require.NoError(t, err)
	resp, ok := result.(models.TokensResolveResponse)
	require.True(t, ok)
	return resp
}

func TestHandleTokensResolve_NoToken(t *testing.T) {
	t.Parallel()
	f := newTokensResolveFixture(t)
	_, err := HandleTokensResolve(f.env)
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}

func TestHandleTokensResolve_OtherReader(t *testing.T) {
	t.Parallel()
	f := newTokensResolveFixture(t)
	f.scan(&tokens.Token{UID: "04a1", ReaderID: "reader-a"})
	f.env.Params = json.RawMessage(`{"readerId":"reader-b"}`)
	_, err := HandleTokensResolve(f.env)
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)

	resp := f.resolve(t, `{"readerId":"reader-a"}`)
	assert.Equal(t, "reader-a", resp.Token.ReaderID)
}

func TestHandleTokensResolve_DatabaseMappingAndMedia(t *testing.T) {
	t.Parallel()
	row := makeMediaFullRow(9300, 9310)
	f := newTokensResolveFixture(t, database.Mapping{
		DBID: 7, Label: "Test card", Type: userdb.MappingTypeID, Match: userdb.MatchTypeExact,
		Pattern: "04a1", Override: "NES/" + row.Path, Enabled: true,
	})
	expectMediaImageResolve(f.mediaDB, row)
	f.scan(&tokens.Token{UID: "04:A1", Type: "ntag", ReaderID: "reader-a"})

	resp := f.resolve(t, "")
	assert.Equal(t, "04:A1", resp.Token.UID)
	assert.Equal(t, "ntag", resp.Token.Type)
	assert.Equal(t, "NES/"+row.Path, resp.Script)
	assert.False(t, resp.Redacted)
	assert.Equal(t, &models.ResolvedMapping{ID: "7", Source: "database", Label: "Test card"}, resp.Mapping)
	require.NotNil(t, resp.Media)
	assert.Equal(t, "NES", resp.Media.System)
	assert.Equal(t, row.Path, resp.Media.Path)
	assert.Equal(t, "Test Game", resp.Media.Name)
	assert.NotEmpty(t, resp.Media.SystemName)
}

func TestHandleTokensResolve_AbsolutePath(t *testing.T) {
	t.Parallel()
	row := makeMediaFullRow(9400, 9410)
	path := "/media/fat/games/NES/test.nes"
	f := newTokensResolveFixture(t)
	f.mediaDB.On("FindMediaIDsByPaths", mock.Anything, []string{path}).
		Return([]database.MediaPathID{{SystemID: "NES", Path: path, DBID: row.DBID}}, nil)
	f.mediaDB.On("GetMediaWithTitleAndSystem", mock.Anything, row.DBID).Return(row, nil)
	f.scan(&tokens.Token{UID: "04a1", Text: path})

	resp := f.resolve(t, "")
	assert.Nil(t, resp.Mapping)
	assert.Equal(t, path, resp.Script)
	require.NotNil(t, resp.Media)
	assert.Equal(t, "Test Game", resp.Media.Name)
}

func TestHandleTokensResolve_NoLaunch(t *testing.T) {
	t.Parallel()
	f := newTokensResolveFixture(t)
	f.scan(&tokens.Token{UID: "04a1", Text: "**input.keyboard:a"})

	resp := f.resolve(t, "")
	assert.Equal(t, "**input.keyboard:a", resp.Script)
	assert.Nil(t, resp.Media)
	f.mediaDB.AssertNotCalled(t, "FindMediaIDsByPaths", mock.Anything, mock.Anything)
}

func TestHandleTokensResolve_PlatformMapping(t *testing.T) {
	t.Parallel()
	f := newTokensResolveFixture(t)
	f.platform.ExpectedCalls = nil
	f.platform.On("LookupMapping", mock.Anything).Return("**input.keyboard:b", true)
	f.scan(&tokens.Token{UID: "04a1"})

	resp := f.resolve(t, "")
	assert.Equal(t, "**input.keyboard:b", resp.Script)
	assert.Equal(t, &models.ResolvedMapping{Source: "platform"}, resp.Mapping)
}

func TestHandleTokensResolve_RedactsCredentials(t *testing.T) {
	t.Parallel()
	f := newTokensResolveFixture(t)
	f.scan(&tokens.Token{UID: "04a1", Text: "**profile:corn-arm-truck"})

	resp := f.resolve(t, "")
	assert.True(t, resp.Redacted)
	assert.NotContains(t, resp.Token.Text, "corn-arm-truck")
	assert.NotContains(t, resp.Script, "corn-arm-truck")
}
