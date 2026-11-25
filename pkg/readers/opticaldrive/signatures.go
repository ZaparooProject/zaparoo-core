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

package opticaldrive

import (
	"bytes"
	"context"
	"encoding/binary"
	"path"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/cdrom"
)

const (
	// maxSignatureRootBytes bounds the root directory scanned for MD+ and
	// MSU-1 files.
	maxSignatureRootBytes = 1 << 20
	// maxSignatureRootNames bounds the .md and .cue names remembered.
	maxSignatureRootNames = 64
	// signatureScanFirst and signatureScanLast bound the sectors searched for
	// the Neo Geo CD and CD-i boot files.
	signatureScanFirst = 16
	signatureScanLast  = 40
)

// sectorReader reads the 2048 bytes of user data of one data sector.
type sectorReader func(ctx context.Context, lba int, p []byte) error

// identifyDiscSystem names the console a data disc boots on from its boot
// sectors, in the same order of checks the MiSTer Main fork used for disc
// auto-run. It returns "" when no signature matched. complete is false when
// a read failed, so a retry may still find one.
func identifyDiscSystem(ctx context.Context, toc cdrom.TOC, read sectorReader) (system string, complete bool) {
	first, ok := toc.FirstDataTrack()
	if !ok {
		return "", true
	}
	base := first.Start
	ioFailed := false
	sector := make([]byte, cdrom.DataSectorSize)

	root, rootErr := scanSignatureRoot(ctx, base, read)
	if rootErr != nil {
		ioFailed = true
	}
	if root.mdPlus {
		return systemdefs.SystemGenesisPlus, true
	}

	if err := read(ctx, base, sector); err == nil {
		switch {
		case bytes.HasPrefix(sector, []byte("SEGADISCSYSTEM")):
			return systemdefs.SystemMegaCD, true
		case bytes.HasPrefix(sector, []byte("SEGA SEGASATURN")):
			return systemdefs.SystemSaturn, true
		case bytes.HasPrefix(sector, []byte{0x01, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A}):
			return systemdefs.System3DO, true
		}
	} else {
		ioFailed = true
	}
	if ctx.Err() != nil {
		return "", false
	}

	if err := read(ctx, base+16, sector); err == nil {
		if string(sector[1:6]) == "CD001" {
			if bytes.HasPrefix(sector[8:], []byte("PLAYSTATION")) {
				return systemdefs.SystemPSX, true
			}
			if bytes.HasPrefix(sector[8:], []byte("NGCD")) {
				return systemdefs.SystemNeoGeoCD, true
			}
		}
		if string(sector[1:5]) == "CD-I" {
			return systemdefs.SystemCDI, true
		}
	} else {
		ioFailed = true
	}

	for offset := signatureScanFirst; offset <= signatureScanLast; offset++ {
		if ctx.Err() != nil {
			return "", false
		}
		if err := read(ctx, base+offset, sector); err != nil {
			ioFailed = true
			continue
		}
		if bytes.Contains(sector, []byte("IPL.TXT")) {
			return systemdefs.SystemNeoGeoCD, true
		}
		if bytes.Contains(sector, []byte("CDI_APPL")) {
			return systemdefs.SystemCDI, true
		}
	}

	// The PC Engine CD boot header sits in the second sector of the first
	// data track; it may straddle the first two.
	pce := make([]byte, 2*cdrom.DataSectorSize)
	errA := read(ctx, base, pce[:cdrom.DataSectorSize])
	errB := read(ctx, base+1, pce[cdrom.DataSectorSize:])
	if errA == nil && errB == nil {
		if bytes.Contains(pce, []byte("PC Engine CD-ROM SYSTEM")) {
			return systemdefs.SystemTurboGrafx16CD, true
		}
	} else {
		ioFailed = true
	}

	if root.msu1 {
		return systemdefs.SystemSNESMSU1, true
	}
	return "", !ioFailed
}

type signatureRoot struct {
	mdPlus bool
	msu1   bool
}

// scanSignatureRoot looks in the ISO9660 root of the first data track for an
// MD+ pair (a .md file with a .cue of the same name) or an MSU-1 ROM.
func scanSignatureRoot(ctx context.Context, base int, read sectorReader) (signatureRoot, error) {
	var out signatureRoot
	pvd := make([]byte, cdrom.DataSectorSize)
	if err := read(ctx, base+16, pvd); err != nil {
		return out, err
	}
	if pvd[0] != iso9660DescriptorTypePrimary || string(pvd[1:6]) != "CD001" {
		return out, nil
	}
	rootRecord := pvd[iso9660RootDirectoryRecordOffset:]
	if rootRecord[0] < iso9660DirectoryRecordMinSize {
		return out, nil
	}
	extent := int(binary.LittleEndian.Uint32(rootRecord[iso9660ExtentLocationOffset:]))
	// The length is capped before it becomes an int, which is 32 bits wide
	// on some builds.
	size := int(min(binary.LittleEndian.Uint32(rootRecord[iso9660DataLengthOffset:]), maxSignatureRootBytes))
	if extent == 0 || size == 0 {
		return out, nil
	}

	mdNames := make(map[string]struct{})
	cueNames := make(map[string]struct{})
	sector := make([]byte, cdrom.DataSectorSize)
	for done := 0; done < size; done += cdrom.DataSectorSize {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if err := read(ctx, base+extent+done/cdrom.DataSectorSize, sector); err != nil {
			return out, err
		}
		for off := 0; off < cdrom.DataSectorSize && done+off < size; {
			length := int(sector[off])
			if length == 0 {
				break
			}
			if length < iso9660DirectoryRecordMinSize || off+length > cdrom.DataSectorSize {
				break
			}
			// The record is at least the minimum size and ends inside the
			// sector, so its name length byte is in range.
			nameLen := int(sector[off+iso9660FileIdentifierLengthOffset]) //nolint:gosec // bounds checked above
			nameEnd := off + iso9660FileIdentifierOffset + nameLen
			if nameLen > 0 && nameEnd <= off+length {
				name := string(sector[off+iso9660FileIdentifierOffset : nameEnd])
				name, _, _ = strings.Cut(name, ";")
				ext := strings.ToLower(path.Ext(name))
				stem := strings.ToLower(strings.TrimSuffix(name, path.Ext(name)))
				switch ext {
				case ".sfc", ".smc":
					out.msu1 = true
				case ".md":
					if len(mdNames) < maxSignatureRootNames {
						mdNames[stem] = struct{}{}
					}
				case ".cue":
					if len(cueNames) < maxSignatureRootNames {
						cueNames[stem] = struct{}{}
					}
				}
			}
			off += length
		}
	}
	for name := range mdNames {
		if _, ok := cueNames[name]; ok {
			out.mdPlus = true
			break
		}
	}
	return out, nil
}
