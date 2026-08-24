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

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeMethodResolver map[string]func(requests.RequestEnv) (any, error)

func (f fakeMethodResolver) GetMethod(name string) (func(requests.RequestEnv) (any, error), bool) {
	fn, ok := f[name]
	return fn, ok
}

func TestRunMethod_SucceedsUnderLimit(t *testing.T) {
	t.Parallel()

	m := &manager{deps: Deps{Methods: fakeMethodResolver{
		"probe": func(requests.RequestEnv) (any, error) {
			return map[string]string{"ok": "yes"}, nil
		},
	}}}

	result := m.runMethod(context.Background(), opSpec{method: "probe", limit: resultLimit}, nil)
	require.Equal(t, "succeeded", result.Status)
	assert.JSONEq(t, `{"ok":"yes"}`, string(result.Result))
}

func TestRunMethod_UnregisteredMethodIsInternalError(t *testing.T) {
	t.Parallel()

	m := &manager{deps: Deps{Methods: fakeMethodResolver{}}}
	result := m.runMethod(context.Background(), opSpec{method: "missing", limit: resultLimit}, nil)
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "internal_error", result.ErrorCode)
}

// TestRunMethod_ShrinksUntilItFits verifies an oversized result is retried
// with a smaller page via spec.shrink, and that the smaller request is what
// actually reaches the handler (not just a smaller response).
func TestRunMethod_ShrinksUntilItFits(t *testing.T) {
	t.Parallel()

	var sawMaxResults []int
	m := &manager{deps: Deps{Methods: fakeMethodResolver{
		"probe": func(env requests.RequestEnv) (any, error) {
			var params struct {
				MaxResults int `json:"maxResults"`
			}
			require.NoError(t, json.Unmarshal(env.Params, &params))
			sawMaxResults = append(sawMaxResults, params.MaxResults)
			// Oversized until maxResults has been shrunk down to 25.
			size := params.MaxResults
			if size > 25 {
				size = 200
			} else {
				size = 1
			}
			return map[string]string{"payload": strings.Repeat("x", size)}, nil
		},
	}}}

	spec := opSpec{method: "probe", shrink: shrinkPage, limit: 50}
	result := m.runMethod(context.Background(), spec, json.RawMessage(`{"maxResults":100}`))
	require.Equal(t, "succeeded", result.Status)
	assert.Equal(t, []int{100, 50, 25}, sawMaxResults)
}

func TestRunMethod_OversizeWithNoShrinkFails(t *testing.T) {
	t.Parallel()

	m := &manager{deps: Deps{Methods: fakeMethodResolver{
		"probe": func(requests.RequestEnv) (any, error) {
			return map[string]string{"payload": strings.Repeat("x", 1000)}, nil
		},
	}}}

	result := m.runMethod(context.Background(), opSpec{method: "probe", limit: 10}, nil)
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "result_too_large", result.ErrorCode)
}

func TestRunMethod_ShrinkFloorFails(t *testing.T) {
	t.Parallel()

	// Always oversized, regardless of maxResults — the shrink loop must
	// terminate once maxResults would drop below 1, not loop forever.
	m := &manager{deps: Deps{Methods: fakeMethodResolver{
		"probe": func(requests.RequestEnv) (any, error) {
			return map[string]string{"payload": strings.Repeat("x", 1000)}, nil
		},
	}}}

	spec := opSpec{method: "probe", shrink: shrinkPage, limit: 10}
	result := m.runMethod(context.Background(), spec, json.RawMessage(`{"maxResults":2}`))
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, "result_too_large", result.ErrorCode)
}

func TestRunMethod_HandlerErrorIsClassified(t *testing.T) {
	t.Parallel()

	m := &manager{deps: Deps{Methods: fakeMethodResolver{
		"probe": func(requests.RequestEnv) (any, error) {
			return nil, state.ErrLaunchInProgress
		},
	}}}

	result := m.runMethod(context.Background(), opSpec{method: "probe", limit: resultLimit}, nil)
	assert.Equal(t, "busy", result.Status)
}

func TestShrinkPage(t *testing.T) {
	t.Parallel()

	t.Run("halves an explicit maxResults", func(t *testing.T) {
		t.Parallel()
		next, ok := shrinkPage(json.RawMessage(`{"maxResults":100,"query":"sonic"}`))
		require.True(t, ok)
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(next, &decoded))
		assert.InEpsilon(t, float64(50), decoded["maxResults"], 0)
		assert.Equal(t, "sonic", decoded["query"])
	})

	t.Run("defaults to remoteDefaultMaxResults when absent", func(t *testing.T) {
		t.Parallel()
		next, ok := shrinkPage(json.RawMessage(`{"query":"sonic"}`))
		require.True(t, ok)
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(next, &decoded))
		assert.InEpsilon(t, float64(remoteDefaultMaxResults/2), decoded["maxResults"], 0)
	})

	t.Run("stops at the floor", func(t *testing.T) {
		t.Parallel()
		_, ok := shrinkPage(json.RawMessage(`{"maxResults":1}`))
		assert.False(t, ok)
	})

	t.Run("empty params is a valid object", func(t *testing.T) {
		t.Parallel()
		next, ok := shrinkPage(nil)
		require.True(t, ok)
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(next, &decoded))
		assert.InEpsilon(t, float64(remoteDefaultMaxResults/2), decoded["maxResults"], 0)
	})

	t.Run("malformed params is a miss", func(t *testing.T) {
		t.Parallel()
		_, ok := shrinkPage(json.RawMessage(`not json`))
		assert.False(t, ok)
	})
}

func TestClassifyHandlerError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err        error
		wantStatus string
		wantCode   string
	}{
		{err: state.ErrLaunchInProgress, wantStatus: "busy"},
		{err: methods.ErrForbidden, wantStatus: "failed", wantCode: "forbidden"},
		{err: context.DeadlineExceeded, wantStatus: "failed", wantCode: "timeout"},
		{err: context.Canceled, wantStatus: "failed", wantCode: "timeout"},
		{err: models.ClientErrf("bad input"), wantStatus: "failed", wantCode: "bad_params"},
		{err: models.QuietClientErrf("bad input"), wantStatus: "failed", wantCode: "bad_params"},
		{err: errors.New("boom"), wantStatus: "failed", wantCode: "query_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			t.Parallel()
			result := classifyHandlerError(tt.err)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantCode, result.ErrorCode)
			assert.Nil(t, result.Result, "handler error text must never reach the wire")
		})
	}
}
