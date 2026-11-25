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

//go:build linux

package opticaldrive

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/shared/sidecar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR" +
	"\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

// testISOEntry is one directory record in a testISO directory.
type testISOEntry struct {
	name   string
	extent uint32
	size   uint32
	flags  uint8
}

func testISOFile(name string, extent, size uint32) testISOEntry {
	return testISOEntry{name: name, extent: extent, size: size}
}

func testISODir(name string, extent uint32) testISOEntry {
	return testISOEntry{name: name, extent: extent, size: iso9660SectorSize, flags: iso9660DirectoryFlag}
}

// testISO builds an ISO9660 image sector by sector.
type testISO struct {
	img []byte
}

func newTestISO(sectors int) *testISO {
	return &testISO{img: make([]byte, sectors*iso9660SectorSize)}
}

func (b *testISO) sector(lba int) []byte {
	return b.img[lba*iso9660SectorSize : (lba+1)*iso9660SectorSize]
}

func jolietIdentifier(name string) []byte {
	units := utf16.Encode([]rune(name))
	out := make([]byte, 2*len(units))
	for i, u := range units {
		binary.BigEndian.PutUint16(out[2*i:], u)
	}
	return out
}

// descriptor writes a volume descriptor at lba whose root directory is
// rootLBA. escape is the supplementary descriptor's escape sequence.
func (b *testISO) descriptor(lba int, kind byte, rootLBA uint32, escape string) {
	desc := b.sector(lba)
	desc[0] = kind
	copy(desc[1:6], "CD001")
	desc[6] = 1
	copy(desc[iso9660VolumeIDOffset:iso9660VolumeIDOffset+iso9660VolumeIDSize], "TESTDISC")
	copy(desc[iso9660ModifiedOffset:], "2026010100000000")
	copy(desc[iso9660EscapeSequencesOffset:], escape)
	writeTestISO9660DirectoryRecord(desc[iso9660RootDirectoryRecordOffset:],
		rootLBA, iso9660SectorSize, iso9660DirectoryFlag, []byte{0})
}

func (b *testISO) primary(rootLBA uint32) {
	b.descriptor(16, iso9660DescriptorTypePrimary, rootLBA, "")
}

func (b *testISO) joliet(lba int, rootLBA uint32) {
	b.descriptor(lba, iso9660DescriptorTypeSupplementary, rootLBA, "%/E")
}

func (b *testISO) terminator(lba int) {
	desc := b.sector(lba)
	desc[0] = iso9660DescriptorTypeEnd
	copy(desc[1:6], "CD001")
}

// dir writes a one-sector directory at lba with self and parent entries.
func (b *testISO) dir(lba uint32, joliet bool, entries ...testISOEntry) {
	sector := b.sector(int(lba))
	off := writeTestISO9660DirectoryRecord(sector, lba, iso9660SectorSize, iso9660DirectoryFlag, []byte{0})
	off += writeTestISO9660DirectoryRecord(sector[off:], lba, iso9660SectorSize, iso9660DirectoryFlag, []byte{1})
	for _, e := range entries {
		id := []byte(e.name)
		if joliet {
			id = jolietIdentifier(e.name)
		}
		off += writeTestISO9660DirectoryRecord(sector[off:], e.extent, e.size, e.flags, id)
	}
}

func (b *testISO) file(lba uint32, data []byte) uint32 {
	copy(b.img[int(lba)*iso9660SectorSize:], data)
	return uint32(len(data)) //nolint:gosec // Test files are small.
}

func (b *testISO) reader() contextReaderAt {
	return testReaderAtContextAdapter{reader: bytes.NewReader(b.img)}
}

func (b *testISO) identity(t *testing.T) discIdentity {
	t.Helper()
	identity, found, err := readISO9660IdentityContext(context.Background(), b.reader())
	require.NoError(t, err)
	require.True(t, found)
	return identity
}

func testRootTree(lba uint32, joliet bool) iso9660Tree {
	return iso9660Tree{
		root:   iso9660DirectoryRecord{ExtentLocation: lba, DataLength: iso9660SectorSize, Flags: iso9660DirectoryFlag},
		joliet: joliet,
	}
}

// primarySidecarISO is a primary-only disc whose root holds zaparoo.json
// with the given contents, plus COVERS/FRONT.PNG and COVER.TXT.
func primarySidecarISO(json string, extra ...testISOEntry) *testISO {
	b := newTestISO(40)
	b.primary(20)
	b.terminator(17)
	jsonSize := b.file(30, []byte(json))
	pngSize := b.file(32, testPNG)
	txtSize := b.file(33, []byte("not a cover"))
	entries := make([]testISOEntry, 0, 3+len(extra))
	entries = append(entries,
		testISODir("COVERS", 22),
		testISOFile("COVER.TXT;1", 33, txtSize),
		testISOFile("ZAPAROO.JSON;1", 30, jsonSize),
	)
	b.dir(20, false, append(entries, extra...)...)
	b.dir(22, false, testISOFile("FRONT.PNG;1", 32, pngSize))
	return b
}

func TestReadISO9660IdentityPrefersJolietSidecar(t *testing.T) {
	t.Parallel()

	b := newTestISO(40)
	b.primary(20)
	b.joliet(17, 21)
	b.terminator(18)

	primaryJSON := b.file(30, []byte(`{"name":"Primary Name","image":"COVERS/FRONT.PNG"}`))
	jolietJSON := b.file(31, []byte(
		`{"name":"Joliet Name","description":"Long names","image":"Covers/Front Cover Ünïcode.png"}`))
	primaryPNG := b.file(32, []byte("\x89PNG primary"))
	jolietPNG := b.file(33, testPNG)

	b.dir(20, false, testISODir("COVERS", 22), testISOFile("ZAPAROO.JSON;1", 30, primaryJSON))
	b.dir(21, true, testISODir("Covers", 23), testISOFile("zaparoo.json;1", 31, jolietJSON))
	b.dir(22, false, testISOFile("FRONT.PNG;1", 32, primaryPNG))
	b.dir(23, true, testISOFile("Front Cover Ünïcode.png;1", 33, jolietPNG))

	identity := b.identity(t)
	assert.Equal(t, "TESTDISC", identity.Label)
	require.NotNil(t, identity.Sidecar)
	assert.Equal(t, sidecar.Fields{
		Name:        "Joliet Name",
		Description: "Long names",
		Image:       "Covers/Front Cover Ünïcode.png",
	}, identity.Sidecar.Fields)
	assert.Equal(t, testPNG, identity.Sidecar.Image)
	assert.Equal(t, ".png", identity.Sidecar.ImageExt)
}

func TestReadISO9660IdentityIgnoresNonJolietSupplementary(t *testing.T) {
	t.Parallel()

	b := primarySidecarISO(`{"name":"Primary Name"}`)
	// A supplementary descriptor without a Joliet escape sequence.
	b.descriptor(17, iso9660DescriptorTypeSupplementary, 21, "%/F")
	b.terminator(18)
	b.dir(21, true, testISOFile("zaparoo.json;1", 31, b.file(31, []byte(`{"name":"Wrong"}`))))

	identity := b.identity(t)
	require.NotNil(t, identity.Sidecar)
	assert.Equal(t, "Primary Name", identity.Sidecar.Fields.Name)
}

func TestReadISO9660IdentitySidecarPrimaryOnly(t *testing.T) {
	t.Parallel()

	b := primarySidecarISO(`{"name":"Disc Game","image":"covers/Front.PNG"}`)
	identity := b.identity(t)
	require.NotNil(t, identity.Sidecar)
	assert.Equal(t, "Disc Game", identity.Sidecar.Fields.Name)
	assert.Equal(t, "covers/Front.PNG", identity.Sidecar.Fields.Image)
	assert.Equal(t, testPNG, identity.Sidecar.Image)
	assert.Equal(t, ".png", identity.Sidecar.ImageExt, "the extension is lower-cased")
}

func TestReadISO9660IdentitySidecarImageRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		image string
		extra []testISOEntry
	}{
		{name: "parent escape", image: "../COVERS/FRONT.PNG"},
		{name: "parent inside the path", image: "COVERS/../COVERS/FRONT.PNG"},
		{name: "absolute path", image: "/COVERS/FRONT.PNG"},
		{name: "backslash path", image: `COVERS\\FRONT.PNG`},
		{name: "non-image extension", image: "COVER.TXT"},
		{name: "missing image", image: "COVERS/BACK.PNG"},
		{
			name:  "directory named like an image",
			image: "COVERS.PNG",
			extra: []testISOEntry{testISODir("COVERS.PNG", 22)},
		},
		{
			name:  "oversized image",
			image: "HUGE.PNG",
			extra: []testISOEntry{testISOFile("HUGE.PNG;1", 32, imagefiles.MaxBytes+1)},
		},
		{name: "empty image", image: "EMPTY.PNG", extra: []testISOEntry{testISOFile("EMPTY.PNG;1", 32, 0)}},
		{
			name:  "image past the end of the disc",
			image: "GONE.PNG",
			extra: []testISOEntry{testISOFile("GONE.PNG;1", 39, 3*iso9660SectorSize)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := primarySidecarISO(`{"name":"Kept Name","image":"`+tt.image+`"}`, tt.extra...)
			identity := b.identity(t)
			require.NotNil(t, identity.Sidecar, "the other fields survive a bad image")
			assert.Equal(t, "Kept Name", identity.Sidecar.Fields.Name)
			assert.Empty(t, identity.Sidecar.Fields.Image)
			assert.Nil(t, identity.Sidecar.Image)
			assert.Empty(t, identity.Sidecar.ImageExt)
		})
	}
}

func TestReadISO9660IdentitySidecarOnlyABadImageIsDropped(t *testing.T) {
	t.Parallel()

	b := primarySidecarISO(`{"image":"COVERS/BACK.PNG"}`)
	identity := b.identity(t)
	assert.Nil(t, identity.Sidecar)
}

func TestReadISO9660IdentitySidecarIgnored(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() *testISO
		name  string
	}{
		{
			name: "no zaparoo.json",
			build: func() *testISO {
				b := newTestISO(40)
				b.primary(20)
				b.dir(20, false, testISOFile("README.TXT;1", 30, b.file(30, []byte("hi"))))
				return b
			},
		},
		{name: "invalid json", build: func() *testISO { return primarySidecarISO(`{"name":`) }},
		{name: "json array", build: func() *testISO { return primarySidecarISO(`["name"]`) }},
		{name: "json null", build: func() *testISO { return primarySidecarISO(`null`) }},
		{name: "no known fields", build: func() *testISO { return primarySidecarISO(`{"other":"x"}`) }},
		{
			name: "zaparoo.json is a directory",
			build: func() *testISO {
				b := newTestISO(40)
				b.primary(20)
				b.dir(20, false, testISODir("ZAPAROO.JSON", 22))
				b.dir(22, false)
				return b
			},
		},
		{
			name: "zaparoo.json too large",
			build: func() *testISO {
				b := newTestISO(40)
				b.primary(20)
				b.dir(20, false, testISOFile("ZAPAROO.JSON;1", 30, sidecar.MaxBytes+1))
				return b
			},
		},
		{
			name: "zaparoo.json truncated",
			build: func() *testISO {
				b := newTestISO(40)
				b.primary(20)
				b.dir(20, false, testISOFile("ZAPAROO.JSON;1", 39, 2*iso9660SectorSize))
				return b
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			identity := tt.build().identity(t)
			assert.Equal(t, "TESTDISC", identity.Label, "the disc is still identified")
			assert.Nil(t, identity.Sidecar)
		})
	}
}

func TestISO9660Name(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ZAPAROO.JSON", iso9660Name([]byte("ZAPAROO.JSON;1"), false))
	assert.Equal(t, "README", iso9660Name([]byte("README.;1"), false))
	assert.Equal(t, "README", iso9660Name([]byte("README."), false))
	assert.Equal(t, "NOVERSION.BIN", iso9660Name([]byte("NOVERSION.BIN"), false))
	assert.Equal(t, "A;B", iso9660Name([]byte("A;B;1"), false), "only the last version suffix is dropped")
	assert.Equal(t, "Front Cover Ünïcode.png", iso9660Name(jolietIdentifier("Front Cover Ünïcode.png;1"), true))
	cjk := "\u65e5\u672c\u8a9e \U0001F3AE.png"
	assert.Equal(t, cjk, iso9660Name(jolietIdentifier(cjk), true), "surrogate pairs decode")
	assert.Equal(t, "A", iso9660Name([]byte{0x00, 'A', 0x00}, true), "an odd trailing byte is dropped")
	assert.Empty(t, iso9660Name(nil, true))
}

func TestIsJolietDescriptor(t *testing.T) {
	t.Parallel()

	for _, esc := range []string{"%/@", "%/C", "%/E"} {
		desc := make([]byte, iso9660SectorSize)
		copy(desc[iso9660EscapeSequencesOffset:], esc)
		assert.True(t, isJolietDescriptor(desc), esc)
	}
	for _, esc := range []string{"", "%/F", "%@E", "/%E"} {
		desc := make([]byte, iso9660SectorSize)
		copy(desc[iso9660EscapeSequencesOffset:], esc)
		assert.False(t, isJolietDescriptor(desc), esc)
	}
}

// lookupISO builds a primary tree: root(20) -> GAMES(21) -> SAVES(22), with
// files at each level.
func lookupISO() *testISO {
	b := newTestISO(40)
	b.primary(20)
	b.dir(20, false,
		testISODir("GAMES", 21),
		testISOFile("README.;1", 30, b.file(30, []byte("root readme"))),
	)
	b.dir(21, false,
		testISOFile("BOOT.BIN;1", 31, b.file(31, []byte("boot"))),
		testISODir("SAVES", 22),
	)
	b.dir(22, false, testISOFile("SLOT1.SAV;1", 32, b.file(32, []byte("slot one"))))
	return b
}

func TestLookupISO9660(t *testing.T) {
	t.Parallel()

	b := lookupISO()
	tree := testRootTree(20, false)
	ctx := context.Background()

	tests := []struct {
		name  string
		want  string
		parts []string
	}{
		{name: "root file without extension", parts: []string{"README"}, want: "root readme"},
		{name: "subfolder", parts: []string{"GAMES", "BOOT.BIN"}, want: "boot"},
		{name: "case-insensitive", parts: []string{"games", "Boot.Bin"}, want: "boot"},
		{name: "nested", parts: []string{"Games", "saves", "slot1.sav"}, want: "slot one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec, err := lookupISO9660(ctx, b.reader(), tree, tt.parts)
			require.NoError(t, err)
			data, err := readISO9660File(ctx, b.reader(), rec, 1024)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}
}

func TestLookupISO9660Misses(t *testing.T) {
	t.Parallel()

	b := lookupISO()
	tree := testRootTree(20, false)
	ctx := context.Background()

	tests := []struct {
		err   error
		name  string
		parts []string
	}{
		{name: "no parts", parts: nil, err: errISO9660NotFound},
		{name: "missing", parts: []string{"NOPE.TXT"}, err: errISO9660NotFound},
		{
			name:  "version suffix is not part of the name",
			parts: []string{"GAMES", "BOOT.BIN;1"},
			err:   errISO9660NotFound,
		},
		{name: "directory as the last part", parts: []string{"GAMES"}, err: errISO9660NotFile},
		{name: "file in the middle", parts: []string{"README", "X"}, err: errISO9660NotFound},
		{name: "missing in a subfolder", parts: []string{"GAMES", "SAVES", "SLOT2.SAV"}, err: errISO9660NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := lookupISO9660(ctx, b.reader(), tree, tt.parts)
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestLookupISO9660DepthLimit(t *testing.T) {
	t.Parallel()

	// Nested directories named D from the root at 20 down to 29.
	b := newTestISO(40)
	for i := range uint32(9) {
		b.dir(20+i, false, testISODir("D", 21+i))
	}
	tree := testRootTree(20, false)
	ctx := context.Background()

	atLimit := make([]string, 0, iso9660MaxPathDepth)
	for range iso9660MaxPathDepth - 1 {
		atLimit = append(atLimit, "D")
	}
	// Seven D components reach directory 27; give it a file too.
	b.dir(27, false, testISODir("D", 28), testISOFile("G.BIN;1", 36, b.file(36, []byte("at limit"))))
	rec, err := lookupISO9660(ctx, b.reader(), tree, append(atLimit, "G.BIN"))
	require.NoError(t, err)
	data, err := readISO9660File(ctx, b.reader(), rec, 64)
	require.NoError(t, err)
	assert.Equal(t, "at limit", string(data))

	// Eight D components reach directory 28; its file is one part too deep.
	b.dir(28, false, testISODir("D", 29), testISOFile("H.BIN;1", 37, b.file(37, []byte("too deep"))))
	tooDeep := []string{"D", "D", "D", "D", "D", "D", "D", "D", "H.BIN"}
	require.Len(t, tooDeep, iso9660MaxPathDepth+1)
	_, err = lookupISO9660(ctx, b.reader(), tree, tooDeep)
	require.ErrorIs(t, err, errISO9660NotFound)
}

func TestLookupISO9660Joliet(t *testing.T) {
	t.Parallel()

	b := newTestISO(40)
	b.dir(21, true,
		testISODir("Long Folder Name", 23),
		testISOFile("readme", 30, b.file(30, []byte("x"))),
	)
	b.dir(23, true, testISOFile("Ça Marche.JPEG;1", 31, b.file(31, []byte("jpeg"))))
	tree := testRootTree(21, true)

	rec, err := lookupISO9660(context.Background(), b.reader(), tree, []string{"long folder name", "ça marche.jpeg"})
	require.NoError(t, err)
	assert.Equal(t, uint32(31), rec.ExtentLocation)

	// The same tree read as 8-bit names does not match.
	_, err = lookupISO9660(context.Background(), b.reader(), testRootTree(21, false), []string{"readme"})
	require.ErrorIs(t, err, errISO9660NotFound)
}

func TestWalkISO9660Directory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("not a directory", func(t *testing.T) {
		t.Parallel()
		b := lookupISO()
		err := walkISO9660Directory(ctx, b.reader(), iso9660DirectoryRecord{ExtentLocation: 20, DataLength: 2048},
			func(iso9660DirectoryRecord) bool { return true })
		require.Error(t, err)
	})

	t.Run("directory size cap", func(t *testing.T) {
		t.Parallel()
		b := lookupISO()
		called := false
		root := iso9660DirectoryRecord{
			ExtentLocation: 20,
			DataLength:     iso9660MaxDirectoryBytes + 1,
			Flags:          iso9660DirectoryFlag,
		}
		err := walkISO9660Directory(ctx, b.reader(), root, func(iso9660DirectoryRecord) bool {
			called = true
			return true
		})
		require.ErrorIs(t, err, errISO9660TooLarge)
		assert.False(t, called, "nothing is read from an oversized directory")

		_, err = lookupISO9660(ctx, b.reader(), iso9660Tree{root: root}, []string{"GAMES"})
		require.ErrorIs(t, err, errISO9660TooLarge)
	})

	t.Run("directory at the size cap", func(t *testing.T) {
		t.Parallel()
		b := newTestISO(20 + iso9660MaxDirectoryBytes/iso9660SectorSize)
		b.dir(20, false, testISOFile("A.BIN;1", 30, 1))
		last := 20 + iso9660MaxDirectoryBytes/iso9660SectorSize - 1
		writeTestISO9660DirectoryRecord(b.sector(last), 31, 1, 0, []byte("LAST.BIN;1"))
		var names []string
		root := iso9660DirectoryRecord{
			ExtentLocation: 20,
			DataLength:     iso9660MaxDirectoryBytes,
			Flags:          iso9660DirectoryFlag,
		}
		err := walkISO9660Directory(ctx, b.reader(), root, func(rec iso9660DirectoryRecord) bool {
			names = append(names, iso9660Name(rec.Identifier, false))
			return true
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"A.BIN", "LAST.BIN"}, names)
	})

	t.Run("stops when visit says so", func(t *testing.T) {
		t.Parallel()
		b := lookupISO()
		visits := 0
		err := walkISO9660Directory(ctx, b.reader(), testRootTree(21, false).root, func(iso9660DirectoryRecord) bool {
			visits++
			return false
		})
		require.NoError(t, err)
		assert.Equal(t, 1, visits)
	})

	t.Run("record past the directory end", func(t *testing.T) {
		t.Parallel()
		b := lookupISO()
		root := iso9660DirectoryRecord{ExtentLocation: 20, DataLength: 100, Flags: iso9660DirectoryFlag}
		err := walkISO9660Directory(ctx, b.reader(), root, func(iso9660DirectoryRecord) bool { return true })
		require.ErrorIs(t, err, errISO9660BadRecord)
	})

	t.Run("record shorter than the minimum", func(t *testing.T) {
		t.Parallel()
		b := lookupISO()
		b.sector(20)[68] = 10
		err := walkISO9660Directory(ctx, b.reader(), testRootTree(20, false).root,
			func(iso9660DirectoryRecord) bool { return true })
		require.Error(t, err)
	})

	t.Run("directory past the end of the disc", func(t *testing.T) {
		t.Parallel()
		b := lookupISO()
		root := iso9660DirectoryRecord{
			ExtentLocation: 39,
			DataLength:     3 * iso9660SectorSize,
			Flags:          iso9660DirectoryFlag,
		}
		err := walkISO9660Directory(ctx, b.reader(), root, func(iso9660DirectoryRecord) bool { return true })
		require.Error(t, err)
	})

	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()
		b := lookupISO()
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := lookupISO9660(cctx, b.reader(), testRootTree(20, false), []string{"README"})
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestReadISO9660File(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newTestISO(40)
	b.file(30, []byte("0123456789"))

	data, err := readISO9660File(ctx, b.reader(), iso9660DirectoryRecord{ExtentLocation: 30, DataLength: 10}, 10)
	require.NoError(t, err)
	assert.Equal(t, "0123456789", string(data), "a file exactly at the cap is read")

	_, err = readISO9660File(ctx, b.reader(), iso9660DirectoryRecord{ExtentLocation: 30, DataLength: 11}, 10)
	require.ErrorIs(t, err, errISO9660TooLarge)

	_, err = readISO9660File(ctx, b.reader(), iso9660DirectoryRecord{ExtentLocation: 30, DataLength: 0xFFFFFFFF}, 10)
	require.ErrorIs(t, err, errISO9660TooLarge)

	_, err = readISO9660File(ctx, b.reader(),
		iso9660DirectoryRecord{ExtentLocation: 30, DataLength: 10, Flags: iso9660DirectoryFlag}, 10)
	require.ErrorIs(t, err, errISO9660NotFile)

	_, err = readISO9660File(ctx, b.reader(),
		iso9660DirectoryRecord{ExtentLocation: 30, DataLength: 10, Flags: iso9660MultiExtentFlag}, 10)
	require.ErrorIs(t, err, errISO9660NotFile)

	data, err = readISO9660File(ctx, b.reader(), iso9660DirectoryRecord{ExtentLocation: 30}, 10)
	require.NoError(t, err)
	assert.NotNil(t, data)
	assert.Empty(t, data)

	_, err = readISO9660File(ctx, b.reader(), iso9660DirectoryRecord{ExtentLocation: 39, DataLength: 4096}, 8192)
	require.Error(t, err, "a file running off the disc is an error")

	// The extended attribute length moves the data start.
	b.file(31, []byte("after xattr"))
	data, err = readISO9660File(ctx, b.reader(),
		iso9660DirectoryRecord{ExtentLocation: 30, ExtendedAttributeLength: 1, DataLength: 11}, 64)
	require.NoError(t, err)
	assert.Equal(t, "after xattr", string(data))
}

// Fuzz inputs describe an image sparsely, as chunks of a sector byte, a
// little-endian uint16 length and that many bytes written at the start of the
// sector. A real layout fits in a couple of KB that way, which keeps each exec
// cheap and lets the fuzzer minimize what it finds.
const (
	fuzzISOChunkHeader = 3
	fuzzISOMaxSectors  = 64
	fuzzISOMaxInput    = 8 << 10
)

// fuzzISOSeed encodes img's non-empty sectors, without their trailing zeros.
func fuzzISOSeed(img []byte) []byte {
	var seed []byte
	for sector := 0; sector < fuzzISOMaxSectors && (sector+1)*iso9660SectorSize <= len(img); sector++ {
		body := bytes.TrimRight(img[sector*iso9660SectorSize:(sector+1)*iso9660SectorSize], "\x00")
		if len(body) == 0 {
			continue
		}
		seed = append(seed, byte(sector))
		seed = binary.LittleEndian.AppendUint16(seed, uint16(len(body))) //nolint:gosec // At most a sector.
		seed = append(seed, body...)
	}
	return seed
}

// fuzzISOImage expands an input written by fuzzISOSeed, or mutated from one.
func fuzzISOImage(data []byte) []byte {
	var img []byte
	for len(data) >= fuzzISOChunkHeader {
		sector := int(data[0]) % fuzzISOMaxSectors
		n := min(int(binary.LittleEndian.Uint16(data[1:fuzzISOChunkHeader]))%(iso9660SectorSize+1),
			len(data)-fuzzISOChunkHeader)
		off := sector * iso9660SectorSize
		if need := off + iso9660SectorSize; len(img) < need {
			img = append(img, make([]byte, need-len(img))...)
		}
		copy(img[off:], data[fuzzISOChunkHeader:fuzzISOChunkHeader+n])
		data = data[fuzzISOChunkHeader+n:]
	}
	return img
}

func TestFuzzISOSeedRoundTrip(t *testing.T) {
	t.Parallel()

	img := primarySidecarISO(`{"name":"Disc Game","image":"COVERS/FRONT.PNG"}`).img
	want := bytes.TrimRight(img, "\x00")
	assert.Equal(t, want, bytes.TrimRight(fuzzISOImage(fuzzISOSeed(img)), "\x00"))
}

func FuzzISO9660Lookup(f *testing.F) {
	f.Add(fuzzISOSeed(primarySidecarISO(`{"name":"Disc Game","image":"COVERS/FRONT.PNG"}`).img))
	f.Add(fuzzISOSeed(lookupISO().img))
	joliet := primarySidecarISO(`{"name":"x"}`)
	joliet.joliet(17, 21)
	joliet.dir(21, true, testISOFile("zaparoo.json", 30, 40), testISODir("A", 22))
	f.Add(fuzzISOSeed(joliet.img))
	f.Add([]byte{})
	// A lone directory sector keeps small inputs in play for the raw walk.
	f.Add(bytes.TrimRight(lookupISO().sector(20), "\x00"))
	f.Add(bytes.Repeat([]byte{0xff}, 64))

	f.Fuzz(checkISO9660Input)
}

func checkISO9660Input(t *testing.T, data []byte) {
	const maxFileBytes = 4096
	if len(data) > fuzzISOMaxInput {
		return
	}
	ctx := context.Background()
	img := fuzzISOImage(data)
	identity, found, err := readISO9660IdentityContext(ctx, testReaderAtContextAdapter{reader: bytes.NewReader(img)})
	if (err != nil || !found) && identity.Sidecar != nil {
		t.Fatal("sidecar without an identity")
	}
	if sc := identity.Sidecar; sc != nil {
		if len(sc.Image) > imagefiles.MaxBytes {
			t.Fatalf("image is %d bytes, cap %d", len(sc.Image), imagefiles.MaxBytes)
		}
		if (sc.Image == nil) != (sc.Fields.Image == "") {
			t.Fatalf("image field %q does not match %d image bytes", sc.Fields.Image, len(sc.Image))
		}
		if sc.Image != nil && sc.ImageExt != ".png" && sc.ImageExt != ".jpg" && sc.ImageExt != ".jpeg" {
			t.Fatalf("unexpected image extension %q", sc.ImageExt)
		}
	}

	// Treat the input as a raw directory too, so short inputs reach the
	// record walker.
	r := testReaderAtContextAdapter{reader: bytes.NewReader(data)}
	size := uint32(min(len(data), iso9660MaxDirectoryBytes)) //nolint:gosec // Bounded above.
	for _, joliet := range []bool{false, true} {
		tree := iso9660Tree{
			root:   iso9660DirectoryRecord{DataLength: size, Flags: iso9660DirectoryFlag},
			joliet: joliet,
		}
		lookups := [][]string{{"A"}, {"A", "B.PNG"}, {"zaparoo.json"}, strings.Split("a/b/c/d/e/f/g/h", "/")}
		for _, parts := range lookups {
			rec, err := lookupISO9660(ctx, r, tree, parts)
			if err != nil {
				continue
			}
			if rec.Flags&iso9660DirectoryFlag != 0 {
				t.Fatalf("lookup %v returned a directory", parts)
			}
			out, err := readISO9660File(ctx, r, rec, maxFileBytes)
			if err == nil && len(out) > maxFileBytes {
				t.Fatalf("read %d bytes, cap %d", len(out), maxFileBytes)
			}
		}
	}
}
