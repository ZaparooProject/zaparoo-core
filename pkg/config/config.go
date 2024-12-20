package config

import (
	"bytes"
	"errors"
	"github.com/BurntSushi/toml"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	CfgEnv       = "ZAPAROO_CFG"
	AppEnv       = "ZAPAROO_APP"
	ScanModeTap  = "tap"
	ScanModeCart = "cart"
)

type Values struct {
	AudioFeedback bool      `toml:"audio_feedback,omitempty"`
	DebugLogging  bool      `toml:"debug_logging"`
	Readers       Readers   `toml:"readers,omitempty"`
	Systems       Systems   `toml:"systems,omitempty"`
	Launchers     Launchers `toml:"launchers,omitempty"`
	ZapScript     ZapScript `toml:"zapscript,omitempty"`
	Api           Api       `toml:"api,omitempty"`
}

type Readers struct {
	AutoDetect bool             `toml:"auto_detect"`
	Scan       ReadersScan      `toml:"scan,omitempty"`
	Connect    []ReadersConnect `toml:"connect,omitempty"`
}

type ReadersScan struct {
	Mode         string   `toml:"mode"`
	ExitDelay    float32  `toml:"exit_delay,omitzero"`
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
	System   string `toml:"system"`
	Launcher string `toml:"launcher,omitempty"`
}

type Launchers struct {
	IndexRoot []string `toml:"index_root,omitempty"`
	AllowFile []string `toml:"allow_file,omitempty"`
}

type ZapScript struct {
	AllowShell []string `toml:"allow_shell,omitempty"`
}

type Api struct {
	Port        int      `toml:"port"`
	AllowLaunch []string `toml:"allow_launch,omitempty"`
}

var BaseDefaults = Values{
	AudioFeedback: true,
	Readers: Readers{
		AutoDetect: true,
		Scan: ReadersScan{
			Mode: ScanModeTap,
		},
	},
	Api: Api{
		Port: 7497,
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

func (c *Instance) LogValues() {
	c.mu.RLock()
	defer c.mu.RUnlock()
	log.Info().Any("config", c.vals).Msg("config values")
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
	_, err = toml.Decode(string(data), &newVals)
	if err != nil {
		return err
	}
	log.Debug().Any("config", newVals).Msg("loaded new config values")

	c.vals = newVals

	log.Debug().Any("config", newVals).Msg("second new config values")

	return nil
}

func (c *Instance) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cfgPath == "" {
		return errors.New("config path not set")
	}

	buf := new(bytes.Buffer)
	enc := toml.NewEncoder(buf)
	enc.Indent = ""
	err := enc.Encode(c.vals)
	if err != nil {
		return err
	}

	return os.WriteFile(c.cfgPath, buf.Bytes(), 0644)
}

func (c *Instance) AudioFeedback() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.AudioFeedback
}

func (c *Instance) SetAudioFeedback(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vals.AudioFeedback = enabled
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

func (c *Instance) CartModeEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Readers.Scan.Mode == ScanModeCart
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

func (c *Instance) SetAutoConnect(enabled bool) {
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

func (c *Instance) IsLauncherFileAllowed(path string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, allowed := range c.vals.Launchers.AllowFile {
		if allowed == "*" {
			return true
		}

		// TODO: case insensitive on mister? platform option?
		if runtime.GOOS == "windows" {
			// do a case-insensitive comparison on windows
			allowed = strings.ToLower(allowed)
			path = strings.ToLower(path)
		}

		// convert all slashes to OS preferred
		if filepath.FromSlash(allowed) == filepath.FromSlash(path) {
			return true
		}
	}
	return false
}

func (c *Instance) ApiPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.vals.Api.Port
}

func (c *Instance) IsShellCmdAllowed(cmd string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, allowed := range c.vals.ZapScript.AllowShell {
		if allowed == "*" {
			return true
		}

		if allowed == cmd {
			return true
		}
	}
	return false
}
