package config

import "time"

const (
	AppVersion        = "2.2.0"
	AppName           = "zaparoo"
	MediaDbFile       = "media.db"
	UserDbFile        = "user.db"
	LogFile           = "core.log"
	PidFile           = "core.pid"
	CfgFile           = "config.toml"
	ApiRequestTimeout = 30 * time.Second
)
