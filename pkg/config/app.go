package config

import "time"

var AppVersion = "DEVELOPMENT"

const (
	AppName              = "zaparoo"
	MediaDbFile          = "media.db"
	UserDbFile           = "user.db"
	LogFile              = "core.log"
	PidFile              = "core.pid"
	CfgFile              = "config.toml"
	AuthFile             = "auth.toml"
	UserDir              = "user"
	ApiRequestTimeout    = 30 * time.Second
	SuccessSoundFilename = "success.wav"
	FailSoundFilename    = "fail.wav"
	AssetsDir            = "assets"
	MappingsDir          = "mappings"
	LaunchersDir         = "launchers"
	MediaDir             = "media"
)
