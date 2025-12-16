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

package helpers

import (
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/mock"
)

// NewMockCommandExecutor creates a MockCommandExecutor that succeeds by default.
// All Run(), Output(), and Start() calls will return success unless explicitly overridden with On().
//
// This provides sensible defaults for tests where command execution details don't matter.
// Override specific commands in tests that need to verify exact behavior:
//
//	cmd := helpers.NewMockCommandExecutor()
//	// Clear defaults first
//	cmd.ExpectedCalls = nil
//	// Set specific expectations (note: args is []string not variadic in mock)
//	cmd.On("Run", mock.Anything, "systemctl", []string{"--user", "daemon-reload"}).Return(nil)
//	cmd.On("Output", mock.Anything, "xprop", mock.Anything).Return([]byte("output"), nil)
func NewMockCommandExecutor() *mocks.MockCommandExecutor {
	cmd := &mocks.MockCommandExecutor{}
	// Match any command with any arguments - all succeed by default
	cmd.On("Run", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(nil).Maybe()
	cmd.On("Output", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return([]byte{}, nil).Maybe()
	cmd.On("Start", mock.Anything, mock.AnythingOfType("string"), mock.Anything).Return(nil).Maybe()
	cmd.On(
		"StartWithOptions", mock.Anything, mock.Anything, mock.AnythingOfType("string"), mock.Anything,
	).Return(nil).Maybe()
	return cmd
}
