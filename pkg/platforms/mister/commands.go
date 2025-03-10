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

func CmdIni(_ platforms.Platform, env platforms.CmdEnv) error {
	inis, err := mister.GetAllMisterIni()
	if err != nil {
		return err
	}

	if len(inis) == 0 {
		return fmt.Errorf("no ini files found")
	}

	id, err := strconv.Atoi(env.Args)
	if err != nil {
		return err
	}

	if id < 1 || id > len(inis) {
		return fmt.Errorf("ini id out of range: %d", id)
	}

	doRelaunch := true
	// only relaunch if there aren't any more commands
	if env.TotalCommands > 1 && env.CurrentIndex < env.TotalCommands-1 {
		doRelaunch = false
	}

	err = mister.SetActiveIni(id, doRelaunch)
	if err != nil {
		return err
	}

	return nil
}

func CmdLaunchCore(_ platforms.Platform, env platforms.CmdEnv) error {
	return mister.LaunchShortCore(env.Args)
}

func cmdMisterScript(plm *Platform) func(platforms.Platform, platforms.CmdEnv) error {
	return func(pl platforms.Platform, env platforms.CmdEnv) error {
		// TODO: generic read bool function
		hidden := env.NamedArgs["hidden"] == "true" || env.NamedArgs["hidden"] == "yes"

		args := strings.Fields(env.Args)

		if len(args) == 0 {
			return fmt.Errorf("no script specified")
		}

		script := args[0]

		if !strings.HasSuffix(script, ".sh") {
			return fmt.Errorf("invalid script: %s", script)
		}

		scriptPath := filepath.Join(ScriptsDir, script)
		if _, err := os.Stat(scriptPath); err != nil {
			return fmt.Errorf("script not found: %s", script)
		}

		script = scriptPath

		args = args[1:]
		if len(args) == 0 {
			return runScript(plm, script, "", hidden)
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
		return runScript(plm, script, cleaned, hidden)
	}
}

func CmdMisterMgl(_ platforms.Platform, env platforms.CmdEnv) error {
	if env.Args == "" {
		return fmt.Errorf("no mgl specified")
	}

	tmpFile, err := os.CreateTemp("", "*.mgl")
	if err != nil {
		return err
	}

	_, err = tmpFile.WriteString(env.Args)
	if err != nil {
		return err
	}

	err = tmpFile.Close()
	if err != nil {
		return err
	}

	cmd, err := os.OpenFile(CmdInterface, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func(cmd *os.File) {
		err := cmd.Close()
		if err != nil {
			log.Warn().Err(err).Msg("failed to close mgl file")
		}
	}(cmd)

	_, err = cmd.WriteString(fmt.Sprintf("load_core %s\n", tmpFile.Name()))
	if err != nil {
		return err
	}

	go func() {
		time.Sleep(5 * time.Second)
		_ = os.Remove(tmpFile.Name())
	}()

	return nil
}
