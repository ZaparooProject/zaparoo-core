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

package restart

import (
	"os"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBinaryPath_WithAppEnv(t *testing.T) {
	t.Setenv(config.AppEnv, "/usr/bin/zaparoo")

	path, err := BinaryPath()
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/zaparoo", path)
}

func TestBinaryPath_WithoutAppEnv(t *testing.T) {
	t.Setenv(config.AppEnv, "")

	path, err := BinaryPath()
	require.NoError(t, err)

	exe, err := os.Executable()
	require.NoError(t, err)
	assert.Equal(t, exe, path)
}

func TestBinaryPath_AppEnvTakesPrecedence(t *testing.T) {
	t.Setenv(config.AppEnv, "/custom/path/zaparoo")

	path, err := BinaryPath()
	require.NoError(t, err)
	assert.Equal(t, "/custom/path/zaparoo", path)

	exe, err := os.Executable()
	require.NoError(t, err)
	assert.NotEqual(t, exe, path)
}
