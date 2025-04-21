package config

import "time"

var AppVersion = "DEVELOPMENT"

const (
	AppName           = "zaparoo"
	GamesDbFile       = "games.db"
	TapToDbFile       = "tapto.db"
	LogFile           = "core.log"
	PidFile           = "core.pid"
	CfgFile           = "config.toml"
	UserDir           = "user"
	ApiRequestTimeout = 30 * time.Second
)
