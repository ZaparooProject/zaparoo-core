package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
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
	ConfigSchema int       `toml:"config_schema"`
	DebugLogging bool      `toml:"debug_logging"`
	Audio        Audio     `toml:"audio,omitempty"`
	Readers      Readers   `toml:"readers,omitempty"`
	Systems      Systems   `toml:"systems,omitempty"`
	Launchers    Launchers `toml:"launchers,omitempty"`
	ZapScript    ZapScript `toml:"zapscript,omitempty"`
	Service      Service   `toml:"service,omitempty"`
	Mappings     Mappings  `toml:"mappings,omitempty"`
	Groovy       Groovy    `toml:"groovy,omitempty"`
}

type Audio struct {
	ScanFeedback bool `toml:"scan_feedback,omitempty"`
}

type Readers struct {
	AutoDetect bool             `toml:"auto_detect"`
	Scan       ReadersScan      `toml:"scan,omitempty"`
	Connect    []ReadersConnect `toml:"connect,omitempty"`
}

type ReadersScan struct {
	Mode         string   `toml:"mode"`
	ExitDelay    float32  `toml:"exit_delay,omitempty"`
	IgnoreSystem []string `toml:"ignore_system,omitempty"`
	OnScan       string   `toml:"on_scan,omitempty"`
	OnRemove     string   `toml:"on_remove,omitempty"`
}

type ReadersConnect struct {
	Driver   string `toml:"driver"`
	Path     string `toml:"path,omitempty"`
	IDSource string `toml:"id_source,omitempty"`
}

func (r ReadersConnect) ConnectionString() string {
	return fmt.Sprintf("%s:%s", r.Driver, r.Path)
}

type Systems struct {
	Default []SystemsDefault `toml:"default,omitempty"`
}

type SystemsDefault struct {
	System     string `toml:"system"`
	Launcher   string `toml:"launcher,omitempty"`
	BeforeExit string `toml:"before_exit,omitempty"`
}

type Launchers struct {
	IndexRoot   []string `toml:"index_root,omitempty,multiline"`
	AllowFile   []string `toml:"allow_file,omitempty,multiline"`
	allowFileRe []*regexp.Regexp
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
	MediaDirs []string `toml:"media_dirs"`
	FileExts  []string `toml:"file_exts"`
	Execute   string   `toml:"execute"`
}

type ZapScript struct {
	AllowExecute   []string `toml:"allow_execute,omitempty,multiline"`
	allowExecuteRe []*regexp.Regexp
}

type Service struct {
	ApiPort    int      `toml:"api_port"`
	DeviceId   string   `toml:"device_id"`
	AllowRun   []string `toml:"allow_run,omitempty,multiline"`
	allowRunRe []*regexp.Regexp
}

type MappingsEntry struct {
	TokenKey     string `toml:"token_key,omitempty"`
	MatchPattern string `toml:"match_pattern"`
	ZapScript    string `toml:"zapscript"`
}

type Mappings struct {
	Entry []MappingsEntry `toml:"entry,omitempty"`
}

type Groovy struct {
	GmcProxyEnabled        bool   `toml:"gmc_proxy_enabled"`
	GmcProxyPort           int    `toml:"gmc_proxy_port"`
	GmcProxyBeaconInterval string `toml:"gmc_proxy_beacon_interval"`
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
		ApiPort: 7497,
	},
	Groovy: Groovy{
		GmcProxyEnabled:        false,
		GmcProxyPort:           32106,
		GmcProxyBeaconInterval: "2s",
	},
}

type Instance struct {
	mu      sync.RWMutex
	appPath string
	cfgPath string
	vals    Values
}

func NewConfig(configDir string, defaults Values) (*Instance, error) {
	cfgPath := os.Getenv(CfgEnv)
	log.Info().Msgf("env config path: %s", cfgPath)

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

		err := os.MkdirAll(filepath.Dir(cfgPath), 0755)
		if err != nil {
			return nil, err
		}

		err = cfg.Save()
		if err != nil {
			return nil, err
		}
	}

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
		return err
	}

	data, err := os.ReadFile(c.cfgPath)
	if err != nil {
		return err
	}

	var newVals Values
	err = toml.Unmarshal(data, &newVals)
	if err != nil {
		return err
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

	log.Info().Any("config", c.vals).Msg("loaded config")

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
	if c.vals.Service.DeviceId == "" {
		newId := uuid.New().String()
		c.vals.Service.DeviceId = newId
		log.Info().Msgf("generated new device id: %s", newId)
	}

	tmpMappings := c.vals.Mappings
	c.vals.Mappings = Mappings{}
	tmpCustomLauncher := c.vals.Launchers.Custom
	c.vals.Launchers.Custom = []LaunchersCustom{}

	data, err := toml.Marshal(&c.vals)
	if err != nil {
		return err
	}

	c.vals.Mappings = tmpMappings
	c.vals.Launchers.Custom = tmpCustomLauncher

	return os.WriteFile(c.cfgPath, data, 0644)
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

func (c *Instance) ReadersScan() ReadersScan {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan
}

func (c *Instance) IsHoldModeIgnoredSystem(systemID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var blocklist []string
	for _, v := range c.vals.Readers.Scan.IgnoreSystem {
		blocklist = append(blocklist, strings.ToLower(v))
	}
	return slices.Contains(blocklist, strings.ToLower(systemID))
}

func (c *Instance) TapModeEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.vals.Readers.Scan.Mode == ScanModeTap {
		return true
	} else if c.vals.Readers.Scan.Mode == "" {
		return true
	} else {
		return false
	}
}

func (c *Instance) HoldModeEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan.Mode == ScanModeHold
}

func (c *Instance) SetScanMode(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.Mode = mode
}

func (c *Instance) SetScanExitDelay(exitDelay float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.ExitDelay = exitDelay
}

func (c *Instance) SetScanIgnoreSystem(ignoreSystem []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Scan.IgnoreSystem = ignoreSystem
}

func (c *Instance) Readers() Readers {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers
}

func (c *Instance) AutoDetect() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.AutoDetect
}

func (c *Instance) SetAutoDetect(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.AutoDetect = enabled
}

func (c *Instance) SetReaderConnections(rcs []ReadersConnect) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.Readers.Connect = rcs
}

func (c *Instance) SystemDefaults() []SystemsDefault {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Systems.Default
}

func (c *Instance) IndexRoots() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Launchers.IndexRoot
}

func checkAllow(allow []string, allowRe []*regexp.Regexp, s string) bool {
	if s == "" {
		return false
	}

	for i, _ := range allow {
		if allowRe[i] != nil &&
			allowRe[i].MatchString(s) {
			return true
		}
	}

	return false
}

func (c *Instance) IsLauncherFileAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.Launchers.AllowFile, c.vals.Launchers.allowFileRe, s)
}

func (c *Instance) ApiPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Service.ApiPort
}

func (c *Instance) IsExecuteAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.ZapScript.AllowExecute, c.vals.ZapScript.allowExecuteRe, s)
}

func (c *Instance) LoadMappings(mappingsDir string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := os.Stat(mappingsDir)
	if err != nil {
		return err
	}

	var mapFiles []string

	err = filepath.WalkDir(
		mappingsDir,
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

			mapFiles = append(mapFiles, path)

			return nil
		},
	)
	if err != nil {
		return err
	}
	log.Info().Msgf("found %d mapping files", len(mapFiles))

	filesCounts := 0
	mappingsCount := 0

	for _, mapPath := range mapFiles {
		log.Debug().Msgf("loading mapping file: %s", mapPath)

		data, err := os.ReadFile(mapPath)
		if err != nil {
			log.Error().Msgf("error reading mapping file: %s", mapPath)
			continue
		}

		var newVals Values
		err = toml.Unmarshal(data, &newVals)
		if err != nil {
			log.Error().Msgf("error parsing mapping file: %s", mapPath)
			continue
		}

		c.vals.Mappings.Entry = append(c.vals.Mappings.Entry, newVals.Mappings.Entry...)

		filesCounts++
		mappingsCount += len(newVals.Mappings.Entry)
	}

	log.Info().Msgf("loaded %d mapping files, %d mappings", filesCounts, mappingsCount)

	return nil
}

func (c *Instance) Mappings() []MappingsEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Mappings.Entry
}

func (c *Instance) IsRunAllowed(s string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return checkAllow(c.vals.Service.AllowRun, c.vals.Service.allowRunRe, s)
}

func (c *Instance) LookupSystemDefaults(systemId string) (SystemsDefault, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, defaultSystem := range c.vals.Systems.Default {
		if strings.EqualFold(defaultSystem.System, systemId) {
			return defaultSystem, true
		}
	}
	return SystemsDefault{}, false
}

func (c *Instance) LookupLauncherDefaults(launcherId string) (LaunchersDefault, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, defaultLauncher := range c.vals.Launchers.Default {
		if strings.EqualFold(defaultLauncher.Launcher, launcherId) {
			return defaultLauncher, true
		}
	}
	return LaunchersDefault{}, false
}

func (c *Instance) GmcProxyEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Groovy.GmcProxyEnabled
}

func (c *Instance) GmcProxyPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Groovy.GmcProxyPort
}

func (c *Instance) GmcProxyBeaconInterval() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Groovy.GmcProxyBeaconInterval
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
