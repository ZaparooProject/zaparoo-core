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
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleConfirm_Success(t *testing.T) {
	t.Parallel()

	cfq := make(chan chan error, 1)
	env := requests.RequestEnv{
		Context:      context.Background(),
		ConfirmQueue: cfq,
	}

	go func() {
		result := <-cfq
		result <- nil
	}()

	got, err := HandleConfirm(env)
	require.NoError(t, err)
	assert.Equal(t, NoContent{}, got)
}

func TestHandleConfirm_Error(t *testing.T) {
	t.Parallel()

	cfq := make(chan chan error, 1)
	env := requests.RequestEnv{
		Context:      context.Background(),
		ConfirmQueue: cfq,
	}

	go func() {
		result := <-cfq
		result <- errors.New("no staged token to confirm")
	}()

	_, err := HandleConfirm(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no staged token to confirm")

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}

func TestHandleConfirm_ContextCancelledBeforeSend(t *testing.T) {
	t.Parallel()

	// Unbuffered channel that nobody reads — send will block
	cfq := make(chan chan error)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	env := requests.RequestEnv{
		Context:      ctx,
		ConfirmQueue: cfq,
	}

	_, err := HandleConfirm(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirm cancelled")

	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr)
}

func TestHandleConfirm_ContextCancelledBeforeResult(t *testing.T) {
	t.Parallel()

	cfq := make(chan chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	env := requests.RequestEnv{
		Context:      ctx,
		ConfirmQueue: cfq,
	}

	go func() {
		<-cfq    // consume the send but never reply
		cancel() // cancel context while handler waits for result
	}()

	_, err := HandleConfirm(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "confirm cancelled")

	var clientErr2 *models.ClientError
	require.ErrorAs(t, err, &clientErr2)
}
