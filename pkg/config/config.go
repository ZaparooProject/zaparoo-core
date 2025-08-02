package config

import (
	"errors"
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

type ZapScript struct {
	AllowExecute   []string `toml:"allow_execute,omitempty,multiline"`
	allowExecuteRe []*regexp.Regexp
}

type Service struct {
	APIPort    int      `toml:"api_port"`
	DeviceID   string   `toml:"device_id"`
	AllowRun   []string `toml:"allow_run,omitempty,multiline"`
	allowRunRe []*regexp.Regexp
}

type Auth struct {
	Creds map[string]CredentialEntry `toml:"creds,omitempty"`
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
}

type Instance struct {
	mu       sync.RWMutex
	appPath  string
	cfgPath  string
	authPath string
	vals     Values
}

var authCfg atomic.Value

func GetAuthCfg() Auth {
	val := authCfg.Load()
	if val == nil {
		return Auth{}
	}
	return val.(Auth)
}

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

		err := os.MkdirAll(filepath.Dir(cfgPath), 0o755)
		if err != nil {
			return nil, err
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

	// load auth file
	if _, err := os.Stat(c.authPath); err == nil {
		log.Info().Msg("loading auth file")
		authData, err := os.ReadFile(c.authPath)
		if err != nil {
			return err
		}

		var authVals Auth
		err = toml.Unmarshal(authData, &authVals)
		if err != nil {
			return err
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
		newId := uuid.New().String()
		c.vals.Service.DeviceID = newId
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

func checkAllow(allow []string, allowRe []*regexp.Regexp, s string) bool {
	if s == "" {
		return false
	}

	for i := range allow {
		if allowRe[i] != nil &&
			allowRe[i].MatchString(s) {
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
