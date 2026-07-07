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

package gameid

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	upstream "github.com/ZaparooProject/go-gameid"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/require"
)

func TestConsoleForSystem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		system string
		want   upstream.Console
		wantOK bool
	}{
		{"psx", systemdefs.SystemPSX, upstream.ConsolePSX, true},
		{"megacd", systemdefs.SystemMegaCD, upstream.ConsoleSegaCD, true},
		{"segacd alias", "SegaCD", upstream.ConsoleSegaCD, true},
		{"unsupported", systemdefs.SystemNES, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ConsoleForSystem(tt.system)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestShouldIndexPath(t *testing.T) {
	t.Parallel()

	require.True(t, ShouldIndexPath(filepath.Join("games", "psx", "game.cue")))
	require.True(t, ShouldIndexPath(filepath.Join("games", "psx", "game.CHD")))
	require.True(t, ShouldIndexPath(filepath.Join("games", "gc", "game.gcm")))
	require.True(t, ShouldIndexPath(filepath.Join("games", "gc", "game.iso")))
	require.False(t, ShouldIndexPath(filepath.Join("games", "psx", "track.bin")))

	// go-gameid has no decompression support for these compressed container
	// formats, so identification always fails; they must not be indexed.
	require.False(t, ShouldIndexPath(filepath.Join("games", "gc", "game.gcz")))
	require.False(t, ShouldIndexPath(filepath.Join("games", "gc", "game.rvz")))
	require.False(t, ShouldIndexPath(filepath.Join("games", "psp", "game.cso")))
}

func TestIdentifyLiveDisc_UsesDetectedConsoleOnly(t *testing.T) {
	t.Parallel()

	isoPath := filepath.Join(t.TempDir(), "disc.iso")
	require.NoError(t, os.WriteFile(isoPath, minimalPSXISO("SCES-01420", "SCES_014.20;1"), 0o600))

	candidates := IdentifyLiveDisc(isoPath)
	require.Equal(t, []Candidate{{SystemID: systemdefs.SystemPSX, ID: "SCES-01420"}}, candidates)
}

func TestIdentifyLiveDisc_DetectFailureReturnsNoCandidates(t *testing.T) {
	t.Parallel()

	require.Empty(t, IdentifyLiveDisc(filepath.Join(t.TempDir(), "missing.iso")))
}

func minimalPSXISO(volumeID, serialFile string) []byte {
	const blockSize = 2048
	data := make([]byte, 21*blockSize)
	pvdOffset := 16 * blockSize
	pathTableOffset := 18 * blockSize
	rootOffset := 19 * blockSize

	data[pvdOffset] = 0x01
	copy(data[pvdOffset+1:], "CD001")
	data[pvdOffset+6] = 0x01
	copy(data[pvdOffset+8:], "PLAYSTATION")
	copy(data[pvdOffset+40:], volumeID)
	binary.LittleEndian.PutUint32(data[pvdOffset+80:], 21)
	binary.BigEndian.PutUint32(data[pvdOffset+84:], 21)
	binary.LittleEndian.PutUint16(data[pvdOffset+120:], 1)
	binary.BigEndian.PutUint16(data[pvdOffset+122:], 1)
	binary.LittleEndian.PutUint16(data[pvdOffset+124:], 1)
	binary.BigEndian.PutUint16(data[pvdOffset+126:], 1)
	binary.LittleEndian.PutUint16(data[pvdOffset+128:], blockSize)
	binary.BigEndian.PutUint16(data[pvdOffset+130:], blockSize)
	binary.LittleEndian.PutUint32(data[pvdOffset+132:], 10)
	binary.BigEndian.PutUint32(data[pvdOffset+136:], 10)
	binary.LittleEndian.PutUint32(data[pvdOffset+140:], 18)
	writeDirRecord(data[pvdOffset+156:], 19, blockSize, 0x02, "\x00")
	copy(data[pvdOffset+813:], "1998102813221100")

	data[pathTableOffset] = 1
	binary.LittleEndian.PutUint32(data[pathTableOffset+2:], 19)
	binary.LittleEndian.PutUint16(data[pathTableOffset+6:], 1)

	offset := 0
	offset += writeDirRecord(data[rootOffset+offset:], 19, blockSize, 0x02, "\x00")
	offset += writeDirRecord(data[rootOffset+offset:], 19, blockSize, 0x02, "\x01")
	writeDirRecord(data[rootOffset+offset:], 20, 0, 0x00, serialFile)

	return data
}

func writeDirRecord(dst []byte, lba, size uint32, flags byte, name string) int {
	nameLen := len(name)
	recordLen := 33 + nameLen
	if recordLen%2 == 1 {
		recordLen++
	}
	dst[0] = byte(recordLen)
	binary.LittleEndian.PutUint32(dst[2:], lba)
	binary.BigEndian.PutUint32(dst[6:], lba)
	binary.LittleEndian.PutUint32(dst[10:], size)
	binary.BigEndian.PutUint32(dst[14:], size)
	dst[25] = flags
	dst[28] = 1
	binary.BigEndian.PutUint16(dst[30:], 1)
	dst[32] = byte(nameLen)
	copy(dst[33:], name)
	return recordLen
}

func TestNormalizeID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "SLUS-12345", NormalizeID(" slus_12345 "))
}
