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

package mediascanner

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestStagedPropertiesFromPath_IdentifiesDiscImage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.iso")
	require.NoError(t, os.WriteFile(path, minimalScannerPSXISO("SCES-01420", "SCES_014.20;1"), 0o600))

	mockDB := &helpers.MockMediaDBI{}
	mockDB.On(
		"HasMediaPropertyForPath", mock.Anything, systemdefs.SystemPSX, path, string(tags.TagPropertyGameID),
	).Return(false, nil).Once()

	props := stagedPropertiesFromPath(mockDB, systemdefs.SystemPSX, path)

	require.Equal(t, []database.ScanStagedProperty{{
		Type: string(tags.TagTypeProperty),
		Name: string(tags.TagPropertyGameID),
		Text: "SCES-01420",
	}}, props)
	mockDB.AssertExpectations(t)
}

func TestStagedPropertiesFromPath_PropertyCheckErrorStillIdentifies(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.iso")
	require.NoError(t, os.WriteFile(path, minimalScannerPSXISO("SCES-01420", "SCES_014.20;1"), 0o600))

	mockDB := &helpers.MockMediaDBI{}
	mockDB.On(
		"HasMediaPropertyForPath", mock.Anything, systemdefs.SystemPSX, path, string(tags.TagPropertyGameID),
	).Return(false, context.Canceled).Once()

	props := stagedPropertiesFromPath(mockDB, systemdefs.SystemPSX, path)

	require.Len(t, props, 1)
	require.Equal(t, "SCES-01420", props[0].Text)
	mockDB.AssertExpectations(t)
}

func minimalScannerPSXISO(volumeID, serialFile string) []byte {
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
	writeScannerDirRecord(data[pvdOffset+156:], 19, blockSize, 0x02, "\x00")
	copy(data[pvdOffset+813:], "1998102813221100")

	data[pathTableOffset] = 1
	binary.LittleEndian.PutUint32(data[pathTableOffset+2:], 19)
	binary.LittleEndian.PutUint16(data[pathTableOffset+6:], 1)

	offset := 0
	offset += writeScannerDirRecord(data[rootOffset+offset:], 19, blockSize, 0x02, "\x00")
	offset += writeScannerDirRecord(data[rootOffset+offset:], 19, blockSize, 0x02, "\x01")
	writeScannerDirRecord(data[rootOffset+offset:], 20, 0, 0x00, serialFile)

	return data
}

func writeScannerDirRecord(dst []byte, lba, size uint32, flags byte, name string) int {
	nameLen := len(name)
	recordLen := 33 + nameLen
	if recordLen%2 == 1 {
		recordLen++
	}
	dst[0] = checkedScannerByte(recordLen)
	binary.LittleEndian.PutUint32(dst[2:], lba)
	binary.BigEndian.PutUint32(dst[6:], lba)
	binary.LittleEndian.PutUint32(dst[10:], size)
	binary.BigEndian.PutUint32(dst[14:], size)
	dst[25] = flags
	dst[28] = 1
	binary.BigEndian.PutUint16(dst[30:], 1)
	dst[32] = checkedScannerByte(nameLen)
	copy(dst[33:], name)
	return recordLen
}

func checkedScannerByte(value int) byte {
	if value < 0 || value > 255 {
		panic("value does not fit in byte")
	}
	return byte(value)
}
