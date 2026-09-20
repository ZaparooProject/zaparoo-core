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

package esapi

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGameBox2DAlias verifies that the ZapScraper box2d alias decodes into the
// canonical Boxart2D field, that an explicit boxart2d value wins regardless of
// element order, and that marshalling only ever emits boxart2d.
func TestGameBox2DAlias(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		fields string
		want   string
	}{
		{name: "alias", fields: "<box2d>./covers/alias.png</box2d>", want: "./covers/alias.png"},
		{name: "canonical", fields: "<boxart2d>./covers/canonical.png</boxart2d>", want: "./covers/canonical.png"},
		{
			name:   "canonical wins",
			fields: "<boxart2d>./covers/canonical.png</boxart2d><box2d>./covers/alias.png</box2d>",
			want:   "./covers/canonical.png",
		},
		{
			name:   "alias first",
			fields: "<box2d>./covers/alias.png</box2d><boxart2d>./covers/canonical.png</boxart2d>",
			want:   "./covers/canonical.png",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := []byte("<gameList><game><path>./Game.gbc</path><image>./image.png</image>" +
				tc.fields + "</game></gameList>")
			gl, err := ParseGameListXML(data)
			require.NoError(t, err)
			require.Len(t, gl.Games, 1)
			assert.Equal(t, tc.want, gl.Games[0].Boxart2D)
			assert.Equal(t, "./image.png", gl.Games[0].Image)
			assert.Equal(t, "./Game.gbc", gl.Games[0].Path)
			encoded, err := xml.Marshal(gl)
			require.NoError(t, err)
			assert.Contains(t, string(encoded), "<boxart2d>"+tc.want+"</boxart2d>")
			assert.NotContains(t, string(encoded), "<box2d>")
		})
	}
}

// TestGameDecodeErrorSkipsEntry covers a malformed typed element: gamelist.xml
// is untrusted input, so the entry must never decode as a silently zeroed
// game, but one bad value must not cost the user the rest of the file either.
func TestGameDecodeErrorSkipsEntry(t *testing.T) {
	t.Parallel()

	gl, err := ParseGameListXML([]byte(`<gameList>` +
		`<game><path>./Before.gb</path></game>` +
		`<game><path>./Bad.gb</path><playcount>many</playcount><desc>after <b>the</b> error</desc></game>` +
		`<folder><path>./Hacks</path></folder>` +
		`<game><path>./Hidden.gb</path><hidden>maybe</hidden></game>` +
		`<game><path>./After.gb</path><playcount>3</playcount></game>` +
		`</gameList>`))
	require.NoError(t, err)
	require.Len(t, gl.Games, 2)
	assert.Equal(t, "./Before.gb", gl.Games[0].Path)
	assert.Equal(t, "./After.gb", gl.Games[1].Path)
	assert.Equal(t, 3, gl.Games[1].PlayCount)
	require.Len(t, gl.Folders, 1)
	assert.Equal(t, "./Hacks", gl.Folders[0].Path)
	assert.Equal(t, 2, gl.Skipped)
}

// TestGameDecodeSyntaxErrorFailsDocument keeps malformed XML fatal: unlike a
// bad value, it leaves no reliable place to resume from.
func TestGameDecodeSyntaxErrorFailsDocument(t *testing.T) {
	t.Parallel()

	_, err := ParseGameListXML([]byte(
		`<gameList><game><path>./Game.gb</path><desc>Tom & Jerry</desc></game></gameList>`,
	))
	require.Error(t, err)
}

// TestUnmarshalGameIDVariants verifies that both the XML attribute form
// (ScreenScraperIDAttr) and the element form (ScreenScraperID) of the "id"
// field parse correctly in isolation and together.
func TestUnmarshalGameIDVariants(t *testing.T) {
	t.Parallel()

	t.Run("both attribute and element", func(t *testing.T) {
		t.Parallel()
		data := []byte(`<game id="attr-val"><id>42</id><path>./rom.nes</path></game>`)
		var g Game
		require.NoError(t, xml.Unmarshal(data, &g))
		assert.Equal(t, "attr-val", g.ScreenScraperIDAttr)
		assert.Equal(t, 42, g.ScreenScraperID)
	})

	t.Run("attribute only", func(t *testing.T) {
		t.Parallel()
		data := []byte(`<game id="only-attr"><path>./rom.nes</path></game>`)
		var g Game
		require.NoError(t, xml.Unmarshal(data, &g))
		assert.Equal(t, "only-attr", g.ScreenScraperIDAttr)
		assert.Equal(t, 0, g.ScreenScraperID, "element form should be zero when absent")
	})

	t.Run("element only", func(t *testing.T) {
		t.Parallel()
		data := []byte(`<game><id>99</id><path>./rom.nes</path></game>`)
		var g Game
		require.NoError(t, xml.Unmarshal(data, &g))
		assert.Empty(t, g.ScreenScraperIDAttr, "attribute form should be empty when absent")
		assert.Equal(t, 99, g.ScreenScraperID)
	})

	t.Run("neither present", func(t *testing.T) {
		t.Parallel()
		data := []byte(`<game><path>./rom.nes</path></game>`)
		var g Game
		require.NoError(t, xml.Unmarshal(data, &g))
		assert.Empty(t, g.ScreenScraperIDAttr)
		assert.Equal(t, 0, g.ScreenScraperID)
	})
}

func TestReadGameListXML(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gamelist.xml")
	xmlData := []byte(`<?xml version="1.0"?>
<gameList>
  <game>
    <path>./Sonic.nes</path>
    <name>Sonic</name>
    <image>./media/sonic.png</image>
    <unknown>ignored</unknown>
  </game>
  <folder>
    <path>./Hacks</path>
    <name>Hacks</name>
  </folder>
</gameList>`)
	require.NoError(t, os.WriteFile(path, xmlData, 0o600))

	gameList, err := ReadGameListXML(path)
	require.NoError(t, err)
	require.Len(t, gameList.Games, 1)
	require.Len(t, gameList.Folders, 1)
	assert.Equal(t, "./Sonic.nes", gameList.Games[0].Path)
	assert.Equal(t, "Sonic", gameList.Games[0].Name)
	assert.Equal(t, "./media/sonic.png", gameList.Games[0].Image)
	assert.Equal(t, "./Hacks", gameList.Folders[0].Path)
	assert.Equal(t, "Hacks", gameList.Folders[0].Name)
}

func TestReadGameReferencesXMLFS(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := filepath.Join("lists", "gamelist.xml")
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, afero.WriteFile(fs, path, []byte(`<gameList>
  <game><name>Game</name><path>./game.rom</path><desc>ignored</desc></game>
  <folder><name>Folder</name><path>./folder</path></folder>
</gameList>`), 0o600))

	references, err := ReadGameReferencesXMLFS(fs, path)
	require.NoError(t, err)
	assert.Equal(t, []GameReference{{Name: "Game", Path: "./game.rom"}}, references)
}

func TestGameListXMLBOM(t *testing.T) {
	t.Parallel()

	const body = `<gameList><game><name>Game</name><path>./game.rom</path></game></gameList>`
	for _, tc := range []struct {
		name  string
		data  string
		valid bool
	}{
		{name: "plain", data: body, valid: true},
		{name: "BOM", data: "\xef\xbb\xbf" + body, valid: true},
		{
			name: "BOM with declaration", valid: true,
			data: "\xef\xbb\xbf" + `<?xml version="1.0" encoding="UTF-8"?>` + "\r\n" + body,
		},
		{name: "duplicate BOM", data: "\xef\xbb\xbf\xef\xbb\xbf" + body},
		{name: "BOM after whitespace", data: " \xef\xbb\xbf" + body},
		{name: "trailing BOM", data: body + "\xef\xbb\xbf"},
		{name: "trailing text", data: "\xef\xbb\xbf" + body + "suffix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			path := filepath.Join("lists", "gamelist.xml")
			require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
			require.NoError(t, afero.WriteFile(fs, path, []byte(tc.data), 0o600))
			validationErr := ValidateGameListXML([]byte(tc.data))
			parsed, parseErr := ParseGameListXML([]byte(tc.data))
			full, fullErr := ReadGameListXMLFS(fs, path)
			refs, refsErr := ReadGameReferencesXMLFS(fs, path)
			for _, err := range []error{validationErr, parseErr, fullErr, refsErr} {
				if tc.valid {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "character data outside gameList root element")
				}
			}
			if tc.valid {
				require.Len(t, parsed.Games, 1)
				assert.Equal(t, "Game", parsed.Games[0].Name)
				assert.Equal(t, parsed, full)
				assert.Equal(t, []GameReference{{Name: "Game", Path: "./game.rom"}}, refs)
			}
		})
	}
}

func TestGameListXMLBOMSizeLimit(t *testing.T) {
	t.Parallel()
	data := []byte("\xef\xbb\xbf<gameList/>" + strings.Repeat(" ", MaxGameListXMLSize-len("<gameList/>")))
	require.ErrorIs(t, ValidateGameListXML(data), ErrGameListTooLarge)
	_, err := ParseGameListXML(data)
	require.ErrorIs(t, err, ErrGameListTooLarge)
}

func TestGameListXMLLimits(t *testing.T) {
	t.Parallel()

	t.Run("size", func(t *testing.T) {
		t.Parallel()
		err := ValidateGameListXML([]byte(strings.Repeat(" ", MaxGameListXMLSize+1)))
		require.ErrorIs(t, err, ErrGameListTooLarge)
	})

	t.Run("depth", func(t *testing.T) {
		t.Parallel()
		data := []byte("<gameList>" + strings.Repeat("<group>", MaxGameListXMLDepth) +
			strings.Repeat("</group>", MaxGameListXMLDepth) + "</gameList>")
		err := ValidateGameListXML(data)
		require.ErrorIs(t, err, ErrGameListTooDeep)
	})

	t.Run("entries", func(t *testing.T) {
		t.Parallel()
		data := []byte("<gameList>" + strings.Repeat("<game/>", MaxGameListEntries+1) + "</gameList>")
		err := ValidateGameListXML(data)
		require.ErrorIs(t, err, ErrGameListTooManyItems)
	})

	t.Run("root", func(t *testing.T) {
		t.Parallel()
		err := ValidateGameListXML([]byte(`<notGameList/>`))
		require.Error(t, err)
	})

	t.Run("text before root", func(t *testing.T) {
		t.Parallel()
		err := ValidateGameListXML([]byte(`prefix<gameList/>`))
		require.ErrorContains(t, err, "character data outside gameList root element")
	})

	t.Run("text after root", func(t *testing.T) {
		t.Parallel()
		err := ValidateGameListXML([]byte(`<gameList/>suffix`))
		require.ErrorContains(t, err, "character data outside gameList root element")
	})

	t.Run("whitespace around root", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, ValidateGameListXML([]byte(" \n\t<gameList/>\r\n ")))
	})
}

func TestReadGameListXMLFSSizeLimit(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := "gamelist.xml"
	oversize := strings.Repeat(" ", MaxGameListXMLSizeConstrained+1)
	require.NoError(t, afero.WriteFile(fs, path, []byte(oversize), 0o600))

	_, err := ReadGameListXMLLimitFS(fs, path, MaxGameListXMLSizeConstrained)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrGameListTooLarge)
	require.ErrorContains(t, err, "file is larger than the 16 MB limit")

	// The same file is within the default limit, which is what a platform that
	// does not report ResourceConstrained uses.
	_, err = ReadGameListXMLFS(fs, path)
	require.ErrorContains(t, err, "missing gameList root element")

	_, err = ReadGameReferencesXMLFS(fs, path)
	require.ErrorContains(t, err, "missing gameList root element")
}

// TestGameListTooLargeErrorNamesItsLimit checks the reported limit is the one
// that actually applied, since it differs between platforms.
func TestGameListTooLargeErrorNamesItsLimit(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "file is larger than the 16 MB limit",
		(&GameListTooLargeError{Limit: MaxGameListXMLSizeConstrained}).Error())
	assert.Equal(t, "file is larger than the 128 MB limit",
		(&GameListTooLargeError{Limit: MaxGameListXMLSize}).Error())

	body := `<gameList><game><path>./game.rom</path></game></gameList>`
	_, err := decodeGameList(strings.NewReader(body), 16<<20)
	require.NoError(t, err)

	_, err = decodeGameList(strings.NewReader(body), int64(len(body))-1)
	require.ErrorIs(t, err, ErrGameListTooLarge)
	var tooLarge *GameListTooLargeError
	require.ErrorAs(t, err, &tooLarge)
	assert.Equal(t, int64(len(body))-1, tooLarge.Limit)
}

// TestGameListInvalidRootIsReportable covers every way a file fails to be a
// gameList document. Each carries one sentinel so a caller reporting the
// failure to a user has a condition to name, rather than echoing a parser
// string that repeats the file path back at them.
func TestGameListInvalidRootIsReportable(t *testing.T) {
	t.Parallel()

	for name, doc := range map[string]string{
		"empty file":       "",
		"whitespace only":  "   \n",
		"prolog only":      `<?xml version="1.0"?>`,
		"wrong root":       `<systemList><system/></systemList>`,
		"second root":      `<gameList/><gameList/>`,
		"text before root": `junk<gameList/>`,
		"text after root":  `<gameList/>junk`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseGameListXML([]byte(doc))
			require.ErrorIs(t, err, ErrGameListInvalidRoot)
			require.ErrorIs(t, ValidateGameListXML([]byte(doc)), ErrGameListInvalidRoot)

			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "gamelist.xml", []byte(doc), 0o600))
			_, err = ReadGameListXMLFS(fs, "gamelist.xml")
			require.ErrorIs(t, err, ErrGameListInvalidRoot)
			_, err = ReadGameReferencesXMLFS(fs, "gamelist.xml")
			require.ErrorIs(t, err, ErrGameListInvalidRoot)
		})
	}
}

// TestGameListTooLargeErrorBelowOneMegabyte keeps the reported limit truthful
// for a caller that passes ReadGameListXMLLimitFS a limit under a megabyte,
// which integer megabytes render as "0 MB".
func TestGameListTooLargeErrorBelowOneMegabyte(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "file is larger than the 900 byte limit",
		(&GameListTooLargeError{Limit: 900}).Error())
	assert.Equal(t, "file is larger than the 1 MB limit",
		(&GameListTooLargeError{Limit: 1 << 20}).Error())
}

// TestGameListEntryErrorPaths covers the failures that reach the decoder in
// the middle of an entry rather than between entries: a document truncated
// inside a <folder>, inside a <game> on the reference walk, and immediately
// after a value that was going to drop its entry anyway.
func TestGameListEntryErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("truncated inside a folder", func(t *testing.T) {
		t.Parallel()
		_, err := ParseGameListXML([]byte(`<gameList><folder><path>./F</path>`))
		require.Error(t, err)
		var syntaxErr *xml.SyntaxError
		require.ErrorAs(t, err, &syntaxErr)
		assert.Contains(t, err.Error(), "decode gamelist folder")
	})

	t.Run("truncated inside a game on the reference walk", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "gamelist.xml",
			[]byte(`<gameList><game><path>./A</path>`), 0o600))
		_, err := ReadGameReferencesXMLFS(fs, "gamelist.xml")
		require.Error(t, err)
		var syntaxErr *xml.SyntaxError
		require.ErrorAs(t, err, &syntaxErr)
		assert.Contains(t, err.Error(), "decode gamelist game reference")
	})

	t.Run("truncated after a value that drops its entry", func(t *testing.T) {
		t.Parallel()
		// The bad <playcount> would normally be skipped and the rest of the
		// entry drained; here the drain runs straight into the truncation, so
		// the document has to fail rather than report a clean partial decode.
		_, err := ParseGameListXML([]byte(
			`<gameList><game><playcount>many</playcount><desc>unterminated`))
		require.Error(t, err)
		var syntaxErr *xml.SyntaxError
		assert.ErrorAs(t, err, &syntaxErr)
	})
}

// TestGameListStreamsPastOldBufferLimit decodes a document larger than the
// 16 MiB whole-file buffer the parser used to require, which a large scraped
// library such as C64 exceeds.
func TestGameListStreamsPastOldBufferLimit(t *testing.T) {
	t.Parallel()

	const entries = 20_000
	desc := strings.Repeat("d", 1024)
	var doc bytes.Buffer
	_, _ = doc.WriteString("\xef\xbb\xbf<?xml version=\"1.0\" encoding=\"utf-8\" standalone=\"yes\"?>\n<gameList>")
	for i := range entries {
		_, _ = fmt.Fprintf(&doc,
			`<game id="%d" source="ScreenScraper.fr"><path>./Game %d.d64</path><desc>%s</desc></game>`,
			i, i, desc)
	}
	_, _ = doc.WriteString("</gameList>")
	require.Greater(t, doc.Len(), 16<<20)

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "gamelist.xml", doc.Bytes(), 0o600))
	gl, err := ReadGameListXMLFS(fs, "gamelist.xml")
	require.NoError(t, err)
	require.Len(t, gl.Games, entries)
	assert.Equal(t, "./Game 19999.d64", gl.Games[entries-1].Path)
	assert.Equal(t, "19999", gl.Games[entries-1].ScreenScraperIDAttr)
	assert.Zero(t, gl.Skipped)
}

// TestGameListStreamStopsAtByteCap checks the cap is enforced on the stream
// itself, so an oversized source fails without being read to the end.
func TestGameListStreamStopsAtByteCap(t *testing.T) {
	t.Parallel()

	const limit = 64
	body := `<gameList><game><path>./game.rom</path></game></gameList>`
	require.LessOrEqual(t, len(body), limit)

	gl, err := decodeGameList(strings.NewReader(body+strings.Repeat(" ", limit-len(body))), limit)
	require.NoError(t, err)
	require.Len(t, gl.Games, 1)

	source := strings.NewReader(body + strings.Repeat(" ", 4096))
	_, err = decodeGameList(source, limit)
	require.ErrorIs(t, err, ErrGameListTooLarge)
	assert.Positive(t, source.Len(), "source must not be drained past the cap")
}

func TestReadGameListXMLErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		_, err := ReadGameListXML(filepath.Join(t.TempDir(), "missing.xml"))
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to open gamelist XML file")
	})

	t.Run("malformed xml", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "gamelist.xml")
		require.NoError(t, os.WriteFile(path, []byte(`<gameList><game>`), 0o600))
		_, err := ReadGameListXML(path)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to unmarshal gamelist XML")
	})
}

func TestESDateHelpers(t *testing.T) {
	t.Parallel()

	date := time.Date(1995, time.March, 11, 1, 2, 3, 0, time.UTC)
	formatted := FormatESDate(date)
	assert.Equal(t, "19950311T010203", formatted)

	parsed, err := ParseESDate(formatted)
	require.NoError(t, err)
	assert.Equal(t, date, parsed)

	_, err = ParseESDate("")
	require.Error(t, err)
	require.ErrorContains(t, err, "empty datetime string")

	_, err = ParseESDate("1995-03-11")
	require.Error(t, err)
	assert.ErrorContains(t, err, "parsing ES datetime")
}

func TestParseRating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr string
		want    float64
	}{
		{name: "zero", input: "0", want: 0},
		{name: "fraction", input: "0.75", want: 0.75},
		{name: "one", input: "1", want: 1},
		{name: "empty", input: "", wantErr: "empty rating string"},
		{name: "malformed", input: "good", wantErr: "parsing rating"},
		{name: "negative", input: "-0.1", wantErr: "rating out of range"},
		{name: "too high", input: "1.1", wantErr: "rating out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseRating(tt.input)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tt.want, got, 0.0001)
		})
	}
}
