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

package mocks

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/stretchr/testify/mock"
)

// MockCommandExecutor is a testify mock for command.Executor.
// It allows testing code that executes system commands without actually running them.
type MockCommandExecutor struct {
	mock.Mock
}

// Run mocks the execution of a system command.
// Use On() to set expectations and Return() to control the mock behavior.
//
// Example:
//
//	mockCmd := &MockCommandExecutor{}
//	mockCmd.On("Run", mock.Anything, "systemctl", mock.Anything).Return(nil)
func (m *MockCommandExecutor) Run(ctx context.Context, name string, args ...string) error {
	called := m.Called(ctx, name, args)
	//nolint:wrapcheck // Mock returns are already wrapped by caller
	return called.Error(0)
}

// Output mocks command execution that returns output.
// Use On() to set expectations and Return() to control the mock behavior.
//
// Example:
//
//	mockCmd := &MockCommandExecutor{}
//	mockCmd.On("Output", mock.Anything, "xprop", mock.Anything).Return([]byte("output"), nil)
func (m *MockCommandExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	called := m.Called(ctx, name, args)
	var output []byte
	if v := called.Get(0); v != nil {
		output = v.([]byte) //nolint:revive // type assertion is safe, mock always returns []byte
	}
	//nolint:wrapcheck // Mock returns are already wrapped by caller
	return output, called.Error(1)
}

// Start mocks starting a command without waiting for completion.
// Use On() to set expectations and Return() to control the mock behavior.
//
// Example:
//
//	mockCmd := &MockCommandExecutor{}
//	mockCmd.On("Start", mock.Anything, "steam", mock.Anything).Return(nil)
func (m *MockCommandExecutor) Start(ctx context.Context, name string, args ...string) error {
	called := m.Called(ctx, name, args)
	//nolint:wrapcheck // Mock returns are already wrapped by caller
	return called.Error(0)
}

// StartWithOptions mocks starting a command with platform-specific options.
// Use On() to set expectations and Return() to control the mock behavior.
//
// Example:
//
//	mockCmd := &MockCommandExecutor{}
//	opts := command.StartOptions{HideWindow: true}
//	mockCmd.On("StartWithOptions", mock.Anything, opts, "cmd", mock.Anything).Return(nil)
func (m *MockCommandExecutor) StartWithOptions(
	ctx context.Context,
	opts command.StartOptions,
	name string,
	args ...string,
) error {
	called := m.Called(ctx, opts, name, args)
	//nolint:wrapcheck // Mock returns are already wrapped by caller
	return called.Error(0)
}
