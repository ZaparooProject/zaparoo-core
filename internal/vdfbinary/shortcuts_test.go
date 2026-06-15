package vdfbinary_test

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"strconv"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/vdfbinary"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/shortcuts.vdf
var shortcutVdf []byte

func TestParseShortcuts(t *testing.T) {
	t.Parallel()

	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(shortcutVdf))
	require.NoError(t, err)
	require.Len(t, shortcuts, 3)

	// Verify first shortcut
	assert.Equal(t, uint32(3414143657), shortcuts[0].AppID)
	assert.Equal(t, "Control", shortcuts[0].AppName)
	assert.Contains(t, shortcuts[0].Exe, "Control_DX12.exe")
	assert.Empty(t, shortcuts[0].Icon)
	assert.True(t, shortcuts[0].IsHidden)
	assert.Empty(t, shortcuts[0].Tags)

	// Verify second shortcut has an icon and tag
	assert.Equal(t, uint32(3022575626), shortcuts[1].AppID)
	assert.Equal(t, "Cyberpunk 2077", shortcuts[1].AppName)
	assert.Contains(t, shortcuts[1].Icon, "cyberpunk.ico")
	assert.False(t, shortcuts[1].IsHidden)
	assert.Equal(t, []string{"favorite"}, shortcuts[1].Tags)

	// Verify third shortcut has multiple tags
	assert.Equal(t, uint32(3043193801), shortcuts[2].AppID)
	assert.Equal(t, "Skate 3", shortcuts[2].AppName)
	assert.Equal(t, []string{"Sport", "Action", "Skate"}, shortcuts[2].Tags)
}

func TestParseShortcuts_EmptyFile(t *testing.T) {
	t.Parallel()

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader([]byte{}))
	assert.ErrorIs(t, err, vdfbinary.ErrEmptyVDF)
}

func TestParseShortcuts_InvalidFormat(t *testing.T) {
	t.Parallel()

	// Text VDF format instead of binary
	textVdf := []byte(`"shortcuts" { }`)
	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(textVdf))
	assert.ErrorIs(t, err, vdfbinary.ErrNotBinaryVDF)
}

func TestParseShortcuts_NoShortcutsKey(t *testing.T) {
	t.Parallel()

	// Valid binary VDF but missing "shortcuts" key
	// Binary VDF with empty map: marker(0x00) + "other" + null + end(0x08) + end(0x08)
	emptyVdf := []byte{0x00, 'o', 't', 'h', 'e', 'r', 0x00, 0x08, 0x08}
	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(emptyVdf))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shortcuts")
}

// TestParseShortcuts_MissingOptionalFields tests that shortcuts without
// optional fields (tags, icon, IsHidden) are parsed successfully.
// This is the key fix for issue #451 - EmuDeck/Lutris shortcuts.
func TestParseShortcuts_MissingOptionalFields(t *testing.T) {
	t.Parallel()

	// Build a minimal binary VDF with a shortcut missing optional fields
	// Structure: shortcuts { 0 { appid, AppName, Exe, StartDir } }
	var buf bytes.Buffer

	// shortcuts map start
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	// shortcut "0" map start
	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString("0") //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails

	// appid (number)
	buf.WriteByte(0x02)                       //nolint:revive // never fails
	buf.WriteString("appid")                  //nolint:revive // never fails
	buf.WriteByte(0x00)                       //nolint:revive // never fails
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04}) //nolint:revive // never fails

	// AppName (string)
	buf.WriteByte(0x01)          //nolint:revive // never fails
	buf.WriteString("AppName")   //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("Test Game") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	// Exe (string)
	buf.WriteByte(0x01)              //nolint:revive // never fails
	buf.WriteString("Exe")           //nolint:revive // never fails
	buf.WriteByte(0x00)              //nolint:revive // never fails
	buf.WriteString("/path/to/game") //nolint:revive // never fails
	buf.WriteByte(0x00)              //nolint:revive // never fails

	// StartDir (string)
	buf.WriteByte(0x01)         //nolint:revive // never fails
	buf.WriteString("StartDir") //nolint:revive // never fails
	buf.WriteByte(0x00)         //nolint:revive // never fails
	buf.WriteString("/path/to") //nolint:revive // never fails
	buf.WriteByte(0x00)         //nolint:revive // never fails

	// Note: deliberately missing icon, IsHidden, and tags

	// End of shortcut "0" map
	buf.WriteByte(0x08) //nolint:revive // never fails

	// End of shortcuts map
	buf.WriteByte(0x08) //nolint:revive // never fails

	// End of root map
	buf.WriteByte(0x08) //nolint:revive // never fails

	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err, "should parse shortcuts with missing optional fields")
	require.Len(t, shortcuts, 1)

	assert.Equal(t, uint32(0x04030201), shortcuts[0].AppID)
	assert.Equal(t, "Test Game", shortcuts[0].AppName)
	assert.Equal(t, "/path/to/game", shortcuts[0].Exe)
	assert.Equal(t, "/path/to", shortcuts[0].StartDir)
	assert.Empty(t, shortcuts[0].Icon, "missing icon should default to empty string")
	assert.False(t, shortcuts[0].IsHidden, "missing IsHidden should default to false")
	assert.Empty(t, shortcuts[0].Tags, "missing tags should default to empty slice")
}

func TestParseShortcuts_MissingRequiredField_AppID(t *testing.T) {
	t.Parallel()

	// Shortcut missing required appid field
	var buf bytes.Buffer
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString("0") //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails

	// Only AppName, missing appid
	buf.WriteByte(0x01)        //nolint:revive // never fails
	buf.WriteString("AppName") //nolint:revive // never fails
	buf.WriteByte(0x00)        //nolint:revive // never fails
	buf.WriteString("Test")    //nolint:revive // never fails
	buf.WriteByte(0x00)        //nolint:revive // never fails

	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "appid")
}

func TestParseShortcuts_MissingRequiredField_AppName(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString("0") //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails

	// appid present
	buf.WriteByte(0x02)                       //nolint:revive // never fails
	buf.WriteString("appid")                  //nolint:revive // never fails
	buf.WriteByte(0x00)                       //nolint:revive // never fails
	buf.Write([]byte{0x01, 0x00, 0x00, 0x00}) //nolint:revive // never fails

	// Missing AppName, only Exe
	buf.WriteByte(0x01)      //nolint:revive // never fails
	buf.WriteString("Exe")   //nolint:revive // never fails
	buf.WriteByte(0x00)      //nolint:revive // never fails
	buf.WriteString("/path") //nolint:revive // never fails
	buf.WriteByte(0x00)      //nolint:revive // never fails

	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "appname")
}

func TestParseShortcuts_MissingRequiredField_Exe(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString("0") //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails

	buf.WriteByte(0x02)                       //nolint:revive // never fails
	buf.WriteString("appid")                  //nolint:revive // never fails
	buf.WriteByte(0x00)                       //nolint:revive // never fails
	buf.Write([]byte{0x01, 0x00, 0x00, 0x00}) //nolint:revive // never fails

	buf.WriteByte(0x01)        //nolint:revive // never fails
	buf.WriteString("AppName") //nolint:revive // never fails
	buf.WriteByte(0x00)        //nolint:revive // never fails
	buf.WriteString("Test")    //nolint:revive // never fails
	buf.WriteByte(0x00)        //nolint:revive // never fails

	// Missing Exe, only StartDir
	buf.WriteByte(0x01)         //nolint:revive // never fails
	buf.WriteString("StartDir") //nolint:revive // never fails
	buf.WriteByte(0x00)         //nolint:revive // never fails
	buf.WriteString("/path")    //nolint:revive // never fails
	buf.WriteByte(0x00)         //nolint:revive // never fails

	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exe")
}

func TestParseShortcuts_MissingRequiredField_StartDir(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString("0") //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails

	buf.WriteByte(0x02)                       //nolint:revive // never fails
	buf.WriteString("appid")                  //nolint:revive // never fails
	buf.WriteByte(0x00)                       //nolint:revive // never fails
	buf.Write([]byte{0x01, 0x00, 0x00, 0x00}) //nolint:revive // never fails

	buf.WriteByte(0x01)        //nolint:revive // never fails
	buf.WriteString("AppName") //nolint:revive // never fails
	buf.WriteByte(0x00)        //nolint:revive // never fails
	buf.WriteString("Test")    //nolint:revive // never fails
	buf.WriteByte(0x00)        //nolint:revive // never fails

	buf.WriteByte(0x01)             //nolint:revive // never fails
	buf.WriteString("Exe")          //nolint:revive // never fails
	buf.WriteByte(0x00)             //nolint:revive // never fails
	buf.WriteString("/path/to/exe") //nolint:revive // never fails
	buf.WriteByte(0x00)             //nolint:revive // never fails

	// Missing StartDir
	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "startdir")
}

func TestParseShortcuts_TruncatedNumber(t *testing.T) {
	t.Parallel()

	// Number field with only 2 bytes instead of 4
	var buf bytes.Buffer
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString("0") //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails

	buf.WriteByte(0x02)           //nolint:revive // never fails
	buf.WriteString("appid")      //nolint:revive // never fails
	buf.WriteByte(0x00)           //nolint:revive // never fails
	buf.Write([]byte{0x01, 0x02}) //nolint:revive // never fails

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
}

func TestParseShortcuts_CorruptedFile(t *testing.T) {
	t.Parallel()

	// Valid start but truncated mid-parse
	corrupted := []byte{0x00, 's', 'h', 'o', 'r', 't', 'c', 'u', 't', 's', 0x00, 0x00}
	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(corrupted))
	require.Error(t, err)
}

func TestParseShortcuts_NonSequentialIndex(t *testing.T) {
	t.Parallel()

	// shortcuts { 1 { ... } } - starts at 1 instead of 0. Third-party tools can
	// leave non-zero starting indices; this should parse rather than error.
	var buf bytes.Buffer
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails
	writeEntry(&buf, "1", 0x04030201, "Only Game")
	buf.WriteByte(0x08) //nolint:revive // never fails (end shortcuts)
	buf.WriteByte(0x08) //nolint:revive // never fails (end root)

	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Len(t, shortcuts, 1)
	assert.Equal(t, uint32(0x04030201), shortcuts[0].AppID)
	assert.Equal(t, "Only Game", shortcuts[0].AppName)
}

func TestParseShortcuts_GappedIndices(t *testing.T) {
	t.Parallel()

	// shortcuts { 0 {...} 2 {...} } - a gap where index 1 was deleted. Both
	// present entries should parse, in ascending index order.
	var buf bytes.Buffer
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails
	writeEntry(&buf, "0", 100, "First")
	writeEntry(&buf, "2", 300, "Third")
	buf.WriteByte(0x08) //nolint:revive // never fails (end shortcuts)
	buf.WriteByte(0x08) //nolint:revive // never fails (end root)

	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Len(t, shortcuts, 2)
	assert.Equal(t, "First", shortcuts[0].AppName)
	assert.Equal(t, "Third", shortcuts[1].AppName)
}

func TestParseShortcuts_NonNumericKeySkipped(t *testing.T) {
	t.Parallel()

	// shortcuts { 0 {...} junk {...} } - a non-numeric key should be skipped,
	// not fail the whole file.
	var buf bytes.Buffer
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails
	writeEntry(&buf, "0", 100, "Real")
	writeEntry(&buf, "junk", 999, "Ignored")
	buf.WriteByte(0x08) //nolint:revive // never fails (end shortcuts)
	buf.WriteByte(0x08) //nolint:revive // never fails (end root)

	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Len(t, shortcuts, 1)
	assert.Equal(t, "Real", shortcuts[0].AppName)
}

// TestParseShortcuts_NumberAcrossBufferBoundary is the regression test for the
// intermittent, size-dependent parse failures: a 4-byte number value whose bytes
// straddle a 4096-byte bufio refill boundary previously caused a short read and
// failed the whole file ("number did not have the required amount of bytes").
func TestParseShortcuts_NumberAcrossBufferBoundary(t *testing.T) {
	t.Parallel()

	// Straddle positions for the 4096 and 8192 boundaries: a value starting at
	// these offsets crosses the boundary part-way through its 4 bytes.
	for _, valueStart := range []int{4093, 4094, 4095, 8189, 8190, 8191} {
		t.Run(strconv.Itoa(valueStart), func(t *testing.T) {
			t.Parallel()

			data := buildVDFWithAppIDAt(valueStart, 0xDEADBEEF)

			// Sanity-check the construction actually straddles a boundary.
			require.NotEqual(t, valueStart/4096, (valueStart+3)/4096,
				"test fixture should straddle a 4096 boundary")

			shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(data))
			require.NoError(t, err)
			require.Len(t, shortcuts, 1)
			assert.Equal(t, uint32(0xDEADBEEF), shortcuts[0].AppID)
			assert.Equal(t, "Boundary Game", shortcuts[0].AppName)
		})
	}
}

func TestParseShortcuts_EmptyShortcutsMap(t *testing.T) {
	t.Parallel()

	// shortcuts { } - empty map
	var buf bytes.Buffer
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteByte(0x08)          //nolint:revive // never fails
	buf.WriteByte(0x08)          //nolint:revive // never fails

	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assert.Empty(t, shortcuts)
}

// writeStr writes a string field (marker 0x01, null-terminated key and value).
func writeStr(buf *bytes.Buffer, key, val string) {
	buf.WriteByte(0x01)  //nolint:revive // never fails
	buf.WriteString(key) //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString(val) //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails
}

// writeEntry writes one shortcut entry map (appid, AppName, Exe, StartDir) keyed
// by the given index string.
func writeEntry(buf *bytes.Buffer, key string, appID uint32, name string) {
	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString(key) //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails

	buf.WriteByte(0x02)      //nolint:revive // never fails
	buf.WriteString("appid") //nolint:revive // never fails
	buf.WriteByte(0x00)      //nolint:revive // never fails
	idb := make([]byte, 4)
	binary.LittleEndian.PutUint32(idb, appID)
	buf.Write(idb) //nolint:revive // never fails

	writeStr(buf, "AppName", name)
	writeStr(buf, "Exe", "/path/to/game")
	writeStr(buf, "StartDir", "/path/to")

	buf.WriteByte(0x08) //nolint:revive // never fails (end entry)
}

// buildVDFWithAppIDAt builds a single-shortcut binary VDF where the appid's
// 4-byte value begins exactly at file offset valueStart, by padding a filler
// string field. Used to place a number value across a bufio refill boundary.
func buildVDFWithAppIDAt(valueStart int, appID uint32) []byte {
	var buf bytes.Buffer

	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString("0") //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails

	// filler string field sized so the appid value lands at valueStart.
	// bytes before value = 11 (header) + 3 (entry) + (9 + pad) (filler) + 7 (appid field)
	pad := valueStart - 30
	buf.WriteByte(0x01)                       //nolint:revive // never fails
	buf.WriteString("filler")                 //nolint:revive // never fails
	buf.WriteByte(0x00)                       //nolint:revive // never fails
	buf.Write(bytes.Repeat([]byte{'A'}, pad)) //nolint:revive // never fails
	buf.WriteByte(0x00)                       //nolint:revive // never fails

	buf.WriteByte(0x02)      //nolint:revive // never fails
	buf.WriteString("appid") //nolint:revive // never fails
	buf.WriteByte(0x00)      //nolint:revive // never fails
	idb := make([]byte, 4)
	binary.LittleEndian.PutUint32(idb, appID)
	buf.Write(idb) //nolint:revive // never fails

	writeStr(&buf, "AppName", "Boundary Game")
	writeStr(&buf, "Exe", "/path/to/game")
	writeStr(&buf, "StartDir", "/path/to")

	buf.WriteByte(0x08) //nolint:revive // never fails (end entry)
	buf.WriteByte(0x08) //nolint:revive // never fails (end shortcuts)
	buf.WriteByte(0x08) //nolint:revive // never fails (end root)

	return buf.Bytes()
}

// buildShortcutVDF builds a binary VDF with a single shortcut using the given key names.
// Keys map: "appname" key, "exe" key, "startdir" key (actual string to write in the binary).
func buildShortcutVDF(appNameKey, exeKey, startDirKey string) []byte {
	var buf bytes.Buffer

	// shortcuts map start
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("shortcuts") //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	// shortcut "0" map start
	buf.WriteByte(0x00)  //nolint:revive // never fails
	buf.WriteString("0") //nolint:revive // never fails
	buf.WriteByte(0x00)  //nolint:revive // never fails

	// appid (number)
	buf.WriteByte(0x02)                       //nolint:revive // never fails
	buf.WriteString("appid")                  //nolint:revive // never fails
	buf.WriteByte(0x00)                       //nolint:revive // never fails
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04}) //nolint:revive // never fails

	// AppName (string) - key name varies
	buf.WriteByte(0x01)               //nolint:revive // never fails
	buf.WriteString(appNameKey)       //nolint:revive // never fails
	buf.WriteByte(0x00)               //nolint:revive // never fails
	buf.WriteString("Case Test Game") //nolint:revive // never fails
	buf.WriteByte(0x00)               //nolint:revive // never fails

	// Exe (string) - key name varies
	buf.WriteByte(0x01)              //nolint:revive // never fails
	buf.WriteString(exeKey)          //nolint:revive // never fails
	buf.WriteByte(0x00)              //nolint:revive // never fails
	buf.WriteString("/path/to/game") //nolint:revive // never fails
	buf.WriteByte(0x00)              //nolint:revive // never fails

	// StartDir (string) - key name varies
	buf.WriteByte(0x01)          //nolint:revive // never fails
	buf.WriteString(startDirKey) //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails
	buf.WriteString("/path/to")  //nolint:revive // never fails
	buf.WriteByte(0x00)          //nolint:revive // never fails

	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails
	buf.WriteByte(0x08) //nolint:revive // never fails

	return buf.Bytes()
}

func TestParseShortcuts_CaseInsensitiveKeys(t *testing.T) {
	t.Parallel()

	// All-lowercase keys as seen in some shortcuts.vdf files (issue #514)
	data := buildShortcutVDF("appname", "exe", "startdir")
	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(data))

	require.NoError(t, err, "should parse shortcuts with all-lowercase keys")
	require.Len(t, shortcuts, 1)
	assert.Equal(t, "Case Test Game", shortcuts[0].AppName)
	assert.Equal(t, "/path/to/game", shortcuts[0].Exe)
	assert.Equal(t, "/path/to", shortcuts[0].StartDir)
}

func TestParseShortcuts_MixedCaseKeys(t *testing.T) {
	t.Parallel()

	// Unusual mixed case as a stress test
	data := buildShortcutVDF("APPNAME", "eXe", "sTaRtDiR")
	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(data))

	require.NoError(t, err, "should parse shortcuts with unusual mixed-case keys")
	require.Len(t, shortcuts, 1)
	assert.Equal(t, "Case Test Game", shortcuts[0].AppName)
	assert.Equal(t, "/path/to/game", shortcuts[0].Exe)
	assert.Equal(t, "/path/to", shortcuts[0].StartDir)
}
