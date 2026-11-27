//go:build linux && !android

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

package sim

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeIndexServer answers media and media.generate. media.generate refuses
// with refusal for the first refusals calls, then succeeds and adds media.
type fakeIndexServer struct {
	refusal  string
	mu       syncutil.Mutex
	refusals int
	calls    int
	total    int
}

func (f *fakeIndexServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Method string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	switch req.Method {
	case models.MethodMedia:
		total := f.total
		resp["result"] = map[string]any{
			"database": map[string]any{"exists": true, "totalMedia": total},
			"active":   []any{},
		}
	case models.MethodMediaGenerate:
		f.calls++
		if f.calls <= f.refusals {
			resp["error"] = map[string]any{
				"code":    1,
				"message": f.refusal,
				"data":    map[string]any{"category": models.ErrorCategoryBusy},
			}
			break
		}
		f.total += 3
		resp["result"] = nil
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// TestIndexRetriesBriefMediaWork covers a scenario that indexes straight
// after start: Core's own startup maintenance briefly holds the media
// database, and the refusal it gets must not fail the scenario.
func TestIndexRetriesBriefMediaWork(t *testing.T) {
	t.Parallel()

	for _, refusal := range []string{
		"media database maintenance in progress",
		"database optimization in progress",
	} {
		t.Run(refusal, func(t *testing.T) {
			t.Parallel()
			fake := &fakeIndexServer{refusal: refusal, refusals: 2}
			srv := httptest.NewServer(fake)
			t.Cleanup(srv.Close)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			status, err := NewAPI(srv.URL).Index(ctx, []string{"SNES"})
			require.NoError(t, err)
			require.NotNil(t, status.TotalMedia)
			assert.Equal(t, 3, *status.TotalMedia)
			fake.mu.Lock()
			assert.Equal(t, 3, fake.calls, "two refusals, then the run")
			fake.mu.Unlock()
		})
	}
}

// TestIndexDoesNotRetryOtherRefusals keeps a refusal from work a scenario
// started, such as an index already running, visible to the scenario.
func TestIndexDoesNotRetryOtherRefusals(t *testing.T) {
	t.Parallel()

	fake := &fakeIndexServer{refusal: "indexing already in progress", refusals: 1}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := NewAPI(srv.URL).Index(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "indexing already in progress")
	fake.mu.Lock()
	assert.Equal(t, 1, fake.calls)
	fake.mu.Unlock()
}
