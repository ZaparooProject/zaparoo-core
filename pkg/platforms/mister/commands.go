//go:build linux

package mister

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	advargtypes "github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript/advargs/types"
	"github.com/rs/zerolog/log"
)

func CmdIni(_ platforms.Platform, env *platforms.CmdEnv) (platforms.CmdResult, error) {
	inis, err := mistermain.GetAllINIFiles()
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to get MiSTer ini files: %w", err)
	}

	if len(inis) == 0 {
		return platforms.CmdResult{}, errors.New("no ini files found")
	}

	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, errors.New("no ini specified")
	}

	id, err := strconv.Atoi(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to parse ini ID %q: %w", env.Cmd.Args[0], err)
	}

	if id < 1 || id > len(inis) {
		return platforms.CmdResult{}, fmt.Errorf("ini id out of range: %d", id)
	}

	doRelaunch := env.TotalCommands <= 1 || env.CurrentIndex >= env.TotalCommands-1
	// only relaunch if there aren't any more commands

	selectedIni := inis[id-1]
	log.Info().
		Str("ini_file", selectedIni.Filename).
		Str("display_name", selectedIni.DisplayName).
		Bool("will_relaunch", doRelaunch).
		Msg("setting active INI")

	err = mistermain.SetActiveIni(id, doRelaunch)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to set active ini: %w", err)
	}

	return platforms.CmdResult{
		MediaChanged: doRelaunch,
	}, nil
}

func CmdLaunchCore(_ platforms.Platform, env *platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, errors.New("no core specified")
	}

	corePath := env.Cmd.Args[0]
	log.Info().Str("core_path", corePath).Msg("launching core via command")

	err := mgls.LaunchShortCore(corePath)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to launch core: %w", err)
	}
	return platforms.CmdResult{
		MediaChanged: true,
	}, nil
}

func cmdMisterScript(plm *Platform) func(platforms.Platform, *platforms.CmdEnv) (platforms.CmdResult, error) {
	return func(pl platforms.Platform, env *platforms.CmdEnv) (platforms.CmdResult, error) {
		var advArgs advargtypes.MisterScriptArgs
		if err := zapscript.ParseAdvArgs(pl, env, &advArgs); err != nil {
			return platforms.CmdResult{}, fmt.Errorf("invalid advanced arguments: %w", err)
		}
		hidden := helpers.IsTruthy(advArgs.Hidden)

		if len(env.Cmd.Args) == 0 {
			return platforms.CmdResult{}, errors.New("no script specified")
		}

		args := strings.Fields(env.Cmd.Args[0])

		if len(args) == 0 {
			return platforms.CmdResult{}, errors.New("no script specified")
		}

		script := args[0]

		if !strings.HasSuffix(script, ".sh") {
			return platforms.CmdResult{}, fmt.Errorf("invalid script: %s", script)
		}

		scriptPath := filepath.Join(config.ScriptsDir, script)
		if _, err := os.Stat(scriptPath); err != nil {
			return platforms.CmdResult{}, fmt.Errorf("script not found: %s", script)
		}

		script = scriptPath

		args = args[1:]
		if len(args) == 0 {
			return platforms.CmdResult{}, runScript(plm, script, "", hidden)
		}

		var cleaned strings.Builder
		_ = cleaned.WriteByte('\'')
		inQuote := false
		for _, arg := range strings.Join(args, " ") {
			if arg == '"' {
				inQuote = !inQuote
			}

			if arg == ' ' && !inQuote {
				_, _ = cleaned.WriteString("' '")
				continue
			}

			if arg == '\'' {
				_, _ = cleaned.WriteString("'\\''")
				continue
			}

			_, _ = cleaned.WriteRune(arg)
		}
		_ = cleaned.WriteByte('\'')

		log.Info().Msgf("running script: %s", script+" "+cleaned.String())
		return platforms.CmdResult{}, runScript(plm, script, cleaned.String(), hidden)
	}
}

func CmdMisterMgl(_ platforms.Platform, env *platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, errors.New("no mgl specified")
	}

	if env.Cmd.Args[0] == "" {
		return platforms.CmdResult{}, errors.New("no mgl specified")
	}

	tmpFile, err := os.CreateTemp("", "*.mgl")
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to create temp mgl file: %w", err)
	}

	_, err = tmpFile.WriteString(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to write to temp mgl file: %w", err)
	}

	err = tmpFile.Close()
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to close temp mgl file: %w", err)
	}

	cmd, err := os.OpenFile(config.CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to open command interface: %w", err)
	}
	defer func(cmd *os.File) {
		closeErr := cmd.Close()
		if closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close mgl file")
		}
	}(cmd)

	_, err = fmt.Fprintf(cmd, "load_core %s\n", tmpFile.Name())
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to write command: %w", err)
	}

	go func() {
		time.Sleep(5 * time.Second)
		_ = os.Remove(tmpFile.Name())
	}()

	return platforms.CmdResult{
		MediaChanged: true,
	}, nil
}
