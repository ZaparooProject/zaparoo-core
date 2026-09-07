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
	"errors"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleMediaLookupCandidates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		params string
		limit  int
	}{
		{`{"system":"NES","name":"Mario"}`, 5},
		{`{"system":"nes","fuzzySystem":true,"name":"Mario"}`, 5},
		{`{"system":"NES","name":"Mario","maxResults":2}`, 2},
		{`{"system":"Nintendo Entertainment System","fuzzySystem":true,"name":"Mario"}`, 5},
	} {
		t.Run(tc.params, func(t *testing.T) {
			t.Parallel()
			db := testhelpers.NewMockMediaDBI()
			want := []database.TitleCandidate{
				{SystemID: "NES", Name: "Mario", Rank: 1, MatchType: "exact", Confidence: 1},
			}
			db.On("TitleCandidates", mock.Anything, "NES", "Mario", tc.limit).Return(want, nil).Once()
			got, err := HandleMediaLookupCandidates(requests.RequestEnv{
				Context: context.Background(), Params: json.RawMessage(tc.params),
				Database: &database.Database{MediaDB: db},
			})
			require.NoError(t, err)
			assert.Equal(t, models.MediaLookupCandidatesResponse{Candidates: want}, got)
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			assert.JSONEq(t,
				`{"candidates":[{"systemId":"NES","name":"Mario","rank":1,"matchType":"exact","confidence":1}]}`,
				string(encoded))
			db.AssertExpectations(t)
			// Any enrichment, UserDB write, or resolution-cache access would hit
			// an unset mock expectation (or the intentionally absent UserDB).
			assert.Len(t, db.Calls, 1)
		})
	}
}

func TestHandleMediaLookupCandidatesInvalidParams(t *testing.T) {
	t.Parallel()
	for _, params := range []string{
		`{}`, `null`, `[]`, `{"name":"Mario"}`, `{"system":"NES"}`,
		`{"system":"","name":"Mario"}`, `{"system":"bad-system","name":"Mario"}`,
		`{"system":["NES","SNES"],"name":"Mario"}`, `{"systems":["NES"],"name":"Mario"}`,
		`{"system":"NES","name":"  "}`, `{"system":"NES","name":"!!!"}`,
		`{"system":"NES","name":"Mario","maxResults":0}`, `{"system":"NES","name":"Mario","maxResults":6}`,
		`{"system":"NES","name":"Mario","maxResults":-1}`, `{"system":"NES","name":"Mario","maxResults":1.5}`,
		`{"system":"NES","name":"` + strings.Repeat("\u754c", 257) + `"}`,
		`{"system":"NES","name":"` + strings.Repeat("a", 257) + `"}`,
	} {
		t.Run(params, func(t *testing.T) {
			t.Parallel()
			_, err := HandleMediaLookupCandidates(requests.RequestEnv{
				Context: context.Background(), Params: json.RawMessage(params),
			})
			require.Error(t, err)
			var clientErr *models.ClientError
			assert.ErrorAs(t, err, &clientErr)
		})
	}
}

func TestHandleMediaLookupCandidatesEmptyAndErrors(t *testing.T) {
	t.Parallel()
	for _, dbErr := range []error{nil, context.Canceled, errors.New("database replaced")} {
		db := testhelpers.NewMockMediaDBI()
		db.On("TitleCandidates", mock.Anything, "NES", "Mario", 5).Return(nil, dbErr).Once()
		got, err := HandleMediaLookupCandidates(requests.RequestEnv{
			Context: context.Background(), Params: json.RawMessage(`{"system":"NES","name":"Mario"}`),
			Database: &database.Database{MediaDB: db},
		})
		if dbErr == nil {
			require.NoError(t, err)
			encoded, marshalErr := json.Marshal(got)
			require.NoError(t, marshalErr)
			assert.JSONEq(t, `{"candidates":[]}`, string(encoded))
		} else {
			require.ErrorIs(t, err, dbErr)
			assert.Nil(t, got)
		}
		db.AssertExpectations(t)
	}
}

func TestHandleMediaLookupCandidatesCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := HandleMediaLookupCandidates(requests.RequestEnv{
		Context: ctx, Params: json.RawMessage(`{"system":"NES","name":"Mario"}`),
	})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestHandleMediaLookupCandidatesSharesSearchLimit(t *testing.T) {
	// Nonparallel: this test owns the package-wide semaphore while exercising
	// cancellation, rather than contending with other handler tests.
	for range cap(searchSem) {
		searchSem <- struct{}{}
	}
	defer func() {
		for range cap(searchSem) {
			<-searchSem
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	waiting := &candidateHandlerContext{Context: ctx, waiting: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		_, err := HandleMediaLookupCandidates(requests.RequestEnv{
			Context: waiting, Params: json.RawMessage(`{"system":"NES","name":"Mario"}`),
		})
		result <- err
	}()
	<-waiting.waiting
	cancel()
	assert.ErrorIs(t, <-result, context.Canceled)
}

type candidateHandlerContext struct {
	context.Context
	waiting chan struct{}
}

func (c *candidateHandlerContext) Done() <-chan struct{} {
	close(c.waiting)
	return c.Context.Done()
}

func FuzzHandleMediaLookupCandidates(f *testing.F) {
	for _, seed := range []string{
		`{"system":"NES","name":"Mario"}`, `{"system":"NES","name":"ドラゴンクエスト"}`,
		`{}`, `null`, `{"system":"NES","name":"!!!"}`, `{"system":"NES","name":"Mario","maxResults":6}`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 8192 {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// Valid inputs stop at cancellation before reaching the absent DB.
		// Invalid inputs must be client errors; neither may panic or succeed.
		_, err := HandleMediaLookupCandidates(requests.RequestEnv{Context: ctx, Params: json.RawMessage(raw)})
		require.Error(t, err)
		var clientErr *models.ClientError
		assert.True(t, errors.Is(err, context.Canceled) || errors.As(err, &clientErr))
	})
}
