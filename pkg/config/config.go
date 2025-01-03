package config

import (
	"errors"
	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
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
}

type ReadersConnect struct {
	Driver string `toml:"driver"`
	Path   string `toml:"path,omitempty"`
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

	data, err := toml.Marshal(&c.vals)
	if err != nil {
		return err
	}

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

	mapFiles, err := os.ReadDir(mappingsDir)
	if err != nil {
		return err
	}

	filesCounts := 0
	mappingsCount := 0

	for _, mapFile := range mapFiles {
		if mapFile.IsDir() {
			continue
		}

		if filepath.Ext(mapFile.Name()) != ".toml" {
			continue
		}

		mapPath := filepath.Join(mappingsDir, mapFile.Name())
		log.Debug().Msgf("loading mapping file: %s", mapPath)

		data, err := os.ReadFile(mapPath)
		if err != nil {
			return err
		}

		var newVals Values
		err = toml.Unmarshal(data, &newVals)
		if err != nil {
			return err
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
