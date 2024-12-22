package zapscript

import (
	"fmt"
	"strconv"
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

func cmdShell(pl platforms.Platform, env platforms.CmdEnv) error {
	if !env.Manual {
		return fmt.Errorf("shell commands must be manually run")
	}

	return pl.Shell(env.Args)
}
