//go:build linux

package mister

import (
	"encoding/xml"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/catalog"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKitrinxAutomaticPreference(t *testing.T) {
	for _, installed := range []string{"Kitrinx", "Jotego", "both"} {
		t.Run(installed, func(t *testing.T) {
			var rbfs []cores.RBFInfo
			if installed != "Jotego" {
				rbfs = append(rbfs, cores.RBFInfo{
					ShortName: "NGPC", MglName: "_Console/NGPC", Filename: "NGPC_20260824.rbf",
				})
			}
			if installed != "Kitrinx" {
				rbfs = append(rbfs, cores.RBFInfo{
					ShortName: "JTNGPC", MglName: "_Arcade/JTNGPC", Filename: "JTNGPC_20260824.rbf",
				})
			}
			withRBFCache(t, rbfs)
			pl := mocks.NewMockPlatform()
			cfg := &config.Instance{}
			pl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()})
			root := filepath.Join(misterconfig.SDRootDir, "games")
			pl.On("RootDirs", cfg).Return([]string{root})
			launchers := CreateLaunchers(pl)
			oldCache := helpers.GlobalLauncherCache
			helpers.GlobalLauncherCache = &helpers.LauncherCache{}
			t.Cleanup(func() { helpers.GlobalLauncherCache = oldCache })
			helpers.GlobalLauncherCache.InitializeFromSlice(launchers)
			path := filepath.Join(root, "NGPC", "Game.ngc")
			want := kitrinxNGPCCore.LauncherID
			if installed == "Jotego" {
				want = "NeoGeoPocketColor"
			}
			got, err := helpers.FindLauncher(cfg, pl, path)
			require.NoError(t, err)
			assert.Equal(t, want, got.ID)
			matcher := helpers.NewLauncherMatcher(cfg, pl)
			got, err = matcher.FindLauncher(path)
			require.NoError(t, err)
			assert.Equal(t, want, got.ID)
			legacy := findLauncher(launchers, "NeoGeoPocketColor")
			require.NotNil(t, legacy)
			core, err := cores.GetCore(legacy.ID)
			require.NoError(t, err)
			assert.Equal(t, "_Arcade/JTNGPC", core.RBF)
			assert.Equal(t, "JTNGPC", core.SetName)
		})
	}
}

func TestCDTVLauncherMatching(t *testing.T) {
	pl := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	pl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()})
	root := filepath.Join(misterconfig.SDRootDir, "games")
	pl.On("RootDirs", cfg).Return([]string{root})
	launchers := CreateLaunchers(pl)
	oldCache := helpers.GlobalLauncherCache
	helpers.GlobalLauncherCache = &helpers.LauncherCache{}
	t.Cleanup(func() { helpers.GlobalLauncherCache = oldCache })
	helpers.GlobalLauncherCache.InitializeFromSlice(launchers)
	launcher := findLauncher(launchers, "CommodoreCDTV")
	require.NotNil(t, launcher)
	assert.Contains(t, launcher.Extensions, ".chd")
	for _, folder := range []string{"CDTV", "AmigaCDTV", "CommodoreCDTV"} {
		for _, ext := range []string{".chd", ".CHD", ".cue", ".iso", ".mgl"} {
			path := filepath.Join(root, folder, "Game"+ext)
			got, err := helpers.FindLauncher(cfg, pl, path)
			require.NoError(t, err)
			assert.Equal(t, "CommodoreCDTV", got.SystemID)
			matcher := helpers.NewLauncherMatcher(cfg, pl)
			assert.True(t, matcher.MatchSystemFileForScan("CommodoreCDTV", path))
		}
	}
}

func TestDVDAndNGPCMediaDefinitions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		alternate                                     *cores.Core
		name, system, extension, rbf, setName, method string
		index                                         int
	}{
		{nil, "FPGA DVD", "DVDPlayer", ".iso", "_Other/DVD", "DVD", "s", 0},
		{&hybridDVDCore, "Hybrid DVD", "DVDPlayer", ".iso", "DVD_Player", "DVD-Player", "f", 0},
		{nil, "Jotego NGPC", "NeoGeoPocketColor", ".ngc", "_Arcade/JTNGPC", "JTNGPC", "f", 1},
		{&kitrinxNGPCCore, "Kitrinx NGC", "NeoGeoPocketColor", ".ngc", "_Console/NGPC", "NGPC", "f", 1},
		{&kitrinxNGPCCore, "Kitrinx NPC", "NeoGeoPocketColor", ".npc", "_Console/NGPC", "NGPC", "f", 1},
		{&kitrinxNGPCCore, "Kitrinx mono", "NeoGeoPocketColor", ".ngp", "_Console/NGPC", "NGPC", "f", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, err := catalog.Get(tc.system)
			require.NoError(t, err)
			core := *base
			if tc.alternate != nil {
				require.NoError(t, configureAltCoreDefinition(&core, tc.alternate, nil))
			}
			assert.Equal(t, tc.rbf, core.RBF)
			path := filepath.Join(misterconfig.SDRootDir, "games", "test", "Media"+tc.extension)
			document, err := mgls.GenerateMgl(&core, core.RBF, path, "")
			require.NoError(t, err)
			type setNameElement struct {
				Name    string `xml:",chardata"`
				SameDir string `xml:"same_dir,attr"`
			}
			type fileElement struct {
				Method string `xml:"type,attr"`
				Index  int    `xml:"index,attr"`
				Delay  int    `xml:"delay,attr"`
			}
			var decoded struct {
				SetName setNameElement `xml:"setname"`
				File    fileElement    `xml:"file"`
			}
			require.NoError(t, xml.Unmarshal([]byte(document), &decoded))
			assert.Equal(t, tc.setName, decoded.SetName.Name)
			assert.Empty(t, decoded.SetName.SameDir)
			assert.Equal(t, tc.method, decoded.File.Method)
			assert.Equal(t, tc.index, decoded.File.Index)
			assert.Equal(t, 2, decoded.File.Delay)
			unchanged, err := catalog.Get(tc.system)
			require.NoError(t, err)
			assert.Equal(t, unchanged, base, "alternate configuration must not mutate primary")
		})
	}
}

func TestDVDAndNGPCLauncherMetadata(t *testing.T) {
	t.Parallel()
	launchers := CreateLaunchers(NewPlatform())
	dvd := findLauncher(launchers, "DVDPlayer")
	require.NotNil(t, dvd)
	assert.Equal(t, []string{"DVD", "DVD-Player"}, dvd.Folders)
	assert.Equal(t, []string{".iso", ".mgl"}, dvd.Extensions)
	hybrid := findLauncher(launchers, "HybridDVDPlayer")
	require.NotNil(t, hybrid)
	assert.Equal(t, "DVDPlayer", hybrid.SystemID)
	assert.Empty(t, hybrid.Folders, "primary owns DVD indexing")
	assert.Equal(t, []string{"DVD_Player", "_Other/DVD_Player"}, cores.GlobalRBFCache.AltCorePaths(hybrid.ID))
	ngpc := findLauncher(launchers, "KitrinxNeoGeoPocketColor")
	require.NotNil(t, ngpc)
	assert.Equal(t, "NeoGeoPocketColor", ngpc.SystemID)
	assert.Equal(t, []string{"NGPC"}, ngpc.Folders)
	assert.Equal(t, []string{".ngc", ".npc"}, ngpc.Extensions)
	assert.Equal(t, []string{"_Console/NGPC"}, cores.GlobalRBFCache.AltCorePaths(ngpc.ID))
}

func TestHybridDVDLauncherRuntime(t *testing.T) {
	cache := withRBFCache(t, []cores.RBFInfo{{
		Path: filepath.Join(misterconfig.SDRootDir, "DVD_Player.rbf"), Filename: "DVD_Player.rbf",
		ShortName: "DVD_Player", MglName: "DVD_Player",
	}})
	cache.RegisterAltCore(hybridDVDCore.LauncherID, hybridDVDCore.RBF)
	pl := &Platform{}
	runtime := pl.LauncherRuntime(nil, &platforms.Launcher{ID: hybridDVDCore.LauncherID, SystemID: "DVDPlayer"})
	require.NotNil(t, runtime.MisterCore)
	assert.Equal(t, "DVD-Player", runtime.MisterCore.Name, "tracker must recognize actual CORENAME")
}

func TestMediaAlternateSetNameOverrides(t *testing.T) {
	t.Parallel()
	core, err := catalog.Get("DVDPlayer")
	require.NoError(t, err)
	require.NoError(t, configureAltCoreDefinition(core, &hybridDVDCore, &platforms.LaunchOptions{
		SetName: "CustomDVD", SetNameSameDir: "yes",
	}))
	assert.Equal(t, "CustomDVD", core.SetName)
	assert.True(t, core.SetNameSameDir)
	params, err := catalog.PathToMGLDef(core, "Movie.iso")
	require.NoError(t, err)
	assert.Equal(t, "f", params.Method)
	require.Error(t, configureAltCoreDefinition(core, &hybridDVDCore, &platforms.LaunchOptions{SetName: "../DVD"}))
}

func TestDVDAndNGPCRBFResolution(t *testing.T) {
	t.Parallel()
	for _, hybridPath := range []string{"DVD_Player.rbf", filepath.Join("_Other", "DVD_Player.rbf")} {
		t.Run(hybridPath, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			dvdFile := filepath.Join("_Other", "DVD_20260903.rbf")
			ngpcFile := filepath.Join("_Console", "NGPC_20260824.rbf")
			for _, path := range []string{hybridPath, dvdFile, ngpcFile} {
				full := filepath.Join(misterconfig.SDRootDir, path)
				require.NoError(t, fs.MkdirAll(filepath.Dir(full), 0o750))
				require.NoError(t, afero.WriteFile(fs, full, nil, 0o600))
			}
			cache := &cores.RBFCache{}
			cache.SetFilesystem(fs)
			cache.RegisterAltCore(hybridDVDCore.LauncherID, hybridDVDCore.RBF, "_Other/DVD_Player")
			cache.RegisterAltCore(kitrinxNGPCCore.LauncherID, kitrinxNGPCCore.RBF)
			cache.Refresh()
			for _, tc := range []struct{ launcher, system, want, file string }{
				{"DVDPlayer", "DVDPlayer", filepath.Join("_Other", "DVD"), dvdFile},
				{"HybridDVDPlayer", "DVDPlayer", hybridPath[:len(hybridPath)-len(".rbf")], hybridPath},
				{"KitrinxNeoGeoPocketColor", "NeoGeoPocketColor", filepath.Join("_Console", "NGPC"), ngpcFile},
			} {
				info, ok := cache.ResolveLauncherStrict(nil, tc.launcher, tc.system)
				require.True(t, ok, tc.launcher)
				assert.Equal(t, tc.want, info.MglName)
				assert.Equal(t, filepath.Join(misterconfig.SDRootDir, tc.file), info.Path)
			}
			require.NoError(t, fs.Remove(filepath.Join(misterconfig.SDRootDir, hybridPath)))
			require.NoError(t, cache.ForceRefresh())
			_, ok := cache.ResolveLauncherStrict(nil, "HybridDVDPlayer", "DVDPlayer")
			assert.False(t, ok, "missing hybrid must not be mistaken for installed FPGA DVD")
		})
	}
}
