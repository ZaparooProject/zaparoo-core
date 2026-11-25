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
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/cdrom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTestSectorRead = errors.New("test sector read failed")

// fakeSectorDisc serves synthetic 2048-byte sectors by absolute LBA. Sectors
// that were never written read as zeros; sectors in fail return an error.
type fakeSectorDisc struct {
	sectors map[int][]byte
	fail    map[int]bool
	onRead  func(lba int)
	reads   []int
	failAll bool
	honour  bool
}

func newFakeSectorDisc() *fakeSectorDisc {
	return &fakeSectorDisc{sectors: map[int][]byte{}, fail: map[int]bool{}}
}

func (d *fakeSectorDisc) sector(lba int) []byte {
	s, ok := d.sectors[lba]
	if !ok {
		s = make([]byte, cdrom.DataSectorSize)
		d.sectors[lba] = s
	}
	return s
}

func (d *fakeSectorDisc) read(ctx context.Context, lba int, p []byte) error {
	d.reads = append(d.reads, lba)
	if d.onRead != nil {
		d.onRead(lba)
	}
	if d.honour {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if d.failAll || d.fail[lba] {
		return errTestSectorRead
	}
	if len(p) != cdrom.DataSectorSize {
		panic("sector reads must be exactly one data sector")
	}
	if s, ok := d.sectors[lba]; ok {
		copy(p, s)
	} else {
		clear(p)
	}
	return nil
}

func (d *fakeSectorDisc) readsIn(first, last int) int {
	n := 0
	for _, lba := range d.reads {
		if lba >= first && lba <= last {
			n++
		}
	}
	return n
}

// dataTOC is a single-track data disc whose data track starts at base.
func dataTOC(base int) cdrom.TOC {
	return cdrom.TOC{
		Tracks:  []cdrom.Track{{Number: 1, Start: base, End: base + 100000, Data: true}},
		LeadOut: base + 100000,
	}
}

// pceTOC is the PC Engine CD layout: an audio warning track, then data.
func pceTOC() cdrom.TOC {
	return cdrom.TOC{
		Tracks: []cdrom.Track{
			{Number: 1, Start: 0, End: 3000},
			{Number: 2, Start: 3000, End: 90000, Data: true},
			{Number: 3, Start: 90000, End: 120000},
		},
		LeadOut: 120000,
	}
}

// writeSignatureRoot writes an ISO9660 primary volume descriptor at base+16
// whose root directory is at base+extent and holds names.
func (d *fakeSectorDisc) writeSignatureRoot(base, extent int, size uint32, names ...string) {
	pvd := d.sector(base + 16)
	pvd[0] = iso9660DescriptorTypePrimary
	copy(pvd[1:6], "CD001")
	pvd[6] = 1
	copy(pvd[8:40], "                                ")
	writeTestISO9660DirectoryRecord(pvd[iso9660RootDirectoryRecordOffset:],
		uint32(extent), size, iso9660DirectoryFlag, []byte{0}) //nolint:gosec // Small test extent.

	lba := base + extent
	dir := d.sector(lba)
	ext := uint32(extent) //nolint:gosec // Small test extent.
	off := writeTestISO9660DirectoryRecord(dir, ext, size, iso9660DirectoryFlag, []byte{0})
	off += writeTestISO9660DirectoryRecord(dir[off:], ext, size, iso9660DirectoryFlag, []byte{1})
	for i, name := range names {
		recLen := iso9660FileIdentifierOffset + len(name) + len(name)%2 + 1
		if off+recLen > cdrom.DataSectorSize {
			lba++
			dir = d.sector(lba)
			off = 0
		}
		fileExt := ext + 10 + uint32(i) //nolint:gosec // Small test index.
		off += writeTestISO9660DirectoryRecord(dir[off:], fileExt, 100, 0, []byte(name))
	}
}

func TestIdentifyDiscSystem(t *testing.T) {
	t.Parallel()

	const base = 0
	tests := []struct {
		setup func(t *testing.T, d *fakeSectorDisc)
		name  string
		want  string
		toc   cdrom.TOC
	}{
		{
			name: "mega cd",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				copy(d.sector(base), "SEGADISCSYSTEM  SEGABOOTDISC")
			},
			want: systemdefs.SystemMegaCD,
		},
		{
			name: "saturn",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				copy(d.sector(base), "SEGA SEGASATURN SEGA ENTERPRISES")
			},
			want: systemdefs.SystemSaturn,
		},
		{
			name: "3do",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				copy(d.sector(base), []byte{0x01, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x01, 0x00})
			},
			want: systemdefs.System3DO,
		},
		{
			name: "psx",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				d.writeSignatureRoot(base, 22, iso9660SectorSize, "SYSTEM.CNF;1", "SLUS_007.70;1")
				copy(d.sector(base + 16)[8:], "PLAYSTATION                     ")
			},
			want: systemdefs.SystemPSX,
		},
		{
			name: "neo geo cd descriptor",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				pvd := d.sector(base + 16)
				pvd[0] = iso9660DescriptorTypePrimary
				copy(pvd[1:6], "CD001")
				copy(pvd[8:], "NGCD")
			},
			want: systemdefs.SystemNeoGeoCD,
		},
		{
			name: "neo geo cd boot file",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				d.writeSignatureRoot(base, 22, iso9660SectorSize, "ABS.TXT;1")
				copy(d.sector(base + 23)[300:], "IPL.TXT;1")
			},
			want: systemdefs.SystemNeoGeoCD,
		},
		{
			name: "cd-i descriptor",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				copy(d.sector(base+16), "\x01CD-I \x01")
			},
			want: systemdefs.SystemCDI,
		},
		{
			name: "cd-i application in the last scanned sector",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				copy(d.sector(base + signatureScanLast)[2040:], "CDI_APPL")
			},
			want: systemdefs.SystemCDI,
		},
		{
			name: "pc engine cd header in the second sector",
			toc:  pceTOC(),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				copy(d.sector(3001)[0x20:], "PC Engine CD-ROM SYSTEM")
			},
			want: systemdefs.SystemTurboGrafx16CD,
		},
		{
			name: "pc engine cd header straddling two sectors",
			toc:  pceTOC(),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				header := "PC Engine CD-ROM SYSTEM"
				copy(d.sector(3000)[cdrom.DataSectorSize-9:], header[:9])
				copy(d.sector(3001), header[9:])
			},
			want: systemdefs.SystemTurboGrafx16CD,
		},
		{
			name: "md+ pair in the root",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				d.writeSignatureRoot(base, 22, iso9660SectorSize, "README.TXT;1", "SONIC.CUE;1", "sonic.md;1")
			},
			want: systemdefs.SystemGenesisPlus,
		},
		{
			name: "md+ wins over a mega cd header",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				copy(d.sector(base), "SEGADISCSYSTEM")
				d.writeSignatureRoot(base, 22, iso9660SectorSize, "GAME.MD;1", "GAME.CUE;1")
			},
			want: systemdefs.SystemGenesisPlus,
		},
		{
			name: "md+ pair split across root sectors",
			toc:  dataTOC(base),
			setup: func(t *testing.T, d *fakeSectorDisc) {
				names := make([]string, 0, 80)
				names = append(names, "GAME.MD;1")
				for range 70 {
					names = append(names, "PADDING_FILE.BIN;1")
				}
				names = append(names, "GAME.CUE;1")
				d.writeSignatureRoot(base, 22, 2*iso9660SectorSize, names...)
				require.Contains(t, d.sectors, base+23, "the root spans two sectors")
			},
			want: systemdefs.SystemGenesisPlus,
		},
		{
			name: "snes msu-1 sfc",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				d.writeSignatureRoot(base, 22, iso9660SectorSize, "GAME-1.PCM;1", "GAME.MSU;1", "GAME.SFC;1")
			},
			want: systemdefs.SystemSNESMSU1,
		},
		{
			name: "snes msu-1 smc",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				d.writeSignatureRoot(base, 22, iso9660SectorSize, "game.smc")
			},
			want: systemdefs.SystemSNESMSU1,
		},
		{
			name: "boot signature wins over an msu-1 rom",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				copy(d.sector(base), "SEGA SEGASATURN")
				d.writeSignatureRoot(base, 22, iso9660SectorSize, "GAME.SFC;1")
			},
			want: systemdefs.SystemSaturn,
		},
		{
			name: "data track after audio uses its own start",
			toc: cdrom.TOC{
				Tracks: []cdrom.Track{
					{Number: 1, Start: 0, End: 500},
					{Number: 2, Start: 500, End: 9000, Data: true},
				},
				LeadOut: 9000,
			},
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				copy(d.sector(0), "SEGADISCSYSTEM")
				d.writeSignatureRoot(500, 22, iso9660SectorSize, "GAME.SFC;1")
			},
			want: systemdefs.SystemSNESMSU1,
		},
		{
			name: "unknown data disc",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				d.writeSignatureRoot(base, 22, iso9660SectorSize, "SETUP.EXE;1")
			},
			want: "",
		},
		{
			name: "md without a matching cue",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				d.writeSignatureRoot(base, 22, iso9660SectorSize, "GAME.MD;1", "OTHER.CUE;1")
			},
			want: "",
		},
		{
			name: "cd001 without a known system id",
			toc:  dataTOC(base),
			setup: func(_ *testing.T, d *fakeSectorDisc) {
				d.writeSignatureRoot(base, 22, iso9660SectorSize)
				copy(d.sector(base + 16)[8:], "WIN32")
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := newFakeSectorDisc()
			tt.setup(t, d)
			got, complete := identifyDiscSystem(context.Background(), tt.toc, d.read)
			assert.Equal(t, tt.want, got)
			assert.True(t, complete)
		})
	}
}

func TestIdentifyDiscSystemAudioDiscReadsNothing(t *testing.T) {
	t.Parallel()

	d := newFakeSectorDisc()
	toc := cdrom.TOC{Tracks: []cdrom.Track{{Number: 1, Start: 0, End: 1000}}, LeadOut: 1000}
	got, complete := identifyDiscSystem(context.Background(), toc, d.read)
	assert.Empty(t, got)
	assert.True(t, complete)
	assert.Empty(t, d.reads)

	got, complete = identifyDiscSystem(context.Background(), cdrom.TOC{}, d.read)
	assert.Empty(t, got)
	assert.True(t, complete)
	assert.Empty(t, d.reads)
}

func TestIdentifyDiscSystemReadErrors(t *testing.T) {
	t.Parallel()

	t.Run("every read fails", func(t *testing.T) {
		t.Parallel()
		d := newFakeSectorDisc()
		d.failAll = true
		got, complete := identifyDiscSystem(context.Background(), dataTOC(0), d.read)
		assert.Empty(t, got)
		assert.False(t, complete)
	})

	t.Run("one scanned sector fails", func(t *testing.T) {
		t.Parallel()
		d := newFakeSectorDisc()
		d.fail[30] = true
		got, complete := identifyDiscSystem(context.Background(), dataTOC(0), d.read)
		assert.Empty(t, got)
		assert.False(t, complete, "a later read might still find the signature")
	})

	t.Run("a failed sector does not stop the scan", func(t *testing.T) {
		t.Parallel()
		d := newFakeSectorDisc()
		d.fail[20] = true
		copy(d.sector(25)[100:], "IPL.TXT")
		got, complete := identifyDiscSystem(context.Background(), dataTOC(0), d.read)
		assert.Equal(t, systemdefs.SystemNeoGeoCD, got)
		assert.True(t, complete)
	})

	t.Run("root unreadable but boot sector matches", func(t *testing.T) {
		t.Parallel()
		d := newFakeSectorDisc()
		d.fail[16] = true
		copy(d.sector(0), "SEGA SEGASATURN")
		got, complete := identifyDiscSystem(context.Background(), dataTOC(0), d.read)
		assert.Equal(t, systemdefs.SystemSaturn, got)
		assert.True(t, complete)
	})

	t.Run("root directory unreadable", func(t *testing.T) {
		t.Parallel()
		d := newFakeSectorDisc()
		d.writeSignatureRoot(0, 22, iso9660SectorSize, "GAME.MD;1", "GAME.CUE;1")
		d.fail[22] = true
		got, complete := identifyDiscSystem(context.Background(), dataTOC(0), d.read)
		assert.Empty(t, got)
		assert.False(t, complete)
	})

	t.Run("pc engine second sector fails", func(t *testing.T) {
		t.Parallel()
		d := newFakeSectorDisc()
		copy(d.sector(3000)[cdrom.DataSectorSize-9:], "PC Engine")
		d.fail[3001] = true
		got, complete := identifyDiscSystem(context.Background(), pceTOC(), d.read)
		assert.Empty(t, got)
		assert.False(t, complete)
	})

	t.Run("msu-1 still reported after an unrelated failure", func(t *testing.T) {
		t.Parallel()
		d := newFakeSectorDisc()
		d.writeSignatureRoot(0, 22, iso9660SectorSize, "GAME.SFC;1")
		d.fail[35] = true
		got, complete := identifyDiscSystem(context.Background(), dataTOC(0), d.read)
		assert.Equal(t, systemdefs.SystemSNESMSU1, got)
		assert.True(t, complete)
	})
}

func TestIdentifyDiscSystemCancelled(t *testing.T) {
	t.Parallel()

	t.Run("before the first read, reader ignores context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		d := newFakeSectorDisc()
		copy(d.sector(25), "IPL.TXT")
		got, complete := identifyDiscSystem(ctx, dataTOC(0), d.read)
		assert.Empty(t, got)
		assert.False(t, complete)
		assert.Zero(t, d.readsIn(signatureScanFirst+1, signatureScanLast), "the sector scan never starts")
	})

	t.Run("before the first read, reader honours context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		d := newFakeSectorDisc()
		d.honour = true
		got, complete := identifyDiscSystem(ctx, dataTOC(0), d.read)
		assert.Empty(t, got)
		assert.False(t, complete)
	})

	t.Run("during the sector scan", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		d := newFakeSectorDisc()
		copy(d.sector(35), "CDI_APPL")
		d.onRead = func(lba int) {
			if lba == 20 {
				cancel()
			}
		}
		got, complete := identifyDiscSystem(ctx, dataTOC(0), d.read)
		assert.Empty(t, got)
		assert.False(t, complete)
		assert.Zero(t, d.readsIn(21, signatureScanLast), "no reads after cancellation")
	})

	t.Run("during the root scan", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		d := newFakeSectorDisc()
		d.writeSignatureRoot(0, 100, 50*iso9660SectorSize, "GAME.SFC;1")
		d.onRead = func(lba int) {
			if lba == 100 {
				cancel()
			}
		}
		root, err := scanSignatureRoot(ctx, 0, d.read)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 1, d.readsIn(100, 149), "the root scan stops after the cancelling read")
		assert.False(t, root.mdPlus)
	})
}

func TestScanSignatureRootBoundsDirectory(t *testing.T) {
	t.Parallel()

	d := newFakeSectorDisc()
	d.writeSignatureRoot(0, 100, 0xFFFFFFFF)
	root, err := scanSignatureRoot(context.Background(), 0, d.read)
	require.NoError(t, err)
	assert.Equal(t, signatureRoot{}, root)
	assert.Equal(t, maxSignatureRootBytes/cdrom.DataSectorSize, d.readsIn(100, 1<<30))
}

func TestScanSignatureRootNameLimit(t *testing.T) {
	t.Parallel()

	// More .md names than are remembered: a cue matching a forgotten name
	// is not an MD+ pair.
	names := make([]string, 0, maxSignatureRootNames+2)
	for i := range maxSignatureRootNames {
		names = append(names, "A"+string(rune('A'+i/26))+string(rune('A'+i%26))+".MD;1")
	}
	names = append(names, "LATE.MD;1", "LATE.CUE;1")
	d := newFakeSectorDisc()
	d.writeSignatureRoot(0, 22, 4*iso9660SectorSize, names...)
	root, err := scanSignatureRoot(context.Background(), 0, d.read)
	require.NoError(t, err)
	assert.False(t, root.mdPlus)
}

func TestScanSignatureRootIgnoresBadDescriptors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup func(d *fakeSectorDisc)
		name  string
	}{
		{name: "blank", setup: func(*fakeSectorDisc) {}},
		{
			name: "not primary",
			setup: func(d *fakeSectorDisc) {
				d.writeSignatureRoot(0, 22, iso9660SectorSize, "GAME.SFC;1")
				d.sector(16)[0] = iso9660DescriptorTypeSupplementary
			},
		},
		{
			name: "short root record",
			setup: func(d *fakeSectorDisc) {
				d.writeSignatureRoot(0, 22, iso9660SectorSize, "GAME.SFC;1")
				d.sector(16)[iso9660RootDirectoryRecordOffset] = iso9660DirectoryRecordMinSize - 1
			},
		},
		{
			name: "zero extent",
			setup: func(d *fakeSectorDisc) {
				d.writeSignatureRoot(0, 22, iso9660SectorSize, "GAME.SFC;1")
				root := d.sector(16)[iso9660RootDirectoryRecordOffset:]
				binary.LittleEndian.PutUint32(root[iso9660ExtentLocationOffset:], 0)
			},
		},
		{
			name: "zero size",
			setup: func(d *fakeSectorDisc) {
				d.writeSignatureRoot(0, 22, iso9660SectorSize, "GAME.SFC;1")
				root := d.sector(16)[iso9660RootDirectoryRecordOffset:]
				binary.LittleEndian.PutUint32(root[iso9660DataLengthOffset:], 0)
			},
		},
		{
			name: "record running past the sector",
			setup: func(d *fakeSectorDisc) {
				off := writeRootNearSectorEnd(d, "A.SFC")
				d.sector(22)[off] = byte(cdrom.DataSectorSize - off + 8) //nolint:gosec // Under 255 here.
			},
		},
		{
			name: "name longer than its record",
			setup: func(d *fakeSectorDisc) {
				d.writeSignatureRoot(0, 22, iso9660SectorSize, "A.SFC")
				dir := d.sector(22)
				off := 2 * 34
				dir[off+iso9660FileIdentifierLengthOffset] = 200
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := newFakeSectorDisc()
			tt.setup(d)
			root, err := scanSignatureRoot(context.Background(), 0, d.read)
			require.NoError(t, err)
			assert.Equal(t, signatureRoot{}, root)
		})
	}
}

// writeRootNearSectorEnd fills the first root sector at LBA 22 with long
// records, then writes name as the last record, returning its offset.
func writeRootNearSectorEnd(d *fakeSectorDisc, name string) int {
	d.writeSignatureRoot(0, 22, iso9660SectorSize)
	dir := d.sector(22)
	off := 68 // After the self and parent records.
	filler := make([]byte, 220)
	for i := range filler {
		filler[i] = 'F'
	}
	for range 7 {
		off += writeTestISO9660DirectoryRecord(dir[off:], 30, 1, 0, filler)
	}
	writeTestISO9660DirectoryRecord(dir[off:], 30, 1, 0, []byte(name))
	return off
}

func TestScanSignatureRootReadsToSectorEnd(t *testing.T) {
	t.Parallel()

	d := newFakeSectorDisc()
	writeRootNearSectorEnd(d, "A.SFC")
	root, err := scanSignatureRoot(context.Background(), 0, d.read)
	require.NoError(t, err)
	assert.True(t, root.msu1)
}

func writeTestISO9660DirectoryRecord(
	destination []byte,
	extentLocation uint32,
	dataLength uint32,
	flags uint8,
	identifier []byte,
) int {
	if len(identifier) > int(^uint8(0)) {
		panic("test ISO9660 identifier exceeds byte-sized field")
	}
	identifierLength := byte(len(identifier)) //nolint:gosec // Length is bounds-checked above.

	recordLength := iso9660FileIdentifierOffset + len(identifier)
	if recordLength%2 != 0 {
		recordLength++
	}
	if recordLength > int(^uint8(0)) {
		panic("test ISO9660 directory record exceeds byte-sized field")
	}
	recordLengthByte := byte(recordLength)

	record := destination[:recordLength]
	record[0] = recordLengthByte
	binary.LittleEndian.PutUint32(record[2:6], extentLocation)
	binary.BigEndian.PutUint32(record[6:10], extentLocation)
	binary.LittleEndian.PutUint32(record[10:14], dataLength)
	binary.BigEndian.PutUint32(record[14:18], dataLength)
	record[iso9660FileFlagsOffset] = flags
	binary.LittleEndian.PutUint16(record[28:30], 1)
	binary.BigEndian.PutUint16(record[30:32], 1)
	record[iso9660FileIdentifierLengthOffset] = identifierLength
	copy(record[iso9660FileIdentifierOffset:], identifier)
	return recordLength
}
