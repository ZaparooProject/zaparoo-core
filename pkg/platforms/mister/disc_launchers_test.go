//go:build linux && !android

package mister

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sdRoot = misterconfig.SDRootDir

func writeFiles(t *testing.T, fs afero.Fs, paths ...string) {
	t.Helper()
	for _, path := range paths {
		require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, afero.WriteFile(fs, path, []byte("x"), 0o644))
	}
}

func TestResolveDiscProviderMGL(t *testing.T) {
	t.Parallel()
	physicalCD := sdRoot + "/MiSTer_Physical-CD"
	misterDisc := sdRoot + "/MiSTer-disc"
	tests := []struct {
		wantErr error
		name    string
		system  string
		want    string
		files   []string
	}{
		{name: "no provider", system: systemdefs.SystemPSX, wantErr: errNoDiscProvider},
		{
			name:   "first provider",
			system: systemdefs.SystemPSX,
			files: []string{
				physicalCD, sdRoot + "/_Physical Disc Cores/PSX.mgl",
				misterDisc, sdRoot + "/_Disc_Cores/PlayStation.mgl",
			},
			want: sdRoot + "/_Physical Disc Cores/PSX.mgl",
		},
		{
			name:   "first provider lacks the MGL",
			system: systemdefs.SystemPSX,
			files:  []string{physicalCD, misterDisc, sdRoot + "/_Disc_Cores/PlayStation.mgl"},
			want:   sdRoot + "/_Disc_Cores/PlayStation.mgl",
		},
		{
			name:    "MGL without its provider binary",
			system:  systemdefs.SystemPSX,
			files:   []string{sdRoot + "/_Physical Disc Cores/PSX.mgl"},
			wantErr: errNoDiscProvider,
		},
		{
			name:    "provider has no MGL for the system",
			system:  systemdefs.SystemSNESMSU1,
			files:   []string{misterDisc, sdRoot + "/_Disc_Cores/PlayStation.mgl"},
			wantErr: errNoDiscLauncher,
		},
		{
			name:   "system only one provider plays",
			system: systemdefs.SystemGenesisPlus,
			files:  []string{physicalCD, sdRoot + "/_Physical Disc Cores/MDPlus.mgl"},
			want:   sdRoot + "/_Physical Disc Cores/MDPlus.mgl",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			writeFiles(t, fs, tt.files...)
			got, err := resolveDiscProviderMGL(fs, sdRoot, tt.system)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveDVDCore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		want  string
		files []string
	}{
		{name: "none"},
		{
			name:  "hybrid only",
			files: []string{sdRoot + "/_Other/dvd_player.RBF"},
			want:  sdRoot + "/_Other/dvd_player.RBF",
		},
		{
			name: "newest dated core beats the hybrid",
			files: []string{
				sdRoot + "/_Other/DVD_Player.rbf",
				sdRoot + "/_Other/DVD_20240101.rbf",
				sdRoot + "/_Console/_Extra/DVD_20250315.rbf",
			},
			want: sdRoot + "/_Console/_Extra/DVD_20250315.rbf",
		},
		{
			name: "folders without an underscore are not searched",
			files: []string{
				sdRoot + "/games/DVD_20250315.rbf",
				sdRoot + "/_Other/DVD_20240101.rbf",
			},
			want: sdRoot + "/_Other/DVD_20240101.rbf",
		},
		{
			name: "malformed dated names are ignored",
			files: []string{
				sdRoot + "/_Other/DVD_2024010.rbf",
				sdRoot + "/_Other/DVD_2024A101.rbf",
				sdRoot + "/_Other/DVD_20240101.mgl",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			writeFiles(t, fs, tt.files...)
			got, err := resolveDVDCore(fs, sdRoot)
			if tt.want == "" {
				require.ErrorIs(t, err, errNoDVDCore)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseDiscPaths(t *testing.T) {
	t.Parallel()
	drive, system, ok := parsePhysicalDiscPath("physicaldisc://sr0/PSX")
	assert.True(t, ok)
	assert.Equal(t, "sr0", drive)
	assert.Equal(t, "PSX", system)
	for _, bad := range []string{
		"physicaldisc://sr0", "physicaldisc://sda/PSX", "physicaldisc://sr0/../PSX",
		"physicaldisc://sr0/PSX/x", "physicaldisc://sr1234/PSX", "cdaudio://sr0/PSX",
	} {
		_, _, valid := parsePhysicalDiscPath(bad)
		assert.False(t, valid, bad)
	}

	drive, track, ok := parseCDAudioPath("cdaudio://sr1/12")
	assert.True(t, ok)
	assert.Equal(t, "sr1", drive)
	assert.Equal(t, 12, track)
	_, track, ok = parseCDAudioPath("CDAUDIO://sr0")
	assert.True(t, ok)
	assert.Equal(t, 0, track)
	for _, bad := range []string{"cdaudio://sr0/0", "cdaudio://sr0/100", "cdaudio://sr0/a", "cdaudio://hda"} {
		_, _, valid := parseCDAudioPath(bad)
		assert.False(t, valid, bad)
	}
}

func physicalDiscLauncher(t *testing.T, pl *Platform, system string) platforms.Launcher {
	t.Helper()
	launchers := createPhysicalDiscLaunchers(pl)
	for i := range launchers {
		if launchers[i].SystemID == system {
			return launchers[i]
		}
	}
	t.Fatalf("no physical disc launcher for %s", system)
	return platforms.Launcher{}
}

func TestPhysicalDiscLauncher(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	writeFiles(t, fs, sdRoot+"/MiSTer_Physical-CD", sdRoot+"/_Physical Disc Cores/PSX.mgl", "/dev/sr0")
	var launched []string
	pl := &Platform{
		fs:           fs,
		closeConsole: func() error { return nil },
		launchBasicFile: func(path string) error {
			launched = append(launched, path)
			return nil
		},
	}
	cfg := &config.Instance{}
	psx := physicalDiscLauncher(t, pl, systemdefs.SystemPSX)
	saturn := physicalDiscLauncher(t, pl, systemdefs.SystemSaturn)

	assert.Equal(t, "PhysicalDiscPSX", psx.ID)
	assert.True(t, psx.Test(cfg, "physicaldisc://sr0/PSX"))
	assert.False(t, psx.Test(cfg, "physicaldisc://sr0/Saturn"))
	assert.True(t, psx.VirtualPathExists(cfg, "physicaldisc://sr0/PSX"))
	assert.False(t, psx.VirtualPathExists(cfg, "physicaldisc://sr1/PSX"), "no such drive")
	require.NoError(t, psx.Availability(cfg))
	require.ErrorIs(t, saturn.Availability(cfg), errNoDiscLauncher)

	require.ErrorIs(t, psx.Preflight(cfg, "physicaldisc://sr0/Saturn", nil), errNotDiscPath)
	require.ErrorIs(t, saturn.Preflight(cfg, "physicaldisc://sr0/Saturn", nil), errNoDiscLauncher)
	require.NoError(t, psx.Preflight(cfg, "physicaldisc://sr0/PSX", nil))
	assert.Empty(t, launched, "preflight never launches")

	_, err := psx.Launch(cfg, "physicaldisc://sr0/PSX", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{sdRoot + "/_Physical Disc Cores/PSX.mgl"}, launched)
}

func TestPhysicalDiscLauncher_DVD(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	var launched string
	pl := &Platform{
		fs:           fs,
		closeConsole: func() error { return nil },
		launchBasicFile: func(path string) error {
			launched = path
			return nil
		},
	}
	cfg := &config.Instance{}
	dvd := physicalDiscLauncher(t, pl, systemdefs.SystemDVDPlayer)
	require.ErrorIs(t, dvd.Preflight(cfg, "physicaldisc://sr0/DVDPlayer", nil), errNoDVDCore)

	// Launch resolution checks every launcher's availability, so the DVD
	// launcher must not walk the SD card for a core there.
	opens := &openCountingFs{Fs: fs}
	pl.fs = opens
	require.NoError(t, dvd.Availability(cfg))
	assert.Zero(t, opens.opens.Load(), "availability must not read the SD card")
	pl.fs = fs

	writeFiles(t, fs, sdRoot+"/_Other/DVD_20250101.rbf")
	_, err := dvd.Launch(cfg, "physicaldisc://sr0/DVDPlayer", nil)
	require.NoError(t, err)
	assert.Equal(t, sdRoot+"/_Other/DVD_20250101.rbf", launched)
}

// Not parallel: swaps the global core list.
func TestPhysicalDiscLauncher_DVDAvailability(t *testing.T) {
	cfg := &config.Instance{}
	dvd := physicalDiscLauncher(t, &Platform{fs: afero.NewMemMapFs()}, systemdefs.SystemDVDPlayer)
	snes := cores.RBFInfo{
		Path: sdRoot + "/_Console/SNES_20260311.rbf", Filename: "SNES_20260311.rbf",
		ShortName: "SNES", MglName: "_Console/SNES",
	}

	withRBFCache(t, nil)
	require.NoError(t, dvd.Availability(cfg), "before the first core scan every core counts")

	withRBFCache(t, []cores.RBFInfo{snes})
	require.ErrorIs(t, dvd.Availability(cfg), errNoDVDCore,
		"disc_first falls through to the library image")

	withRBFCache(t, []cores.RBFInfo{snes, {
		Path: sdRoot + "/_Other/DVD_20250101.rbf", Filename: "DVD_20250101.rbf",
		ShortName: "DVD", MglName: "_Other/DVD",
	}})
	require.NoError(t, dvd.Availability(cfg))

	withRBFCache(t, []cores.RBFInfo{snes, {
		Path: sdRoot + "/_Other/DVD_Player.rbf", Filename: "DVD_Player.rbf",
		ShortName: "DVD_Player", MglName: "_Other/DVD_Player",
	}})
	require.NoError(t, dvd.Availability(cfg))
}

type openCountingFs struct {
	afero.Fs
	opens atomic.Int64
}

func (f *openCountingFs) Open(name string) (afero.File, error) {
	f.opens.Add(1)
	return f.Fs.Open(name) //nolint:wrapcheck // test passthrough
}

func (f *openCountingFs) Stat(name string) (os.FileInfo, error) {
	f.opens.Add(1)
	return f.Fs.Stat(name) //nolint:wrapcheck // test passthrough
}

func TestPSXCDPlayerLauncher(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	var launched string
	pl := &Platform{
		fs:           fs,
		closeConsole: func() error { return nil },
		launchBasicFile: func(path string) error {
			launched = path
			return nil
		},
	}
	cfg := &config.Instance{}
	l := createPSXCDPlayerLauncher(pl)

	assert.True(t, l.ExplicitOnly)
	assert.True(t, l.Test(cfg, "cdaudio://sr0"))
	assert.True(t, l.Test(cfg, "/media/fat/Music/Album.cue/3"))
	assert.False(t, l.Test(cfg, "/media/fat/Music/Song.mp3"))
	require.NoError(t, l.Preflight(cfg, "/media/fat/Music/Album.cue", nil))
	require.Error(t, l.Preflight(cfg, "/media/fat/Music/Song.mp3", nil))
	require.ErrorIs(t, l.Preflight(cfg, "cdaudio://sr0", nil), errNoDiscProvider)

	writeFiles(t, fs, sdRoot+"/MiSTer_Physical-CD", sdRoot+"/_Physical Disc Cores/PSX.mgl")
	_, err := l.Launch(cfg, "cdaudio://sr0", nil)
	require.NoError(t, err)
	assert.Equal(t, sdRoot+"/_Physical Disc Cores/PSX.mgl", launched)
}

func TestMiSTerHiFiLauncher(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	pl := &Platform{fs: fs}
	cfg := &config.Instance{}
	l := createMiSTerHiFiLauncher(pl)

	assert.True(t, l.ExplicitOnly)
	availErr := l.Availability(cfg)
	var repair *platforms.LaunchRepairError
	require.ErrorAs(t, availErr, &repair, "a missing Hi-Fi says what to install")
	assert.Equal(t, platforms.LaunchRepairLauncherNotInstalled, repair.Reason())
	assert.Equal(t, "MiSTer Hi-Fi is not installed", repair.Error())
	writeFiles(t, fs, hiFiScriptPath())
	require.NoError(t, l.Availability(cfg))
	assert.Equal(t, "/media/fat/Scripts/misterhifi.sh", hiFiScriptPath())

	assert.True(t, l.Test(cfg, "cdaudio://sr0"))
	assert.False(t, l.Test(cfg, "cdaudio://sr0/2"), "Hi-Fi plays the whole disc only")

	tests := []struct {
		path    string
		want    string
		wantErr bool
	}{
		{path: "cdaudio://sr0", want: "--physical-cd"},
		{path: "cdaudio://sr0/2", wantErr: true},
		{path: "/media/fat/Music/It's Here.flac", want: `'/media/fat/Music/It'\''s Here.flac'`},
		{path: "/media/fat/Music/List.M3U", want: `'/media/fat/Music/List.M3U'`},
		{path: "/media/fat/Music/Album.cue", wantErr: true},
		{path: "/media/fat/Music/Album.cue/3", wantErr: true},
		{path: "http://example.com/a.mp3", wantErr: true},
	}
	for _, tt := range tests {
		got, err := hiFiArgs(tt.path)
		if tt.wantErr {
			require.Error(t, err, tt.path)
			require.Error(t, l.Preflight(cfg, tt.path, nil), tt.path)
			continue
		}
		require.NoError(t, err, tt.path)
		assert.Equal(t, tt.want, got, tt.path)
	}
}
