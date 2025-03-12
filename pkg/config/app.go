package config

import "time"

const (
	AppVersion        = "2.2.0-dev"
	AppName           = "zaparoo"
	GamesDbFile       = "games.db"
	TapToDbFile       = "tapto.db"
	LogFile           = "core.log"
	PidFile           = "core.pid"
	CfgFile           = "config.toml"
	ApiRequestTimeout = 30 * time.Second
)
