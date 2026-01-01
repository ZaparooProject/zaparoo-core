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
