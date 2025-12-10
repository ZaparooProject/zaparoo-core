// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package command

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRealExecutor_Run(t *testing.T) {
	t.Parallel()

	executor := &RealExecutor{}

	t.Run("executes_successful_command", func(t *testing.T) {
		t.Parallel()

		err := executor.Run(context.Background(), "true")

		assert.NoError(t, err)
	})

	t.Run("returns_error_for_failed_command", func(t *testing.T) {
		t.Parallel()

		err := executor.Run(context.Background(), "false")

		assert.Error(t, err)
	})

	t.Run("returns_error_for_nonexistent_command", func(t *testing.T) {
		t.Parallel()

		err := executor.Run(context.Background(), "nonexistent_command_that_should_not_exist_12345")

		require.Error(t, err)
	})
}

func TestRealExecutor_Start(t *testing.T) {
	t.Parallel()

	executor := &RealExecutor{}

	t.Run("starts_command_without_waiting", func(t *testing.T) {
		t.Parallel()

		err := executor.Start(context.Background(), "true")

		assert.NoError(t, err)
	})

	t.Run("returns_error_for_nonexistent_command", func(t *testing.T) {
		t.Parallel()

		err := executor.Start(context.Background(), "nonexistent_command_that_should_not_exist_12345")

		require.Error(t, err)
	})
}

func TestRealExecutor_StartWithOptions(t *testing.T) {
	t.Parallel()

	executor := &RealExecutor{}

	t.Run("starts_command_with_options", func(t *testing.T) {
		t.Parallel()

		opts := StartOptions{HideWindow: true}
		err := executor.StartWithOptions(context.Background(), opts, "true")

		assert.NoError(t, err)
	})

	t.Run("starts_command_without_hide_window", func(t *testing.T) {
		t.Parallel()

		opts := StartOptions{HideWindow: false}
		err := executor.StartWithOptions(context.Background(), opts, "true")

		assert.NoError(t, err)
	})

	t.Run("returns_error_for_nonexistent_command", func(t *testing.T) {
		t.Parallel()

		opts := StartOptions{}
		err := executor.StartWithOptions(context.Background(), opts, "nonexistent_command_that_should_not_exist_12345")

		require.Error(t, err)
	})
}

func TestExecutor_Interface(t *testing.T) {
	t.Parallel()

	// Verify that RealExecutor implements Executor
	var _ Executor = (*RealExecutor)(nil)
}
