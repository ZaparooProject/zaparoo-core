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

//go:build darwin

package power_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestReadDarwin_Success(t *testing.T) {
	t.Parallel()

	executor := &mocks.MockCommandExecutor{}
	executor.On("Output", mock.Anything, power.PmsetPathForTest(), []string{"-g", "batt"}).Return([]byte(
		"Now drawing from 'Battery Power'\n"+
			" -InternalBattery-0 (id=4653155)\t62%; discharging; 3:32 remaining present: true\n",
	), nil).Once()

	status, err := power.ReadDarwinWithExecutorForTest(t.Context(), executor)

	require.NoError(t, err)
	assert.Equal(t, power.Status{Source: power.SourceBattery, Percent: 62}, status)
	executor.AssertExpectations(t)
}

func TestReadDarwin_CommandFailure(t *testing.T) {
	t.Parallel()

	commandErr := errors.New("pmset failed")
	executor := &mocks.MockCommandExecutor{}
	executor.On(
		"Output", mock.Anything, power.PmsetPathForTest(), []string{"-g", "batt"},
	).Return(nil, commandErr).Once()

	status, err := power.ReadDarwinWithExecutorForTest(t.Context(), executor)

	require.ErrorIs(t, err, commandErr)
	assert.Equal(t, power.Status{Source: power.SourceUnknown}, status)
	executor.AssertExpectations(t)
}

func TestReadDarwin_ContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	executor := &mocks.MockCommandExecutor{}
	executor.On(
		"Output",
		mock.MatchedBy(func(callCtx context.Context) bool {
			return errors.Is(callCtx.Err(), context.Canceled)
		}),
		power.PmsetPathForTest(),
		[]string{"-g", "batt"},
	).Return(nil, context.Canceled).Once()

	status, err := power.ReadDarwinWithExecutorForTest(ctx, executor)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, power.Status{Source: power.SourceUnknown}, status)
	executor.AssertExpectations(t)
}
