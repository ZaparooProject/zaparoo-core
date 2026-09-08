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

package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadersScanAllowRelaunch(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, option string
		want         bool
	}{
		{name: "existing config adopts new default"},
		{name: "explicit false", option: "allow_relaunch = false"},
		{name: "legacy opt out", option: "allow_relaunch = true", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			dir := t.TempDir()
			cfg, err := NewConfigWithFs(dir, BaseDefaults, fs)
			require.NoError(t, err)
			path := filepath.Join(dir, CfgFile)
			require.NoError(t, afero.WriteFile(fs, path,
				[]byte("config_schema = 1\n[readers.scan]\nmode = 'tap'\n"+tt.option+"\n"), 0o600))
			require.NoError(t, cfg.Load())
			assert.Equal(t, tt.want, cfg.ReadersScan().AllowRelaunch)
			require.NoError(t, cfg.Save())
			reloaded, err := NewConfigWithFs(dir, BaseDefaults, fs)
			require.NoError(t, err)
			require.NoError(t, reloaded.Load())
			assert.Equal(t, tt.want, reloaded.ReadersScan().AllowRelaunch)
		})
	}
}
