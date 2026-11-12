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

package hoststatus

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingExecutor records what it was asked to run.
type recordingExecutor struct {
	err error
	ran [][]string
}

func (e *recordingExecutor) Run(_ context.Context, name string, args ...string) error {
	e.ran = append(e.ran, append([]string{name}, args...))
	return e.err
}

func (*recordingExecutor) Output(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

func (*recordingExecutor) Start(context.Context, string, ...string) error { return nil }

func (*recordingExecutor) StartWithOptions(context.Context, command.StartOptions, string, ...string) error {
	return nil
}

func TestExecPowerControl(t *testing.T) {
	t.Parallel()

	t.Run("runs the configured command", func(t *testing.T) {
		t.Parallel()
		executor := &recordingExecutor{}
		control := &ExecPowerControl{
			Executor: executor,
			Commands: map[PowerAction][]string{PowerSuspend: {"pmset", "sleepnow"}},
		}
		actions := control.PowerActions(context.Background())
		assert.Equal(t, map[PowerAction]Availability{PowerSuspend: Supported}, actions)

		commit, err := control.PreparePowerAction(context.Background(), PowerSuspend)
		require.NoError(t, err)
		assert.Empty(t, executor.ran, "nothing runs until commit")
		require.NoError(t, commit())
		assert.Equal(t, [][]string{{"pmset", "sleepnow"}}, executor.ran)
	})

	t.Run("an action without a command is unsupported", func(t *testing.T) {
		t.Parallel()
		control := &ExecPowerControl{
			Executor: &recordingExecutor{},
			Commands: map[PowerAction][]string{PowerReboot: {"reboot"}},
		}
		_, err := control.PreparePowerAction(context.Background(), PowerShutdown)
		require.ErrorIs(t, err, ErrUnsupported)
	})

	t.Run("an unprivileged process is not permitted", func(t *testing.T) {
		t.Parallel()
		control := &ExecPowerControl{
			Executor:  &recordingExecutor{},
			Commands:  map[PowerAction][]string{PowerReboot: {"reboot"}},
			Permitted: func() bool { return false },
		}
		actions := control.PowerActions(context.Background())
		assert.Equal(t, map[PowerAction]Availability{PowerReboot: NotPermitted}, actions)
		_, err := control.PreparePowerAction(context.Background(), PowerReboot)
		require.ErrorIs(t, err, ErrNotPermitted)
	})

	t.Run("a failing command is reported by commit", func(t *testing.T) {
		t.Parallel()
		control := &ExecPowerControl{
			Executor: &recordingExecutor{err: errors.New("exit status 1")},
			Commands: map[PowerAction][]string{PowerReboot: {"reboot"}},
		}
		commit, err := control.PreparePowerAction(context.Background(), PowerReboot)
		require.NoError(t, err)
		require.Error(t, commit())
	})
}

func TestNoPowerControl(t *testing.T) {
	t.Parallel()

	control := NoPowerControl{}
	assert.Empty(t, control.PowerActions(context.Background()))
	_, err := control.PreparePowerAction(context.Background(), PowerReboot)
	require.ErrorIs(t, err, ErrUnsupported)
}
