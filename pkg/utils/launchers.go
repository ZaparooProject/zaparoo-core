package utils

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/parser"
	"github.com/rs/zerolog/log"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func formatExtensions(exts []string) []string {
	newExts := make([]string, 0)
	for _, v := range exts {
		if v == "" {
			continue
		}
		newExt := strings.TrimSpace(v)
		if newExt[0] != '.' {
			newExt = "." + newExt
		}
		newExt = strings.ToLower(newExt)
		newExts = append(newExts, newExt)
	}
	return newExts
}

func ParseCustomLaunchers(
	pl platforms.Platform,
	customLaunchers []config.LaunchersCustom,
) []platforms.Launcher {
	launchers := make([]platforms.Launcher, 0)
	for _, v := range customLaunchers {
		systemID := ""
		if v.System != "" {
			system, err := systemdefs.LookupSystem(v.System)
			if err != nil {
				log.Err(err).Msgf("custom launcher %s: system not found: %s", v.ID, v.System)
				continue
			}
			systemID = system.ID
		}

		launchers = append(launchers, platforms.Launcher{
			ID:         v.ID,
			SystemID:   systemID,
			Folders:    v.MediaDirs,
			Extensions: formatExtensions(v.FileExts),
			Launch: func(_ *config.Instance, path string) error {
				hostname, err := os.Hostname()
				if err != nil {
					log.Debug().Err(err).Msgf("error getting hostname, continuing")
				}

				exprEnv := parser.CustomLauncherExprEnv{
					Platform: pl.ID(),
					Version:  config.AppVersion,
					Device: parser.ExprEnvDevice{
						Hostname: hostname,
						OS:       runtime.GOOS,
						Arch:     runtime.GOARCH,
					},
					MediaPath: path,
				}

				reader := parser.NewParser(v.Execute)
				output, err := reader.EvalExpressions(exprEnv)
				if err != nil {
					return fmt.Errorf("error evaluating execute expression: %w", err)
				}

				if runtime.GOOS == "windows" {
					cmd := exec.Command("cmd", "/c", output)
					err = cmd.Run()
				} else {
					cmd := exec.Command("sh", "-c", output)
					err = cmd.Run()
				}

				if err != nil {
					log.Error().Err(err).Msgf("error running custom launcher: %s", output)
					return err
				}

				return nil
			},
		})
	}
	return launchers
}
