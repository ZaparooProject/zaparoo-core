package zapscript

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
)

func cmdDelay(_ platforms.Platform, env platforms.CmdEnv) error {
	log.Info().Msgf("delaying for: %s", env.Args)

	amount, err := strconv.Atoi(env.Args)
	if err != nil {
		return err
	}

	time.Sleep(time.Duration(amount) * time.Millisecond)

	return nil
}

func cmdExecute(_ platforms.Platform, env platforms.CmdEnv) error {
	if env.Untrusted {
		return fmt.Errorf("command cannot be run from a remote source")
	}

	if !env.Cfg.IsExecuteAllowed(env.Args) {
		return fmt.Errorf("execute not allowed: %s", env.Args)
	}

	// very basic support for treating quoted strings as a single field
	// probably needs to be expanded to include single quotes and
	// escaped characters
	sb := &strings.Builder{}
	quoted := false
	var tokenArgs []string
	for _, r := range env.Args {
		if r == '"' {
			quoted = !quoted
			sb.WriteRune(r)
		} else if !quoted && r == ' ' {
			tokenArgs = append(tokenArgs, sb.String())
			sb.Reset()
		} else {
			sb.WriteRune(r)
		}
	}
	if sb.Len() > 0 {
		tokenArgs = append(tokenArgs, sb.String())
	}

	if len(tokenArgs) == 0 {
		return fmt.Errorf("execute command is empty")
	}

	cmd := tokenArgs[0]
	var cmdArgs []string

	if len(tokenArgs) > 1 {
		cmdArgs = tokenArgs[1:]
	}

	return exec.Command(cmd, cmdArgs...).Run()
}
