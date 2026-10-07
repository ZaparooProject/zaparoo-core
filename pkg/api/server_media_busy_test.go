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

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A media write refused because another operation holds the media database
// reaches the wire as an ordinary error response whose data names the "busy"
// category, so a client never has to match the message text.
func TestHandleRequestMediaWriteRefusalIsBusyOnTheWire(t *testing.T) {
	t.Parallel()

	mediaDB, mediaCleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(mediaCleanup)
	userDB, userCleanup := helpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	scantest.IndexMediaPaths(t, mediaDB, "NES", filepath.Join("roms", "NES", "Game.nes"))
	rows, err := mediaDB.GetMediaBySystemID("NES")
	require.NoError(t, err)
	require.Len(t, rows, 1)

	methodMap := &MethodMap{}
	require.NoError(t, methodMap.AddMethod(models.MethodMediaTagsUpdate, methods.HandleMediaTagsUpdate))
	require.NoError(t, methodMap.AddMethod(models.MethodMediaMetaUpdate, methods.HandleMediaMetaUpdate))

	tests := []struct {
		name      string
		method    string
		params    string
		wantError string
		operation database.MediaWriteOperation
	}{
		{
			name:      "tags update during indexing",
			operation: database.MediaWriteOperationIndexing,
			method:    models.MethodMediaTagsUpdate,
			params:    fmt.Sprintf(`{"mediaId":%d,"add":["user:favorite"]}`, rows[0].DBID),
			wantError: `{"data":{"category":"busy"},"message":"media indexing is in progress","code":1}`,
		},
		{
			name:      "meta update during recovery",
			operation: database.MediaWriteOperationRecovery,
			method:    models.MethodMediaMetaUpdate,
			params:    fmt.Sprintf(`{"mediaId":%d,"media":{"launcherOverride":null}}`, rows[0].DBID),
			wantError: `{"data":{"category":"busy"},"message":"media database maintenance in progress","code":1}`,
		},
		{
			name:      "tags update during optimization",
			operation: database.MediaWriteOperationOptimization,
			method:    models.MethodMediaTagsUpdate,
			params:    fmt.Sprintf(`{"mediaId":%d,"add":["user:favorite"]}`, rows[0].DBID),
			wantError: `{"data":{"category":"busy"},"message":"database optimization in progress","code":1}`,
		},
		{
			name:      "tags update during maintenance",
			operation: database.MediaWriteOperationMaintenance,
			method:    models.MethodMediaTagsUpdate,
			params:    fmt.Sprintf(`{"mediaId":%d,"add":["user:favorite"]}`, rows[0].DBID),
			wantError: `{"data":{"category":"busy"},"message":"media database maintenance in progress","code":1}`,
		},
	}

	coordinator, err := database.GetMediaDBWriteCoordinator(mediaDB)
	require.NoError(t, err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lease, leaseErr := coordinator.AcquireMediaWrite(tt.operation)
			require.NoError(t, leaseErr)
			defer lease.Release()

			result, rpcErr := handleRequest(methodMap, requests.RequestEnv{
				Context:  context.Background(),
				Database: &database.Database{MediaDB: mediaDB, UserDB: userDB},
				IsLocal:  true,
			}, models.RequestObject{JSONRPC: "2.0", Method: tt.method, Params: []byte(tt.params)})
			assert.Nil(t, result)
			require.NotNil(t, rpcErr)

			wire, marshalErr := json.Marshal(rpcErr)
			require.NoError(t, marshalErr)
			assert.Equal(t, tt.wantError, string(wire))
		})
	}
}
