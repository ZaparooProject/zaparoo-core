//go:build linux

package mister

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/rs/zerolog/log"
)

func preNoticeTime() time.Duration {
	if misterconfig.MainHasFeature(misterconfig.MainFeatureNotice) {
		return 3 * time.Second
	}
	// accounting for the time it takes to boot up the console
	return 5 * time.Second
}

func showNotice(
	pl *Platform,
	text string,
	loader bool,
) (string, error) {
	log.Info().Msgf("showing notice: %s", text)
	argsID, err := helpers.RandSeq(10)
	if err != nil {
		return "", fmt.Errorf("failed to generate random sequence: %w", err)
	}
	argsName := "notice-" + argsID + ".json"
	if loader {
		argsName = "loader-" + argsID + ".json"
	}
	argsPath := filepath.Join(pl.Settings().TempDir, argsName)
	completePath := argsPath + ".complete"

	if misterconfig.MainHasFeature(misterconfig.MainFeatureNotice) {
		err := mistermain.RunDevCmd("show_notice", text)
		if err != nil {
			return "", fmt.Errorf("error running dev cmd: %w", err)
		}
	} else {
		log.Debug().Msg("launching script notice")
		// fall back on script
		args := widgetmodels.NoticeArgs{
			Text:     text,
			Complete: completePath,
		}
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("error marshalling notice args: %w", err)
		}
		err = os.WriteFile(argsPath, argsJSON, 0o600)
		if err != nil {
			return "", fmt.Errorf("error writing notice args: %w", err)
		}

		scriptPath := filepath.Join(misterconfig.ScriptsDir, "zaparoo.sh")
		scriptArg := "'-show-notice' '" + argsPath + "'"
		if loader {
			scriptArg = "'-show-loader' '" + argsPath + "'"
		}
		log.Debug().Msgf("running script notice: %s %s", scriptPath, scriptArg)
		err = runScript(pl, scriptPath, scriptArg, false)
		if err != nil {
			return "", fmt.Errorf("error running notice script: %w", err)
		}
	}

	return argsPath, nil
}

func hideNotice(argsPath string) error {
	if !misterconfig.MainHasFeature(misterconfig.MainFeatureNotice) {
		err := os.Remove(argsPath)
		if err != nil {
			return fmt.Errorf("error removing notice args: %w", err)
		}
		err = os.WriteFile(argsPath+".complete", []byte{}, 0o600)
		if err != nil {
			return fmt.Errorf("error writing notice complete: %w", err)
		}
	}
	return nil
}

func misterSetupMainPicker(args widgetmodels.PickerArgs) error {
	// remove existing items
	files, err := os.ReadDir(misterconfig.MainPickerDir)
	if err != nil {
		log.Error().Msgf("error reading picker items dir: %s", err)
	} else {
		for _, file := range files {
			removeErr := os.Remove(filepath.Join(misterconfig.MainPickerDir, file.Name()))
			if removeErr != nil {
				log.Error().Msgf("error deleting file %s: %s", file.Name(), removeErr)
			}
		}
	}

	// write items to dir
	for _, item := range args.Items {
		name := item.Name
		if strings.TrimSpace(item.Name) == "" {
			name = item.ZapScript
		}

		if len(name) > 25 {
			name = name[:25] + "..."
		}

		contents, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return fmt.Errorf("failed to marshal picker item: %w", marshalErr)
		}

		path := filepath.Join(misterconfig.MainPickerDir, name+".txt")
		err = os.WriteFile(path, contents, 0o600)
		if err != nil {
			return fmt.Errorf("failed to write picker item file: %w", err)
		}
	}

	// launch
	err = os.WriteFile(misterconfig.CmdInterface, []byte("show_picker\n"), 0o600)
	if err != nil {
		return fmt.Errorf("failed to write show_picker command: %w", err)
	}

	return nil
}

func showPicker(
	pl *Platform,
	args widgetmodels.PickerArgs,
) error {
	// use custom main ui if available
	if misterconfig.MainHasFeature(misterconfig.MainFeaturePicker) {
		err := misterSetupMainPicker(args)
		if err != nil {
			return err
		}
		return nil
	}

	// fall back to launching script menu
	argsPath := filepath.Join(pl.Settings().TempDir, "picker.json")
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("failed to marshal picker args: %w", err)
	}
	err = os.WriteFile(argsPath, argsJSON, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write picker args file: %w", err)
	}

	scriptPath := filepath.Join(misterconfig.ScriptsDir, "zaparoo.sh")
	return runScript(pl, scriptPath, "'-show-picker' '"+argsPath+"'", false)
}
