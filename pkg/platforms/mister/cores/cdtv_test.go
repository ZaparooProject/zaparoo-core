//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package cores

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgl"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cdtvConfigFixture(size int) []byte {
	data := bytes.Repeat([]byte{0x5a}, size)
	copy(data, "MNMGCFG0")
	data[minimigCD32DriveOffset] = 0
	data[minimigCDTVDriveOffset] = 1
	return data
}

func TestPrepareCDTVConfig(t *testing.T) {
	t.Parallel()
	for _, size := range []int{minimigCDTVConfigSize, minimigCDTVConfigSize + 2} {
		for _, root := range []string{
			misterconfig.SDRootDir,
			filepath.Join(string(filepath.Separator), "media", "usb0"),
			filepath.Join(string(filepath.Separator), "media", "network", "Movies and Games"),
		} {
			fs := afero.NewMemMapFs()
			cfgPath := filepath.Join(misterconfig.SDRootDir, "config", "CDTV.cfg")
			mediaPath := filepath.Join(root, "games", "CDTV", "Test & Game.CHD")
			original := cdtvConfigFixture(size)
			require.NoError(t, fs.MkdirAll(filepath.Dir(cfgPath), 0o750))
			require.NoError(t, fs.MkdirAll(filepath.Dir(mediaPath), 0o750))
			require.NoError(t, afero.WriteFile(fs, cfgPath, original, 0o600))
			require.NoError(t, afero.WriteFile(fs, mediaPath, nil, 0o600))

			core, err := GetCore("CommodoreCDTV")
			require.NoError(t, err)
			override, err := hookCDTVWithFS(fs, core, mediaPath)
			require.NoError(t, err)
			document, err := mgl.Generate(core, core.RBF, mediaPath, override)
			require.NoError(t, err)
			assert.Contains(t, document, "<setname>CDTV</setname>")
			assert.Contains(t, document, "<rbf>_Computer/Minimig</rbf>")
			assert.NotContains(t, document, "<file", "CDTV must not attempt a floppy MGL mount")
			got, err := afero.ReadFile(fs, cfgPath)
			require.NoError(t, err)
			expected := bytes.Clone(original)
			clear(expected[minimigCDTVPathOffset : minimigCDTVPathOffset+minimigCDPathSize])
			copy(expected[minimigCDTVPathOffset:], mediaPath)
			assert.Equal(t, expected, got, "only CDTV filename should change")
		}
	}
}

func TestPrepareCDTVConfigRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"signature", "old layout", "unknown layout", "CD32 enabled", "CDTV disabled",
		"missing config", "missing image", "directory", "relative path", "NUL", "long path", "unsupported extension",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			cfgPath := filepath.Join(misterconfig.SDRootDir, "config", "CDTV.cfg")
			mediaPath := filepath.Join(misterconfig.SDRootDir, "games", "CDTV", "Game.chd")
			data := cdtvConfigFixture(minimigCDTVConfigSize)
			switch name {
			case "signature":
				data[0] = 0
			case "old layout":
				data = data[:3208]
			case "unknown layout":
				data = append(data, 0)
			case "CD32 enabled":
				data[minimigCD32DriveOffset] = 1
			case "CDTV disabled":
				data[minimigCDTVDriveOffset] = 0
			case "relative path":
				mediaPath = filepath.Join("games", "CDTV", "Game.chd")
			case "NUL":
				mediaPath = filepath.Join(misterconfig.SDRootDir, "Game.chd") + "\x00.chd"
			case "long path":
				mediaPath = filepath.Join(misterconfig.SDRootDir, strings.Repeat("a", minimigCDPathSize)+".chd")
			case "unsupported extension":
				mediaPath = filepath.Join(misterconfig.SDRootDir, "Game.mkv")
			}
			require.NoError(t, fs.MkdirAll(filepath.Dir(cfgPath), 0o750))
			if name != "missing config" {
				require.NoError(t, afero.WriteFile(fs, cfgPath, data, 0o600))
			}
			require.NoError(t, fs.MkdirAll(filepath.Dir(mediaPath), 0o750))
			if name == "directory" {
				require.NoError(t, fs.Mkdir(mediaPath, 0o750))
			} else if name != "missing image" {
				require.NoError(t, afero.WriteFile(fs, mediaPath, nil, 0o600))
			}
			require.Error(t, prepareCDTVConfig(fs, cfgPath, mediaPath))
			if name != "missing config" {
				got, err := afero.ReadFile(fs, cfgPath)
				require.NoError(t, err)
				assert.Equal(t, data, got, "rejected launch must leave configuration untouched")
			}
		})
	}
}

func TestHookCDTVCoreOnlyAndSetName(t *testing.T) {
	t.Parallel()
	core, err := GetCore("CommodoreCDTV")
	require.NoError(t, err)
	override, err := RunSystemHook(nil, core, "")
	require.NoError(t, err)
	assert.Empty(t, override)
	custom := *core
	custom.SetNameSameDir = true
	_, err = RunSystemHook(nil, &custom, filepath.Join(misterconfig.SDRootDir, "Game.chd"))
	require.ErrorContains(t, err, "without same_dir")
}

func FuzzPrepareCDTVConfig(f *testing.F) {
	f.Add(cdtvConfigFixture(minimigCDTVConfigSize), "Game.chd")
	f.Add([]byte("MNMGCFG0"), "Game.chd")
	f.Fuzz(func(t *testing.T, data []byte, name string) {
		if len(data) > minimigCDTVConfigSize+2 || len(name) > minimigCDPathSize {
			t.Skip()
		}
		fs := afero.NewMemMapFs()
		cfgPath := filepath.Join(misterconfig.SDRootDir, "config", "CDTV.cfg")
		// Keep arbitrary fuzz names out of filesystem setup; exercise the path
		// validation separately from accessibility with one known image.
		imagePath := filepath.Join(misterconfig.SDRootDir, "Game.chd")
		require.NoError(t, fs.MkdirAll(filepath.Dir(cfgPath), 0o750))
		require.NoError(t, afero.WriteFile(fs, cfgPath, data, 0o600))
		require.NoError(t, afero.WriteFile(fs, imagePath, nil, 0o600))
		err := prepareCDTVConfig(fs, cfgPath, filepath.Join(misterconfig.SDRootDir, name))
		got, readErr := afero.ReadFile(fs, cfgPath)
		require.NoError(t, readErr)
		if err != nil {
			require.Equal(t, data, got)
		} else {
			require.Equal(t, data[:minimigCDTVPathOffset], got[:minimigCDTVPathOffset])
			end := minimigCDTVPathOffset + minimigCDPathSize
			require.Equal(t, data[end:], got[end:])
		}
	})
}
