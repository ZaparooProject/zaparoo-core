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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleRequestRejectsOtherMethodsDuringRestore(t *testing.T) {
	t.Parallel()
	st, _ := state.NewState(nil, "test-boot")
	defer st.StopService()
	methodMap := &MethodMap{}
	called := false
	require.NoError(t, methodMap.AddMethod(models.MethodSettingsUpdate, func(requests.RequestEnv) (any, error) {
		called = true
		return "ok", nil
	}))
	finishRestore, err := st.BeginRestoreGate()
	require.NoError(t, err)

	type handleResult struct {
		result any
		err    *models.ErrorObject
	}
	blocked := make(chan handleResult, 1)
	go func() {
		result, rpcErr := handleRequest(methodMap, requests.RequestEnv{
			Context: context.Background(), State: st,
		}, models.RequestObject{JSONRPC: "2.0", Method: models.MethodSettingsUpdate})
		blocked <- handleResult{result: result, err: rpcErr}
	}()
	blockedResult := <-blocked
	assert.Nil(t, blockedResult.result)
	require.NotNil(t, blockedResult.err)
	assert.Equal(t, "backup restore is in progress", blockedResult.err.Message)
	assert.False(t, called)

	finishRestore(false)
	result, rpcErr := handleRequest(methodMap, requests.RequestEnv{
		Context: context.Background(), State: st,
	}, models.RequestObject{JSONRPC: "2.0", Method: models.MethodSettingsUpdate})
	require.Nil(t, rpcErr)
	assert.Equal(t, "ok", result)
	assert.True(t, called)
}

func TestHandleRequestAllowsRestoreMethodToOwnExclusiveGate(t *testing.T) {
	t.Parallel()
	st, _ := state.NewState(nil, "test-boot")
	defer st.StopService()
	methodMap := &MethodMap{}
	require.NoError(t, methodMap.AddMethod(
		models.MethodSettingsBackupRestore,
		func(requests.RequestEnv) (any, error) { return "restore", nil },
	))

	result, rpcErr := handleRequest(methodMap, requests.RequestEnv{
		Context: context.Background(), State: st,
	}, models.RequestObject{JSONRPC: "2.0", Method: models.MethodSettingsBackupRestore})
	require.Nil(t, rpcErr)
	assert.Equal(t, "restore", result)
}
