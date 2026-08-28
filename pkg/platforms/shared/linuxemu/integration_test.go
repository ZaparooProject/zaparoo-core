//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package linuxemu

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDesktopEmulationOptionsWiresLaunchEnvironment(t *testing.T) {
	t.Parallel()

	options := DesktopEmulationOptions(nil, sharedretroarch.Options{Exec: []string{"retroarch"}})
	require.NotNil(t, options.RetroArch)
	assert.Equal(t, []string{"retroarch"}, options.RetroArch.Exec)
	assert.NotNil(t, options.LaunchEnv)
	assert.NotNil(t, options.RetroArch.LaunchEnv)
}

func TestDefaultEmuDeckPathsReadsSettings(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	settingsDir := filepath.Join(home, filepath.FromSlash(emuDeckSettingsDir))
	require.NoError(t, os.MkdirAll(settingsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(settingsDir, "settings.sh"), []byte(
		"romsPath=\"$HOME/Games/Emulation/roms\"\nmalicious=$(touch /tmp/nope)\n",
	), 0o600))

	paths := DefaultEmuDeckPaths(home)
	assert.Equal(t, filepath.Join(home, "Games", "Emulation", "roms"), paths.RomsPath)
	assert.Equal(t, filepath.Join(home, "ES-DE", "gamelists"), paths.GamelistPath)
}

func TestDefaultEmuDeckPathsRejectsShellExpansion(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	settingsDir := filepath.Join(home, filepath.FromSlash(emuDeckSettingsDir))
	require.NoError(t, os.MkdirAll(settingsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(settingsDir, "settings.sh"), []byte(
		"romsPath=$(printf /tmp/unsafe)\n",
	), 0o600))

	paths := DefaultEmuDeckPaths(home)
	assert.Equal(t, filepath.Join(home, "Emulation", "roms"), paths.RomsPath)
}

func TestDefaultRetroDECKPathsReadsJSON(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	configDir := filepath.Join(home, filepath.FromSlash(retroDECKConfigDir))
	require.NoError(t, os.MkdirAll(configDir, 0o750))
	customHome := filepath.Join(home, "Games", "RetroDECK")
	customRoms := filepath.Join(home, "External", "roms")
	content := `{"version":"1","paths":{"rd_home_path":"` + customHome + `","roms_path":"` + customRoms + `"}}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "retrodeck.json"), []byte(content), 0o600))

	paths := DefaultRetroDECKPaths(home)
	assert.Equal(t, customRoms, paths.RomsPath)
	assert.Equal(t, filepath.Join(customHome, "ES-DE", "gamelists"), paths.GamelistPath)
}

func TestReadProviderSystemFoldersRejectsExcessiveDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for i := range maxProviderSystemDirs + 1 {
		require.NoError(t, os.Mkdir(filepath.Join(root, fmt.Sprintf("system-%04d", i)), 0o750))
	}

	_, err := readProviderSystemFolders(root)
	require.ErrorContains(t, err, "system limit")
}

func TestReadProviderSystemFoldersRejectsSymlinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "nes"), 0o750))
	target := t.TempDir()
	require.NoError(t, os.Symlink(target, filepath.Join(root, "linked")))

	folders, err := readProviderSystemFolders(root)
	require.NoError(t, err)
	assert.Equal(t, []string{"nes"}, folders)
}

func TestReadLauncherTargetRejectsOversizedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.ps3")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("a", maxLauncherTargetSize+1)), 0o600))

	_, err := readLauncherTarget(path, ".ps3")
	require.ErrorContains(t, err, "invalid launcher target")
}

func TestStandaloneDiscoversNativeBeforeFlatpak(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	options := NewOptions(home, sharedretroarch.Options{})
	options.IncludeRetroArch = false
	options.IncludeProviderDecks = false
	options.LookPath = func(name string) (string, error) {
		if name == "scummvm" {
			return "/usr/bin/scummvm", nil
		}
		return "", errors.New("missing")
	}
	options.IsFlatpakInstalled = func(string) bool { return true }
	launchers := Launchers(nil, options, nil)
	for i := range launchers {
		if launchers[i].ID != "ScummVMStandalone" {
			continue
		}
		target := filepath.Join(home, "game.scummvm")
		require.NoError(t, os.WriteFile(target, []byte("monkey"), 0o600))
		command, err := launchers[i].BuildLaunchCommand(nil, target, nil)
		require.NoError(t, err)
		assert.Equal(t, "/usr/bin/scummvm", command.Executable)
		assert.Equal(t, platforms.LifecycleBlocking, launchers[i].Lifecycle)
		return
	}
	t.Fatal("native ScummVM launcher not discovered")
}

func TestStandaloneDiscoversEmuDeckAppImage(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	application := filepath.Join(home, "Applications", "pcsx2-Qt-2.0.AppImage")
	require.NoError(t, os.MkdirAll(filepath.Dir(application), 0o750))
	require.NoError(t, os.WriteFile(application, []byte("appimage"), 0o600))
	require.NoError(t, os.Chmod(application, 0o700)) //nolint:gosec // Test executable must be runnable.
	options := NewOptions(home, sharedretroarch.Options{})
	options.IncludeRetroArch = false
	options.IncludeProviderDecks = false
	options.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	options.IsFlatpakInstalled = func(string) bool { return false }
	launchers := Launchers(nil, options, nil)
	for i := range launchers {
		if launchers[i].ID != "PCSX2" {
			continue
		}
		mediaPath := filepath.Join(home, "game.iso")
		require.NoError(t, os.WriteFile(mediaPath, []byte("rom"), 0o600))
		command, err := launchers[i].BuildLaunchCommand(nil, mediaPath, nil)
		require.NoError(t, err)
		assert.Equal(t, application, command.Executable)
		assert.Equal(t, platforms.LifecycleBlocking, launchers[i].Lifecycle)
		return
	}
	t.Fatal("PCSX2 AppImage launcher not discovered")
}

func TestStandaloneDiscoversCurrentUpstreamNames(t *testing.T) {
	t.Parallel()

	for executable, launcherID := range map[string]string{
		"PPSSPPSDL":    "PPSSPP",
		"xenia_canary": "XeniaCanary",
		"Ryujinx":      "Ryubing",
	} {
		t.Run(executable, func(t *testing.T) {
			t.Parallel()
			options := NewOptions(t.TempDir(), sharedretroarch.Options{})
			options.IncludeRetroArch = false
			options.IncludeProviderDecks = false
			options.LookPath = func(name string) (string, error) {
				if name == executable {
					return filepath.Join(string(filepath.Separator), "usr", "bin", name), nil
				}
				return "", errors.New("missing")
			}
			options.IsFlatpakInstalled = func(string) bool { return false }
			assert.Contains(t, launcherIDs(Launchers(nil, options, nil)), launcherID)
		})
	}
}

func TestStandaloneDiscoversRyujinxAppImage(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	application := filepath.Join(home, "Applications", "ryujinx-canary.AppImage")
	require.NoError(t, os.MkdirAll(filepath.Dir(application), 0o750))
	require.NoError(t, os.WriteFile(application, []byte("appimage"), 0o600))
	require.NoError(t, os.Chmod(application, 0o700)) //nolint:gosec // Test executable must be runnable.
	options := NewOptions(home, sharedretroarch.Options{})
	options.IncludeRetroArch = false
	options.IncludeProviderDecks = false
	options.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	options.IsFlatpakInstalled = func(string) bool { return false }
	assert.Contains(t, launcherIDs(Launchers(nil, options, nil)), "Ryubing")
}

func TestStandaloneCommandRequiresExistingAbsoluteMedia(t *testing.T) {
	t.Parallel()

	options := NewOptions(t.TempDir(), sharedretroarch.Options{})
	options.IsFlatpakInstalled = func(string) bool { return true }
	def := standaloneDef{id: "Test", flatpakID: "example.App", args: batchArgs()}

	_, err := buildStandaloneLaunchCommand(&options, &def, filepath.Join("host", "game.rom"))
	require.ErrorContains(t, err, "absolute")
	_, err = buildStandaloneLaunchCommand(&options, &def, filepath.Join(t.TempDir(), "missing.rom"))
	require.ErrorContains(t, err, "stat media path")
}

func TestStandaloneInstallationAvailability(t *testing.T) {
	t.Parallel()

	executable := filepath.Join(t.TempDir(), "emulator")
	require.NoError(t, os.WriteFile(executable, []byte("binary"), 0o600))
	require.NoError(t, os.Chmod(executable, 0o700)) //nolint:gosec // Test executable must be runnable.
	options := NewOptions(t.TempDir(), sharedretroarch.Options{})
	assert.True(t, standaloneInstallationAvailable(&options, standaloneInstallation{executable: executable}))
	require.NoError(t, os.Chmod(executable, 0o600))
	assert.False(t, standaloneInstallationAvailable(&options, standaloneInstallation{executable: executable}))

	options.IsFlatpakInstalled = func(id string) bool { return id == "example.Installed" }
	assert.True(t, standaloneInstallationAvailable(&options, standaloneInstallation{
		executable: "flatpak", flatpakID: "example.Installed",
	}))
	assert.False(t, standaloneInstallationAvailable(&options, standaloneInstallation{
		executable: "flatpak", flatpakID: "example.Missing",
	}))
}

func TestMAMEArgsUseROMDirectoryAndMachineName(t *testing.T) {
	t.Parallel()

	path := filepath.Join(string(filepath.Separator), "roms", "arcade", "outrun.zip")
	args, err := mameArgs(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"-nowindow", "-rompath", filepath.Dir(path), "outrun"}, args)
	_, err = mameArgs(filepath.Join(string(filepath.Separator), "roms", "arcade", "outrun.chd"))
	require.Error(t, err)
}

func TestStandaloneTargetArguments(t *testing.T) {
	t.Parallel()

	vitaTarget := filepath.Join(t.TempDir(), "game.psvita")
	require.NoError(t, os.WriteFile(vitaTarget, []byte("PCSA00001"), 0o600))
	args, err := vita3KArgs(vitaTarget)
	require.NoError(t, err)
	assert.Equal(t, []string{"-F", "-r", "PCSA00001"}, args)

	ps3Target := filepath.Join(t.TempDir(), "game.ps3")
	require.NoError(t, os.WriteFile(ps3Target, []byte("/games/PS3_GAME"), 0o600))
	args, err = rpcs3Args(ps3Target)
	require.NoError(t, err)
	assert.Equal(t, []string{"--no-gui", "--fullscreen", "/games/PS3_GAME"}, args)

	romPath := filepath.Join(string(filepath.Separator), "roms", "wiiu", "game.rpx")
	args, err = cemuArgs(romPath)
	require.NoError(t, err)
	assert.Equal(t, []string{"-g", romPath, "-f"}, args)
}

func TestAuditedStandaloneArguments(t *testing.T) {
	t.Parallel()

	path := filepath.Join(string(filepath.Separator), "roms", "game.rom")
	cases := map[string][]string{
		"XeniaCanary":       {path},
		"Ryubing":           {"--fullscreen", path},
		"PCSX2":             {"-fullscreen", "-batch", path},
		"RPCS3":             {"--no-gui", "--fullscreen", path},
		"PPSSPP":            {"--fullscreen", path},
		"DolphinGameCube":   {"-b", "-f", "-e", path},
		"DolphinWii":        {"-b", "-f", "-e", path},
		"FlycastDreamcast":  {"-config", "window:fullscreen=yes", path},
		"FlycastNaomi":      {"-config", "window:fullscreen=yes", path},
		"FlycastAtomiswave": {"-config", "window:fullscreen=yes", path},
		"RMG":               {"--fullscreen", "--quit-after-emulation", path},
		"Ruffle":            {"--fullscreen", path},
		"PrimeHackGameCube": {"-b", "-f", "-e", path},
		"PrimeHackWii":      {"-b", "-f", "-e", path},
	}
	for id, expected := range cases {
		var def *standaloneDef
		for i := range nativeStandaloneDefs {
			if nativeStandaloneDefs[i].id == id {
				def = &nativeStandaloneDefs[i]
				break
			}
		}
		require.NotNil(t, def, id)
		actual, err := def.args(path)
		require.NoError(t, err, id)
		assert.Equal(t, expected, actual, id)
	}
}

func TestShadPS4FlatpakRunsEmulatorDirectly(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	target := filepath.Join(home, "game.ps4")
	require.NoError(t, os.WriteFile(target, []byte("CUSA00001"), 0o600))
	options := NewOptions(home, sharedretroarch.Options{})
	def := standaloneDef{
		id: "ShadPS4", flatpakID: "net.shadps4.shadPS4", flatpakCommand: "shadps4", args: shadPS4Args,
	}
	command, err := buildStandaloneLaunchCommandForInstallation(&options, &def, standaloneInstallation{
		executable: "flatpak", flatpakID: def.flatpakID,
	}, target)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"run", "--filesystem=" + home + ":ro", "--die-with-parent", "--command=shadps4",
		"net.shadps4.shadPS4", "-g", "CUSA00001", "--fullscreen", "true",
	}, command.Args)
}

func TestStandaloneDiscoversNewNativeEmulators(t *testing.T) {
	t.Parallel()

	options := NewOptions(t.TempDir(), sharedretroarch.Options{})
	options.IncludeRetroArch = false
	options.IncludeProviderDecks = false
	options.LookPath = func(name string) (string, error) {
		switch name {
		case "mame":
			return "/usr/bin/mame", nil
		case "mgba-qt":
			return "/usr/bin/mgba-qt", nil
		default:
			return "", errors.New("missing")
		}
	}
	options.IsFlatpakInstalled = func(string) bool { return false }
	ids := launcherIDs(Launchers(nil, options, nil))
	assert.Contains(t, ids, "MAME")
	assert.Contains(t, ids, "mGBAGBA")
	assert.Contains(t, ids, "mGBAGB")
	assert.Contains(t, ids, "mGBAGBC")
}

func TestFlatpakRunArgsRejectsUnsafeMediaDirectory(t *testing.T) {
	t.Parallel()

	_, err := flatpakRunArgs("example.App", filepath.Join("host", "game.rom"), nil)
	require.Error(t, err)
	_, err = flatpakRunArgs("example.App", filepath.Join(string(filepath.Separator), "game.rom"), nil)
	require.Error(t, err)
	_, err = flatpakRunArgs("example.App", filepath.Join(string(filepath.Separator), "media:rw", "game.rom"), nil)
	require.Error(t, err)
}

func TestReadLauncherTargetRejectsOptionInjection(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "game.scummvm")
	require.NoError(t, os.WriteFile(path, []byte("--help"), 0o600))

	_, err := readLauncherTarget(path, ".scummvm")
	require.ErrorContains(t, err, "invalid launcher target")
}

func TestProviderLauncherGuardRejectsOutsidePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inside := filepath.Join(root, "nes", "inside.nes")
	outside := filepath.Join(t.TempDir(), "outside.nes")
	writeTestFile(t, inside)
	writeTestFile(t, outside)
	buildCalls := 0
	launcher := platforms.Launcher{
		Test: providerPathTest(root, "nes"),
		BuildLaunchCommand: func(
			*config.Instance,
			string,
			*platforms.LaunchOptions,
		) (*platforms.LaunchCommand, error) {
			buildCalls++
			return &platforms.LaunchCommand{Executable: "emulator"}, nil
		},
	}
	guardProviderLauncher(&launcher)

	_, err := launcher.BuildLaunchCommand(nil, outside, nil)
	require.Error(t, err)
	_, err = launcher.BuildLaunchCommand(nil, inside, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, buildCalls)
}

func TestProviderPathTestRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	systemDir := filepath.Join(root, "nes")
	require.NoError(t, os.MkdirAll(systemDir, 0o750))
	inside := filepath.Join(systemDir, "inside.nes")
	require.NoError(t, os.WriteFile(inside, []byte("rom"), 0o600))
	outside := filepath.Join(t.TempDir(), "outside.nes")
	require.NoError(t, os.WriteFile(outside, []byte("rom"), 0o600))
	linked := filepath.Join(systemDir, "linked.nes")
	require.NoError(t, os.Symlink(outside, linked))
	testPath := providerPathTest(root, "nes")

	assert.True(t, testPath(nil, inside))
	assert.False(t, testPath(nil, linked))
	assert.False(t, testPath(nil, outside))
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("rom"), 0o600))
}

func TestLaunchersDiscoversAvailableProvidersAndSuppressesIDs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	emuPaths := DefaultEmuDeckPaths(home)
	require.NoError(t, os.MkdirAll(filepath.Join(emuPaths.RomsPath, "nes"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(emuPaths.GamelistPath, "nes"), 0o750))
	retroPaths := DefaultRetroDECKPaths(home)
	require.NoError(t, os.MkdirAll(filepath.Join(retroPaths.RomsPath, "snes"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(retroPaths.GamelistPath, "snes"), 0o750))

	installed := map[string]bool{
		RetroArchFlatpakID: true,
		RetroDECKFlatpakID: true,
	}
	options := NewOptions(home, sharedretroarch.Options{Exec: []string{"flatpak"}})
	options.IsFlatpakInstalled = func(id string) bool { return installed[id] }
	options.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	existing := []platforms.Launcher{{ID: "RetroArchSNES9x"}}

	launchers := Launchers(nil, options, existing)
	ids := make(map[string]int)
	byID := make(map[string]platforms.Launcher)
	for i := range launchers {
		ids[launchers[i].ID]++
		byID[launchers[i].ID] = launchers[i]
	}
	assert.Equal(t, 1, ids["EmuDeckNES"])
	assert.Equal(t, 1, ids["RetroDECKSNES"])
	assert.Equal(t, platforms.LifecycleBlocking, byID["EmuDeckNES"].Lifecycle)
	assert.Equal(t, platforms.LifecycleBlocking, byID["RetroDECKSNES"].Lifecycle)
	retroROM := filepath.Join(retroPaths.RomsPath, "snes", "game.sfc")
	require.NoError(t, os.WriteFile(retroROM, []byte("rom"), 0o600))
	command, err := byID["RetroDECKSNES"].BuildLaunchCommand(nil, retroROM, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"run", "--die-with-parent", "--env=LOG_BUFFER=", RetroDECKFlatpakID, retroROM,
	}, command.Args)
	assert.Zero(t, ids["RetroArchSNES9x"])
	assert.NotZero(t, ids["RetroArchFCEUMM"])
	assert.Zero(t, ids["PCSX2"])
}

func TestAttachPlainESDEScannersUsesOnlyExplicitRoot(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "custom-roms")
	romPath := filepath.Join(root, "nes", "game.nes")
	require.NoError(t, os.MkdirAll(filepath.Dir(romPath), 0o750))
	require.NoError(t, os.WriteFile(romPath, []byte("rom"), 0o600))
	gamelistDir := filepath.Join(home, "ES-DE", "gamelists", "nes")
	require.NoError(t, os.MkdirAll(gamelistDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(gamelistDir, "gamelist.xml"), []byte(
		`<gameList><game><name>Friendly Name</name><path>./game.nes</path></game></gameList>`,
	), 0o600))
	cfg := &config.Instance{}
	require.NoError(t, cfg.LoadTOML("[launchers]\nindex_root = [\""+root+"\"]\n"))
	launchers := []platforms.Launcher{{
		ID: "RetroArchFCEUMM", SystemID: systemdefs.SystemNES, Folders: []string{"nes"}, Extensions: []string{".nes"},
	}}
	options := NewOptions(home, sharedretroarch.Options{})

	AttachPlainESDEScanners(cfg, options, launchers)
	require.NotNil(t, launchers[0].Scanner)
	results, err := launchers[0].Scanner(context.Background(), cfg, systemdefs.SystemNES, []platforms.ScanResult{{
		Path: romPath, Name: "game",
	}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Friendly Name", results[0].Name)
}

func TestLaunchersUsesConfiguredNativeRetroArchForEmuDeck(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	paths := DefaultEmuDeckPaths(home)
	romPath := filepath.Join(paths.RomsPath, "nes", "game.nes")
	require.NoError(t, os.MkdirAll(filepath.Dir(romPath), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(paths.GamelistPath, "nes"), 0o750))
	require.NoError(t, os.WriteFile(romPath, []byte("rom"), 0o600))
	executable := filepath.Join(t.TempDir(), "retroarch")
	require.NoError(t, os.WriteFile(executable, []byte("binary"), 0o600))
	require.NoError(t, os.Chmod(executable, 0o700)) //nolint:gosec // Test executable must be runnable.
	options := NewOptions(home, sharedretroarch.Options{Exec: []string{executable}})
	options.IncludeStandalone = false
	options.IsFlatpakInstalled = func(string) bool { return false }

	launchers := Launchers(nil, options, nil)
	assert.Contains(t, launcherIDs(launchers), "EmuDeckNES")
}

func TestEmuDeckUsesProviderWrapperAndFolderAlias(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	paths := DefaultEmuDeckPaths(home)
	romPath := filepath.Join(paths.RomsPath, "gc", "game.rvz")
	require.NoError(t, os.MkdirAll(filepath.Dir(romPath), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(paths.GamelistPath, "gc"), 0o750))
	require.NoError(t, os.WriteFile(romPath, []byte("rom"), 0o600))
	wrapper := filepath.Join(filepath.Dir(paths.RomsPath), "tools", "launchers", "dolphin-emu.sh")
	require.NoError(t, os.MkdirAll(filepath.Dir(wrapper), 0o750))
	require.NoError(t, os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o600))
	require.NoError(t, os.Chmod(wrapper, 0o700)) //nolint:gosec // Test wrapper must be runnable.

	options := NewOptions(home, sharedretroarch.Options{})
	options.IncludeRetroArch = false
	options.IncludeStandalone = false
	options.IsFlatpakInstalled = func(string) bool { return false }
	launchers := Launchers(nil, options, nil)
	var launcher *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "EmuDeckGameCube" {
			launcher = &launchers[i]
			break
		}
	}
	require.NotNil(t, launcher)
	assert.False(t, launcher.SkipFilesystemScan)
	assert.Equal(t, []string{filepath.Dir(romPath)}, launcher.Folders)
	command, err := launcher.BuildLaunchCommand(nil, romPath, nil)
	require.NoError(t, err)
	assert.Equal(t, wrapper, command.Executable)
	assert.Equal(t, []string{"-b", "-f", "-e", romPath}, command.Args)
}

func TestEmuDeckStandaloneFallsBackToFlatpak(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	paths := DefaultEmuDeckPaths(home)
	romPath := filepath.Join(paths.RomsPath, "ps2", "game.iso")
	require.NoError(t, os.MkdirAll(filepath.Dir(romPath), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(paths.GamelistPath, "ps2"), 0o750))
	require.NoError(t, os.WriteFile(romPath, []byte("rom"), 0o600))

	options := NewOptions(home, sharedretroarch.Options{})
	options.IncludeRetroArch = false
	options.IncludeStandalone = false
	options.IsFlatpakInstalled = func(id string) bool { return id == "net.pcsx2.PCSX2" }
	launchers := Launchers(nil, options, nil)
	var launcher *platforms.Launcher
	for i := range launchers {
		if launchers[i].ID == "EmuDeckPS2" {
			launcher = &launchers[i]
			break
		}
	}
	require.NotNil(t, launcher)
	assert.Equal(t, platforms.LifecycleBlocking, launcher.Lifecycle)
	command, err := launcher.BuildLaunchCommand(nil, romPath, nil)
	require.NoError(t, err)
	assert.Equal(t, "flatpak", command.Executable)
	assert.Equal(t, []string{
		"run", "--filesystem=" + filepath.Dir(romPath) + ":ro", "--die-with-parent", "net.pcsx2.PCSX2",
		"-batch", "-fullscreen", romPath,
	}, command.Args)
}

func TestLaunchersHandlesMissingRetroArchOptions(t *testing.T) {
	t.Parallel()

	options := Options{
		HomeDir:          t.TempDir(),
		IncludeRetroArch: true,
		IsFlatpakInstalled: func(string) bool {
			return true
		},
		LookPath: func(string) (string, error) { return "", errors.New("missing") },
	}

	assert.NotPanics(t, func() { assert.Empty(t, Launchers(nil, options, nil)) })
}

func launcherIDs(launchers []platforms.Launcher) []string {
	ids := make([]string, 0, len(launchers))
	for i := range launchers {
		ids = append(ids, launchers[i].ID)
	}
	return ids
}

func TestLaunchersRegistersNothingWithoutDependencies(t *testing.T) {
	t.Parallel()

	options := NewOptions(t.TempDir(), sharedretroarch.Options{})
	options.IsFlatpakInstalled = func(string) bool { return false }
	options.LookPath = func(string) (string, error) { return "", errors.New("missing") }

	assert.Empty(t, Launchers(nil, options, nil))
}
