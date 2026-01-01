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

package steam

import (
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBinaryVDFReader_ReadUint8(t *testing.T) {
	t.Parallel()

	t.Run("reads_single_byte", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{0x42})
		v, err := r.readUint8()

		require.NoError(t, err)
		assert.Equal(t, uint8(0x42), v)
		assert.Equal(t, 1, r.pos)
	})

	t.Run("returns_EOF_when_empty", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{})
		_, err := r.readUint8()

		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("returns_EOF_at_end", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{0x01})
		_, _ = r.readUint8() // consume the byte

		_, err := r.readUint8()
		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestBinaryVDFReader_ReadUint32(t *testing.T) {
	t.Parallel()

	t.Run("reads_little_endian_uint32", func(t *testing.T) {
		t.Parallel()

		// 0x12345678 in little-endian
		r := newBinaryVDFReader([]byte{0x78, 0x56, 0x34, 0x12})
		v, err := r.readUint32()

		require.NoError(t, err)
		assert.Equal(t, uint32(0x12345678), v)
		assert.Equal(t, 4, r.pos)
	})

	t.Run("returns_EOF_when_not_enough_bytes", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{0x01, 0x02, 0x03}) // only 3 bytes
		_, err := r.readUint32()

		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestBinaryVDFReader_ReadUint64(t *testing.T) {
	t.Parallel()

	t.Run("reads_little_endian_uint64", func(t *testing.T) {
		t.Parallel()

		data := make([]byte, 8)
		binary.LittleEndian.PutUint64(data, 0x123456789ABCDEF0)
		r := newBinaryVDFReader(data)
		v, err := r.readUint64()

		require.NoError(t, err)
		assert.Equal(t, uint64(0x123456789ABCDEF0), v)
		assert.Equal(t, 8, r.pos)
	})

	t.Run("returns_EOF_when_not_enough_bytes", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}) // only 7 bytes
		_, err := r.readUint64()

		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestBinaryVDFReader_ReadInt64(t *testing.T) {
	t.Parallel()

	t.Run("reads_positive_int64", func(t *testing.T) {
		t.Parallel()

		data := make([]byte, 8)
		binary.LittleEndian.PutUint64(data, uint64(123456789))
		r := newBinaryVDFReader(data)
		v, err := r.readInt64()

		require.NoError(t, err)
		assert.Equal(t, int64(123456789), v)
	})

	t.Run("reads_negative_int64", func(t *testing.T) {
		t.Parallel()

		data := make([]byte, 8)
		// -1 in two's complement is all 1s
		binary.LittleEndian.PutUint64(data, 0xFFFFFFFFFFFFFFFF)
		r := newBinaryVDFReader(data)
		v, err := r.readInt64()

		require.NoError(t, err)
		assert.Equal(t, int64(-1), v)
	})

	t.Run("returns_EOF_when_not_enough_bytes", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07})
		_, err := r.readInt64()

		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestBinaryVDFReader_ReadBytes(t *testing.T) {
	t.Parallel()

	t.Run("reads_requested_bytes", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{0x01, 0x02, 0x03, 0x04, 0x05})
		v, err := r.readBytes(3)

		require.NoError(t, err)
		assert.Equal(t, []byte{0x01, 0x02, 0x03}, v)
		assert.Equal(t, 3, r.pos)
	})

	t.Run("returns_EOF_when_not_enough_bytes", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{0x01, 0x02})
		_, err := r.readBytes(5)

		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestBinaryVDFReader_ReadNullString(t *testing.T) {
	t.Parallel()

	t.Run("reads_null_terminated_string", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{'h', 'e', 'l', 'l', 'o', 0x00, 'x'})
		v, err := r.readNullString()

		require.NoError(t, err)
		assert.Equal(t, "hello", v)
		assert.Equal(t, 6, r.pos) // includes null terminator
	})

	t.Run("reads_empty_string", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{0x00, 'x'})
		v, err := r.readNullString()

		require.NoError(t, err)
		assert.Empty(t, v)
		assert.Equal(t, 1, r.pos)
	})

	t.Run("returns_EOF_when_no_null_terminator", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{'h', 'e', 'l', 'l', 'o'}) // no null terminator
		_, err := r.readNullString()

		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestBinaryVDFReader_ReadKey(t *testing.T) {
	t.Parallel()

	t.Run("reads_null_string_for_v27", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{'k', 'e', 'y', 0x00})
		r.magic = magic27
		v, err := r.readKey()

		require.NoError(t, err)
		assert.Equal(t, "key", v)
	})

	t.Run("reads_null_string_for_v28", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{'k', 'e', 'y', 0x00})
		r.magic = magic28
		v, err := r.readKey()

		require.NoError(t, err)
		assert.Equal(t, "key", v)
	})

	t.Run("reads_from_pool_for_v29", func(t *testing.T) {
		t.Parallel()

		// Index 2 in little-endian
		r := newBinaryVDFReader([]byte{0x02, 0x00, 0x00, 0x00})
		r.magic = magic29
		r.pool = []string{"zero", "one", "two", "three"}
		v, err := r.readKey()

		require.NoError(t, err)
		assert.Equal(t, "two", v)
	})

	t.Run("returns_error_for_invalid_pool_index", func(t *testing.T) {
		t.Parallel()

		// Index 10 but pool only has 4 entries
		r := newBinaryVDFReader([]byte{0x0A, 0x00, 0x00, 0x00})
		r.magic = magic29
		r.pool = []string{"zero", "one", "two", "three"}
		_, err := r.readKey()

		assert.ErrorIs(t, err, ErrInvalidFormat)
	})

	t.Run("falls_back_to_null_string_when_pool_empty", func(t *testing.T) {
		t.Parallel()

		r := newBinaryVDFReader([]byte{'k', 'e', 'y', 0x00})
		r.magic = magic29
		r.pool = []string{} // empty pool
		v, err := r.readKey()

		require.NoError(t, err)
		assert.Equal(t, "key", v)
	})
}

func TestBinaryVDFReader_ReadObject(t *testing.T) {
	t.Parallel()

	t.Run("reads_empty_object", func(t *testing.T) {
		t.Parallel()

		// Just an end marker
		r := newBinaryVDFReader([]byte{vdfTypeEnd})
		r.magic = magic27
		obj, err := r.readObject()

		require.NoError(t, err)
		assert.Empty(t, obj)
	})

	t.Run("reads_object_with_string", func(t *testing.T) {
		t.Parallel()

		// type=string, key="name\0", value="test\0", end
		data := []byte{
			vdfTypeString,
			'n', 'a', 'm', 'e', 0x00, // key
			't', 'e', 's', 't', 0x00, // value
			vdfTypeEnd,
		}
		r := newBinaryVDFReader(data)
		r.magic = magic27
		obj, err := r.readObject()

		require.NoError(t, err)
		assert.Equal(t, "test", obj["name"])
	})

	t.Run("reads_object_with_uint32", func(t *testing.T) {
		t.Parallel()

		// type=uint32, key="appid\0", value=12345 (little-endian), end
		data := []byte{
			vdfTypeUint32,
			'a', 'p', 'p', 'i', 'd', 0x00, // key
			0x39, 0x30, 0x00, 0x00, // 12345 in little-endian
			vdfTypeEnd,
		}
		r := newBinaryVDFReader(data)
		r.magic = magic27
		obj, err := r.readObject()

		require.NoError(t, err)
		assert.Equal(t, uint32(12345), obj["appid"])
	})

	t.Run("reads_nested_object", func(t *testing.T) {
		t.Parallel()

		// type=nested, key="config\0", { type=string, key="exe\0", value="game.exe\0", end }, end
		data := []byte{
			vdfTypeNested,
			'c', 'o', 'n', 'f', 'i', 'g', 0x00, // key
			// nested object content
			vdfTypeString,
			'e', 'x', 'e', 0x00, // key
			'g', 'a', 'm', 'e', '.', 'e', 'x', 'e', 0x00, // value
			vdfTypeEnd, // end of nested
			vdfTypeEnd, // end of outer
		}
		r := newBinaryVDFReader(data)
		r.magic = magic27
		obj, err := r.readObject()

		require.NoError(t, err)
		config, ok := obj["config"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "game.exe", config["exe"])
	})

	t.Run("returns_error_for_unknown_type", func(t *testing.T) {
		t.Parallel()

		// type=0xFF (unknown), key="test\0"
		data := []byte{
			0xFF, // unknown type
			't', 'e', 's', 't', 0x00,
		}
		r := newBinaryVDFReader(data)
		r.magic = magic27
		_, err := r.readObject()

		assert.ErrorIs(t, err, ErrInvalidFormat)
	})
}

func TestExtractLaunchConfigs(t *testing.T) {
	t.Parallel()

	t.Run("extracts_launch_configs", func(t *testing.T) {
		t.Parallel()

		obj := map[string]any{
			"config": map[string]any{
				"launch": map[string]any{
					"0": map[string]any{
						"executable": "game.exe",
						"arguments":  "-fullscreen",
						"type":       "default",
						"workingdir": "bin",
						"config": map[string]any{
							"oslist": "windows",
						},
					},
					"1": map[string]any{
						"executable": "game.sh",
						"type":       "default",
						"config": map[string]any{
							"oslist": "linux",
						},
					},
				},
			},
		}

		configs := extractLaunchConfigs(obj)

		assert.Len(t, configs, 2)

		// Find the windows config
		var windowsConfig *LaunchConfig
		for i := range configs {
			if configs[i].OSList == "windows" {
				windowsConfig = &configs[i]
				break
			}
		}
		require.NotNil(t, windowsConfig)
		assert.Equal(t, "game.exe", windowsConfig.Executable)
		assert.Equal(t, "-fullscreen", windowsConfig.Arguments)
		assert.Equal(t, "default", windowsConfig.Type)
		assert.Equal(t, "bin", windowsConfig.WorkingDir)
	})

	t.Run("skips_entries_without_executable", func(t *testing.T) {
		t.Parallel()

		obj := map[string]any{
			"config": map[string]any{
				"launch": map[string]any{
					"0": map[string]any{
						"type": "none", // no executable
					},
					"1": map[string]any{
						"executable": "game.exe",
					},
				},
			},
		}

		configs := extractLaunchConfigs(obj)

		assert.Len(t, configs, 1)
		assert.Equal(t, "game.exe", configs[0].Executable)
	})

	t.Run("returns_empty_when_no_config", func(t *testing.T) {
		t.Parallel()

		obj := map[string]any{
			"other": "data",
		}

		configs := extractLaunchConfigs(obj)

		assert.Empty(t, configs)
	})

	t.Run("returns_empty_when_no_launch", func(t *testing.T) {
		t.Parallel()

		obj := map[string]any{
			"config": map[string]any{
				"other": "data",
			},
		}

		configs := extractLaunchConfigs(obj)

		assert.Empty(t, configs)
	})

	t.Run("handles_non_map_entries", func(t *testing.T) {
		t.Parallel()

		obj := map[string]any{
			"config": map[string]any{
				"launch": map[string]any{
					"0":    "not a map",
					"type": "also not a map",
				},
			},
		}

		configs := extractLaunchConfigs(obj)

		assert.Empty(t, configs)
	})
}

func TestMatchesOS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		oslist  string
		target  string
		matches bool
	}{
		{"exact_match_windows", "windows", "windows", true},
		{"exact_match_linux", "linux", "linux", true},
		{"exact_match_macos", "macos", "macos", true},
		{"comma_separated_first", "windows,linux", "windows", true},
		{"comma_separated_second", "windows,linux", "linux", true},
		{"no_match", "windows", "linux", false},
		{"partial_match_not_allowed", "win", "windows", false},
		{"empty_oslist", "", "windows", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := matchesOS(tc.oslist, tc.target)
			assert.Equal(t, tc.matches, result)
		})
	}
}

func TestGetCurrentOSString(t *testing.T) {
	t.Parallel()

	// Just verify it returns a non-empty string
	// The actual value depends on the OS running the test
	result := getCurrentOSString()
	assert.NotEmpty(t, result)
	assert.Contains(t, []string{"windows", "linux", "macos"}, result)
}
