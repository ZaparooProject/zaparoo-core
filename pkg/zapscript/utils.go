package zapscript

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

func cmdEcho(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msg(strings.Join(env.Cmd.Args, ", "))
	return platforms.CmdResult{}, nil
}

func cmdStop(pl platforms.Platform, _ platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msg("stopping media")
	return platforms.CmdResult{
		MediaChanged: true,
	}, pl.StopActiveLauncher()
}

func cmdDelay(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	amount, err := strconv.Atoi(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, err
	}

	log.Info().Msgf("delaying for: %d", amount)
	time.Sleep(time.Duration(amount) * time.Millisecond)

	return platforms.CmdResult{}, nil
}

func cmdExecute(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	execStr := env.Cmd.Args[0]

	if env.Unsafe {
		return platforms.CmdResult{}, errors.New("command cannot be run from a remote source")
	} else if !env.Cfg.IsExecuteAllowed(execStr) {
		return platforms.CmdResult{}, fmt.Errorf("execute not allowed: %s", execStr)
	}

	// very basic support for treating quoted strings as a single field
	// probably needs to be expanded to include single quotes and
	// escaped characters
	// TODO: this probably doesn't work on windows?
	sb := &strings.Builder{}
	quoted := false
	var tokenArgs []string
	for _, r := range execStr {
		switch {
		case r == '"':
			quoted = !quoted
			_, _ = sb.WriteRune(r)
		case !quoted && r == ' ':
			tokenArgs = append(tokenArgs, sb.String())
			sb.Reset()
		default:
			_, _ = sb.WriteRune(r)
		}
	}
	if sb.Len() > 0 {
		tokenArgs = append(tokenArgs, sb.String())
	}

	if len(tokenArgs) == 0 {
		return platforms.CmdResult{}, errors.New("execute command is empty")
	}

	cmd := tokenArgs[0]
	var cmdArgs []string

	if len(tokenArgs) > 1 {
		cmdArgs = tokenArgs[1:]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return platforms.CmdResult{}, exec.CommandContext(ctx, cmd, cmdArgs...).Run()
}
