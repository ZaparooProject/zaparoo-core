//go:build linux

package mister

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/arcadedb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

type arcadeSystemSpec struct {
	systemID  string
	platforms []string
}

var misterArcadeSystemSpecs = []arcadeSystemSpec{
	{systemID: systemdefs.SystemCPS1, platforms: []string{"Capcom CPS-1", "Capcom CPS-1.5"}},
	{systemID: systemdefs.SystemCPS2, platforms: []string{"Capcom CPS-2"}},
	{systemID: systemdefs.SystemCPS3, platforms: []string{"Capcom CPS-3"}},
	{systemID: systemdefs.SystemIremM72, platforms: []string{"Irem M72"}},
	{systemID: systemdefs.SystemIremM92, platforms: []string{"Irem M92"}},
	{systemID: systemdefs.SystemJalecoMegaSystem1, platforms: []string{"Jaleco Mega System 1"}},
	{systemID: systemdefs.SystemNamcoSystem1, platforms: []string{"Namco System-1"}},
	{systemID: systemdefs.SystemPGM, platforms: []string{"IGS PGM"}},
	{systemID: systemdefs.SystemSegaSTV, platforms: []string{"Sega ST-V"}},
	{systemID: systemdefs.SystemSegaSystem16, platforms: []string{"Sega System 16"}},
	{systemID: systemdefs.SystemSegaSystem18, platforms: []string{"Sega System 18"}},
	{systemID: systemdefs.SystemTaitoF2, platforms: []string{"Taito F2 System"}},
}

type arcadeSystemCache struct {
	platform        *Platform
	scanArcadeFiles func(context.Context, *config.Instance) ([]platforms.ScanResult, error)
	readArcadeDB    func(platforms.Platform) ([]arcadedb.ArcadeDbEntry, error)
	results         map[string][]platforms.ScanResult
	mu              syncutil.Mutex
	loaded          bool
}

func newArcadeSystemCache(platform *Platform) *arcadeSystemCache {
	cache := &arcadeSystemCache{platform: platform}
	cache.scanArcadeFiles = cache.scanFiles
	cache.readArcadeDB = arcadedb.ReadArcadeDb
	return cache
}

func (c *arcadeSystemCache) captureScanner(
	ctx context.Context,
	cfg *config.Instance,
	_ string,
	results []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	if err := c.load(ctx, cfg, results); err != nil {
		return nil, err
	}
	return results, nil
}

func (c *arcadeSystemCache) scanner(systemID string) func(
	context.Context, *config.Instance, string, []platforms.ScanResult,
) ([]platforms.ScanResult, error) {
	return func(
		ctx context.Context,
		cfg *config.Instance,
		_ string,
		_ []platforms.ScanResult,
	) ([]platforms.ScanResult, error) {
		if err := c.load(ctx, cfg, nil); err != nil {
			return nil, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return append([]platforms.ScanResult(nil), c.results[systemID]...), nil
	}
}

func (c *arcadeSystemCache) load(
	ctx context.Context,
	cfg *config.Instance,
	files []platforms.ScanResult,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(files) == 0 {
		var err error
		files, err = c.scanArcadeFiles(ctx, cfg)
		if err != nil {
			return err
		}
	}

	entries, err := c.readArcadeDB(c.platform)
	if err != nil {
		log.Warn().Err(err).Msg("unable to classify MiSTer arcade systems")
		c.loaded = true
		return nil
	}

	setSystems := arcadeSetSystems(entries)

	classified := make(map[string][]platforms.ScanResult, len(misterArcadeSystemSpecs))
	for i := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !strings.EqualFold(filepath.Ext(files[i].Path), ".mra") {
			continue
		}
		mra, readErr := mgls.ReadMRA(files[i].Path)
		if readErr != nil {
			log.Debug().Err(readErr).Str("path", files[i].Path).Msg("unable to classify arcade MRA")
			continue
		}
		if systemID := setSystems[strings.ToLower(mra.SetName)]; systemID != "" {
			classified[systemID] = append(classified[systemID], files[i])
		}
	}
	c.results = classified
	c.loaded = true
	return nil
}

func arcadeSetSystems(entries []arcadedb.ArcadeDbEntry) map[string]string {
	platformSystems := make(map[string]string)
	for _, spec := range misterArcadeSystemSpecs {
		for _, platformName := range spec.platforms {
			platformSystems[platformName] = spec.systemID
		}
	}
	setSystems := make(map[string]string, len(entries))
	for i := range entries {
		if systemID := platformSystems[entries[i].Platform]; systemID != "" {
			setSystems[strings.ToLower(entries[i].Setname)] = systemID
		}
	}
	return setSystems
}

func (c *arcadeSystemCache) scanFiles(
	ctx context.Context,
	cfg *config.Instance,
) ([]platforms.ScanResult, error) {
	var results []platforms.ScanResult
	for _, root := range c.platform.RootDirs(cfg) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		arcadePath, err := mediascanner.FindPath(ctx, filepath.Join(root, "_Arcade"))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		walkErr := afero.Walk(
			c.platform.filesystem(),
			arcadePath,
			func(path string, info os.FileInfo, walkEntryErr error) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if walkEntryErr != nil {
					return walkEntryErr
				}
				if info.IsDir() {
					return nil
				}
				ext := filepath.Ext(path)
				if strings.EqualFold(ext, ".mra") || strings.EqualFold(ext, ".mgl") {
					results = append(results, platforms.ScanResult{Path: path})
				}
				return nil
			},
		)
		if walkErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			log.Warn().Err(walkErr).Str("path", arcadePath).Msg("unable to scan MiSTer arcade files for classification")
		}
	}
	return results, nil
}

func addNeoGeoMVSLauncher(
	platform *Platform,
	neoGeo *platforms.Launcher,
) (updatedNeoGeo, neoGeoMVS platforms.Launcher) {
	updatedNeoGeo = *neoGeo
	baseScanner := updatedNeoGeo.Scanner
	var mu syncutil.Mutex
	var cached []platforms.ScanResult
	var loaded bool

	scanAndCache := func(
		ctx context.Context,
		cfg *config.Instance,
		systemID string,
		results []platforms.ScanResult,
	) ([]platforms.ScanResult, error) {
		scanned, err := baseScanner(ctx, cfg, systemID, results)
		if err != nil {
			return scanned, err
		}
		mu.Lock()
		cached = append([]platforms.ScanResult(nil), scanned...)
		loaded = true
		mu.Unlock()
		return scanned, nil
	}
	updatedNeoGeo.Scanner = scanAndCache

	neoGeoMVS = platforms.Launcher{
		ID:         systemdefs.SystemNeoGeoMVS,
		SystemID:   systemdefs.SystemNeoGeoMVS,
		Folders:    []string{"NEOGEO"},
		Extensions: []string{".neo", ".zip"},
		Test: func(_ *config.Instance, path string) bool {
			return filepath.Ext(path) == ""
		},
		SkipFilesystemScan: true,
		Launch:             launchNeoGeoMVS(platform),
		Scanner: func(
			ctx context.Context,
			cfg *config.Instance,
			_ string,
			_ []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			mu.Lock()
			wasLoaded := loaded
			results := append([]platforms.ScanResult(nil), cached...)
			mu.Unlock()
			if wasLoaded {
				return results, nil
			}
			return scanAndCache(ctx, cfg, systemdefs.SystemNeoGeo, nil)
		},
	}
	return updatedNeoGeo, neoGeoMVS
}

func launchNeoGeoMVS(platform *Platform) func(
	*config.Instance, string, *platforms.LaunchOptions,
) (*os.Process, error) {
	baseLaunch := launch(platform, systemdefs.SystemNeoGeo)
	return func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		launchOpts := neoGeoMVSLaunchOptions(opts)
		return baseLaunch(cfg, path, &launchOpts)
	}
}

func neoGeoMVSLaunchOptions(opts *platforms.LaunchOptions) platforms.LaunchOptions {
	launchOpts := platforms.LaunchOptions{}
	if opts != nil {
		launchOpts = *opts
	}
	if launchOpts.SetName == "" {
		launchOpts.SetName = systemdefs.SystemNeoGeoMVS
	}
	if launchOpts.SetNameSameDir == "" {
		launchOpts.SetNameSameDir = "true"
	}
	return launchOpts
}

func addArcadeSystemLaunchers(platform *Platform, launchers []platforms.Launcher) []platforms.Launcher {
	cache := newArcadeSystemCache(platform)
	for i := range launchers {
		if launchers[i].ID == systemdefs.SystemArcade {
			launchers[i].Scanner = cache.captureScanner
			break
		}
	}

	for _, spec := range misterArcadeSystemSpecs {
		launchers = append(launchers, platforms.Launcher{
			ID:                 spec.systemID,
			SystemID:           spec.systemID,
			Folders:            []string{"_Arcade"},
			Extensions:         []string{".mra"},
			SkipFilesystemScan: true,
			Scanner:            cache.scanner(spec.systemID),
			Launch:             launchArcade(platform, systemdefs.SystemArcade),
		})
	}
	return launchers
}
