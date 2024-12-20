package utils

import (
	"io"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

func InitLogging(pl platforms.Platform) error {
	err := os.MkdirAll(pl.LogDir(), 0755)
	if err != nil {
		return err
	}

	var BaseLogWriters = []io.Writer{&lumberjack.Logger{
		Filename:   filepath.Join(pl.LogDir(), config.LogFile),
		MaxSize:    1,
		MaxBackups: 2,
	}}

	// TODO: need some way to enable console logging per platform

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	log.Logger = log.Output(io.MultiWriter(BaseLogWriters...))

	return nil
}
