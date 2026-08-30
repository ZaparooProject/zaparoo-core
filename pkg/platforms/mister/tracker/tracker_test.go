//go:build linux

package tracker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
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

	t.Run("an unreadable timestamp fails open", func(t *testing.T) {
		t.Parallel()
		files, gamePath := build(t, "Blockade.mra", 0, 31*time.Second)
		require.NoError(t, os.Remove(files.currentPath))
		require.NoError(t, os.WriteFile(files.currentPath, []byte("Blockade.mra"), 0o600))
		path, ok := resolveSelectedLaunchPath(files, time.Hour)
		require.True(t, ok, "a window that cannot be exceeded still resolves")
		assert.Equal(t, gamePath, path)
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
		statusFile, _ := write(t, 0, 0)
		_, err := selectionIsStale(statusFile, filepath.Join(t.TempDir(), "absent"), selectionStaleWindow)
		require.Error(t, err)
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
