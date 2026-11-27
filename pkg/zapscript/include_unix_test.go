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

//go:build unix

package zapscript

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// A named pipe in the folder is refused at once instead of holding the run
// until something writes to it.
func TestInclude_NamedPipeIsRefused(t *testing.T) {
	t.Parallel()
	pl, scripts := includePlatform(t)
	require.NoError(t, syscall.Mkfifo(filepath.Join(scripts, "pipe.txt"), 0o600))

	_, err := runInclude(pl, "pipe")
	require.ErrorIs(t, err, ErrScriptUnreadable)
}
