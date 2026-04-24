//go:build linux

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

package mgls

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadMRA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mraContent  string
		wantSetName string
		wantName    string
		wantRbf     string
		wantErr     bool
	}{
		{
			name: "valid MRA with all fields",
			mraContent: `<?xml version="1.0" encoding="UTF-8"?>
<misterromdescription>
	<setname>sf2</setname>
	<name>Street Fighter II - The World Warrior</name>
	<rbf>jt1943</rbf>
	<rom index="0" zip="sf2.zip" md5="none">
		<part name="sf2_23.8f" crc="3f846b74"/>
	</rom>
</misterromdescription>`,
			wantSetName: "sf2",
			wantName:    "Street Fighter II - The World Warrior",
			wantRbf:     "jt1943",
			wantErr:     false,
		},
		{
			name: "valid MRA with minimal fields",
			mraContent: `<?xml version="1.0" encoding="UTF-8"?>
<misterromdescription>
	<setname>pacman</setname>
	<name>Pac-Man</name>
</misterromdescription>`,
			wantSetName: "pacman",
			wantName:    "Pac-Man",
			wantRbf:     "",
			wantErr:     false,
		},
		{
			name: "MRA with only setname",
			mraContent: `<?xml version="1.0" encoding="UTF-8"?>
<misterromdescription>
	<setname>dkong</setname>
</misterromdescription>`,
			wantSetName: "dkong",
			wantName:    "",
			wantRbf:     "",
			wantErr:     false,
		},
		{
			name: "MRA with complex game name",
			mraContent: `<?xml version="1.0" encoding="UTF-8"?>
<misterromdescription>
	<setname>mk2</setname>
	<name>Mortal Kombat II (rev L3.1)</name>
	<rbf>midway</rbf>
</misterromdescription>`,
			wantSetName: "mk2",
			wantName:    "Mortal Kombat II (rev L3.1)",
			wantRbf:     "midway",
			wantErr:     false,
		},
		{
			name:        "invalid XML",
			mraContent:  `<misterromdescription><setname>broken`,
			wantSetName: "",
			wantName:    "",
			wantRbf:     "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create temp file with MRA content
			tmpDir := t.TempDir()
			mraPath := filepath.Join(tmpDir, "test.mra")
			err := os.WriteFile(mraPath, []byte(tt.mraContent), 0o600)
			require.NoError(t, err)

			// Read MRA
			mra, err := ReadMRA(mraPath)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantSetName, mra.SetName)
			assert.Equal(t, tt.wantName, mra.Name)
			assert.Equal(t, tt.wantRbf, mra.Rbf)
		})
	}
}

func TestReadMRA_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := ReadMRA("/nonexistent/path/test.mra")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat MRA file")
}

func TestReadMRA_EmptyFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	mraPath := filepath.Join(tmpDir, "empty.mra")
	err := os.WriteFile(mraPath, []byte(""), 0o600)
	require.NoError(t, err)

	_, err = ReadMRA(mraPath)
	require.Error(t, err)
}

func TestGenerateMgl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		core     *cores.Core
		path     string
		override string
		want     string
		wantErr  bool
	}{
		{
			name:    "nil core returns error",
			core:    nil,
			path:    "/path/to/game.nes",
			wantErr: true,
		},
		{
			name: "core only (no path)",
			core: &cores.Core{
				ID:  "NES",
				RBF: "_Console/NES",
			},
			path: "",
			want: "<mistergamedescription>\n\t<rbf>_Console/NES</rbf>\n</mistergamedescription>",
		},
		{
			name: "core with setname",
			core: &cores.Core{
				ID:      "FDS",
				SetName: "FDS",
				RBF:     "_Console/NES",
			},
			path: "",
			want: "<mistergamedescription>\n\t<rbf>_Console/NES</rbf>\n" +
				"\t<setname>FDS</setname>\n</mistergamedescription>",
		},
		{
			name: "core with setname and same_dir",
			core: &cores.Core{
				ID:             "Atari2600",
				SetName:        "Atari2600",
				RBF:            "_Console/Atari7800",
				SetNameSameDir: true,
			},
			path: "",
			want: "<mistergamedescription>\n\t<rbf>_Console/Atari7800</rbf>\n" +
				"\t<setname same_dir=\"1\">Atari2600</setname>\n</mistergamedescription>",
		},
		{
			name: "standard game launch",
			core: &cores.Core{
				ID:  "NES",
				RBF: "_Console/NES",
				Slots: []cores.Slot{
					{
						Exts: []string{".nes"},
						Mgl: &cores.MGLParams{
							Delay:  2,
							Method: "f",
							Index:  1,
						},
					},
				},
			},
			path: "/media/fat/games/NES/Mario.nes",
			want: `<mistergamedescription>
	<rbf>_Console/NES</rbf>
	<file delay="2" type="f" index="1" path="../../../../../media/fat/games/NES/Mario.nes"/>
</mistergamedescription>`,
		},
		{
			name: "game launch with reset tag (Jaguar)",
			core: &cores.Core{
				ID:  "Jaguar",
				RBF: "_Console/Jaguar",
				Slots: []cores.Slot{
					{
						Exts: []string{".jag", ".j64", ".rom", ".bin"},
						Mgl: &cores.MGLParams{
							Delay:      1,
							Method:     "f",
							Index:      0,
							ResetDelay: 1,
							ResetHold:  1,
						},
					},
				},
			},
			path: "/media/fat/games/Jaguar/Tempest2000.jag",
			want: `<mistergamedescription>
	<rbf>_Console/Jaguar</rbf>
	<file delay="1" type="f" index="0" path="../../../../../media/fat/games/Jaguar/Tempest2000.jag"/>
	<reset delay="1" hold="1"/>
</mistergamedescription>`,
		},
		{
			name: "Saturn CHD with special characters in filename",
			core: &cores.Core{
				ID:  "Saturn",
				RBF: "_Console/Saturn",
				Slots: []cores.Slot{
					{
						Label: "Disk",
						Exts:  []string{".cue", ".chd"},
						Mgl: &cores.MGLParams{
							Delay:  2,
							Method: "s",
							Index:  0,
						},
					},
				},
			},
			path: "/media/fat/games/Saturn/America/NiGHTS into Dreams... (USA, Brazil).chd",
			want: "<mistergamedescription>\n\t<rbf>_Console/Saturn</rbf>\n" +
				"\t<file delay=\"2\" type=\"s\" index=\"0\" " +
				"path=\"../../../../../media/fat/games/Saturn/America/" +
				"NiGHTS into Dreams... (USA, Brazil).chd\"/>\n" +
				"</mistergamedescription>",
		},
		{
			name: "path with XML-reserved characters is escaped",
			core: &cores.Core{
				ID:  "Genesis",
				RBF: "_Console/MegaDrive",
				Slots: []cores.Slot{
					{
						Exts: []string{".bin", ".gen", ".md"},
						Mgl: &cores.MGLParams{
							Delay:  1,
							Method: "f",
							Index:  1,
						},
					},
				},
			},
			path: `/media/fat/games/MegaDrive/Sonic & Knuckles "Lock-On".bin`,
			want: "<mistergamedescription>\n" +
				"\t<rbf>_Console/MegaDrive</rbf>\n" +
				"\t<file delay=\"1\" type=\"f\" index=\"1\" " +
				"path=\"../../../../../media/fat/games/MegaDrive/" +
				"Sonic &amp; Knuckles &quot;Lock-On&quot;.bin\"/>\n" +
				"</mistergamedescription>",
		},
		{
			name: "override takes precedence over path",
			core: &cores.Core{
				ID:  "NES",
				RBF: "_Console/NES",
				Slots: []cores.Slot{
					{
						Exts: []string{".nes"},
						Mgl: &cores.MGLParams{
							Delay:  2,
							Method: "f",
							Index:  1,
						},
					},
				},
			},
			path:     "/media/fat/games/NES/Mario.nes",
			override: "\t<custom>override</custom>\n",
			want: `<mistergamedescription>
	<rbf>_Console/NES</rbf>
	<custom>override</custom>
</mistergamedescription>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rbfPath := ""
			if tt.core != nil {
				rbfPath = tt.core.RBF
			}
			got, err := GenerateMgl(tt.core, rbfPath, tt.path, tt.override)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWriteCurrentPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		path            string
		wantCurrentPath string
		wantFullPath    string
	}{
		{
			name:            "standard arcade MRA path",
			path:            "/media/fat/_Arcade/Pac-Man.mra",
			wantCurrentPath: "Pac-Man.mra",
			wantFullPath:    "/media/fat/_Arcade/Pac-Man.mra",
		},
		{
			name:            "MRA in subdirectory",
			path:            "/media/fat/_Arcade/cores/jotego/Street Fighter II.mra",
			wantCurrentPath: "Street Fighter II.mra",
			wantFullPath:    "/media/fat/_Arcade/cores/jotego/Street Fighter II.mra",
		},
		{
			name:            "USB path",
			path:            "/media/usb0/games/_Arcade/Donkey Kong.mra",
			wantCurrentPath: "Donkey Kong.mra",
			wantFullPath:    "/media/usb0/games/_Arcade/Donkey Kong.mra",
		},
		{
			name:            "bare filename without directory",
			path:            "game.mra",
			wantCurrentPath: "game.mra",
			wantFullPath:    "game.mra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			currentPathFile := filepath.Join(tmpDir, "CURRENTPATH")
			fullPathFile := filepath.Join(tmpDir, "FULLPATH")
			fileSelectFile := filepath.Join(tmpDir, "FILESELECT")

			writeCurrentPathTo(
				tt.path,
				currentPathFile,
				fullPathFile,
				fileSelectFile,
			)

			got, err := os.ReadFile(currentPathFile) //nolint:gosec // test file
			require.NoError(t, err)
			assert.Equal(t, tt.wantCurrentPath, string(got),
				"CURRENTPATH should be filename only, no trailing newline")

			got, err = os.ReadFile(fullPathFile) //nolint:gosec // test file
			require.NoError(t, err)
			assert.Equal(t, tt.wantFullPath, string(got),
				"FULLPATH should be the full path, no trailing newline")

			got, err = os.ReadFile(fileSelectFile) //nolint:gosec // test file
			require.NoError(t, err)
			assert.Equal(t, "selected", string(got),
				"FILESELECT should be exactly 'selected'")
		})
	}
}

func TestGenerateMgl_NoMatchingSlot(t *testing.T) {
	t.Parallel()

	core := &cores.Core{
		ID:  "NES",
		RBF: "_Console/NES",
		Slots: []cores.Slot{
			{
				Exts: []string{".nes"},
				Mgl: &cores.MGLParams{
					Delay:  2,
					Method: "f",
					Index:  1,
				},
			},
		},
	}

	// Try to launch a .sfc file with NES core - no matching slot
	_, err := GenerateMgl(core, core.RBF, "/media/fat/games/NES/game.sfc", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no matching mgl args")
}
