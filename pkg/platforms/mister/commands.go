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

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/mrext/mister"
)

func CmdIni(_ platforms.Platform, env *platforms.CmdEnv) (platforms.CmdResult, error) {
	inis, err := mister.GetAllMisterIni()
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

	err = mister.SetActiveIni(id, doRelaunch)
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

	err := mister.LaunchShortCore(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to launch core: %w", err)
	}
	return platforms.CmdResult{
		MediaChanged: true,
	}, nil
}

func cmdMisterScript(plm *Platform) func(platforms.Platform, *platforms.CmdEnv) (platforms.CmdResult, error) {
	return func(_ platforms.Platform, env *platforms.CmdEnv) (platforms.CmdResult, error) {
		// TODO: generic read bool function
		hidden := env.Cmd.AdvArgs["hidden"] == "true" || env.Cmd.AdvArgs["hidden"] == "yes"

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

		scriptPath := filepath.Join(ScriptsDir, script)
		if _, err := os.Stat(scriptPath); err != nil {
			return platforms.CmdResult{}, fmt.Errorf("script not found: %s", script)
		}

		script = scriptPath

		args = args[1:]
		if len(args) == 0 {
			return platforms.CmdResult{}, runScript(plm, script, "", hidden)
		}

		cleaned := "'"
		inQuote := false
		for _, arg := range strings.Join(args, " ") {
			if arg == '"' {
				inQuote = !inQuote
			}

			if arg == ' ' && !inQuote {
				cleaned += "' '"
				continue
			}

			if arg == '\'' {
				cleaned += "'\\''"
				continue
			}

			cleaned += string(arg)
		}
		cleaned += "'"

		log.Info().Msgf("running script: %s", script+" "+cleaned)
		return platforms.CmdResult{}, runScript(plm, script, cleaned, hidden)
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

	cmd, err := os.OpenFile(CmdInterface, os.O_RDWR, 0)
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
