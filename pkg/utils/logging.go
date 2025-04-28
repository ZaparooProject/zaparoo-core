package utils

import (
	"io"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

func InitLogging(pl platforms.Platform, writers []io.Writer) error {
	err := os.MkdirAll(pl.Settings().TempDir, 0755)
	if err != nil {
		return err
	}

	var logWriters = []io.Writer{&lumberjack.Logger{
		Filename:   filepath.Join(pl.Settings().TempDir, config.LogFile),
		MaxSize:    1,
		MaxBackups: 2,
	}}

	if len(writers) > 0 {
		logWriters = append(logWriters, writers...)
	}

	log.Logger = log.Output(io.MultiWriter(logWriters...))

	return nil
}
