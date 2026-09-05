//go:build linux

package tracker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// fakeFileInfo/fakeDirEntry/fakeFS give composeSelectedPath a small
// in-memory filesystem, so tests can exercise MiSTer's SD/USB root selection
// and directory browsing without depending on real paths like /media/fat
// existing on the test machine.
type fakeFileInfo struct {
	name  string
	isDir bool
}

func (f fakeFileInfo) Name() string { return f.name }
func (fakeFileInfo) Size() int64    { return 0 }
func (f fakeFileInfo) Mode() os.FileMode {
	if f.isDir {
		return os.ModeDir
	}
	return 0
}
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool      { return f.isDir }
func (fakeFileInfo) Sys() any           { return nil }

type fakeDirEntry struct {
	name  string
	isDir bool
}

func (f fakeDirEntry) Name() string { return f.name }
func (f fakeDirEntry) IsDir() bool  { return f.isDir }
func (f fakeDirEntry) Type() os.FileMode {
	if f.isDir {
		return os.ModeDir
	}
	return 0
}

func (f fakeDirEntry) Info() (os.FileInfo, error) {
	return fakeFileInfo(f), nil
}

func fakeFile(name string) fakeDirEntry { return fakeDirEntry{name: name} }
func fakeDir(name string) fakeDirEntry  { return fakeDirEntry{name: name, isDir: true} }

type fakeFS struct {
	dirs  map[string][]fakeDirEntry
	files map[string]bool
}

func newFakeFS() *fakeFS {
	return &fakeFS{dirs: map[string][]fakeDirEntry{}, files: map[string]bool{}}
}

// addDir registers path as a directory containing entries. Any child
// directory entries are registered too (as empty, unless later overridden by
// their own addDir call).
func (f *fakeFS) addDir(path string, entries ...fakeDirEntry) {
	f.dirs[path] = entries
	for _, e := range entries {
		child := filepath.Join(path, e.name)
		if e.isDir {
			if _, ok := f.dirs[child]; !ok {
				f.dirs[child] = nil
			}
		} else {
			f.files[child] = true
		}
	}
}

func (f *fakeFS) stat(path string) (os.FileInfo, error) {
	if _, ok := f.dirs[path]; ok {
		return fakeFileInfo{name: filepath.Base(path), isDir: true}, nil
	}
	if f.files[path] {
		return fakeFileInfo{name: filepath.Base(path), isDir: false}, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeFS) readDir(path string) ([]os.DirEntry, error) {
	entries, ok := f.dirs[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	out := make([]os.DirEntry, len(entries))
	for i, e := range entries {
		out[i] = e
	}
	return out, nil
}

func writeRecentEntry(t *testing.T, filename, directory, name string) {
	t.Helper()
	entry := make([]byte, 1024+256+256)
	copy(entry[:1024], directory)
	copy(entry[1024:1280], name)
	copy(entry[1280:], name)
	require.NoError(t, os.WriteFile(filename, entry, 0o600))
}

func TestResolveStorageRelativePath(t *testing.T) {
	t.Parallel()

	relativePath := filepath.Join("games", "GBC", "Game.gbc")
	assert.Equal(t,
		filepath.Join(misterconfig.SDRootDir, relativePath),
		resolveStorageRelativePath(relativePath, nil, func(string) bool { return false }),
	)

	usb1Path := filepath.Join(filepath.Dir(misterconfig.SDRootDir), "usb1", relativePath)
	assert.Equal(t, usb1Path, resolveStorageRelativePath(
		relativePath,
		[]byte{1, 0, 0, 0},
		func(path string) bool { return path == usb1Path },
	))

	absolutePath := filepath.Join(string(filepath.Separator), "media", "network", "Game.gbc")
	assert.Equal(t,
		absolutePath,
		resolveStorageRelativePath(absolutePath, []byte{1, 0, 0, 0}, func(string) bool { return false }),
	)
}

// MiSTer leaves FILESELECT at "selected" after a launch and rewrites it when
// the core exits, while FULLPATH and CURRENTPATH still name the game that just
// ended. That re-notification must not read as a new launch: it resurrected
// the closed game, which then accrued playtime forever because no further core
// change was coming.
// resolveSelectedLaunchPath is where the staleness rule actually runs, so the
// regression is pinned here rather than only on the predicate: without the
// gate, exiting a core republished the game that had just closed.
func TestResolveSelectedLaunchPath(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T, gameName string, statusAge, pathAge time.Duration) (selectionFiles, string) {
		t.Helper()
		dir := t.TempDir()
		gamesDir := filepath.Join(dir, "games")
		require.NoError(t, os.MkdirAll(gamesDir, 0o750))
		gamePath := filepath.Join(gamesDir, gameName)
		require.NoError(t, os.WriteFile(gamePath, []byte("rom"), 0o600))

		files := selectionFiles{
			status:      filepath.Join(dir, "FILESELECT"),
			fullPath:    filepath.Join(dir, "FULLPATH"),
			currentPath: filepath.Join(dir, "CURRENTPATH"),
			deviceBin:   filepath.Join(dir, "device.bin"),
		}
		require.NoError(t, os.WriteFile(files.status, []byte("selected"), 0o600))
		require.NoError(t, os.WriteFile(files.fullPath, []byte(gamePath), 0o600))
		require.NoError(t, os.WriteFile(files.currentPath, []byte(gameName), 0o600))

		now := time.Now()
		require.NoError(t, os.Chtimes(files.status, now.Add(-statusAge), now.Add(-statusAge)))
		for _, f := range []string{files.fullPath, files.currentPath} {
			require.NoError(t, os.Chtimes(f, now.Add(-pathAge), now.Add(-pathAge)))
		}
		return files, gamePath
	}

	t.Run("a settled selection resolves", func(t *testing.T) {
		t.Parallel()
		files, gamePath := build(t, "Blockade.mra", 10*time.Second, 10*time.Second)
		path, ok := resolveSelectedLaunchPath(files, selectionStaleWindow)
		require.True(t, ok, "a selection written with its paths is a real launch")
		assert.Equal(t, gamePath, path)
	})

	t.Run("a status re-notified after a core exit is ignored", func(t *testing.T) {
		t.Parallel()
		// MiSTer rewrites FILESELECT on exit and leaves it at "selected", while
		// FULLPATH and CURRENTPATH still name the game that just ended.
		files, _ := build(t, "Blockade.mra", 0, 31*time.Second)
		_, ok := resolveSelectedLaunchPath(files, selectionStaleWindow)
		assert.False(t, ok, "the exit re-notification must not read as a launch")
	})

	t.Run("a window that cannot be exceeded still resolves", func(t *testing.T) {
		t.Parallel()
		files, gamePath := build(t, "Blockade.mra", 0, 31*time.Second)
		require.NoError(t, os.Remove(files.currentPath))
		require.NoError(t, os.WriteFile(files.currentPath, []byte("Blockade.mra"), 0o600))
		path, ok := resolveSelectedLaunchPath(files, time.Hour)
		require.True(t, ok, "the gate only drops a status older than the window")
		assert.Equal(t, gamePath, path)
	})

	t.Run("an unreadable trio is ignored", func(t *testing.T) {
		t.Parallel()
		files, _ := build(t, "Blockade.mra", 10*time.Second, 10*time.Second)
		require.NoError(t, os.Remove(files.fullPath))
		_, ok := resolveSelectedLaunchPath(files, selectionStaleWindow)
		assert.False(t, ok, "a half-written trio must not read as a launch")
	})
}

func TestSelectionIsStale(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, statusAge, pathAge time.Duration) (string, string) {
		t.Helper()
		dir := t.TempDir()
		statusFile := filepath.Join(dir, "FILESELECT")
		currentPathFile := filepath.Join(dir, "CURRENTPATH")
		require.NoError(t, os.WriteFile(statusFile, []byte("selected"), 0o600))
		require.NoError(t, os.WriteFile(currentPathFile, []byte("Blockade.mra"), 0o600))
		now := time.Now()
		require.NoError(t, os.Chtimes(statusFile, now.Add(-statusAge), now.Add(-statusAge)))
		require.NoError(t, os.Chtimes(currentPathFile, now.Add(-pathAge), now.Add(-pathAge)))
		return statusFile, currentPathFile
	}

	t.Run("written together is current", func(t *testing.T) {
		t.Parallel()
		statusFile, currentPathFile := write(t, 10*time.Second, 10*time.Second)
		stale, err := selectionIsStale(statusFile, currentPathFile, selectionStaleWindow)
		require.NoError(t, err)
		assert.False(t, stale)
	})

	t.Run("status re-notified long after its paths is stale", func(t *testing.T) {
		t.Parallel()
		// The real capture: paths at 13:39:56, status rewritten at 13:40:27.
		statusFile, currentPathFile := write(t, 0, 31*time.Second)
		stale, err := selectionIsStale(statusFile, currentPathFile, selectionStaleWindow)
		require.NoError(t, err)
		assert.True(t, stale)
	})

	t.Run("paths newer than status is not stale", func(t *testing.T) {
		t.Parallel()
		statusFile, currentPathFile := write(t, 31*time.Second, 0)
		stale, err := selectionIsStale(statusFile, currentPathFile, selectionStaleWindow)
		require.NoError(t, err)
		assert.False(t, stale)
	})

	t.Run("missing file reports an error so the caller can fail open", func(t *testing.T) {
		t.Parallel()
		statusFile, currentPathFile := write(t, 0, 0)
		_, err := selectionIsStale(statusFile, filepath.Join(t.TempDir(), "absent"), selectionStaleWindow)
		require.Error(t, err, "an unreadable current path must be reported, not treated as current")
		_, err = selectionIsStale(filepath.Join(t.TempDir(), "absent"), currentPathFile, selectionStaleWindow)
		require.Error(t, err, "an unreadable status must be reported, not treated as current")
	})
}

func TestReadFileSelectionFrom(t *testing.T) {
	t.Parallel()

	writeFiles := func(t *testing.T, status, fullPath, currentPath string) (string, string, string) {
		t.Helper()
		dir := t.TempDir()
		statusFile := filepath.Join(dir, "FILESELECT")
		fullPathFile := filepath.Join(dir, "FULLPATH")
		currentPathFile := filepath.Join(dir, "CURRENTPATH")
		require.NoError(t, os.WriteFile(statusFile, []byte(status), 0o600))
		require.NoError(t, os.WriteFile(fullPathFile, []byte(fullPath), 0o600))
		require.NoError(t, os.WriteFile(currentPathFile, []byte(currentPath), 0o600))
		return statusFile, fullPathFile, currentPathFile
	}

	t.Run("selected", func(t *testing.T) {
		t.Parallel()
		statusFile, fullPathFile, currentPathFile := writeFiles(
			t, "selected", "/media/fat/_Arcade/Pooyan.mra\x00\x00", "Pooyan\x00",
		)
		sel, err := readFileSelectionFrom(statusFile, fullPathFile, currentPathFile)
		require.NoError(t, err)
		assert.Equal(t, fileSelection{
			Status:      "selected",
			FullPath:    "/media/fat/_Arcade/Pooyan.mra",
			CurrentPath: "Pooyan",
		}, sel)
	})

	for _, status := range []string{"active", "cancelled", ""} {
		t.Run("does not read FULLPATH/CURRENTPATH when "+status, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			statusFile := filepath.Join(dir, "FILESELECT")
			require.NoError(t, os.WriteFile(statusFile, []byte(status), 0o600))
			// FULLPATH/CURRENTPATH don't exist; reading them would error.
			sel, err := readFileSelectionFrom(
				statusFile,
				filepath.Join(dir, "FULLPATH"),
				filepath.Join(dir, "CURRENTPATH"),
			)
			require.NoError(t, err)
			assert.Equal(t, status, sel.Status)
			assert.Empty(t, sel.FullPath)
			assert.Empty(t, sel.CurrentPath)
		})
	}

	t.Run("missing status file errors", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := readFileSelectionFrom(
			filepath.Join(dir, "FILESELECT"),
			filepath.Join(dir, "FULLPATH"),
			filepath.Join(dir, "CURRENTPATH"),
		)
		require.Error(t, err)
	})

	t.Run("missing FULLPATH errors once selected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		statusFile := filepath.Join(dir, "FILESELECT")
		require.NoError(t, os.WriteFile(statusFile, []byte("selected"), 0o600))
		_, err := readFileSelectionFrom(
			statusFile, filepath.Join(dir, "FULLPATH"), filepath.Join(dir, "CURRENTPATH"),
		)
		require.Error(t, err)
	})

	t.Run("missing CURRENTPATH errors once selected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		statusFile, fullPathFile, currentPathFile := writeFiles(t, "selected", "/media/fat/game.rom", "")
		require.NoError(t, os.Remove(currentPathFile))
		_, err := readFileSelectionFrom(statusFile, fullPathFile, filepath.Join(dir, "CURRENTPATH"))
		require.Error(t, err)
	})
}

func TestComposeSelectedPath(t *testing.T) {
	t.Parallel()

	sdSNES := filepath.Join(misterconfig.SDRootDir, "games", "SNES")
	sdArcade := filepath.Join(misterconfig.SDRootDir, "_Arcade")
	sdPSX := filepath.Join(misterconfig.SDRootDir, "games", "PSX")
	sdNeoGeo := filepath.Join(misterconfig.SDRootDir, "games", "NEOGEO")
	sdRoot := misterconfig.SDRootDir
	usbGBC := filepath.Join(filepath.Dir(misterconfig.SDRootDir), "usb1", "games", "GBC")

	fs := newFakeFS()
	fs.addDir(sdSNES, fakeFile("Game1.sfc"), fakeFile("Game2.sfc"))
	fs.addDir(sdArcade,
		fakeFile("Pooyan.mra"), fakeFile("Other.mgl"),
		fakeFile("Game.a"), fakeFile("Game.b"),
		fakeFile("SNES_20220101.rbf"),
	)
	fs.addDir(sdRoot, fakeDir("games"), fakeDir("_Arcade"))
	fs.addDir(sdPSX, fakeDir("Game (Disc 1)"))
	fs.addDir(filepath.Join(sdPSX, "Game (Disc 1)"), fakeFile("Game.cue"))
	fs.addDir(sdNeoGeo, fakeDir("mslug"))
	fs.addDir(filepath.Join(sdNeoGeo, "mslug"), fakeFile("046-p1.p1"), fakeFile("046-s1.s1"))
	fs.addDir(usbGBC, fakeFile("Game.gbc"))

	tests := []struct {
		sel     fileSelection
		name    string
		want    string
		storage []byte
		wantOK  bool
	}{
		{
			name: "not selected",
			sel:  fileSelection{Status: "active", FullPath: sdSNES, CurrentPath: "Game1"},
		},
		{
			name: "current path is parent reference",
			sel:  fileSelection{Status: "selected", FullPath: sdSNES, CurrentPath: ".."},
		},
		{
			name: "current path is empty",
			sel:  fileSelection{Status: "selected", FullPath: sdSNES, CurrentPath: ""},
		},
		{
			name:   "first of two games in the same folder",
			sel:    fileSelection{Status: "selected", FullPath: sdSNES, CurrentPath: "Game1"},
			wantOK: true,
			want:   filepath.Join(sdSNES, "Game1.sfc"),
		},
		{
			name:   "second of two games in the same folder",
			sel:    fileSelection{Status: "selected", FullPath: sdSNES, CurrentPath: "Game2"},
			wantOK: true,
			want:   filepath.Join(sdSNES, "Game2.sfc"),
		},
		{
			name:   "extension-stripped MRA selection",
			sel:    fileSelection{Status: "selected", FullPath: sdArcade, CurrentPath: "Pooyan"},
			wantOK: true,
			want:   filepath.Join(sdArcade, "Pooyan.mra"),
		},
		{
			name: "core datecode name is unrecoverable",
			sel:  fileSelection{Status: "selected", FullPath: sdArcade, CurrentPath: "SNES"},
		},
		{
			name: "ambiguous extension-stripped match is refused",
			sel:  fileSelection{Status: "selected", FullPath: sdArcade, CurrentPath: "Game"},
		},
		{
			name: "MGL-driven absolute path matches basename verbatim",
			sel: fileSelection{
				Status: "selected", FullPath: filepath.Join(sdArcade, "Pooyan.mra"), CurrentPath: "Pooyan.mra",
			},
			wantOK: true,
			want:   filepath.Join(sdArcade, "Pooyan.mra"),
		},
		{
			name: "stale FULLPATH basename mismatch is refused",
			sel: fileSelection{
				Status: "selected", FullPath: filepath.Join(sdArcade, "Pooyan.mra"), CurrentPath: "Other",
			},
		},
		{
			name:   "disc folder with one file resolves to the file",
			sel:    fileSelection{Status: "selected", FullPath: sdPSX, CurrentPath: "Game (Disc 1)"},
			wantOK: true,
			want:   filepath.Join(sdPSX, "Game (Disc 1)", "Game.cue"),
		},
		{
			name:   "multi-file folder set resolves to the folder",
			sel:    fileSelection{Status: "selected", FullPath: sdNeoGeo, CurrentPath: "mslug"},
			wantOK: true,
			want:   filepath.Join(sdNeoGeo, "mslug"),
		},
		{
			name:    "USB root resolution for relative FULLPATH",
			sel:     fileSelection{Status: "selected", FullPath: filepath.Join("games", "GBC"), CurrentPath: "Game"},
			storage: []byte{1, 0, 0, 0},
			wantOK:  true,
			want:    filepath.Join(usbGBC, "Game.gbc"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := composeSelectedPath(tt.sel, tt.storage, fs.stat, fs.readDir)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestIsSystemOrMenuPath(t *testing.T) {
	t.Parallel()

	assert.True(t, isSystemOrMenuPath(filepath.Join(misterconfig.ScriptsDir, "update.sh")))
	assert.True(t, isSystemOrMenuPath(filepath.Join(misterconfig.CoreConfigFolder, "SNES.ini")))
	assert.True(t, isSystemOrMenuPath(filepath.Join(misterconfig.LinuxDir, "menu.rbf")))
	assert.True(t, isSystemOrMenuPath(filepath.Join(misterconfig.SDRootDir, "_Arcade", "cores", "core.rbf")))
	assert.True(t, isSystemOrMenuPath(filepath.Join(misterconfig.SDRootDir, "filters", "snes.ini")))
	assert.True(t, isSystemOrMenuPath(filepath.Join(misterconfig.SDRootDir, "presets", "preset.cfg")))
	assert.True(t, isSystemOrMenuPath(filepath.Join(misterconfig.SDRootDir, "readme.txt")))

	assert.False(t, isSystemOrMenuPath(filepath.Join(misterconfig.SDRootDir, "games", "SNES", "Game.sfc")))
	assert.False(t, isSystemOrMenuPath(filepath.Join(misterconfig.SDRootDir, "_Arcade", "Pooyan.mra")))
}

func TestHasSystemLauncher(t *testing.T) {
	t.Parallel()

	assert.False(t, hasSystemLauncher(nil))
	assert.False(t, hasSystemLauncher([]platforms.Launcher{{ID: "Generic"}}))
	assert.True(t, hasSystemLauncher([]platforms.Launcher{{ID: "Generic"}, {ID: "SNES", SystemID: "SNES"}}))
}

func TestRecentGamePath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	gameDir := filepath.Join("games", "GBC")
	recentFile := filepath.Join(tmpDir, "GBC_recent_0.cfg")
	writeRecentEntry(t, recentFile, gameDir, "Game.gbc")

	path, err := recentGamePath(recentFile, []byte{0, 0, 0, 0})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(misterconfig.SDRootDir, gameDir, "Game.gbc"), path)
}

func TestRecentGamePath_CoreRecents(t *testing.T) {
	t.Parallel()

	t.Run("MGL resolves launched game", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		gamePath := filepath.Join(tmpDir, "games", "Game.rom")
		mglPath := filepath.Join(tmpDir, "Game.MGL")
		mglData := []byte(`<mistergamedescription><file path="` + gamePath + `"/></mistergamedescription>`)
		require.NoError(t, os.WriteFile(mglPath, mglData, 0o600))
		recentFile := filepath.Join(tmpDir, "cores_recent.cfg")
		writeRecentEntry(t, recentFile, tmpDir, filepath.Base(mglPath))

		path, err := recentGamePath(recentFile, nil)
		require.NoError(t, err)
		assert.Equal(t, gamePath, path)
	})

	t.Run("non-MGL core is ignored", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		recentFile := filepath.Join(tmpDir, "cores_recent.cfg")
		writeRecentEntry(t, recentFile, tmpDir, "SNES.rbf")

		path, err := recentGamePath(recentFile, nil)
		require.NoError(t, err)
		assert.Empty(t, path)
	})

	t.Run("malformed MGL returns error", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		mglPath := filepath.Join(tmpDir, "Broken.mgl")
		require.NoError(t, os.WriteFile(mglPath, []byte(`<mistergamedescription>`), 0o600))
		recentFile := filepath.Join(tmpDir, "cores_recent.cfg")
		writeRecentEntry(t, recentFile, tmpDir, filepath.Base(mglPath))

		_, err := recentGamePath(recentFile, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error reading mgl file")
	})
}

func TestRecentGamePath_EmptyRecentFile(t *testing.T) {
	t.Parallel()

	recentFile := filepath.Join(t.TempDir(), "empty_recent.cfg")
	require.NoError(t, os.WriteFile(recentFile, nil, 0o600))

	path, err := recentGamePath(recentFile, nil)
	require.NoError(t, err)
	assert.Empty(t, path)
}

func TestCoreMatchesSystem(t *testing.T) {
	t.Parallel()

	mappings := []NameMapping{
		{CoreName: "SNES", System: "SNES"},
		{CoreName: "RA_SNES", System: "SNES"},
		{CoreName: "Minimig", System: "Amiga"},
		{CoreName: "Minimig", System: "AmigaCD32"},
		{CoreName: "SonicBoom", System: ArcadeSystem, ArcadeName: "Sonic Boom"},
	}
	tests := []struct {
		name    string
		core    string
		system  string
		matches bool
	}{
		{name: "same console", core: "snes", system: "SNES", matches: true},
		{name: "alternate core", core: "RA_SNES", system: "SNES", matches: true},
		{name: "shared core second mapping", core: "Minimig", system: "AmigaCD32", matches: true},
		{name: "arcade set maps to Arcade", core: "SonicBoom", system: ArcadeSystem, matches: true},
		{name: "old arcade game on console core", core: "SNES", system: ArcadeSystem},
		{name: "menu has no game", core: misterconfig.MenuCore, system: ArcadeSystem},
		{name: "unknown core", core: "Utility", system: "SNES"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.matches, coreMatchesSystem(tt.core, tt.system, mappings))
		})
	}
}

// ArcadeDatabase.csv omits hundreds of set names, nearly all of them under
// _Arcade/_alternatives, so the MRA itself has to settle whether a core the
// name map cannot place is the one running the active media.
func TestArcadeSetNameMatches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mraPath := filepath.Join(dir, "Street Fighter Zero 2 Alpha Unlock.mra")
	writeTestMRA(t, mraPath, "sfz2aljbk", "Street Fighter Zero 2 Alpha Unlock")
	noSetNamePath := filepath.Join(dir, "Nameless.mra")
	writeTestMRA(t, noSetNamePath, "", "Nameless")
	mglPath := filepath.Join(dir, "Wrapper.mgl")
	writeTestMRA(t, mglPath, "sfz2aljbk", "Wrapper")

	tests := []struct {
		name    string
		core    string
		system  string
		path    string
		matches bool
	}{
		{name: "unlisted set name", core: "sfz2aljbk", system: ArcadeSystem, path: mraPath, matches: true},
		{name: "case folded", core: "SFZ2ALJBK", system: ArcadeSystem, path: mraPath, matches: true},
		{name: "a different set name", core: "sfz2alja", system: ArcadeSystem, path: mraPath},
		{name: "not the arcade system", core: "sfz2aljbk", system: "SNES", path: mraPath},
		{name: "not an mra", core: "sfz2aljbk", system: ArcadeSystem, path: mglPath},
		{name: "mra declares no set name", core: "", system: ArcadeSystem, path: noSetNamePath},
		{name: "missing file", core: "sfz2aljbk", system: ArcadeSystem, path: filepath.Join(dir, "Gone.mra")},
		{name: "menu core", core: misterconfig.MenuCore, system: ArcadeSystem, path: mraPath},
		{name: "no core", core: "", system: ArcadeSystem, path: mraPath},
		{name: "no path", core: "sfz2aljbk", system: ArcadeSystem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.matches, arcadeSetNameMatches(tt.core, tt.system, tt.path))
		})
	}
}

// An arcade core whose set name ArcadeDatabase.csv omits used to read as a bare
// core, so LoadCore retired the media the launch had just published and hold
// mode then had nothing left to exit on removal.
func TestLoadCoreKeepsMediaForUnlistedArcadeSetName(t *testing.T) {
	t.Parallel()

	mraPath := filepath.Join(t.TempDir(), "Street Fighter Zero 2 Alpha Unlock.mra")
	writeTestMRA(t, mraPath, "sfz2aljbk", "Street Fighter Zero 2 Alpha Unlock")

	tests := []struct {
		name     string
		core     string
		mediaSys string
		mediaPat string
		nameMap  []NameMapping
		keeps    bool
	}{
		{
			name: "unlisted arcade set name keeps its media",
			core: "sfz2aljbk", mediaSys: ArcadeSystem, mediaPat: mraPath,
			keeps: true,
		},
		{
			name: "a different unlisted set name still retires it",
			core: "sfz2alja", mediaSys: ArcadeSystem, mediaPat: mraPath,
		},
		{
			name: "a bare unknown core still retires it",
			core: "Utility", mediaSys: "SNES", mediaPat: "/games/SNES/Game.sfc",
			nameMap: []NameMapping{{CoreName: "SNES", System: "SNES"}},
		},
		{
			name: "a name-mapped core still keeps its media",
			core: "SNES", mediaSys: "SNES", mediaPat: "/games/SNES/Game.sfc",
			nameMap: []NameMapping{{CoreName: "SNES", System: "SNES"}},
			keeps:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			coreFile := filepath.Join(t.TempDir(), "CORENAME")
			require.NoError(t, os.WriteFile(coreFile, []byte(tt.core), 0o600))

			published := models.NewActiveMedia(tt.mediaSys, tt.mediaSys, tt.mediaPat, "Game", "Launcher")
			tr := &Tracker{
				coreNameFile:   coreFile,
				NameMap:        tt.nameMap,
				activeMedia:    func() *models.ActiveMedia { return published },
				setActiveMedia: func(media *models.ActiveMedia) { published = media },
				setActiveGame:  func(string) error { return nil },
				// An empty ACTIVEGAME stops LoadCore at the retention decision,
				// which is the only thing under test here.
				readActiveGame: func() (string, error) { return "", nil },
			}

			tr.LoadCore()

			assert.Equal(t, tt.core, tr.ActiveCore)
			if tt.keeps {
				assert.NotNil(t, published, "the running core owns this media")
				return
			}
			assert.Nil(t, published, "a core that owns nothing must retire the media")
		})
	}
}

// The same set name gap stopped loadGameLocked restoring the media it had just
// retired, and stopped a manual MiSTer menu launch of such an MRA being tracked
// at all.
func TestLoadGameAcceptsUnlistedArcadeSetName(t *testing.T) {
	// Cannot use t.Parallel() - swaps the shared GlobalLauncherCache, and
	// ResolvePath changes the process working directory.

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{})
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})

	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.InitializeFromSlice([]platforms.Launcher{{
		ID:         ArcadeSystem,
		SystemID:   ArcadeSystem,
		Extensions: []string{".mra"},
	}})
	helpers.GlobalLauncherCache = testCache
	t.Cleanup(func() { helpers.GlobalLauncherCache = originalCache })

	mraPath := filepath.Join(t.TempDir(), "Street Fighter Zero 2 Alpha Unlock.mra")
	writeTestMRA(t, mraPath, "sfz2aljbk", "Street Fighter Zero 2 Alpha Unlock")

	newTracker := func(core string, published **models.ActiveMedia) *Tracker {
		return &Tracker{
			pl:             pl,
			cfg:            &config.Instance{},
			ActiveCore:     core,
			readActiveGame: func() (string, error) { return mraPath, nil },
			setActiveMedia: func(media *models.ActiveMedia) { *published = media },
		}
	}

	t.Run("publishes under the core the MRA names", func(t *testing.T) {
		var published *models.ActiveMedia
		tr := newTracker("sfz2aljbk", &published)

		tr.loadGame()

		require.NotNil(t, published)
		assert.Equal(t, ArcadeSystem, published.SystemID)
		assert.Equal(t, mraPath, published.Path)
		assert.Equal(t, ArcadeSystem+"/"+filepath.Base(mraPath), tr.ActiveGameID)

		// What DoLaunch published for the same launch. Equal is the predicate
		// State uses to decide a republish changed nothing, so this pins that
		// the restore raises no stopped/started pair and splits no history row.
		launched := models.NewActiveMedia(
			ArcadeSystem, published.SystemName, mraPath, published.Name, ArcadeSystem,
		)
		assert.True(t, launched.Equal(published), "the restore must not look like new media")
	})

	t.Run("still ignores a game from another core", func(t *testing.T) {
		var published *models.ActiveMedia
		tr := newTracker("sfz2alja", &published)

		tr.loadGame()

		assert.Nil(t, published)
	})
}

// The Zaparoo fork of MiSTer Main rewrites ACTIVEGAME with the MGL it was
// started from, so the same launch is observed twice: once by its game path
// from DoLaunch and once through the MGL wrapper. Both must identify the game
// by the file the MGL loads, or the second observation looks like a new game.
func TestLoadGameIdentifiesMGLByLoadedFile(t *testing.T) {
	// Cannot use t.Parallel() - swaps the shared GlobalLauncherCache, and
	// ResolvePath changes the process working directory.

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{})
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})

	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.InitializeFromSlice([]platforms.Launcher{{
		ID:         "Genesis",
		SystemID:   systemdefs.SystemGenesis,
		Extensions: []string{".md"},
	}})
	helpers.GlobalLauncherCache = testCache
	t.Cleanup(func() { helpers.GlobalLauncherCache = originalCache })

	dir := t.TempDir()
	gamePath := filepath.Join(dir, "Sonic The Hedgehog (USA).md")
	require.NoError(t, os.WriteFile(gamePath, []byte{}, 0o600))
	mglPath := filepath.Join(dir, ".LASTLAUNCH.mgl")
	mgl := `<mistergamedescription><rbf>_Console/Genesis</rbf>` +
		`<file delay="1" type="f" index="0" path="` + gamePath + `"/></mistergamedescription>`
	require.NoError(t, os.WriteFile(mglPath, []byte(mgl), 0o600))

	var published *models.ActiveMedia
	tr := &Tracker{
		pl:             pl,
		cfg:            &config.Instance{},
		ActiveCore:     "Genesis",
		NameMap:        []NameMapping{{CoreName: "Genesis", System: systemdefs.SystemGenesis}},
		readActiveGame: func() (string, error) { return mglPath, nil },
		setActiveMedia: func(media *models.ActiveMedia) { published = media },
	}

	tr.loadGame()

	require.NotNil(t, published)
	assert.Equal(t, systemdefs.SystemGenesis, published.SystemID)
	assert.Equal(t, gamePath, published.Path)
	assert.Equal(t, gamePath, tr.ActiveGamePath)
	assert.Equal(t, systemdefs.SystemGenesis+"/"+filepath.Base(gamePath), tr.ActiveGameID)

	// What DoLaunch published for the same launch, by the game's own path.
	launched := models.NewActiveMedia(
		systemdefs.SystemGenesis, published.SystemName, gamePath, published.Name, "Genesis",
	)
	assert.True(t, launched.Equal(published), "the MGL observation must not look like new media")
}

func TestClearActiveGameRetiresStateEvenWhenSignalWriteFails(t *testing.T) {
	t.Parallel()

	published := models.NewActiveMedia(ArcadeSystem, ArcadeSystem, "game.mra", "Game", "Arcade")
	tr := &Tracker{
		ActiveGameID: "Arcade/game.mra", ActiveGamePath: "game.mra", ActiveGameName: "Game",
		ActiveSystem: ArcadeSystem, ActiveSystemName: ArcadeSystem,
		setActiveGame: func(path string) error {
			assert.Empty(t, path)
			return assert.AnError
		},
		setActiveMedia: func(media *models.ActiveMedia) { published = media },
	}

	err := tr.ClearActiveGame()

	require.ErrorIs(t, err, assert.AnError)
	assert.Empty(t, tr.ActiveGameID)
	assert.Empty(t, tr.ActiveGamePath)
	assert.Empty(t, tr.ActiveGameName)
	assert.Empty(t, tr.ActiveSystem)
	assert.Empty(t, tr.ActiveSystemName)
	assert.Nil(t, published)
}

func TestTrackerFileChanged(t *testing.T) {
	t.Parallel()

	assert.True(t, trackerFileChanged(fsnotify.Write))
	assert.True(t, trackerFileChanged(fsnotify.Create))
	assert.True(t, trackerFileChanged(fsnotify.Rename))
	assert.True(t, trackerFileChanged(fsnotify.Write|fsnotify.Chmod))
	assert.False(t, trackerFileChanged(fsnotify.Chmod))
	assert.False(t, trackerFileChanged(fsnotify.Remove))
}

func TestTrackerRecentFileChanged(t *testing.T) {
	t.Parallel()

	assert.True(t, trackerRecentFileChanged(
		filepath.Join(misterconfig.CoreConfigFolder, "GBC_recent_0.cfg"),
	))
	assert.True(t, trackerRecentFileChanged(
		filepath.Join(misterconfig.CoreConfigFolder, "cores_recent.cfg"),
	))
	assert.False(t, trackerRecentFileChanged(
		filepath.Join(misterconfig.CoreConfigFolder, "device.bin"),
	))
	assert.False(t, trackerRecentFileChanged(
		filepath.Join(filepath.Dir(misterconfig.CoreConfigFolder), "config-backup", "GBC_recent_0.cfg"),
	))
}

func TestDispatchTrackerFileLoad(t *testing.T) {
	t.Parallel()

	settled := make(chan time.Time)
	loaded := make(chan struct{})
	dispatchTrackerFileLoad(settled, func() { close(loaded) })

	select {
	case <-loaded:
		t.Fatal("file load ran before settle signal")
	default:
	}

	settled <- time.Now()
	select {
	case <-loaded:
	case <-time.After(time.Second):
		t.Fatal("file load did not run after settle signal")
	}
}

func TestMediaLookupContextUsesServiceContext(t *testing.T) {
	t.Parallel()

	serviceCtx, cancelService := context.WithCancel(context.Background())
	tr := &Tracker{serviceCtx: serviceCtx}

	ctx, cancelLookup := tr.mediaLookupContext()
	defer cancelLookup()

	select {
	case <-ctx.Done():
		t.Fatal("MediaDB lookup context should not be canceled before service context")
	default:
	}

	cancelService()
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("MediaDB lookup context should follow service context")
	}
}

func TestMisterSelectionFiles(t *testing.T) {
	t.Parallel()

	files := misterSelectionFiles()
	assert.Equal(t, misterconfig.FileSelectFile, files.status)
	assert.Equal(t, misterconfig.FullPathFile, files.fullPath)
	assert.Equal(t, misterconfig.CurrentPathFile, files.currentPath)
	assert.Equal(t, filepath.Join(misterconfig.CoreConfigFolder, "device.bin"), files.deviceBin)
}

// buildSelection writes MiSTer's file-selector trio into a temp directory and
// ages the status file relative to the paths it describes, which is what tells
// a real launch apart from the status MiSTer rewrites when a core exits.
func buildSelection(
	t *testing.T, gameName string, statusAge, pathAge time.Duration,
) (files selectionFiles, gamePath string) {
	t.Helper()
	dir := t.TempDir()
	gamesDir := filepath.Join(dir, "games")
	require.NoError(t, os.MkdirAll(gamesDir, 0o750))
	gamePath = filepath.Join(gamesDir, gameName)
	require.NoError(t, os.WriteFile(gamePath, []byte("rom"), 0o600))

	files = selectionFiles{
		status:      filepath.Join(dir, "FILESELECT"),
		fullPath:    filepath.Join(dir, "FULLPATH"),
		currentPath: filepath.Join(dir, "CURRENTPATH"),
		deviceBin:   filepath.Join(dir, "device.bin"),
	}
	require.NoError(t, os.WriteFile(files.status, []byte("selected"), 0o600))
	require.NoError(t, os.WriteFile(files.fullPath, []byte(gamePath), 0o600))
	require.NoError(t, os.WriteFile(files.currentPath, []byte(gameName), 0o600))

	now := time.Now()
	require.NoError(t, os.Chtimes(files.status, now.Add(-statusAge), now.Add(-statusAge)))
	for _, f := range []string{files.fullPath, files.currentPath} {
		require.NoError(t, os.Chtimes(f, now.Add(-pathAge), now.Add(-pathAge)))
	}
	return files, gamePath
}

// TestLoadFileSelection drives the whole selection path, not just the
// staleness predicate: the exit re-notification only causes harm because
// loadFileSelection went on to publish an active game from it.
func TestLoadFileSelection(t *testing.T) {
	// Cannot use t.Parallel() - swaps the shared GlobalLauncherCache.

	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{})
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string{})

	originalCache := helpers.GlobalLauncherCache
	testCache := &helpers.LauncherCache{}
	testCache.InitializeFromSlice([]platforms.Launcher{{
		ID:         "NES",
		SystemID:   "NES",
		Extensions: []string{".nes"},
	}})
	helpers.GlobalLauncherCache = testCache
	t.Cleanup(func() { helpers.GlobalLauncherCache = originalCache })

	newTracker := func(files selectionFiles, record func(string) error) *Tracker {
		return &Tracker{
			pl:            pl,
			cfg:           &config.Instance{},
			selection:     files,
			setActiveGame: record,
		}
	}

	t.Run("a settled selection is recorded", func(t *testing.T) {
		files, gamePath := buildSelection(t, "Zelda.nes", 10*time.Second, 10*time.Second)
		var recorded []string
		tr := newTracker(files, func(path string) error {
			recorded = append(recorded, path)
			return nil
		})

		tr.loadFileSelection()

		assert.Equal(t, []string{gamePath}, recorded)
	})

	t.Run("a status re-notified after a core exit records nothing", func(t *testing.T) {
		files, _ := buildSelection(t, "Zelda.nes", 0, 31*time.Second)
		var recorded []string
		tr := newTracker(files, func(path string) error {
			recorded = append(recorded, path)
			return nil
		})

		tr.loadFileSelection()

		assert.Empty(t, recorded, "the exit re-notification must not resurrect the closed game")
	})

	t.Run("a system or menu file records nothing", func(t *testing.T) {
		files, _ := buildSelection(t, "Menu.rbf", 10*time.Second, 10*time.Second)
		var recorded []string
		tr := newTracker(files, func(path string) error {
			recorded = append(recorded, path)
			return nil
		})

		tr.loadFileSelection()

		assert.Empty(t, recorded)
	})

	t.Run("a path no launcher claims records nothing", func(t *testing.T) {
		files, _ := buildSelection(t, "Unknown.zzz", 10*time.Second, 10*time.Second)
		var recorded []string
		tr := newTracker(files, func(path string) error {
			recorded = append(recorded, path)
			return nil
		})

		tr.loadFileSelection()

		assert.Empty(t, recorded)
	})

	t.Run("a failed record is reported, not fatal", func(t *testing.T) {
		files, gamePath := buildSelection(t, "Zelda.nes", 10*time.Second, 10*time.Second)
		var seen string
		tr := newTracker(files, func(path string) error {
			seen = path
			return errors.New("write failed")
		})

		tr.loadFileSelection()

		assert.Equal(t, gamePath, seen)
	})
}

// arcadeCachePlatform implements the optional platformWithArcadeCache interface
// so LoadCore's card-launch suppression can be exercised.
type arcadeCachePlatform struct {
	*mocks.MockPlatform
	setname string
}

func (p *arcadeCachePlatform) CheckAndClearArcadeCardLaunch(setname string) bool {
	if p.setname != "" && p.setname == setname {
		p.setname = ""
		return true
	}
	return false
}

// MiSTer truncates CORENAME before rewriting it, so inotify delivers a write
// event while the file is still empty. Acting on that empty read retired the
// active media a moment before the real core name arrived.
func TestLoadCoreIgnoresEmptyCoreNameFile(t *testing.T) {
	t.Parallel()

	coreFile := filepath.Join(t.TempDir(), "CORENAME")
	require.NoError(t, os.WriteFile(coreFile, []byte("  \n"), 0o600))

	published := models.NewActiveMedia(ArcadeSystem, ArcadeSystem, "esprade.mra", "ESP Ra.De.", "Arcade")
	tr := &Tracker{
		coreNameFile:   coreFile,
		ActiveCore:     misterconfig.MenuCore,
		activeMedia:    func() *models.ActiveMedia { return published },
		setActiveMedia: func(media *models.ActiveMedia) { published = media },
		setActiveGame: func(string) error {
			t.Error("empty CORENAME must not touch the active game file")
			return nil
		},
	}

	tr.LoadCore()

	assert.NotNil(t, published, "empty CORENAME must not retire active media")
	assert.Equal(t, misterconfig.MenuCore, tr.ActiveCore, "empty read must not become the active core")
}

// The card-launch cache suppresses the duplicate notification that follows a
// Zaparoo-initiated arcade launch. Once the media has been retired there is
// nothing left to duplicate, so CORENAME is the only chance to restore it.
func TestLoadCoreArcadeCardLaunchSuppression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		existing    *models.ActiveMedia
		name        string
		wantPublish bool
	}{
		{
			name:        "restores media retired before the core name arrived",
			existing:    nil,
			wantPublish: true,
		},
		{
			name: "still suppresses the duplicate while media is live",
			existing: models.NewActiveMedia(
				ArcadeSystem, ArcadeSystem, "esprade", "ESP Ra.De.", "Arcade",
			),
			wantPublish: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			coreFile := filepath.Join(t.TempDir(), "CORENAME")
			require.NoError(t, os.WriteFile(coreFile, []byte("esprade"), 0o600))

			published := tt.existing
			var publishCalled bool
			tr := &Tracker{
				pl:           &arcadeCachePlatform{MockPlatform: mocks.NewMockPlatform(), setname: "esprade"},
				coreNameFile: coreFile,
				NameMap: []NameMapping{
					{CoreName: "esprade", System: ArcadeSystem, ArcadeName: "ESP Ra.De."},
				},
				activeMedia: func() *models.ActiveMedia { return published },
				setActiveMedia: func(media *models.ActiveMedia) {
					publishCalled = true
					published = media
				},
				setActiveGame: func(string) error { return nil },
			}

			tr.LoadCore()

			assert.Equal(t, tt.wantPublish, publishCalled)
			if tt.wantPublish {
				require.NotNil(t, published)
				assert.Equal(t, ArcadeSystem, published.SystemID)
				assert.Equal(t, "ESP Ra.De.", published.Name)
			}
		})
	}
}

// Every tracker file that is rewritten by truncating first has to settle
// before it is read, or the handler acts on the empty intermediate state.
// ACTIVEGAME is the one that bites: SetActiveGame truncates it, and reading
// that empty value retires the media the launch just published.
func TestTrackerFileLoadSettles(t *testing.T) {
	t.Parallel()

	tr := &Tracker{}

	tests := []struct {
		tracker    *Tracker
		name       string
		file       string
		wantLoad   bool
		wantSettle bool
	}{
		{name: "active game settles", file: misterconfig.ActiveGameFile, wantLoad: true, wantSettle: true},
		{name: "file select settles", file: misterconfig.FileSelectFile, wantLoad: true, wantSettle: true},
		{
			name:       "recent file settles",
			file:       filepath.Join(misterconfig.CoreConfigFolder, "GBC_recent_0.cfg"),
			wantLoad:   true,
			wantSettle: true,
		},
		{name: "core name is read immediately", file: misterconfig.CoreNameFile, wantLoad: true, wantSettle: false},
		{
			name:       "core name override is routed, not the real path",
			tracker:    &Tracker{coreNameFile: "/tmp/zaparoo-test-corename"},
			file:       "/tmp/zaparoo-test-corename",
			wantLoad:   true,
			wantSettle: false,
		},
		{
			name:     "an overridden tracker ignores the real core name file",
			tracker:  &Tracker{coreNameFile: "/tmp/zaparoo-test-corename"},
			file:     misterconfig.CoreNameFile,
			wantLoad: false,
		},
		{name: "unknown file has no loader", file: "/tmp/not-a-tracker-file", wantLoad: false, wantSettle: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := tr
			if tt.tracker != nil {
				target = tt.tracker
			}
			load, settle := target.trackerFileLoad(tt.file)
			assert.Equal(t, tt.wantLoad, load != nil)
			assert.Equal(t, tt.wantSettle, settle)
		})
	}
}
