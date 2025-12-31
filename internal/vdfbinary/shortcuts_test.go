package vdfbinary_test

import (
	"bytes"
	_ "embed"
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
	buf.WriteByte(0x00) // map marker
	buf.WriteString("shortcuts")
	buf.WriteByte(0x00) // null terminator

	// shortcut "0" map start
	buf.WriteByte(0x00) // map marker
	buf.WriteString("0")
	buf.WriteByte(0x00) // null terminator

	// appid (number)
	buf.WriteByte(0x02) // number marker
	buf.WriteString("appid")
	buf.WriteByte(0x00)
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04}) // 0x04030201 in little endian

	// AppName (string)
	buf.WriteByte(0x01) // string marker
	buf.WriteString("AppName")
	buf.WriteByte(0x00)
	buf.WriteString("Test Game")
	buf.WriteByte(0x00)

	// Exe (string)
	buf.WriteByte(0x01)
	buf.WriteString("Exe")
	buf.WriteByte(0x00)
	buf.WriteString("/path/to/game")
	buf.WriteByte(0x00)

	// StartDir (string)
	buf.WriteByte(0x01)
	buf.WriteString("StartDir")
	buf.WriteByte(0x00)
	buf.WriteString("/path/to")
	buf.WriteByte(0x00)

	// Note: deliberately missing icon, IsHidden, and tags

	// End of shortcut "0" map
	buf.WriteByte(0x08)

	// End of shortcuts map
	buf.WriteByte(0x08)

	// End of root map
	buf.WriteByte(0x08)

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
	buf.WriteByte(0x00) // map marker for shortcuts
	buf.WriteString("shortcuts")
	buf.WriteByte(0x00)

	buf.WriteByte(0x00) // map marker for "0"
	buf.WriteString("0")
	buf.WriteByte(0x00)

	// Only AppName, missing appid
	buf.WriteByte(0x01)
	buf.WriteString("AppName")
	buf.WriteByte(0x00)
	buf.WriteString("Test")
	buf.WriteByte(0x00)

	buf.WriteByte(0x08) // end "0"
	buf.WriteByte(0x08) // end shortcuts
	buf.WriteByte(0x08) // end root

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "appid")
}

func TestParseShortcuts_MissingRequiredField_AppName(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	buf.WriteByte(0x00)
	buf.WriteString("shortcuts")
	buf.WriteByte(0x00)

	buf.WriteByte(0x00)
	buf.WriteString("0")
	buf.WriteByte(0x00)

	// appid present
	buf.WriteByte(0x02)
	buf.WriteString("appid")
	buf.WriteByte(0x00)
	buf.Write([]byte{0x01, 0x00, 0x00, 0x00})

	// Missing AppName, only Exe
	buf.WriteByte(0x01)
	buf.WriteString("Exe")
	buf.WriteByte(0x00)
	buf.WriteString("/path")
	buf.WriteByte(0x00)

	buf.WriteByte(0x08)
	buf.WriteByte(0x08)
	buf.WriteByte(0x08)

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AppName")
}

func TestParseShortcuts_MissingRequiredField_Exe(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	buf.WriteByte(0x00)
	buf.WriteString("shortcuts")
	buf.WriteByte(0x00)

	buf.WriteByte(0x00)
	buf.WriteString("0")
	buf.WriteByte(0x00)

	buf.WriteByte(0x02)
	buf.WriteString("appid")
	buf.WriteByte(0x00)
	buf.Write([]byte{0x01, 0x00, 0x00, 0x00})

	buf.WriteByte(0x01)
	buf.WriteString("AppName")
	buf.WriteByte(0x00)
	buf.WriteString("Test")
	buf.WriteByte(0x00)

	// Missing Exe, only StartDir
	buf.WriteByte(0x01)
	buf.WriteString("StartDir")
	buf.WriteByte(0x00)
	buf.WriteString("/path")
	buf.WriteByte(0x00)

	buf.WriteByte(0x08)
	buf.WriteByte(0x08)
	buf.WriteByte(0x08)

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Exe")
}

func TestParseShortcuts_MissingRequiredField_StartDir(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	buf.WriteByte(0x00)
	buf.WriteString("shortcuts")
	buf.WriteByte(0x00)

	buf.WriteByte(0x00)
	buf.WriteString("0")
	buf.WriteByte(0x00)

	buf.WriteByte(0x02)
	buf.WriteString("appid")
	buf.WriteByte(0x00)
	buf.Write([]byte{0x01, 0x00, 0x00, 0x00})

	buf.WriteByte(0x01)
	buf.WriteString("AppName")
	buf.WriteByte(0x00)
	buf.WriteString("Test")
	buf.WriteByte(0x00)

	buf.WriteByte(0x01)
	buf.WriteString("Exe")
	buf.WriteByte(0x00)
	buf.WriteString("/path/to/exe")
	buf.WriteByte(0x00)

	// Missing StartDir
	buf.WriteByte(0x08)
	buf.WriteByte(0x08)
	buf.WriteByte(0x08)

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StartDir")
}

func TestParseShortcuts_TruncatedNumber(t *testing.T) {
	t.Parallel()

	// Number field with only 2 bytes instead of 4
	var buf bytes.Buffer
	buf.WriteByte(0x00)
	buf.WriteString("shortcuts")
	buf.WriteByte(0x00)

	buf.WriteByte(0x00)
	buf.WriteString("0")
	buf.WriteByte(0x00)

	buf.WriteByte(0x02) // number marker
	buf.WriteString("appid")
	buf.WriteByte(0x00)
	buf.Write([]byte{0x01, 0x02}) // Only 2 bytes, needs 4

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

	// shortcuts { 1 { ... } } - starts at 1 instead of 0
	var buf bytes.Buffer
	buf.WriteByte(0x00)
	buf.WriteString("shortcuts")
	buf.WriteByte(0x00)

	buf.WriteByte(0x00)
	buf.WriteString("1") // Index 1 instead of 0
	buf.WriteByte(0x00)

	buf.WriteByte(0x02)
	buf.WriteString("appid")
	buf.WriteByte(0x00)
	buf.Write([]byte{0x01, 0x00, 0x00, 0x00})

	buf.WriteByte(0x08)
	buf.WriteByte(0x08)
	buf.WriteByte(0x08)

	_, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "index")
}

func TestParseShortcuts_EmptyShortcutsMap(t *testing.T) {
	t.Parallel()

	// shortcuts { } - empty map
	var buf bytes.Buffer
	buf.WriteByte(0x00)
	buf.WriteString("shortcuts")
	buf.WriteByte(0x00)
	buf.WriteByte(0x08) // end shortcuts immediately
	buf.WriteByte(0x08) // end root

	shortcuts, err := vdfbinary.ParseShortcuts(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assert.Empty(t, shortcuts)
}
