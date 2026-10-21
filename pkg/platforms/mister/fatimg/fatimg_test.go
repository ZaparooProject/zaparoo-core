//go:build linux && !android

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

package fatimg

import (
	"encoding/binary"
	"path/filepath"
	"slices"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openBytes(t *testing.T, data []byte) (*Image, error) {
	t.Helper()
	fs := afero.NewMemMapFs()
	p := filepath.Join("img", "disk.vhd")
	require.NoError(t, afero.WriteFile(fs, p, data, 0o644))
	img, err := Open(fs, p)
	if img != nil {
		t.Cleanup(func() { _ = img.Close() })
	}
	return img, err
}

func mustVolume(t *testing.T, data []byte) *Volume {
	t.Helper()
	img, err := openBytes(t, data)
	require.NoError(t, err)
	vols := img.Volumes()
	require.NotEmpty(t, vols)
	return &vols[0]
}

func names(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func clone(b []byte) []byte { return slices.Clone(b) }

func layouts(v variant) map[string][]byte {
	base := stdImage(v)
	return map[string][]byte{
		"superfloppy": base,
		"mbr":         mbrImage(part{base, 0x0C}),
		"mbr+footer":  withVHDFooter(mbrImage(part{base, 0x0C})),
	}
}

func TestTypeDetectedByClusterCount(t *testing.T) {
	t.Parallel()
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			vol := mustVolume(t, stdImage(v))
			assert.Equal(t, v.typ, vol.Type)
			assert.Equal(t, "TESTVOL", vol.Label)
			assert.Equal(t, 0, vol.Index)
		})
	}
}

func TestTypeBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		typ      Type
		clusters int
		want     Type
	}{
		{"4084 is fat12", FAT12, 4084, FAT12},
		{"4085 is fat16", FAT16, 4085, FAT16},
		{"65524 is fat16", FAT16, 65524, FAT16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newTFS(tt.typ, tt.clusters, 1, 512)
			img := f.format("", []*tNode{{short: "A.TXT", data: []byte("x")}})
			vol := mustVolume(t, img)
			assert.Equal(t, tt.want, vol.Type)
			assert.Empty(t, vol.Label)
			data, err := vol.ReadFile("a.txt", 10)
			require.NoError(t, err)
			assert.Equal(t, []byte("x"), data)
		})
	}
}

func TestStandardTree(t *testing.T) {
	t.Parallel()
	for _, v := range variants {
		for lname, img := range layouts(v) {
			t.Run(v.name+"/"+lname, func(t *testing.T) {
				t.Parallel()
				vol := mustVolume(t, img)
				assert.Equal(t, v.typ, vol.Type)
				assert.Equal(t, "TESTVOL", vol.Label)

				root, err := vol.ReadDir("/")
				require.NoError(t, err)
				assert.Equal(t, []string{"Games", "BIG", "ReadMe.txt", "MULTI.BIN", "EMPTY.DAT"}, names(root))
				assert.True(t, root[0].IsDir)
				assert.Equal(t, "GAMES", root[0].ShortName)
				assert.Equal(t, uint32(9), root[2].Size)
				assert.Equal(t, "README.TXT", root[2].ShortName)

				games, err := vol.ReadDir("games")
				require.NoError(t, err)
				assert.Equal(t, []string{
					"Super Mario Bros.nes", "zelda.nes", "BADSUM~1.NES", "ünï\U0001F600.bin",
				}, names(games))
				assert.Equal(t, "SUPERM~1.NES", games[0].ShortName)
				assert.Equal(t, "zelda.nes", games[1].ShortName)
				assert.Equal(t, uint32(1500), games[0].Size)

				big, err := vol.ReadDir("/BIG/")
				require.NoError(t, err)
				assert.Len(t, big, 40)
				assert.Equal(t, "F39.TXT", big[39].Name)

				data, err := vol.ReadFile("/MULTI.BIN", 4096)
				require.NoError(t, err)
				assert.Equal(t, pattern(3000, 9), data)

				data, err = vol.ReadFile("/Games/Super Mario Bros.nes", 4096)
				require.NoError(t, err)
				assert.Equal(t, pattern(1500, 1), data)

				data, err = vol.ReadFile("/EMPTY.DAT", 1)
				require.NoError(t, err)
				assert.Empty(t, data)
			})
		}
	}
}

func TestCaseInsensitiveLookup(t *testing.T) {
	t.Parallel()
	vol := mustVolume(t, stdImage(variants[1]))
	for _, p := range []string{
		"/GAMES/SUPERM~1.NES", "/games/superm~1.nes", "/Games/SUPER MARIO BROS.NES",
		"games/super mario bros.nes", "/Games/./Super Mario Bros.nes",
	} {
		data, err := vol.ReadFile(p, 4096)
		require.NoError(t, err, p)
		assert.Equal(t, pattern(1500, 1), data, p)
	}
	data, err := vol.ReadFile("/games/ZELDA.NES", 4096)
	require.NoError(t, err)
	assert.Equal(t, pattern(700, 2), data)
}

func TestLFNChecksumMismatchIgnored(t *testing.T) {
	t.Parallel()
	vol := mustVolume(t, stdImage(variants[1]))
	games, err := vol.ReadDir("games")
	require.NoError(t, err)
	var found bool
	for _, e := range games {
		if e.ShortName == "BADSUM~1.NES" {
			found = true
			assert.Equal(t, "BADSUM~1.NES", e.Name)
		}
		assert.NotEqual(t, "Old Game.nes", e.Name)
		assert.NotEqual(t, "OLDGAM~1.NES", e.ShortName)
	}
	assert.True(t, found)
	_, err = vol.ReadFile("games/bad sum.nes", 100)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestPathErrors(t *testing.T) {
	t.Parallel()
	vol := mustVolume(t, stdImage(variants[0]))

	_, err := vol.ReadDir("/nope")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = vol.ReadDir("/readme.txt")
	require.ErrorIs(t, err, ErrNotDir)
	_, err = vol.ReadDir("/readme.txt/x")
	require.ErrorIs(t, err, ErrNotDir)
	_, err = vol.ReadDir("/games/../big")
	require.ErrorIs(t, err, ErrInvalidPath)
	_, err = vol.ReadFile("/games", 100)
	require.ErrorIs(t, err, ErrIsDir)
	_, err = vol.ReadFile("/multi.bin", 2999)
	require.ErrorIs(t, err, ErrTooLarge)
	_, err = vol.ReadFile("/multi.bin", -1)
	require.ErrorIs(t, err, ErrInvalidPath)
	data, err := vol.ReadFile("/multi.bin", 3000)
	require.NoError(t, err)
	assert.Len(t, data, 3000)
}

func TestMBRTwoPartitions(t *testing.T) {
	t.Parallel()
	a := newTFS(FAT16, 4200, 1, 512).format("ONE", []*tNode{{short: "A.TXT", data: []byte("first")}})
	b := newTFS(FAT12, 300, 2, 512).format("TWO", []*tNode{{short: "B.TXT", data: []byte("second")}})
	img, err := openBytes(t, withVHDFooter(mbrImage(part{a, 0x06}, part{b, 0x01})))
	require.NoError(t, err)

	vols := img.Volumes()
	require.Len(t, vols, 2)
	assert.Equal(t, Volume{Index: 0, Label: "ONE", Type: FAT16}, stripInternal(vols[0]))
	assert.Equal(t, Volume{Index: 1, Label: "TWO", Type: FAT12}, stripInternal(vols[1]))

	data, err := vols[0].ReadFile("a.txt", 10)
	require.NoError(t, err)
	assert.Equal(t, []byte("first"), data)
	data, err = vols[1].ReadFile("b.txt", 10)
	require.NoError(t, err)
	assert.Equal(t, []byte("second"), data)
	_, err = vols[1].ReadFile("a.txt", 10)
	require.ErrorIs(t, err, ErrNotFound)
}

func stripInternal(v Volume) Volume {
	v.v = nil
	return v
}

func TestMBRIgnoresNonFATTypes(t *testing.T) {
	t.Parallel()
	a := newTFS(FAT16, 4200, 1, 512).format("ONE", nil)
	_, err := openBytes(t, mbrImage(part{a, 0x83}))
	require.ErrorIs(t, err, ErrNoFilesystem)
}

func TestExtendedPartition(t *testing.T) {
	t.Parallel()
	a := newTFS(FAT12, 300, 2, 512).format("LOG1", []*tNode{{short: "A.TXT", data: []byte("one")}})
	b := newTFS(FAT12, 300, 2, 512).format("LOG2", []*tNode{{short: "B.TXT", data: []byte("two")}})

	const extLBA = 2048
	img := make([]byte, (extLBA+4096+64)*512+len(b))
	put := func(sector []byte, n int, typ byte, rel, count uint32) {
		e := sector[446+n*16:]
		e[4] = typ
		binary.LittleEndian.PutUint32(e[8:], rel)
		binary.LittleEndian.PutUint32(e[12:], count)
	}
	put(img[:512], 0, 0x0F, extLBA, 8192)
	img[510], img[511] = 0x55, 0xAA
	ebr1 := img[extLBA*512 : extLBA*512+512]
	put(ebr1, 0, 0x06, 64, u32(len(a)/512))
	put(ebr1, 1, 0x05, 4096, 64)
	ebr1[510], ebr1[511] = 0x55, 0xAA
	copy(img[(extLBA+64)*512:], a)
	ebr2 := img[(extLBA+4096)*512 : (extLBA+4096)*512+512]
	put(ebr2, 0, 0x06, 64, u32(len(b)/512))
	ebr2[510], ebr2[511] = 0x55, 0xAA
	copy(img[(extLBA+4096+64)*512:], b)

	im, err := openBytes(t, img)
	require.NoError(t, err)
	vols := im.Volumes()
	require.Len(t, vols, 2)
	assert.Equal(t, "LOG1", vols[0].Label)
	assert.Equal(t, "LOG2", vols[1].Label)
	data, err := vols[1].ReadFile("b.txt", 10)
	require.NoError(t, err)
	assert.Equal(t, []byte("two"), data)
}

func TestWalkPaths(t *testing.T) {
	t.Parallel()
	vol := mustVolume(t, stdImage(variants[1]))
	var got []Path
	require.NoError(t, vol.Walk("/games", WalkOptions{}, func(p Path, _ Entry) error {
		got = append(got, p)
		return nil
	}))
	assert.Equal(t, []Path{
		{"/Games/Super Mario Bros.nes", "/GAMES/SUPERM~1.NES"},
		{"/Games/zelda.nes", "/GAMES/zelda.nes"},
		{"/Games/BADSUM~1.NES", "/GAMES/BADSUM~1.NES"},
		{"/Games/ünï\U0001F600.bin", "/GAMES/N~1.BIN"},
	}, got)

	var all []string
	require.NoError(t, vol.Walk("", WalkOptions{}, func(p Path, _ Entry) error {
		all = append(all, p.Long)
		return nil
	}))
	assert.Len(t, all, 5+4+40)
	assert.Equal(t, "/Games", all[0])
	assert.Equal(t, "/Games/Super Mario Bros.nes", all[1])

	require.ErrorIs(t, vol.Walk("/readme.txt", WalkOptions{}, nil), ErrNotDir)
	require.ErrorIs(t, vol.Walk("/zzz", WalkOptions{}, nil), ErrNotFound)
}

func TestWalkDepthLimit(t *testing.T) {
	t.Parallel()
	vol := mustVolume(t, stdImage(variants[0]))
	var all []string
	require.NoError(t, vol.Walk("", WalkOptions{MaxDepth: 1}, func(p Path, _ Entry) error {
		all = append(all, p.Long)
		return nil
	}))
	assert.Equal(t, []string{"/Games", "/BIG", "/ReadMe.txt", "/MULTI.BIN", "/EMPTY.DAT"}, all)

	deep := &tNode{short: "L0", dir: true}
	cur := deep
	for i := 1; i < 10; i++ {
		n := &tNode{short: "L" + string(rune('0'+i)), dir: true}
		cur.kids = []*tNode{n}
		cur = n
	}
	img := newTFS(FAT12, 300, 1, 512).format("", []*tNode{deep})
	dv := mustVolume(t, img)
	count := 0
	require.NoError(t, dv.Walk("", WalkOptions{MaxDepth: 4}, func(Path, Entry) error { count++; return nil }))
	assert.Equal(t, 4, count)
	count = 0
	require.NoError(t, dv.Walk("", WalkOptions{}, func(Path, Entry) error { count++; return nil }))
	assert.Equal(t, 10, count)
}

func TestWalkEntryLimit(t *testing.T) {
	t.Parallel()
	vol := mustVolume(t, stdImage(variants[0]))
	count := 0
	err := vol.Walk("", WalkOptions{MaxEntries: 7}, func(Path, Entry) error { count++; return nil })
	require.ErrorIs(t, err, ErrTooManyEntries)
	assert.Equal(t, 7, count)

	count = 0
	require.NoError(t, vol.Walk("/big", WalkOptions{MaxEntries: 40}, func(Path, Entry) error { count++; return nil }))
	assert.Equal(t, 40, count)
}

func TestWalkCallbackError(t *testing.T) {
	t.Parallel()
	vol := mustVolume(t, stdImage(variants[0]))
	stop := assert.AnError
	count := 0
	err := vol.Walk("", WalkOptions{}, func(Path, Entry) error {
		count++
		if count == 3 {
			return stop
		}
		return nil
	})
	require.ErrorIs(t, err, stop)
	assert.Equal(t, 3, count)
}

// chainOf returns the clusters of a file by walking the test image's FAT.
func chainOf(t *testing.T, vol *Volume, path string) []uint32 {
	t.Helper()
	budget := maxDirSlots
	e, _, err := vol.v.resolve(path, &budget)
	require.NoError(t, err)
	var chain []uint32
	require.NoError(t, vol.v.walkChain(e.cluster, func(c uint32) (bool, error) {
		chain = append(chain, c)
		return false, nil
	}))
	return chain
}

func TestChainCycle(t *testing.T) {
	t.Parallel()
	for _, v := range variants[:2] {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			f := newTFS(v.typ, v.clusters, v.spc, v.rootEnt)
			img := f.format("", stdTree())
			vol := mustVolume(t, img)
			chain := chainOf(t, vol, "/multi.bin")
			require.Greater(t, len(chain), 2)
			f.setFAT(chain[1], chain[1])
			vol = mustVolume(t, f.img)
			_, err := vol.ReadFile("/multi.bin", 1<<20)
			require.ErrorIs(t, err, ErrCorrupt)

			bigChain := chainOf2(t, f, "/big")
			f.setFAT(bigChain[0], bigChain[0])
			vol = mustVolume(t, f.img)
			_, err = vol.ReadDir("/big")
			require.ErrorIs(t, err, ErrCorrupt)
			err = vol.Walk("", WalkOptions{}, func(Path, Entry) error { return nil })
			require.ErrorIs(t, err, ErrCorrupt)
		})
	}
}

func chainOf2(t *testing.T, f *tFS, path string) []uint32 {
	t.Helper()
	return chainOf(t, mustVolume(t, f.img), path)
}

func TestDirectoryLoopTerminates(t *testing.T) {
	t.Parallel()
	f := newTFS(FAT12, 300, 1, 512)
	img := f.format("", []*tNode{{short: "SUB", dir: true, kids: []*tNode{{short: "LOOP", dir: true}}}})
	vol := mustVolume(t, img)
	sub := chainOf(t, vol, "/sub")
	inner := chainOf(t, vol, "/sub/loop")
	// Point LOOP's directory entry (inside SUB) back at SUB.
	off := f.clusterOff(sub[0]) + 64
	require.Equal(t, u16(int(inner[0])), binary.LittleEndian.Uint16(f.img[off+26:]))
	binary.LittleEndian.PutUint16(f.img[off+26:], u16(int(sub[0])))

	vol = mustVolume(t, f.img)
	count := 0
	require.NoError(t, vol.Walk("", WalkOptions{}, func(Path, Entry) error { count++; return nil }))
	assert.Equal(t, 2, count)
}

func TestTruncatedImage(t *testing.T) {
	t.Parallel()
	for _, v := range variants[:2] {
		img := stdImage(v)
		for _, n := range []int{0, 100, 511, 512, 4096, len(img) / 2, len(img) - 512} {
			_, err := openBytes(t, clone(img[:n]))
			require.Error(t, err, "%s truncated to %d", v.name, n)
		}
	}
}

func TestGarbageBPB(t *testing.T) {
	t.Parallel()
	good := stdImage(variants[1])
	put16 := func(off int, v uint16) func([]byte) {
		return func(b []byte) { binary.LittleEndian.PutUint16(b[off:], v) }
	}
	put32 := func(off int, v uint32) func([]byte) {
		return func(b []byte) { binary.LittleEndian.PutUint32(b[off:], v) }
	}
	tests := map[string]func([]byte){
		"bps zero":          put16(11, 0),
		"bps 3000":          put16(11, 3000),
		"bps 256":           put16(11, 256),
		"bps 8192":          put16(11, 8192),
		"spc zero":          func(b []byte) { b[13] = 0 },
		"spc 3":             func(b []byte) { b[13] = 3 },
		"spc 255":           func(b []byte) { b[13] = 255 },
		"reserved zero":     put16(14, 0),
		"reserved huge":     put16(14, 0xFFFF),
		"fats zero":         func(b []byte) { b[16] = 0 },
		"fats many":         func(b []byte) { b[16] = 200 },
		"root entries zero": put16(17, 0),
		"total zero":        func(b []byte) { put16(19, 0)(b); put32(32, 0)(b) },
		"total huge":        func(b []byte) { put16(19, 0)(b); put32(32, 0xFFFFFFFF)(b) },
		"fat size zero":     put16(22, 0),
		"fat size huge":     put16(22, 0xFFFF),
		"fat size tiny":     put16(22, 1),
		"no signature":      func(b []byte) { b[510] = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			img := clone(good[:4096])
			mutate(img)
			_, err := openBytes(t, img)
			require.Error(t, err)
		})
	}

	t.Run("fat32 bad root cluster", func(t *testing.T) {
		t.Parallel()
		img := clone(stdImage(variants[2]))
		for _, rc := range []uint32{0, 1, 0xFFFFFFFF, 66002} {
			binary.LittleEndian.PutUint32(img[44:], rc)
			_, err := openBytes(t, img)
			require.Error(t, err, "root cluster %d", rc)
		}
	})
}

func TestGarbageImages(t *testing.T) {
	t.Parallel()
	for _, img := range [][]byte{nil, make([]byte, 511), make([]byte, 512), make([]byte, 1<<20)} {
		_, err := openBytes(t, img)
		require.ErrorIs(t, err, ErrNoFilesystem)
	}
}

func TestBadClusterLinkFails(t *testing.T) {
	t.Parallel()
	f := newTFS(FAT16, 4200, 1, 512)
	img := f.format("", stdTree())
	vol := mustVolume(t, img)
	chain := chainOf(t, vol, "/multi.bin")
	f.setFAT(chain[1], 0xFFF7)
	vol = mustVolume(t, f.img)
	_, err := vol.ReadFile("/multi.bin", 1<<20)
	require.ErrorIs(t, err, ErrCorrupt)
}

func TestShortChainFails(t *testing.T) {
	t.Parallel()
	f := newTFS(FAT16, 4200, 1, 512)
	img := f.format("", stdTree())
	vol := mustVolume(t, img)
	chain := chainOf(t, vol, "/multi.bin")
	f.setFAT(chain[1], f.eoc())
	vol = mustVolume(t, f.img)
	_, err := vol.ReadFile("/multi.bin", 1<<20)
	require.ErrorIs(t, err, ErrCorrupt)
}

func TestOpenMissingFile(t *testing.T) {
	t.Parallel()
	_, err := Open(afero.NewMemMapFs(), filepath.Join("missing", "disk.vhd"))
	require.Error(t, err)
}

func TestTypeString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "FAT12", FAT12.String())
	assert.Equal(t, "FAT16", FAT16.String())
	assert.Equal(t, "FAT32", FAT32.String())
	assert.Equal(t, "Type(7)", Type(7).String())
}
