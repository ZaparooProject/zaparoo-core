//go:build linux && !android

package mister

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateScriptsLauncher(t *testing.T) {
	t.Parallel()

	launcher := createScriptsLauncher(NewPlatform())

	assert.Equal(t, "Scripts", launcher.ID)
	assert.Equal(t, systemdefs.SystemScript, launcher.SystemID)
	assert.Equal(t, []string{misterconfig.ScriptsDir}, launcher.Folders)
	assert.Equal(t, []string{".sh"}, launcher.Extensions)
	assert.True(t, launcher.NoActiveMedia)
	assert.Equal(t, platforms.LifecycleFireAndForget, launcher.Lifecycle)
	assert.False(t, launcher.SkipFilesystemScan)
	require.NotNil(t, launcher.Scanner)
	require.NotNil(t, launcher.Launch)

	indexed := enableMGLIndexing([]platforms.Launcher{launcher})
	assert.Equal(t, []string{".sh"}, indexed[0].Extensions)
}

func TestScriptsLauncherOwnsOnlyTheScriptsFolder(t *testing.T) {
	// Not parallel: mutates the shared GlobalLauncherCache.
	root := filepath.Join(misterconfig.SDRootDir, "games")
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	pl.On("Settings").Return(platforms.Settings{})
	pl.On("RootDirs", cfg).Return([]string{misterconfig.SDRootDir, root})

	launchers := append(CreateLaunchers(pl), createScriptsLauncher(NewPlatform()))
	pl.On("Launchers", cfg).Return(launchers)
	oldCache := helpers.GlobalLauncherCache
	helpers.GlobalLauncherCache = &helpers.LauncherCache{}
	t.Cleanup(func() { helpers.GlobalLauncherCache = oldCache })
	helpers.GlobalLauncherCache.InitializeFromSlice(launchers)

	matcher := helpers.NewLauncherMatcher(cfg, pl)
	script := filepath.Join(misterconfig.ScriptsDir, "update_all.sh")
	nested := filepath.Join(misterconfig.ScriptsDir, "_Extras", "wifi.sh")

	assert.True(t, matcher.MatchSystemFileForScan(systemdefs.SystemScript, script))
	assert.True(t, matcher.MatchSystemFileForScan(systemdefs.SystemScript, nested))
	assert.False(t, matcher.MatchSystemFileForScan(
		systemdefs.SystemScript, filepath.Join(misterconfig.ScriptsDir, "notes.txt")))
	assert.False(t, matcher.MatchSystemFileForScan(
		systemdefs.SystemScript, filepath.Join(misterconfig.SDRootDir, "linux", "user-startup.sh")))
	assert.False(t, matcher.MatchSystemFileForScan(
		systemdefs.SystemScript, filepath.Join(root, "Scripts", "elsewhere.sh")))

	found := helpers.PathToLaunchers(cfg, pl, script)
	require.Len(t, found, 1)
	assert.Equal(t, "Scripts", found[0].ID)
}

func TestScriptRelPath(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(string(filepath.Separator), "media", "fat", "Scripts")
	tests := []struct {
		name    string
		path    string
		wantRel string
		wantOK  bool
	}{
		{name: "top level", path: filepath.Join(dir, "update_all.sh"), wantRel: "update_all.sh", wantOK: true},
		{
			name: "subfolder", path: filepath.Join(dir, "_Extras", "wifi.sh"),
			wantRel: filepath.Join("_Extras", "wifi.sh"), wantOK: true,
		},
		{name: "upper case extension", path: filepath.Join(dir, "TOOL.SH"), wantRel: "TOOL.SH", wantOK: true},
		{name: "not a script", path: filepath.Join(dir, "notes.txt")},
		{name: "hidden file", path: filepath.Join(dir, ".secret.sh")},
		{name: "hidden folder", path: filepath.Join(dir, ".config", "downloader", "helper.sh")},
		{name: "inside an archive", path: filepath.Join(dir, "Tools.zip", "tool.sh")},
		{name: "the folder itself", path: dir + ".sh"},
		{name: "outside", path: filepath.Join(filepath.Dir(dir), "linux", "boot.sh")},
		{name: "traversal", path: dir + "/../linux/boot.sh"},
		{name: "sibling prefix", path: dir + "2/boot.sh"},
		{name: "relative", path: filepath.Join("Scripts", "update_all.sh")},
		{name: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rel, ok := scriptRelPath(dir, tt.path)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantRel, rel)
		})
	}
}

func TestScanScriptsKeepsOnlyListedScripts(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(string(filepath.Separator), "media", "fat", "Scripts")
	top := filepath.Join(dir, "Zaparoo_Launcher_1080p.sh")
	nested := filepath.Join(dir, "_Extras", "wifi.sh")

	results, err := scanScripts(dir)(t.Context(), nil, systemdefs.SystemScript, []platforms.ScanResult{
		{Path: top},
		{Path: filepath.Join(dir, ".config", "downloader", "helper.sh")},
		{Path: nested},
		{Path: filepath.Join(dir, ".hidden.sh")},
		{Path: filepath.Join(dir, "Tools.zip", "tool.sh")},
	})

	require.NoError(t, err)
	assert.Equal(t, []platforms.ScanResult{
		{Path: top, Name: "Zaparoo_Launcher_1080p"},
		{Path: nested, Name: "wifi"},
	}, results)
}

// failOnScriptSideEffects makes any attempt to touch the device fail the test.
func failOnScriptSideEffects(t *testing.T) {
	t.Helper()
	restoreScriptTestHooks(t)

	checkScriptActive = func(context.Context) bool {
		t.Fatal("refusal must not look for a running script")
		return false
	}
	scriptFPGAActive = func(*Platform) bool {
		t.Fatal("refusal must not read the active core")
		return false
	}
	scriptReturnToMenu = func(*Platform) error {
		t.Fatal("refusal must not return to the menu")
		return nil
	}
	getScriptConsoleManager = func(*Platform) platforms.ConsoleManager {
		t.Fatal("refusal must not open a console")
		return nil
	}
	startScriptCommand = func(*exec.Cmd) error {
		t.Fatal("refusal must not start a script")
		return nil
	}
}

func TestLaunchScript_RefusesPathsItDoesNotList(t *testing.T) {
	dir := t.TempDir()
	outside := newTestScript(t, "outside.sh")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".config"), 0o750))
	hidden := filepath.Join(dir, ".config", "helper.sh")
	require.NoError(t, os.WriteFile(hidden, []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // Test executable.
	notScript := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(notScript, []byte("x"), 0o600))

	for name, path := range map[string]string{
		"outside the folder": outside,
		"traversal":          dir + "/../" + filepath.Base(filepath.Dir(outside)) + "/outside.sh",
		"hidden folder":      hidden,
		"inside an archive":  filepath.Join(dir, "Tools.zip", "tool.sh"),
		"not a script":       notScript,
		"empty":              "",
	} {
		t.Run(name, func(t *testing.T) {
			failOnScriptSideEffects(t)

			proc, err := launchScript(newTestScriptPlatform(), dir)(nil, path, nil)

			require.ErrorContains(t, err, "invalid script")
			assert.Nil(t, proc)
		})
	}
}

func TestLaunchScript_MissingScriptIsNotFound(t *testing.T) {
	failOnScriptSideEffects(t)
	dir := t.TempDir()

	_, err := launchScript(newTestScriptPlatform(), dir)(nil, filepath.Join(dir, "gone.sh"), nil)

	require.ErrorIs(t, err, zapscript.ErrFileNotFound)
}

// captureLaunchedScript runs the launcher up to the point the script would
// start and returns the launcher file it wrote.
func captureLaunchedScript(t *testing.T, dir, path string) string {
	t.Helper()

	var written string
	checkScriptActive = func(context.Context) bool { return false }
	runScriptChvt = func(context.Context, string) error { return nil }
	writeScriptLauncher = func(_ string, data []byte, _ os.FileMode) error {
		written = string(data)
		return nil
	}
	startScriptCommand = func(*exec.Cmd) error { return assert.AnError }

	_, err := launchScript(newTestScriptPlatform(), dir)(nil, path, nil)
	require.ErrorIs(t, err, assert.AnError)
	require.NotEmpty(t, written)
	return written
}

func TestLaunchScript_RunsTheScriptVisiblyWithNoArguments(t *testing.T) {
	restoreScriptTestHooks(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "My Tools")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	path := filepath.Join(sub, "it's wifi.sh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // Test executable.

	cm := &testConsoleManager{}
	getScriptConsoleManager = func(*Platform) platforms.ConsoleManager { return cm }
	scriptFPGAActive = func(*Platform) bool { return false }
	scriptReturnToMenu = func(*Platform) error {
		t.Fatal("must not return to the menu when no core is running")
		return nil
	}

	written := captureLaunchedScript(t, dir, path)

	assert.Equal(t, scriptConsoleVT, cm.openVT)
	assert.Contains(t, written, "\nexport ZAPAROO_RUN_SCRIPT=1\n")
	assert.NotContains(t, written, launchOriginEnv)

	// The launcher file has to run the script whatever its name contains.
	marker := filepath.Join(t.TempDir(), "ran")
	script := "#!/bin/sh\nprintf '%s|%s' \"$PWD\" \"$#\" > '" + marker + "'\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700))  //nolint:gosec // Test executable.
	cmd := exec.CommandContext(t.Context(), "bash", "-c", written) //nolint:gosec // runs the launcher under test
	require.NoError(t, cmd.Run())
	out, err := os.ReadFile(marker) //nolint:gosec // Test-owned path.
	require.NoError(t, err)
	assert.Equal(t, sub+"|0", string(out))
}

func TestLaunchScript_LeavesARunningCoreBeforeOpeningTheConsole(t *testing.T) {
	restoreScriptTestHooks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "update_all.sh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // Test executable.

	var order []string
	core := "SNES"
	scriptFPGAActive = func(*Platform) bool { return core != misterconfig.MenuCore }
	scriptReturnToMenu = func(*Platform) error {
		order = append(order, "menu")
		core = misterconfig.MenuCore
		return nil
	}
	scriptActiveCoreName = func() string { return core }
	getScriptConsoleManager = func(*Platform) platforms.ConsoleManager {
		order = append(order, "console")
		return &testConsoleManager{}
	}

	captureLaunchedScript(t, dir, path)

	assert.Equal(t, []string{"menu", "console"}, order)
}

func TestLaunchScript_StopsWhenTheMenuCannotBeReached(t *testing.T) {
	failOnScriptSideEffects(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "update_all.sh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // Test executable.

	scriptFPGAActive = func(*Platform) bool { return true }
	scriptReturnToMenu = func(*Platform) error { return assert.AnError }

	_, err := launchScript(newTestScriptPlatform(), dir)(nil, path, nil)

	require.ErrorIs(t, err, assert.AnError)
}

func TestScriptIsIndexedUnderItsFileName(t *testing.T) {
	t.Parallel()

	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	path := filepath.Join(misterconfig.ScriptsDir, "Zaparoo_Launcher_1080p.sh")
	scanned, err := scanScripts(misterconfig.ScriptsDir)(
		t.Context(), nil, systemdefs.SystemScript, []platforms.ScanResult{{Path: path}})
	require.NoError(t, err)

	scantest.IndexScanResults(t, mediaDB, systemdefs.SystemScript, database.ScanReconcileOpts{}, scanned...)

	results, err := mediaDB.SearchMediaPathExact(
		t.Context(), []systemdefs.System{{ID: systemdefs.SystemScript}}, path)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, systemdefs.SystemScript, results[0].SystemID)
	assert.Equal(t, "Zaparoo_Launcher_1080p", results[0].Name)
}
