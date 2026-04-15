// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/google/uuid"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	SchemaVersion       = 1
	CfgEnv              = "ZAPAROO_CFG"
	AppEnv              = "ZAPAROO_APP"
	ScanModeTap         = "tap"
	ScanModeHold        = "hold"
	UpdateChannelStable = "stable"
	UpdateChannelBeta   = "beta"
)

const configHeader = `# Zaparoo Core configuration file.
# https://zaparoo.org/docs/core/config/
#
# This file is managed by the Zaparoo service. Manual edits to values are
# preserved on next save, but comments will be lost.
`

type Values struct {
	Groovy         Groovy    `toml:"groovy,omitempty"`
	Input          Input     `toml:"input,omitempty"`
	AutoUpdate     *bool     `toml:"auto_update,omitempty"`
	UpdateChannel  *string   `toml:"update_channel,omitempty"`
	Audio          Audio     `toml:"audio"`
	Service        Service   `toml:"service,omitempty"`
	Launchers      Launchers `toml:"launchers,omitempty"`
	Playtime       Playtime  `toml:"playtime,omitempty"`
	Media          Media     `toml:"media,omitempty"`
	ZapScript      ZapScript `toml:"zapscript,omitempty"`
	Mappings       Mappings  `toml:"mappings,omitempty"`
	Systems        Systems   `toml:"systems,omitempty"`
	Readers        Readers   `toml:"readers,omitempty"`
	ConfigSchema   int       `toml:"config_schema"`
	DebugLogging   bool      `toml:"debug_logging"`
	ErrorReporting bool      `toml:"error_reporting"`
}

type Audio struct {
	SuccessSound *string `toml:"success_sound,omitempty"`
	FailSound    *string `toml:"fail_sound,omitempty"`
	LimitSound   *string `toml:"limit_sound,omitempty"`
	PendingSound *string `toml:"pending_sound,omitempty"`
	ReadySound   *string `toml:"ready_sound,omitempty"`
	Volume       *int    `toml:"volume,omitempty"`
	ScanFeedback bool    `toml:"scan_feedback"`
}

type ZapScript struct {
	AllowExecute    []string `toml:"allow_execute,omitempty,multiline"`
	allowExecuteRe  []*regexp.Regexp
	AllowHTTP       []string `toml:"allow_http,omitempty,multiline"`
	allowHTTPRe     []*regexp.Regexp
	BlockCommands   []string `toml:"block_commands,omitempty,multiline"`
	blockCommandSet map[string]struct{}
	Input           InputConfig `toml:"input,omitempty"`
}

const (
	InputModeCombos       = "combos"
	InputModeUnrestricted = "unrestricted"
)

type InputConfig struct {
	Mode  *string  `toml:"mode,omitempty"`
	Allow []string `toml:"allow,omitempty,multiline"`
	Block []string `toml:"block,omitempty,multiline"`
}

type Input struct {
	GamepadEnabled *bool `toml:"gamepad_enabled,omitempty"`
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
}

type Instance struct {
	fs       afero.Fs
	appPath  string
	cfgPath  string
	authPath string
	vals     Values
	defaults Values
	mu       syncutil.RWMutex
}

// getFs returns the instance's filesystem, defaulting to the OS filesystem
// if none was set. This allows Instance structs created without NewConfigWithFs
// to still function correctly.
func (c *Instance) getFs() afero.Fs {
	if c.fs != nil {
		return c.fs
	}
	return afero.NewOsFs()
}

var (
	authCfg atomic.Value
	apiKeys atomic.Value
)

func GetAuthCfg() map[string]CredentialEntry {
	val := authCfg.Load()
	if val == nil {
		return nil
	}
	creds, ok := val.(map[string]CredentialEntry)
	if !ok {
		return nil
	}
	return creds
}

func GetAPIKeys() []string {
	val := apiKeys.Load()
	if val == nil {
		return nil
	}
	keys, ok := val.([]string)
	if !ok {
		return nil
	}
	return keys
}

//nolint:gocritic // config struct copied for immutability
func NewConfig(configDir string, defaults Values) (*Instance, error) {
	return NewConfigWithFs(configDir, defaults, afero.NewOsFs())
}

// NewConfigWithFs creates a new config instance using the provided filesystem.
// This allows tests to use an in-memory filesystem instead of the real OS filesystem.
//
//nolint:gocritic // config struct copied for immutability
func NewConfigWithFs(configDir string, defaults Values, fs afero.Fs) (*Instance, error) {
	cfgPath := os.Getenv(CfgEnv)
	if cfgPath != "" {
		log.Debug().Str("path", cfgPath).Msg("using config path from environment")
	} else {
		cfgPath = filepath.Join(configDir, CfgFile)
	}

	cfg := Instance{
		fs:       fs,
		mu:       syncutil.RWMutex{},
		appPath:  os.Getenv(AppEnv),
		cfgPath:  cfgPath,
		vals:     defaults,
		defaults: defaults,
	}

	if _, err := fs.Stat(cfgPath); os.IsNotExist(err) {
		log.Info().Msg("saving new default config to disk")

		err := fs.MkdirAll(filepath.Dir(cfgPath), 0o750)
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

	fs := c.getFs()

	if _, err := fs.Stat(c.cfgPath); err != nil {
		return fmt.Errorf("failed to stat config file: %w", err)
	}

	data, err := afero.ReadFile(fs, c.cfgPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Start with defaults, then unmarshal file values on top.
	// This ensures fields not present in the file retain their default values.
	// Save old vals so we can restore on error (Load is called at runtime for
	// config reloads — a bad file must not destroy the running config).
	oldVals := c.vals
	c.vals = c.defaults

	if err := c.applyTOML(string(data)); err != nil {
		c.vals = oldVals
		return err
	}

	if c.vals.ConfigSchema != SchemaVersion {
		log.Error().Msgf(
			"schema version mismatch: got %d, expecting %d",
			c.vals.ConfigSchema,
			SchemaVersion,
		)
		c.vals = oldVals
		return errors.New("schema version mismatch")
	}

	// load auth file
	c.reloadAuth()

	return nil
}

// LoadTOML unmarshals a TOML string onto the current config values and
// rebuilds derived fields (compiled regexes, lookup maps). Fields not
// present in the TOML are left unchanged.
func (c *Instance) LoadTOML(data string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	oldVals := c.vals
	if err := c.applyTOML(data); err != nil {
		c.vals = oldVals
		return err
	}
	return nil
}

// applyTOML is the shared unmarshal + post-processing core.
// Caller must hold c.mu.
func (c *Instance) applyTOML(data string) error {
	if err := toml.Unmarshal([]byte(data), &c.vals); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
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

		re, err := regexp.Compile(anchorPattern(allowFile))
		if err != nil {
			log.Warn().Msgf("invalid allow file regex: %s", allowFile)
			continue
		}
		c.vals.Launchers.allowFileRe[i] = re
	}

	// prepare allow executes regexes
	c.vals.ZapScript.allowExecuteRe = make([]*regexp.Regexp, len(c.vals.ZapScript.AllowExecute))
	for i, allowExecute := range c.vals.ZapScript.AllowExecute {
		re, err := regexp.Compile(anchorPattern(allowExecute))
		if err != nil {
			log.Warn().Msgf("invalid allow execute regex: %s", allowExecute)
			continue
		}
		c.vals.ZapScript.allowExecuteRe[i] = re
	}

	// prepare allow HTTP regexes
	c.vals.ZapScript.allowHTTPRe = make([]*regexp.Regexp, len(c.vals.ZapScript.AllowHTTP))
	for i, allowHTTP := range c.vals.ZapScript.AllowHTTP {
		re, err := regexp.Compile(anchorPattern(allowHTTP))
		if err != nil {
			log.Warn().Msgf("invalid allow HTTP regex: %s", allowHTTP)
			continue
		}
		c.vals.ZapScript.allowHTTPRe[i] = re
	}

	// prepare allow runs regexes
	c.vals.Service.allowRunRe = make([]*regexp.Regexp, len(c.vals.Service.AllowRun))
	for i, allowRun := range c.vals.Service.AllowRun {
		re, err := regexp.Compile(anchorPattern(allowRun))
		if err != nil {
			log.Warn().Msgf("invalid allow run regex: %s", allowRun)
			continue
		}
		c.vals.Service.allowRunRe[i] = re
	}

	// prepare block commands set
	c.vals.ZapScript.blockCommandSet = make(map[string]struct{}, len(c.vals.ZapScript.BlockCommands))
	for _, cmd := range c.vals.ZapScript.BlockCommands {
		c.vals.ZapScript.blockCommandSet[cmd] = struct{}{}
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

	output := append([]byte(configHeader), data...)
	if err := afero.WriteFile(c.getFs(), c.cfgPath, output, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// reloadAuth reads auth.toml from disk and updates the in-memory auth config.
// Must be called with c.mu held (read or write lock).
func (c *Instance) reloadAuth() {
	fs := c.getFs()
	if _, err := fs.Stat(c.authPath); err != nil {
		return
	}

	log.Info().Msg("loading auth file")
	authData, err := afero.ReadFile(fs, c.authPath)
	if err != nil {
		log.Error().Err(err).Msg("failed to read auth file")
		return
	}

	authEntries := LoadAuthFromData(authData)
	log.Info().Msgf("loaded %d auth entries", len(authEntries))
	authCfg.Store(authEntries)

	keys := LoadAPIKeysFromData(authData)
	log.Info().Msgf("loaded %d API keys", len(keys))
	apiKeys.Store(keys)
}

// SaveAuthEntry writes or updates a credential entry in auth.toml for the
// given domain. Creates the file with 0600 permissions if it doesn't exist.
// Preserves existing entries and reloads the in-memory auth config.
func (c *Instance) SaveAuthEntry(domain string, entry CredentialEntry) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	fs := c.getFs()

	// Read existing auth file once, parse both credentials and API keys
	existing := make(map[string]CredentialEntry)
	var existingKeys []string
	if data, err := afero.ReadFile(fs, c.authPath); err == nil {
		existing = LoadAuthFromData(data)
		existingKeys = LoadAPIKeysFromData(data)
	}

	// Upsert the entry
	existing[domain] = entry

	// Build the output: API keys first, then credential entries
	data, err := marshalAuthFile(existing, existingKeys)
	if err != nil {
		return err
	}

	if err := afero.WriteFile(fs, c.authPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write auth file: %w", err)
	}

	c.reloadAuth()
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

// AudioVolume returns the configured volume level (0-200). Defaults to 100 if unset.
// Values above 100 amplify the audio. Clamped to [0, 200].
func (c *Instance) AudioVolume() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Audio.Volume == nil {
		return 100
	}
	return max(0, min(200, *c.vals.Audio.Volume))
}

// SetAudioVolume sets the audio volume level (0-200, default 100).
func (c *Instance) SetAudioVolume(v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Audio.Volume = &v
}

// SuccessSoundPath resolves the success sound file path based on config and disk overrides.
// Returns (path, enabled) where:
//   - ("", false) = ScanFeedback disabled or sound explicitly disabled (empty string config)
//   - ("", true) = use embedded default (nil config, no file override on disk)
//   - (path, true) = use file at path (explicit config or auto-detected in dataDir/assets/)
func (c *Instance) SuccessSoundPath(dataDir string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.vals.Audio.ScanFeedback {
		return "", false
	}

	// nil = check for file override on disk, then use embedded default
	if c.vals.Audio.SuccessSound == nil {
		overridePath := filepath.Join(dataDir, AssetsDir, SuccessSoundFilename)
		if _, err := c.getFs().Stat(overridePath); err == nil {
			return overridePath, true
		}
		return "", true
	}

	// empty string = disabled
	if *c.vals.Audio.SuccessSound == "" {
		return "", false
	}

	path := *c.vals.Audio.SuccessSound

	// absolute path = use as-is
	if filepath.IsAbs(path) {
		return path, true
	}

	// relative path = resolve to dataDir/assets/path
	return filepath.Join(dataDir, AssetsDir, path), true
}

// FailSoundPath resolves the fail sound file path. See SuccessSoundPath for return semantics.
func (c *Instance) FailSoundPath(dataDir string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.vals.Audio.ScanFeedback {
		return "", false
	}

	// nil = check for file override on disk, then use embedded default
	if c.vals.Audio.FailSound == nil {
		overridePath := filepath.Join(dataDir, AssetsDir, FailSoundFilename)
		if _, err := c.getFs().Stat(overridePath); err == nil {
			return overridePath, true
		}
		return "", true
	}

	// empty string = disabled
	if *c.vals.Audio.FailSound == "" {
		return "", false
	}

	path := *c.vals.Audio.FailSound

	// absolute path = use as-is
	if filepath.IsAbs(path) {
		return path, true
	}

	// relative path = resolve to dataDir/assets/path
	return filepath.Join(dataDir, AssetsDir, path), true
}

// LimitSoundPath resolves the limit sound file path. See SuccessSoundPath for return semantics.
func (c *Instance) LimitSoundPath(dataDir string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.vals.Audio.ScanFeedback {
		return "", false
	}

	// nil = check for file override on disk, then use embedded default
	if c.vals.Audio.LimitSound == nil {
		overridePath := filepath.Join(dataDir, AssetsDir, LimitSoundFilename)
		if _, err := c.getFs().Stat(overridePath); err == nil {
			return overridePath, true
		}
		return "", true
	}

	// empty string = disabled
	if *c.vals.Audio.LimitSound == "" {
		return "", false
	}

	path := *c.vals.Audio.LimitSound

	// absolute path = use as-is
	if filepath.IsAbs(path) {
		return path, true
	}

	// relative path = resolve to dataDir/assets/path
	return filepath.Join(dataDir, AssetsDir, path), true
}

// PendingSoundPath resolves the launch guard sound file path. See SuccessSoundPath for return semantics.
func (c *Instance) PendingSoundPath(dataDir string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.vals.Audio.ScanFeedback {
		return "", false
	}

	// nil = check for file override on disk, then use embedded default
	if c.vals.Audio.PendingSound == nil {
		overridePath := filepath.Join(dataDir, AssetsDir, PendingSoundFilename)
		if _, err := c.getFs().Stat(overridePath); err == nil {
			return overridePath, true
		}
		return "", true
	}

	// empty string = disabled
	if *c.vals.Audio.PendingSound == "" {
		return "", false
	}

	path := *c.vals.Audio.PendingSound

	// absolute path = use as-is
	if filepath.IsAbs(path) {
		return path, true
	}

	// relative path = resolve to dataDir/assets/path
	return filepath.Join(dataDir, AssetsDir, path), true
}

// ReadySoundPath resolves the launch guard ready sound file path. See SuccessSoundPath for return semantics.
func (c *Instance) ReadySoundPath(dataDir string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.vals.Audio.ScanFeedback {
		return "", false
	}

	// nil = check for file override on disk, then use embedded default
	if c.vals.Audio.ReadySound == nil {
		overridePath := filepath.Join(dataDir, AssetsDir, ReadySoundFilename)
		if _, err := c.getFs().Stat(overridePath); err == nil {
			return overridePath, true
		}
		return "", true
	}

	// empty string = disabled
	if *c.vals.Audio.ReadySound == "" {
		return "", false
	}

	path := *c.vals.Audio.ReadySound

	// absolute path = use as-is
	if filepath.IsAbs(path) {
		return path, true
	}

	// relative path = resolve to dataDir/assets/path
	return filepath.Join(dataDir, AssetsDir, path), true
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
	switch {
	case os.Getenv("ZAPAROO_TRACE") != "":
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	case enabled:
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	default:
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

// anchorPattern wraps a regex pattern with ^(?:...)$ so it matches the
// entire string rather than a substring. Patterns that need substring
// matching can use .*pattern.* explicitly.
func anchorPattern(pattern string) string {
	return "^(?:" + pattern + ")$"
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

func (c *Instance) IsExecuteAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.ZapScript.AllowExecute, c.vals.ZapScript.allowExecuteRe, s)
}

// IsCommandBlocked returns true if the command name is in the block list.
// An empty block list means no commands are blocked.
func (c *Instance) IsCommandBlocked(name string) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, denied := c.vals.ZapScript.blockCommandSet[name]
	return denied
}

// IsHTTPAllowed returns true if the URL matches the HTTP allow list.
// When the allow list is empty (not configured), all URLs are allowed.
func (c *Instance) IsHTTPAllowed(url string) bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.vals.ZapScript.AllowHTTP) == 0 {
		return true
	}
	return checkAllow(c.vals.ZapScript.AllowHTTP, c.vals.ZapScript.allowHTTPRe, url)
}

// InputMode returns the configured input restriction mode.
// defaultMode is the platform-provided default (e.g., "hotkeys" for desktop,
// "unrestricted" for embedded).
func (c *Instance) InputMode(defaultMode string) string {
	if c == nil {
		return defaultMode
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.ZapScript.Input.Mode == nil {
		return defaultMode
	}
	return *c.vals.ZapScript.Input.Mode
}

// InputAllowList returns the input allow list for "allow" mode.
func (c *Instance) InputAllowList() []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.ZapScript.Input.Allow
}

// InputBlockList returns the input block list for "unrestricted" mode.
func (c *Instance) InputBlockList() []string {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.ZapScript.Input.Block
}

// SetAuthCfgForTesting sets the global auth config for testing purposes
func SetAuthCfgForTesting(creds map[string]CredentialEntry) {
	authCfg.Store(creds)
}

// ClearAuthCfgForTesting clears the global auth config for testing purposes
func ClearAuthCfgForTesting() {
	authCfg.Store(map[string]CredentialEntry{})
}

// VirtualGamepadEnabled returns whether virtual gamepad emulation is enabled.
// The defaultEnabled parameter allows platforms to specify their own default.
func (c *Instance) VirtualGamepadEnabled(defaultEnabled bool) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Input.GamepadEnabled == nil {
		return defaultEnabled
	}
	return *c.vals.Input.GamepadEnabled
}

// SetVirtualGamepadEnabled sets whether virtual gamepad emulation is enabled.
func (c *Instance) SetVirtualGamepadEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Input.GamepadEnabled = &enabled
}

// ErrorReporting returns whether error reporting is enabled.
// Defaults to false (opt-in).
func (c *Instance) ErrorReporting() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.ErrorReporting
}

// SetErrorReporting sets whether error reporting is enabled.
func (c *Instance) SetErrorReporting(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.ErrorReporting = enabled
}

// AutoUpdate returns whether automatic update checking is enabled.
// The defaultEnabled parameter allows platforms to specify their own default
// (e.g. package-managed installs default to false).
// An explicit user setting always takes precedence.
func (c *Instance) AutoUpdate(defaultEnabled bool) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.AutoUpdate == nil {
		return defaultEnabled
	}
	return *c.vals.AutoUpdate
}

// SetAutoUpdate sets whether automatic update checking is enabled.
func (c *Instance) SetAutoUpdate(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.AutoUpdate = &enabled
}

// UpdateChannel returns the configured update channel.
// Defaults to "stable" when not explicitly set.
func (c *Instance) UpdateChannel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.UpdateChannel == nil {
		return UpdateChannelStable
	}
	return *c.vals.UpdateChannel
}

// SetUpdateChannel sets the update channel. Valid values are "stable" and "beta".
func (c *Instance) SetUpdateChannel(channel string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.UpdateChannel = &channel
}
