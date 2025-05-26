package mister

import (
	"encoding/json"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
	"os"
	"path/filepath"
	"time"
)

func preNoticeTime() time.Duration {
	if MainHasFeature(MainFeatureNotice) {
		return 3 * time.Second
	} else {
		// accounting for the time it takes to boot up the console
		return 5 * time.Second
	}
}

func showNotice(
	cfg *config.Instance,
	pl platforms.Platform,
	text string,
	loader bool,
) (string, error) {
	log.Info().Msgf("showing notice: %s", text)
	argsId := utils.RandSeq(10)
	argsName := "notice-" + argsId + ".json"
	if loader {
		argsName = "loader-" + argsId + ".json"
	}
	argsPath := filepath.Join(pl.Settings().TempDir, argsName)
	completePath := argsPath + ".complete"

	if MainHasFeature(MainFeatureNotice) {
		err := RunDevCmd("show_notice", text)
		if err != nil {
			return "", fmt.Errorf("error running dev cmd: %w", err)
		}
	} else {
		log.Debug().Msg("launching script notice")
		// fall back on script
		args := widgetModels.NoticeArgs{
			Text:     text,
			Complete: completePath,
		}
		argsJson, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("error marshalling notice args: %w", err)
		}
		err = os.WriteFile(argsPath, argsJson, 0644)
		if err != nil {
			return "", fmt.Errorf("error writing notice args: %w", err)
		}
		text := fmt.Sprintf("**mister.script:zaparoo.sh -show-notice %s", argsPath)
		if loader {
			text = fmt.Sprintf("**mister.script:zaparoo.sh -show-loader %s", argsPath)
		}
		log.Debug().Msgf("running script notice: %s", text)
		apiArgs := models.RunParams{
			Text: &text,
		}
		ps, err := json.Marshal(apiArgs)
		if err != nil {
			log.Error().Err(err).Msg("error creating run params")
		}
		_, err = client.LocalClient(cfg, models.MethodRun, string(ps))
		if err != nil {
			log.Error().Err(err).Msg("error running local client")
		}
	}

	return argsPath, nil
}

func hideNotice(argsPath string) error {
	if !MainHasFeature(MainFeatureNotice) {
		err := os.Remove(argsPath)
		if err != nil {
			return fmt.Errorf("error removing notice args: %w", err)
		}
		err = os.WriteFile(argsPath+".complete", []byte{}, 0644)
		if err != nil {
			return fmt.Errorf("error writing notice complete: %w", err)
		}
	}
	return nil
}

func misterSetupMainPicker(args widgetModels.PickerArgs) error {
	// remove existing items
	files, err := os.ReadDir(MainPickerDir)
	if err != nil {
		log.Error().Msgf("error reading picker items dir: %s", err)
	} else {
		for _, file := range files {
			err := os.Remove(filepath.Join(MainPickerDir, file.Name()))
			if err != nil {
				log.Error().Msgf("error deleting file %s: %s", file.Name(), err)
			}
		}
	}

	// write items to dir
	for _, item := range args.Items {
		if item.Name == nil || *item.Name == "" {
			continue
		}

		contents, err := json.Marshal(item)
		if err != nil {
			return err
		}

		path := filepath.Join(MainPickerDir, *item.Name+".txt")
		err = os.WriteFile(path, contents, 0644)
		if err != nil {
			return err
		}
	}

	// launch
	err = os.WriteFile(CmdInterface, []byte("show_picker\n"), 0644)
	if err != nil {
		return err
	}

	return nil
}

func showPicker(
	cfg *config.Instance,
	pl platforms.Platform,
	args widgetModels.PickerArgs,
) error {
	// use custom main ui if available
	if MainHasFeature(MainFeaturePicker) {
		err := misterSetupMainPicker(args)
		if err != nil {
			return err
		} else {
			return nil
		}
	}

	// fall back to launching script menu
	argsPath := filepath.Join(pl.Settings().TempDir, "picker.json")
	argsJson, err := json.Marshal(args)
	if err != nil {
		return err
	}
	err = os.WriteFile(argsPath, argsJson, 0644)
	if err != nil {
		return err
	}

	text := fmt.Sprintf("**mister.script:zaparoo.sh -show-picker %s", argsPath)
	apiArgs := models.RunParams{
		Text: &text,
	}
	ps, err := json.Marshal(apiArgs)
	if err != nil {
		log.Error().Err(err).Msg("error creating run params")
	}

	_, err = client.LocalClient(cfg, models.MethodRun, string(ps))
	if err != nil {
		log.Error().Err(err).Msg("error running local client")
	}

	return nil
}
