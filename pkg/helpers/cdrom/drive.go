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

package cdrom

import "context"

// Drive is an open optical drive. Status and MediaChanged only ask the drive
// for its state; they never read the disc, so they make no noise.
type Drive interface {
	// Status reports the tray and media state.
	Status() (Status, error)
	// MediaChanged reports whether the media changed since the last call.
	MediaChanged() (bool, error)
	// IsDVD reports whether the loaded disc is a DVD.
	IsDVD() bool
	// ReadTOC reads the table of contents.
	ReadTOC() (TOC, error)
	// ReadCDTextPacks reads raw CD-Text packs for ParseCDText.
	ReadCDTextPacks() ([]byte, error)
	// ReadAtContext reads 2048-byte user data at a byte offset, as a
	// mounted filesystem would see it.
	ReadAtContext(ctx context.Context, p []byte, off int64) (int, error)
	// ReadDataSector reads the user data of one data sector into p, which
	// must be DataSectorSize long. It falls back to raw reads for discs the
	// block layer will not read.
	ReadDataSector(ctx context.Context, lba int, p []byte) error
	// ReadAudio reads count CD-DA frames starting at lba into p, which must
	// be count*RawSectorSize long: 16-bit little-endian stereo at 44.1 kHz.
	ReadAudio(ctx context.Context, lba, count int, p []byte) error
	Close() error
}
