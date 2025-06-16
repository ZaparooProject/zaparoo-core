//go:build linux || darwin

package mister

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/wizzomafizzo/mrext/pkg/mister"
)

func CmdIni(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	inis, err := mister.GetAllMisterIni()
	if err != nil {
		return platforms.CmdResult{}, err
	}

	if len(inis) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no ini files found")
	}

	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no ini specified")
	}

	id, err := strconv.Atoi(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, err
	}

	if id < 1 || id > len(inis) {
		return platforms.CmdResult{}, fmt.Errorf("ini id out of range: %d", id)
	}

	doRelaunch := true
	// only relaunch if there aren't any more commands
	if env.TotalCommands > 1 && env.CurrentIndex < env.TotalCommands-1 {
		doRelaunch = false
	}

	err = mister.SetActiveIni(id, doRelaunch)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	return platforms.CmdResult{
		MediaChanged: doRelaunch,
	}, nil
}

func CmdLaunchCore(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no core specified")
	}

	return platforms.CmdResult{
		MediaChanged: true,
	}, mister.LaunchShortCore(env.Cmd.Args[0])
}

func cmdMisterScript(plm *Platform) func(platforms.Platform, platforms.CmdEnv) (platforms.CmdResult, error) {
	return func(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
		// TODO: generic read bool function
		hidden := env.Cmd.AdvArgs["hidden"] == "true" || env.Cmd.AdvArgs["hidden"] == "yes"

		if len(env.Cmd.Args) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no script specified")
		}

		args := strings.Fields(env.Cmd.Args[0])

		if len(args) == 0 {
			return platforms.CmdResult{}, fmt.Errorf("no script specified")
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

func CmdMisterMgl(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, fmt.Errorf("no mgl specified")
	}

	if env.Cmd.Args[0] == "" {
		return platforms.CmdResult{}, fmt.Errorf("no mgl specified")
	}

	tmpFile, err := os.CreateTemp("", "*.mgl")
	if err != nil {
		return platforms.CmdResult{}, err
	}

	_, err = tmpFile.WriteString(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, err
	}

	err = tmpFile.Close()
	if err != nil {
		return platforms.CmdResult{}, err
	}

	cmd, err := os.OpenFile(CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return platforms.CmdResult{}, err
	}
	defer func(cmd *os.File) {
		err := cmd.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close mgl file")
		}
	}(cmd)

	_, err = cmd.WriteString(fmt.Sprintf("load_core %s\n", tmpFile.Name()))
	if err != nil {
		return platforms.CmdResult{}, err
	}

	go func() {
		time.Sleep(5 * time.Second)
		_ = os.Remove(tmpFile.Name())
	}()

	return platforms.CmdResult{
		MediaChanged: true,
	}, nil
}
