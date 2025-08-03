package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog/log"
)

type Launchers struct {
	IndexRoot   []string `toml:"index_root,omitempty,multiline"`
	AllowFile   []string `toml:"allow_file,omitempty,multiline"`
	allowFileRe []*regexp.Regexp
	MediaDir    string             `toml:"media_dir,omitempty"`
	Default     []LaunchersDefault `toml:"default,omitempty"`
	Custom      []LaunchersCustom  `toml:"custom,omitempty"`
}

type LaunchersDefault struct {
	Launcher   string `toml:"launcher"`
	InstallDir string `toml:"install_dir,omitempty"`
}

type LaunchersCustom struct {
	ID        string   `toml:"id"`
	System    string   `toml:"system"`
	Execute   string   `toml:"execute"`
	MediaDirs []string `toml:"media_dirs"`
	FileExts  []string `toml:"file_exts"`
}

func (c *Instance) DefaultMediaDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Launchers.MediaDir
}

func (c *Instance) IsLauncherFileAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.Launchers.AllowFile, c.vals.Launchers.allowFileRe, s)
}

func (c *Instance) LookupLauncherDefaults(launcherID string) (LaunchersDefault, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, defaultLauncher := range c.vals.Launchers.Default {
		if strings.EqualFold(defaultLauncher.Launcher, launcherID) {
			return defaultLauncher, true
		}
	}
	return LaunchersDefault{}, false
}

func (c *Instance) LoadCustomLaunchers(launchersDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := os.Stat(launchersDir)
	if err != nil {
		return err
	}

	var launcherFiles []string

	err = filepath.WalkDir(
		launchersDir,
		func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() {
				return nil
			}

			if strings.ToLower(filepath.Ext(d.Name())) != ".toml" {
				return nil
			}

			launcherFiles = append(launcherFiles, path)

			return nil
		},
	)
	if err != nil {
		return err
	}
	log.Info().Msgf("found %d custom launcher files", len(launcherFiles))

	filesCounts := 0
	launchersCount := 0

	for _, launcherPath := range launcherFiles {
		log.Debug().Msgf("loading custom launcher: %s", launcherPath)

		//nolint:gosec // Safe: reads launcher config files from controlled application directories
		data, err := os.ReadFile(launcherPath)
		if err != nil {
			log.Error().Msgf("error reading custom launcher: %s", launcherPath)
			continue
		}

		var newVals Values
		err = toml.Unmarshal(data, &newVals)
		if err != nil {
			log.Error().Msgf("error parsing custom launcher: %s", launcherPath)
			continue
		}

		c.vals.Launchers.Custom = append(c.vals.Launchers.Custom, newVals.Launchers.Custom...)

		filesCounts++
		launchersCount += len(newVals.Launchers.Custom)
	}

	log.Info().Msgf("loaded %d files, %d custom launchers", filesCounts, launchersCount)

	return nil
}

func (c *Instance) CustomLaunchers() []LaunchersCustom {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Launchers.Custom
}

func (c *Instance) IndexRoots() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.vals.Launchers.MediaDir != "" {
		return append([]string{c.vals.Launchers.MediaDir}, c.vals.Launchers.IndexRoot...)
	}
	return c.vals.Launchers.IndexRoot
}
