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

package zapscript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/stretchr/testify/assert"
)

func TestReadPlsFile(t *testing.T) {
	tests := []struct {
		name           string
		plsContent     string
		expectedErrMsg string
		expectedMedia  []playlists.PlaylistItem
	}{
		{
			name: "valid_pls_with_multiple_entries",
			plsContent: `[playlist]
File1=/path/to/song1.mp3
Title1=Song 1
File2=/path/to/song2.mp3
Title2=Song 2`,
			expectedMedia: []playlists.PlaylistItem{
				{Name: "Song 1", ZapScript: "/path/to/song1.mp3"},
				{Name: "Song 2", ZapScript: "/path/to/song2.mp3"},
			},
			expectedErrMsg: "",
		},
		{
			name: "valid_pls_with_missing_titles",
			plsContent: `[playlist]
File1=/path/to/song1.mp3
File2=/path/to/song2.mp3`,
			expectedMedia: []playlists.PlaylistItem{
				{Name: "", ZapScript: "/path/to/song1.mp3"},
				{Name: "", ZapScript: "/path/to/song2.mp3"},
			},
			expectedErrMsg: "",
		},
		{
			name: "valid_pls_with_missing_files",
			plsContent: `[playlist]
Title1=Song 1
File2=/path/to/song2.mp3`,
			expectedMedia: []playlists.PlaylistItem{
				{Name: "", ZapScript: "/path/to/song2.mp3"},
			},
			expectedErrMsg: "",
		},
		{
			name: "missing_header",
			plsContent: `File1=/path/to/song1.mp3
Title1=Song 1
File2=/path/to/song2.mp3
Title2=Song 2`,
			expectedMedia:  nil,
			expectedErrMsg: "no items found in pls file",
		},
		{
			name: "empty_pls_file",
			plsContent: `
			`,
			expectedMedia:  nil,
			expectedErrMsg: "no items found in pls file",
		},
		{
			name: "invalid_entry_ids",
			plsContent: `[playlist]
FileA=/path/to/song1.mp3
TitleB=Song 1`,
			expectedMedia:  nil,
			expectedErrMsg: "no items found in pls file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plsFile := filepath.Join(t.TempDir(), "test.pls")
			err := os.WriteFile(plsFile, []byte(tt.plsContent), 0o600)
			assert.NoError(t, err)

			media, err := readPlsFile(plsFile)
			if tt.expectedErrMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrMsg)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedMedia, media)
			}
		})
	}
}
