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

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cdTextPacks lays text out as MMC CD-Text packs of one type: each pack holds
// 12 bytes of the NUL-separated strings, starting at firstTrack. block and
// doubleByte fill the block/character-position byte. The CRC is left zero;
// ParseCDText does not check it.
func cdTextPacks(packType byte, firstTrack int, block byte, doubleByte bool, text string) []byte {
	raw := []byte(text)
	if pad := len(raw) % cdTextTextSize; pad != 0 {
		raw = append(raw, make([]byte, cdTextTextSize-pad)...)
	}
	out := make([]byte, 0, len(raw)/cdTextTextSize*cdTextPackSize)
	track := firstTrack
	charPos := 0
	seq := 0
	for off := 0; off < len(raw); off += cdTextTextSize {
		pack := make([]byte, cdTextPackSize)
		pack[0] = packType
		pack[1] = byte(track) //nolint:gosec // Test tracks are below 100.
		pack[2] = byte(seq)   //nolint:gosec // Test sequences are small.
		flags := (block & 0x07) << 4
		flags |= byte(min(charPos, 15)) //nolint:gosec // Clamped to 4 bits.
		if doubleByte {
			flags |= 0x80
		}
		pack[3] = flags
		chunk := raw[off : off+cdTextTextSize]
		copy(pack[4:], chunk)
		out = append(out, pack...)
		seq++
		// Track and character position of the first character in the next pack.
		for _, b := range chunk {
			if b == 0 {
				track++
				charPos = 0
			} else {
				charPos++
			}
		}
	}
	return out
}

func TestParseCDTextAlbumAndTracks(t *testing.T) {
	t.Parallel()

	titles := "Greatest Hits Collection\x00Opening Theme\x00A Much Longer Second Track Title\x00Finale\x00"
	artists := "The Band\x00The Band\x00Guest Singer\x00The Band\x00"
	packs := append(cdTextPacks(cdTextTitle, 0, 0, false, titles),
		cdTextPacks(cdTextArtist, 0, 0, false, artists)...)
	require.Greater(t, len(packs)/cdTextPackSize, 4, "strings span several packs")

	text := ParseCDText(packs)
	assert.False(t, text.Empty())
	assert.Equal(t, []string{
		"Greatest Hits Collection",
		"Opening Theme",
		"A Much Longer Second Track Title",
		"Finale",
	}, text.Titles)
	assert.Equal(t, []string{"The Band", "The Band", "Guest Singer", "The Band"}, text.Artists)

	assert.Equal(t, "Greatest Hits Collection", text.Title(0))
	assert.Equal(t, "A Much Longer Second Track Title", text.Title(2))
	assert.Equal(t, "Guest Singer", text.Artist(2))
	assert.Empty(t, text.Title(4))
	assert.Empty(t, text.Title(-1))
	assert.Empty(t, text.Artist(99))
	assert.Empty(t, text.Artist(-1))
}

func TestParseCDTextInterleavedTypes(t *testing.T) {
	t.Parallel()

	titles := cdTextPacks(cdTextTitle, 0, 0, false, "Album Name Here\x00Track\x00")
	artists := cdTextPacks(cdTextArtist, 0, 0, false, "Performer Name\x00Solo\x00")
	// Real discs order packs by type, but a reader must not depend on it.
	var packs []byte
	for i := 0; i < len(titles) || i < len(artists); i += cdTextPackSize {
		if i < len(artists) {
			packs = append(packs, artists[i:i+cdTextPackSize]...)
		}
		if i < len(titles) {
			packs = append(packs, titles[i:i+cdTextPackSize]...)
		}
	}
	text := ParseCDText(packs)
	assert.Equal(t, []string{"Album Name Here", "Track"}, text.Titles)
	assert.Equal(t, []string{"Performer Name", "Solo"}, text.Artists)
}

func TestParseCDTextTabRepeatsPrevious(t *testing.T) {
	t.Parallel()

	packs := cdTextPacks(cdTextArtist, 0, 0, false, "Same Artist\x00\t\x00\t\x00Other\x00\t\x00")
	text := ParseCDText(packs)
	assert.Equal(t, []string{"Same Artist", "Same Artist", "Same Artist", "Other", "Other"}, text.Artists)
	assert.Nil(t, text.Titles)
}

func TestParseCDTextTabFirstIsEmpty(t *testing.T) {
	t.Parallel()

	text := ParseCDText(cdTextPacks(cdTextTitle, 0, 0, false, "\t\x00Real\x00"))
	assert.Equal(t, []string{"", "Real"}, text.Titles)
}

func TestParseCDTextLatin1(t *testing.T) {
	t.Parallel()

	// "Café Señor Über" in ISO 8859-1.
	latin1 := "Caf\xe9 Se\xf1or \xdcber\x00"
	text := ParseCDText(cdTextPacks(cdTextTitle, 0, 0, false, latin1))
	require.Len(t, text.Titles, 1)
	assert.Equal(t, "Café Señor Über", text.Titles[0])
	assert.True(t, utf8.ValidString(text.Titles[0]))
}

func TestParseCDTextStripsControlsAndSpace(t *testing.T) {
	t.Parallel()

	text := ParseCDText(cdTextPacks(cdTextTitle, 0, 0, false, "  Bell\x07 Ring\x1b \x00"))
	assert.Equal(t, []string{"Bell Ring"}, text.Titles)
}

func TestParseCDTextOnlyBlockZero(t *testing.T) {
	t.Parallel()

	block1 := cdTextPacks(cdTextTitle, 0, 1, false, "Other Language Album\x00Other Track\x00")
	block0 := cdTextPacks(cdTextTitle, 0, 0, false, "English Album\x00English Track\x00")
	dbcs := cdTextPacks(cdTextTitle, 0, 0, true, "\x82\xa0\x82\xa2\x00\x00")
	packs := append(append(append([]byte{}, block1...), dbcs...), block0...)

	text := ParseCDText(packs)
	assert.Equal(t, []string{"English Album", "English Track"}, text.Titles)

	onlyOther := ParseCDText(append(append([]byte{}, block1...), dbcs...))
	assert.Nil(t, onlyOther.Titles)
	assert.True(t, onlyOther.Empty())
}

func TestParseCDTextStartsAtFirstPackTrack(t *testing.T) {
	t.Parallel()

	// No album entry: the first string belongs to track 1.
	text := ParseCDText(cdTextPacks(cdTextTitle, 1, 0, false, "First\x00Second\x00"))
	assert.Equal(t, []string{"", "First", "Second"}, text.Titles)
	assert.Empty(t, text.Title(0))
	assert.Equal(t, "First", text.Title(1))

	// The extension flag in bit 7 of the track byte is masked off.
	packs := cdTextPacks(cdTextTitle, 2, 0, false, "Two\x00")
	packs[1] |= 0x80
	assert.Equal(t, []string{"", "", "Two"}, ParseCDText(packs).Titles)
}

func TestParseCDTextRejectsTrackAboveMax(t *testing.T) {
	t.Parallel()

	packs := cdTextPacks(cdTextTitle, 0, 0, false, "Album\x00")
	packs[1] = MaxTracks + 1
	text := ParseCDText(packs)
	assert.Nil(t, text.Titles)
	assert.True(t, text.Empty())
}

func TestParseCDTextRuneCap(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("\xe9", 200) + "\x00" + strings.Repeat("x", 500) + "\x00"
	text := ParseCDText(cdTextPacks(cdTextTitle, 0, 0, false, long))
	require.Len(t, text.Titles, 2)
	assert.Equal(t, strings.Repeat("é", maxCDTextRunes), text.Titles[0])
	assert.Equal(t, strings.Repeat("x", maxCDTextRunes), text.Titles[1])
}

func TestParseCDTextTrackCap(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	for range 300 {
		b.WriteString("t\x00")
	}
	text := ParseCDText(cdTextPacks(cdTextTitle, 0, 0, false, b.String()))
	assert.Len(t, text.Titles, MaxTracks+1)
}

func TestParseCDTextGarbage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		packs []byte
	}{
		{name: "nil", packs: nil},
		{name: "short", packs: []byte{cdTextTitle, 0, 0, 0, 'A'}},
		{name: "no text packs", packs: append(make([]byte, cdTextPackSize), 0x8f, 0, 0, 0)},
		{name: "only NULs", packs: cdTextPacks(cdTextTitle, 0, 0, false, "\x00\x00\x00")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text := ParseCDText(tt.packs)
			assert.True(t, text.Empty())
			assert.Empty(t, text.Title(0))
			assert.Empty(t, text.Artist(0))
		})
	}

	// A trailing partial pack is ignored.
	packs := append(cdTextPacks(cdTextTitle, 0, 0, false, "Album\x00"), cdTextTitle, 0, 0, 0, 'Z')
	assert.Equal(t, []string{"Album"}, ParseCDText(packs).Titles)
}

func TestCDTextEmpty(t *testing.T) {
	t.Parallel()
	assert.True(t, CDText{}.Empty())
	assert.True(t, CDText{Titles: []string{"", ""}, Artists: []string{""}}.Empty())
	assert.False(t, CDText{Titles: []string{"", "x"}}.Empty())
	assert.False(t, CDText{Artists: []string{"x"}}.Empty())
}

func FuzzParseCDText(f *testing.F) {
	f.Add([]byte{})
	f.Add(cdTextPacks(cdTextTitle, 0, 0, false, "Album\x00Track\x00\t\x00"))
	f.Add(append(cdTextPacks(cdTextTitle, 1, 0, false, "Caf\xe9\x00"),
		cdTextPacks(cdTextArtist, 0, 2, true, "x\x00")...))
	f.Add(cdTextPacks(cdTextArtist, 0, 0, false, strings.Repeat("y", 400)))

	f.Fuzz(func(t *testing.T, packs []byte) {
		text := ParseCDText(packs)
		for _, list := range [][]string{text.Titles, text.Artists} {
			if len(list) > MaxTracks+1 {
				t.Fatalf("%d entries, want at most %d", len(list), MaxTracks+1)
			}
			for i, s := range list {
				if n := utf8.RuneCountInString(s); n > maxCDTextRunes {
					t.Fatalf("entry %d has %d runes, want at most %d", i, n, maxCDTextRunes)
				}
				if !utf8.ValidString(s) {
					t.Fatalf("entry %d is not valid UTF-8: %q", i, s)
				}
			}
		}
		_ = text.Empty()
		_ = text.Title(0)
		_ = text.Artist(MaxTracks)
	})
}
