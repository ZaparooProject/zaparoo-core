package config

import (
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
)

const (
	AppVersion  = "2.2.0-dev"
	AppName     = "zaparoo"
	GamesDbFile = "games.db"
	TapToDbFile = "tapto.db"
	LogFile     = "core.log"
	PidFile     = "core.pid"
	CfgFile     = "config.toml"
)

func MkTempDir() string {
	path := filepath.Join(os.TempDir(), AppName)
	err := os.MkdirAll(path, 0755)
	if err != nil {
		log.Error().Err(err).Msg("error creating temp folder")
	}
	return path
}
