// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	SchemaVersion = 1
	CfgEnv        = "ZAPAROO_CFG"
	AppEnv        = "ZAPAROO_APP"
	ScanModeTap   = "tap"
	ScanModeHold  = "hold"
)

type Values struct {
	Launchers    Launchers `toml:"launchers,omitempty"`
	ZapScript    ZapScript `toml:"zapscript,omitempty"`
	Systems      Systems   `toml:"systems,omitempty"`
	Mappings     Mappings  `toml:"mappings,omitempty"`
	Service      Service   `toml:"service,omitempty"`
	Groovy       Groovy    `toml:"groovy,omitempty"`
	Readers      Readers   `toml:"readers,omitempty"`
	Scraper      Scraper   `toml:"scraper,omitempty"`
	ConfigSchema int       `toml:"config_schema"`
	Audio        Audio     `toml:"audio,omitempty"`
	DebugLogging bool      `toml:"debug_logging"`
}

type Audio struct {
	ScanFeedback bool `toml:"scan_feedback,omitempty"`
}

type ZapScript struct {
	AllowExecute   []string `toml:"allow_execute,omitempty,multiline"`
	allowExecuteRe []*regexp.Regexp
}

type Service struct {
	DeviceID       string   `toml:"device_id"`
	AllowRun       []string `toml:"allow_run,omitempty,multiline"`
	allowRunRe     []*regexp.Regexp
	AllowedOrigins []string `toml:"allowed_origins,omitempty"`
	APIPort        int      `toml:"api_port"`
}

type Auth struct {
	Creds map[string]CredentialEntry `toml:"creds,omitempty"`
}

type Scraper struct {
	DefaultScraper      string `toml:"default"`
	Region              string `toml:"region"`
	Language            string `toml:"language"`
	DownloadCovers      bool   `toml:"download_covers"`
	DownloadScreenshots bool   `toml:"download_screenshots"`
	DownloadVideos      bool   `toml:"download_videos"`
}

type CredentialEntry struct {
	Username string `toml:"username"`
	Password string `toml:"password"`
	Bearer   string `toml:"bearer"`
}

var BaseDefaults = Values{
	ConfigSchema: SchemaVersion,
	Audio: Audio{
		ScanFeedback: true,
	},
	Readers: Readers{
		AutoDetect: true,
		Scan: ReadersScan{
			Mode: ScanModeTap,
		},
	},
	Service: Service{
		APIPort: 7497,
	},
	Groovy: Groovy{
		GmcProxyEnabled:        false,
		GmcProxyPort:           32106,
		GmcProxyBeaconInterval: "2s",
	},
	Scraper: Scraper{
		DefaultScraper:      "screenscraper",
		Region:              "us",
		Language:            "en",
		DownloadCovers:      true,
		DownloadScreenshots: true,
		DownloadVideos:      false,
	},
}

type Instance struct {
	appPath  string
	cfgPath  string
	authPath string
	vals     Values
	mu       sync.RWMutex
}

var authCfg atomic.Value

func GetAuthCfg() Auth {
	val := authCfg.Load()
	if val == nil {
		return Auth{}
	}
	auth, ok := val.(Auth)
	if !ok {
		return Auth{}
	}
	return auth
}

//nolint:gocritic // config struct copied for immutability
func NewConfig(configDir string, defaults Values) (*Instance, error) {
	cfgPath := os.Getenv(CfgEnv)
	log.Debug().Msgf("env config path: %s", cfgPath)

	if cfgPath == "" {
		cfgPath = filepath.Join(configDir, CfgFile)
	}

	cfg := Instance{
		mu:      sync.RWMutex{},
		appPath: os.Getenv(AppEnv),
		cfgPath: cfgPath,
		vals:    defaults,
	}

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		log.Info().Msg("saving new default config to disk")

		err := os.MkdirAll(filepath.Dir(cfgPath), 0o750)
		if err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}

		err = cfg.Save()
		if err != nil {
			return nil, err
		}
	}

	cfg.authPath = filepath.Join(filepath.Dir(cfgPath), AuthFile)

	err := cfg.Load()
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Instance) Load() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cfgPath == "" {
		return errors.New("config path not set")
	}

	if _, err := os.Stat(c.cfgPath); err != nil {
		return fmt.Errorf("failed to stat config file: %w", err)
	}

	data, err := os.ReadFile(c.cfgPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var newVals Values
	err = toml.Unmarshal(data, &newVals)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if newVals.ConfigSchema != SchemaVersion {
		log.Error().Msgf(
			"schema version mismatch: got %d, expecting %d",
			newVals.ConfigSchema,
			SchemaVersion,
		)
		return errors.New("schema version mismatch")
	}

	c.vals = newVals

	// load auth file
	if _, err := os.Stat(c.authPath); err == nil {
		log.Info().Msg("loading auth file")
		authData, err := os.ReadFile(c.authPath)
		if err != nil {
			return fmt.Errorf("failed to read auth file: %w", err)
		}

		var authVals Auth
		err = toml.Unmarshal(authData, &authVals)
		if err != nil {
			return fmt.Errorf("failed to unmarshal auth file: %w", err)
		}

		log.Info().Msgf("loaded %d auth entries", len(authVals.Creds))

		authCfg.Store(authVals)
	}

	// prepare allow files regexes
	c.vals.Launchers.allowFileRe = make([]*regexp.Regexp, len(c.vals.Launchers.AllowFile))
	for i, allowFile := range c.vals.Launchers.AllowFile {
		if runtime.GOOS == "windows" {
			// make regex case-insensitive, if not already
			if !strings.HasPrefix(allowFile, "(?i)") {
				allowFile = "(?i)" + allowFile
			}
			// replace forward slashes with backslashes
			allowFile = strings.ReplaceAll(allowFile, "/", "\\\\")
		}

		re, err := regexp.Compile(allowFile)
		if err != nil {
			log.Warn().Msgf("invalid allow file regex: %s", allowFile)
			continue
		}
		c.vals.Launchers.allowFileRe[i] = re
	}

	// prepare allow executes regexes
	c.vals.ZapScript.allowExecuteRe = make([]*regexp.Regexp, len(c.vals.ZapScript.AllowExecute))
	for i, allowExecute := range c.vals.ZapScript.AllowExecute {
		re, err := regexp.Compile(allowExecute)
		if err != nil {
			log.Warn().Msgf("invalid allow execute regex: %s", allowExecute)
			continue
		}
		c.vals.ZapScript.allowExecuteRe[i] = re
	}

	// prepare allow runs regexes
	c.vals.Service.allowRunRe = make([]*regexp.Regexp, len(c.vals.Service.AllowRun))
	for i, allowRun := range c.vals.Service.AllowRun {
		re, err := regexp.Compile(allowRun)
		if err != nil {
			log.Warn().Msgf("invalid allow run regex: %s", allowRun)
			continue
		}
		c.vals.Service.allowRunRe[i] = re
	}

	return nil
}

func (c *Instance) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cfgPath == "" {
		return errors.New("config path not set")
	}

	// set current schema version
	c.vals.ConfigSchema = SchemaVersion

	// generate a device id if one doesn't exist
	if c.vals.Service.DeviceID == "" {
		newID := uuid.New().String()
		c.vals.Service.DeviceID = newID
		log.Info().Msgf("generated new device id: %s", newID)
	}

	tmpMappings := c.vals.Mappings
	c.vals.Mappings = Mappings{}
	tmpCustomLauncher := c.vals.Launchers.Custom
	c.vals.Launchers.Custom = []LaunchersCustom{}

	data, err := toml.Marshal(&c.vals)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	c.vals.Mappings = tmpMappings
	c.vals.Launchers.Custom = tmpCustomLauncher

	if err := os.WriteFile(c.cfgPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

func (c *Instance) AudioFeedback() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Audio.ScanFeedback
}

func (c *Instance) SetAudioFeedback(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Audio.ScanFeedback = enabled
}

func (c *Instance) DebugLogging() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.DebugLogging
}

func (c *Instance) SetDebugLogging(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.DebugLogging = enabled
	if enabled {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

// isWindowsStylePath returns true if the path looks like a Windows path
// (starts with drive letter like "C:" or UNC path like "\\server")
func isWindowsStylePath(path string) bool {
	if path == "" {
		return false
	}

	// Check for UNC path (\\server\share)
	if strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//") {
		return true
	}

	// Check for drive letter (C:, D:, etc.)
	if len(path) >= 2 && path[1] == ':' {
		c := path[0]
		return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}

	return false
}

func checkAllow(allow []string, allowRe []*regexp.Regexp, s string) bool {
	if s == "" {
		return false
	}

	// Normalize path separators on Windows to match the regex patterns
	// Only normalize paths that look like Windows paths (drive letter or UNC path)
	normalizedPath := s
	if runtime.GOOS == "windows" && isWindowsStylePath(s) {
		normalizedPath = strings.ReplaceAll(s, "/", "\\")
	}

	for i := range allow {
		if allowRe[i] != nil &&
			allowRe[i].MatchString(normalizedPath) {
			return true
		}
	}

	return false
}

func (c *Instance) APIPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Service.APIPort
}

func (c *Instance) AllowedOrigins() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Service.AllowedOrigins
}

func (c *Instance) IsExecuteAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.ZapScript.AllowExecute, c.vals.ZapScript.allowExecuteRe, s)
}

func (c *Instance) IsRunAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.Service.AllowRun, c.vals.Service.allowRunRe, s)
}

func (c *Instance) Scraper() Scraper {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Scraper
}

func LookupAuth(authCfg Auth, reqURL string) *CredentialEntry {
	if len(authCfg.Creds) == 0 {
		return nil
	}

	u, err := url.Parse(reqURL)
	if err != nil {
		log.Warn().Msgf("invalid auth request url: %s", reqURL)
		return nil
	}

	for k, v := range authCfg.Creds {
		defURL, err := url.Parse(k)
		if err != nil {
			log.Error().Msgf("invalid auth config url: %s", k)
			continue
		}

		if !strings.EqualFold(defURL.Scheme, u.Scheme) {
			continue
		}

		if !strings.EqualFold(defURL.Host, u.Host) {
			continue
		}

		if !strings.HasPrefix(u.Path, defURL.Path) {
			continue
		}

		return &v
	}

	return nil
}

// SetAuthCfgForTesting sets the global auth config for testing purposes
func SetAuthCfgForTesting(auth Auth) {
	authCfg.Store(auth)
}

// ClearAuthCfgForTesting clears the global auth config for testing purposes
func ClearAuthCfgForTesting() {
	authCfg.Store(Auth{})
}
