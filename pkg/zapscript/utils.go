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

func cmdDelay(pl platforms.Platform, env platforms.CmdEnv) error {
	log.Info().Msgf("delaying for: %s", env.Args)

	amount, err := strconv.Atoi(env.Args)
	if err != nil {
		return err
	}

	time.Sleep(time.Duration(amount) * time.Millisecond)

	return nil
}

func cmdExecute(pl platforms.Platform, env platforms.CmdEnv) error {
	if !env.Cfg.IsExecuteAllowed(env.Args) {
		return fmt.Errorf("execute not allowed: %s", env.Args)
	}

	// TODO: needs to detect stuff like quotes
	ps := strings.Split(env.Args, " ")
	cmd := ps[0]
	var args []string
	if len(ps) > 1 {
		args = ps[1:]
	}

	return exec.Command(cmd, args...).Run()
}
