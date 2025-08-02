/*
Zaparoo Core
Copyright (C) 2023, 2024 Callan Barrett
Copyright (C) 2023 Gareth Jones

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package iniconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	ini "gopkg.in/ini.v1"
)

const (
	UserConfigEnv  = "TAPTO_CONFIG"
	UserAppPathEnv = "TAPTO_APP_PATH"
)

type TapToConfig struct {
	ConnectionString  string   `ini:"connection_string,omitempty"`
	Reader            []string `ini:"reader,omitempty,allowshadow"`
	ExitGameBlocklist []string `ini:"exit_game_blocklist"`
	ExitGameDelay     int      `ini:"exit_game_delay"`
	AllowCommands     bool     `ini:"allow_commands"`
	DisableSounds     bool     `ini:"disable_sounds"`
	ProbeDevice       bool     `ini:"probe_device"`
	ExitGame          bool     `ini:"exit_game"`
	ConsoleLogging    bool     `ini:"console_logging"`
	Debug             bool     `ini:"debug"`
}

type SystemsConfig struct {
	GamesFolder []string `ini:"games_folder,omitempty,allowshadow"` // TODO: rename root_folder?
	SetCore     []string `ini:"set_core,omitempty,allowshadow"`     // TODO: deprecated? change to set_launcher
}

type LaunchersConfig struct {
	AllowFile []string `ini:"allow_file,omitempty,allowshadow"`
	// TODO: allow_shell - contents of shell command
}

type APIConfig struct {
	Port        string   `ini:"port"`
	AllowLaunch []string `ini:"allow_launch,omitempty,allowshadow"`
}

type UserConfig struct {
	TapTo     TapToConfig     `ini:"tapto"`
	AppPath   string          `ini:"-"`
	IniPath   string          `ini:"-"`
	Systems   SystemsConfig   `ini:"systems"`
	API       APIConfig       `ini:"api"`
	Launchers LaunchersConfig `ini:"launchers"`
	mu        sync.RWMutex
}

func (c *UserConfig) GetConnectionString() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TapTo.ConnectionString
}

func (c *UserConfig) SetConnectionString(connectionString string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TapTo.ConnectionString = connectionString
}

func (c *UserConfig) GetReader() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TapTo.Reader
}

func (c *UserConfig) SetReader(reader []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TapTo.Reader = reader
}

func (c *UserConfig) GetAllowCommands() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TapTo.AllowCommands
}

func (c *UserConfig) SetAllowCommands(allowCommands bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TapTo.AllowCommands = allowCommands
}

func (c *UserConfig) GetDisableSounds() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TapTo.DisableSounds
}

func (c *UserConfig) SetDisableSounds(disableSounds bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TapTo.DisableSounds = disableSounds
}

func (c *UserConfig) GetProbeDevice() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TapTo.ProbeDevice
}

func (c *UserConfig) SetProbeDevice(probeDevice bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TapTo.ProbeDevice = probeDevice
}

func (c *UserConfig) GetExitGame() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TapTo.ExitGame
}

func (c *UserConfig) SetExitGameDelay(exitGameDelay int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TapTo.ExitGameDelay = exitGameDelay
}

func (c *UserConfig) SetExitGame(exitGame bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TapTo.ExitGame = exitGame
}

func (c *UserConfig) GetExitGameBlocklist() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TapTo.ExitGameBlocklist
}

func (c *UserConfig) GetExitGameDelay() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TapTo.ExitGameDelay
}

func (c *UserConfig) SetExitGameBlocklist(exitGameBlocklist []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TapTo.ExitGameBlocklist = exitGameBlocklist
}

func (c *UserConfig) GetDebug() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TapTo.Debug
}

func (c *UserConfig) SetDebug(debug bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.TapTo.Debug = debug
	if debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}

func (c *UserConfig) IsFileAllowed(path string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, allowed := range c.Launchers.AllowFile {
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

func (c *UserConfig) LoadConfig() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cfg, err := ini.ShadowLoad(c.IniPath)
	if err != nil {
		return err
	}

	err = cfg.StrictMapTo(c)
	if err != nil {
		return err
	}

	return nil
}

func (c *UserConfig) SaveConfig() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cfg := ini.Empty()

	ini.PrettyEqual = true
	ini.PrettyFormat = false

	err := cfg.ReflectFrom(c)
	if err != nil {
		return err
	}

	err = cfg.SaveTo(c.IniPath)
	if err != nil {
		return err
	}

	return nil
}

func NewUserConfig(defaultConfig *UserConfig) (*UserConfig, error) {
	iniPath := os.Getenv(UserConfigEnv)

	exePath, err := os.Executable()
	if err != nil {
		return defaultConfig, err
	}

	appPath := os.Getenv(UserAppPathEnv)
	if appPath != "" {
		exePath = appPath
	}

	if iniPath == "" {
		iniPath = filepath.Join(filepath.Dir(exePath), config.AppName+".ini")
	}

	defaultConfig.AppPath = exePath
	defaultConfig.IniPath = iniPath

	if _, err := os.Stat(iniPath); os.IsNotExist(err) {
		// create a blank one on disk
		err := defaultConfig.SaveConfig()
		if err != nil {
			log.Error().Err(err).Msg("failed to save new user config to disk")
			return defaultConfig, err
		}

		return defaultConfig, nil
	}

	err = defaultConfig.LoadConfig()
	if err != nil {
		log.Error().Err(err).Msg("failed to load user config")
		return defaultConfig, err
	}

	return defaultConfig, nil
}
