//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testConfigsDir = "/opt/retropie/configs"
	testStateDir   = "/home/pi/.local/share/zaparoo/retropie"
	retroPieAuto   = "emulationstation #auto\n"
)

func allPath(name string) string {
	return filepath.Join(testConfigsDir, "all", name)
}

func writeTestFile(t *testing.T, fs afero.Fs, path, content string, mode os.FileMode) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), mode))
}

func readTestFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()
	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	return string(data)
}

func TestInstallFrontendHooksPreservesUserContent(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	autostart := "#!/bin/bash\n# my splash\nomxplayer intro.mp4\n" + retroPieAuto
	onstart := "#!/bin/bash\necho \"$1\" >>/tmp/played" // no trailing newline
	writeTestFile(t, fs, allPath(autostartFile), autostart, 0o644)
	writeTestFile(t, fs, allPath(onStartFile), onstart, 0o755)

	require.NoError(t, installFrontendHooks(fs, testConfigsDir, testStateDir))

	got := readTestFile(t, fs, allPath(autostartFile))
	assert.True(t, strings.HasPrefix(got, "#!/bin/bash\n"+blockBegin+"\n"), "block goes after the shebang")
	assert.True(t, strings.HasSuffix(got, blockEnd+"\n# my splash\nomxplayer intro.mp4\n"+retroPieAuto),
		"user and RetroPie lines are kept in order after the block")
	assert.Contains(t, got, shellQuote(testStateDir))

	gotStart := readTestFile(t, fs, allPath(onStartFile))
	assert.Contains(t, gotStart, "printf '%s\\0' start")
	assert.True(t, strings.HasSuffix(gotStart, "echo \"$1\" >>/tmp/played"))
	info, err := fs.Stat(allPath(onStartFile))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "existing mode is kept")

	gotEnd := readTestFile(t, fs, allPath(onEndFile))
	assert.Equal(t, hookBlock(eventEnd, testStateDir), gotEnd, "missing hook is created with only the block")

	t.Run("idempotent", func(t *testing.T) {
		require.NoError(t, installFrontendHooks(fs, testConfigsDir, testStateDir))
		assert.Equal(t, got, readTestFile(t, fs, allPath(autostartFile)))
		assert.Equal(t, gotStart, readTestFile(t, fs, allPath(onStartFile)))
		assert.Equal(t, gotEnd, readTestFile(t, fs, allPath(onEndFile)))
	})

	t.Run("uninstall restores exact bytes", func(t *testing.T) {
		require.NoError(t, uninstallFrontendHooks(fs, testConfigsDir))
		assert.Equal(t, autostart, readTestFile(t, fs, allPath(autostartFile)))
		assert.Equal(t, onstart, readTestFile(t, fs, allPath(onStartFile)))
		exists, err := afero.Exists(fs, allPath(onEndFile))
		require.NoError(t, err)
		assert.False(t, exists, "hook file created by install is removed")

		require.NoError(t, uninstallFrontendHooks(fs, testConfigsDir), "uninstall twice is harmless")
		assert.Equal(t, autostart, readTestFile(t, fs, allPath(autostartFile)))
	})
}

func TestInstallFrontendHooksRefreshesStaleBlock(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeTestFile(t, fs, allPath(autostartFile), retroPieAuto, 0o644)
	require.NoError(t, installFrontendHooks(fs, testConfigsDir, "/old/state"))
	require.NoError(t, installFrontendHooks(fs, testConfigsDir, testStateDir))

	got := readTestFile(t, fs, allPath(autostartFile))
	assert.Equal(t, 1, strings.Count(got, blockBegin))
	assert.NotContains(t, got, "/old/state")
	assert.Equal(t, autostartBlock(testStateDir)+retroPieAuto, got)
}

func TestInstallFrontendHooksNeedsAutostart(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	err := installFrontendHooks(fs, testConfigsDir, testStateDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Boot to EmulationStation")
	exists, err := afero.Exists(fs, allPath(onStartFile))
	require.NoError(t, err)
	assert.False(t, exists, "nothing is installed without the autostart script")
}

func TestInstallFrontendHooksRejectsRelativeStateDir(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeTestFile(t, fs, allPath(autostartFile), retroPieAuto, 0o644)
	require.Error(t, installFrontendHooks(fs, testConfigsDir, "relative/state"))
	require.Error(t, installFrontendHooks(fs, testConfigsDir, "/state\nrm -rf /"))
}

func TestBrokenBlockIsNotTouched(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"start without end": blockBegin + "\n" + retroPieAuto,
		"end without start": blockEnd + "\n" + retroPieAuto,
		"nested":            blockBegin + "\n" + blockBegin + "\n" + blockEnd + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			writeTestFile(t, fs, allPath(autostartFile), content, 0o644)
			require.Error(t, installFrontendHooks(fs, testConfigsDir, testStateDir))
			require.Error(t, uninstallFrontendHooks(fs, testConfigsDir))
			assert.Equal(t, content, readTestFile(t, fs, allPath(autostartFile)))
		})
	}
}

func TestInsertBlockWithoutShebang(t *testing.T) {
	t.Parallel()

	out, err := insertBlock([]byte(retroPieAuto), "B\n")
	require.NoError(t, err)
	assert.Equal(t, "B\n"+retroPieAuto, string(out))

	out, err = insertBlock(nil, "B\n")
	require.NoError(t, err)
	assert.Equal(t, "B\n", string(out))

	// A shebang without a newline is the whole file, so the block goes first.
	out, err = insertBlock([]byte("#!/bin/bash"), "B\n")
	require.NoError(t, err)
	assert.Equal(t, "B\n#!/bin/bash", string(out))
}

// RetroPie's autostart reconfiguration deletes every line containing its
// auto marker, which would cut the block in half.
func TestBlocksSurviveRetroPieAutostartRewrite(t *testing.T) {
	t.Parallel()

	for _, block := range []string{
		autostartBlock(testStateDir),
		hookBlock(eventStart, testStateDir),
		hookBlock(eventEnd, testStateDir),
	} {
		assert.NotContains(t, block, "#auto")
	}
}

func TestHookBlockReportsRuncommand(t *testing.T) {
	t.Parallel()
	bash := requireBash(t)

	dir := t.TempDir()
	script := filepath.Join(dir, "hook.sh")
	writeHostFile(t, script, "#!/bin/bash\n"+hookBlock(eventStart, dir)+"exit 3\n", 0o755)

	//nolint:gosec // G204: runs the test's own script with bash
	cmd := exec.CommandContext(context.Background(), bash, script, "nes", "lr-fceumm", "/roms/nes/a b.nes")
	err := cmd.Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "the user's own exit status is kept")
	assert.Equal(t, 3, exitErr.ExitCode())

	data, err := os.ReadFile(filepath.Join(dir, stateFile)) //nolint:gosec // test temp dir
	require.NoError(t, err)
	ev, err := parseState(data)
	require.NoError(t, err)
	assert.Equal(t, runEvent{
		event: eventStart, pid: os.Getpid(), system: "nes", emulator: "lr-fceumm", rom: "/roms/nes/a b.nes",
	}, ev)

	leftovers, err := filepath.Glob(filepath.Join(dir, stateFile+".*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers)
}

func requireBash(t *testing.T) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	return bash
}

func writeHostFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), mode))
	require.NoError(t, os.Chmod(path, mode))
}

// The autostart loop is run by real bash against stand-ins for
// EmulationStation and runcommand. The first EmulationStation run leaves a
// request, as Core does before closing it.
func TestAutostartLoopRunsRequests(t *testing.T) {
	t.Parallel()
	bash := requireBash(t)

	tests := []struct {
		name    string
		kind    requestKind
		system  string
		romName string
		want    []string
	}{
		{
			name: "system", kind: requestSystem, system: "nes", romName: "Super Game (USA).nes",
			want: []string{"es", "runcommand 0 _SYS_ nes ROM", "es"},
		},
		{
			name: "port", kind: requestPort, system: "ports", romName: "Doom's port.sh",
			want: []string{"es", "port", "es"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			stateDir := filepath.Join(dir, "state")
			binDir := filepath.Join(dir, "bin")
			require.NoError(t, os.MkdirAll(binDir, 0o750))
			logPath := filepath.Join(dir, "log")
			rom := filepath.Join(dir, tt.romName)
			writeHostFile(t, rom, "echo port >>\"$ZT_LOG\"\n", 0o644)

			req, err := encodeRequest(tt.kind, tt.system, rom)
			require.NoError(t, err)
			reqPath := filepath.Join(dir, "req")
			writeHostFile(t, reqPath, string(req), 0o600)

			writeHostFile(t, filepath.Join(binDir, "emulationstation"), `#!/bin/bash
echo es >>"$ZT_LOG"
if [[ ! -e "$ZT_DIR/es-ran" ]]; then
    touch "$ZT_DIR/es-ran"
    cp "$ZT_DIR/req" "$ZT_STATE/request"
fi
`, 0o755)
			runcommand := filepath.Join(binDir, "runcommand.sh")
			writeHostFile(t, runcommand, `#!/bin/bash
[[ "$4" == "$ZT_ROM" ]] && rom=ROM || rom="$4"
echo "runcommand $1 $2 $3 $rom" >>"$ZT_LOG"
`, 0o755)

			autostart := filepath.Join(dir, autostartFile)
			writeHostFile(t, autostart,
				"#!/bin/bash\n"+autostartBlockFor(stateDir, runcommand)+retroPieAuto+"echo after-es >>\"$ZT_LOG\"\n",
				0o644)

			cmd := exec.CommandContext(context.Background(), bash, autostart) //nolint:gosec // test script
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+":"+os.Getenv("PATH"),
				"ZT_LOG="+logPath, "ZT_DIR="+dir, "ZT_STATE="+stateDir, "ZT_ROM="+rom,
			)
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))

			logData, err := os.ReadFile(logPath) //nolint:gosec // test temp dir
			require.NoError(t, err)
			want := append(append([]string(nil), tt.want...), "after-es")
			assert.Equal(t, strings.Join(want, "\n")+"\n", string(logData))

			for _, name := range []string{requestFile, holdFile, loopPIDFile} {
				_, err := os.Stat(filepath.Join(stateDir, name))
				assert.ErrorIs(t, err, os.ErrNotExist, name)
			}
		})
	}
}

// A request naming a system that is not a RetroPie system name is dropped
// without running anything.
func TestAutostartLoopDropsBadRequest(t *testing.T) {
	t.Parallel()
	bash := requireBash(t)

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o750))
	logPath := filepath.Join(dir, "log")
	rom := filepath.Join(dir, "game.nes")
	writeHostFile(t, rom, "", 0o644)
	writeHostFile(t, filepath.Join(dir, "req"), "sys\x00../nes\x00"+rom+"\x00", 0o600)

	writeHostFile(t, filepath.Join(binDir, "emulationstation"), `#!/bin/bash
echo es >>"$ZT_LOG"
if [[ ! -e "$ZT_DIR/es-ran" ]]; then
    touch "$ZT_DIR/es-ran"
    cp "$ZT_DIR/req" "$ZT_STATE/request"
fi
`, 0o755)
	runcommand := filepath.Join(binDir, "runcommand.sh")
	writeHostFile(t, runcommand, "#!/bin/bash\necho runcommand >>\"$ZT_LOG\"\n", 0o755)
	autostart := filepath.Join(dir, autostartFile)
	writeHostFile(t, autostart, "#!/bin/bash\n"+autostartBlockFor(stateDir, runcommand)+retroPieAuto, 0o644)

	cmd := exec.CommandContext(context.Background(), bash, autostart) //nolint:gosec // test script
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"), "ZT_LOG="+logPath, "ZT_DIR="+dir, "ZT_STATE="+stateDir,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	logData, err := os.ReadFile(logPath) //nolint:gosec // test temp dir
	require.NoError(t, err)
	assert.Equal(t, "es\nes\n", string(logData))
}
