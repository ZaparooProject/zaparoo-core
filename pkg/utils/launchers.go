package utils

import (
	"bytes"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
	"os/exec"
	"runtime"
	"strings"
	"text/template"
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

func ParseCustomLaunchers(customLaunchers []config.LaunchersCustom) []platforms.Launcher {
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
				data := struct {
					MediaPath string
				}{
					MediaPath: path,
				}

				tmpl, err := template.New("command").Parse(v.Execute)
				if err != nil {
					return err
				}

				var buf bytes.Buffer
				if err := tmpl.Execute(&buf, data); err != nil {
					return err
				}

				if runtime.GOOS == "windows" {
					cmd := exec.Command("cmd", "/c", buf.String())
					err = cmd.Run()
				} else {
					cmd := exec.Command("sh", "-c", buf.String())
					err = cmd.Run()
				}

				if err != nil {
					log.Error().Err(err).Msgf("error running custom launcher: %s", buf.String())
					return err
				}

				return nil
			},
		})
	}
	return launchers
}
