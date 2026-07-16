//go:build linux

package mister

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

func preNoticeTime() time.Duration {
	// Account for time needed to open the script console.
	return 5 * time.Second
}

func showNotice(
	pl *Platform,
	args widgetmodels.NoticeArgs,
	loader bool,
) (string, error) {
	log.Info().Msgf("showing notice: %s", args.Text)
	argsID, err := helpers.RandSeq(10)
	if err != nil {
		return "", fmt.Errorf("failed to generate random sequence: %w", err)
	}
	argsName := "notice-" + argsID + ".json"
	if loader {
		argsName = "loader-" + argsID + ".json"
	}
	argsPath := filepath.Join(pl.Settings().TempDir, argsName)
	args.Complete = argsPath + ".complete"

	log.Debug().Msg("launching script notice")
	if writeErr := writeNoticeArgs(pl.filesystem(), argsPath, args); writeErr != nil {
		return "", writeErr
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

func writeNoticeArgs(fs afero.Fs, argsPath string, args widgetmodels.NoticeArgs) error {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("error marshalling notice args: %w", err)
	}
	if err = afero.WriteFile(fs, argsPath, argsJSON, 0o600); err != nil {
		return fmt.Errorf("error writing notice args: %w", err)
	}
	return nil
}

func hideNotice(fs afero.Fs, argsPath string) error {
	if err := fs.Remove(argsPath); err != nil {
		return fmt.Errorf("error removing notice args: %w", err)
	}
	if err := afero.WriteFile(fs, argsPath+".complete", []byte{}, 0o600); err != nil {
		return fmt.Errorf("error writing notice complete: %w", err)
	}
	return nil
}

func showPicker(
	pl *Platform,
	args *widgetmodels.PickerArgs,
) (string, error) {
	argsID, err := helpers.RandSeq(10)
	if err != nil {
		return "", fmt.Errorf("failed to generate random sequence: %w", err)
	}
	argsPath := filepath.Join(pl.Settings().TempDir, "picker-"+argsID+".json")
	args.Complete = argsPath + ".complete"
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("failed to marshal picker args: %w", err)
	}
	if err = afero.WriteFile(pl.filesystem(), argsPath, argsJSON, 0o600); err != nil {
		return "", fmt.Errorf("failed to write picker args file: %w", err)
	}

	scriptPath := filepath.Join(misterconfig.ScriptsDir, "zaparoo.sh")
	if runErr := runScript(pl, scriptPath, "'-show-picker' '"+argsPath+"'", false); runErr != nil {
		return "", runErr
	}
	return argsPath, nil
}
