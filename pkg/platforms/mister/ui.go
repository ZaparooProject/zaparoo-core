//go:build linux

package mister

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/rs/zerolog/log"
)

func preNoticeTime() time.Duration {
	// Account for time needed to open the script console.
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

	log.Debug().Msg("launching script notice")
	args := widgetmodels.NoticeArgs{
		Text:     text,
		Complete: completePath,
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("error marshalling notice args: %w", err)
	}
	if err = os.WriteFile(argsPath, argsJSON, 0o600); err != nil {
		return "", fmt.Errorf("error writing notice args: %w", err)
	}

	scriptPath := filepath.Join(misterconfig.ScriptsDir, "zaparoo.sh")
	scriptArg := "'-show-notice' '" + argsPath + "'"
	if loader {
		scriptArg = "'-show-loader' '" + argsPath + "'"
	}
	log.Debug().Msgf("running script notice: %s %s", scriptPath, scriptArg)
	if err = runScript(pl, scriptPath, scriptArg, false); err != nil {
		return "", fmt.Errorf("error running notice script: %w", err)
	}

	return argsPath, nil
}

func hideNotice(argsPath string) error {
	if err := os.Remove(argsPath); err != nil {
		return fmt.Errorf("error removing notice args: %w", err)
	}
	if err := os.WriteFile(argsPath+".complete", []byte{}, 0o600); err != nil {
		return fmt.Errorf("error writing notice complete: %w", err)
	}
	return nil
}

func showPicker(
	pl *Platform,
	args widgetmodels.PickerArgs,
) error {
	// Launch picker through the script UI.
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
