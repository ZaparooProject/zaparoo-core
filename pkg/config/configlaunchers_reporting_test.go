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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type launcherReadFailureFS struct {
	afero.Fs
	unreadable string
}

func (fs launcherReadFailureFS) Open(name string) (afero.File, error) {
	if name == fs.unreadable {
		return nil, os.ErrPermission
	}
	file, err := fs.Fs.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open fixture: %w", err)
	}
	return file, nil
}

func TestCustomLauncherUnknownFieldReporting(t *testing.T) {
	// Global logger replacement requires serial subtests.
	for _, mode := range []string{"unknown", "valid_and_unknown", "syntax_and_unknown", "io_and_unknown"} {
		t.Run(mode, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			dir := filepath.Join("data", "launchers")
			require.NoError(t, fs.MkdirAll(dir, 0o750))
			unknown := filepath.Join(dir, "unknown.toml")
			unknownDoc := "[[launchers.custom]]\nid='Typo'\nexcute='PRIVATE_VALUE_CANARY'\n"
			require.NoError(t, afero.WriteFile(fs, unknown, []byte(unknownDoc), 0o600))
			cfg := &Instance{
				fs: fs, customLaunchersExternal: []LaunchersCustom{{ID: "Previous", Execute: "echo previous"}},
			}
			switch mode {
			case "valid_and_unknown":
				validDoc := "[[launchers.custom]]\nid='Valid'\nexecute='echo test'\n"
				require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "valid.toml"), []byte(validDoc), 0o600))
			case "syntax_and_unknown":
				require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "syntax.toml"), []byte("broken {{{"), 0o600))
			case "io_and_unknown":
				unreadable := filepath.Join(dir, "unreadable.toml")
				require.NoError(t, afero.WriteFile(fs, unreadable, []byte(""), 0o600))
				cfg.fs = launcherReadFailureFS{Fs: fs, unreadable: unreadable}
			}
			var output bytes.Buffer
			old, oldLevel := log.Logger, zerolog.GlobalLevel()
			zerolog.SetGlobalLevel(zerolog.WarnLevel)
			log.Logger = zerolog.New(&output).Level(zerolog.WarnLevel)
			t.Cleanup(func() {
				log.Logger = old
				zerolog.SetGlobalLevel(oldLevel)
			})
			err := cfg.LoadCustomLaunchers(dir)
			switch mode {
			case "unknown":
				require.ErrorIs(t, err, ErrCustomLauncherUnknownFields)
				require.EqualError(t, err, "failed to parse any custom launcher files")
				assert.NotContains(t, output.String(), `"level":"error"`)
			case "valid_and_unknown":
				require.NoError(t, err)
				assert.NotContains(t, output.String(), `"level":"error"`)
			default:
				require.Error(t, err)
				require.NotErrorIs(t, err, ErrCustomLauncherUnknownFields)
				assert.Contains(t, output.String(), `"level":"error"`)
			}
			assert.Contains(t, output.String(), `"level":"warn"`)
			assert.Contains(t, output.String(), "launchers.custom.excute")
			assert.Contains(t, output.String(), "line 3, column 1")
			assert.NotContains(t, output.String(), "PRIVATE_VALUE_CANARY")
			entries := cfg.CustomLaunchers()
			require.Len(t, entries, 1)
			if mode == "valid_and_unknown" {
				assert.Equal(t, "Valid", entries[0].ID)
			} else {
				assert.Equal(t, "Previous", entries[0].ID)
			}
		})
	}
}
