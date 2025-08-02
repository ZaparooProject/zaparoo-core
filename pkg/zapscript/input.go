package zapscript

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils/linuxinput/keyboardmap"
	"github.com/rs/zerolog/log"
)

// DEPRECATED
func cmdKey(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, ErrRemoteSource
	}
	if len(env.Cmd.Args) != 1 {
		return platforms.CmdResult{}, ErrArgCount
	}
	legacyCode, err := strconv.Atoi(env.Cmd.Args[0])
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("invalid legacy key code: %s", env.Cmd.Args[0])
	}
	code := keyboardmap.GetLegacyKey(legacyCode)
	if code == "" {
		return platforms.CmdResult{}, fmt.Errorf("invalid legacy key code: %s", env.Cmd.Args[0])
	}
	return platforms.CmdResult{}, pl.KeyboardPress(code)
}

func cmdKeyboard(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, ErrRemoteSource
	}

	log.Info().Msgf("keyboard input: %v", env.Cmd.Args)

	// TODO: stuff like adjust delay, only press, etc.
	//	     basically a filled out mini macro language for key presses

	for _, name := range env.Cmd.Args {
		if err := pl.KeyboardPress(name); err != nil {
			return platforms.CmdResult{}, err
		}
		time.Sleep(100 * time.Millisecond)
	}

	return platforms.CmdResult{}, nil
}

func cmdGamepad(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, ErrRemoteSource
	}

	log.Info().Msgf("gamepad input: %v", env.Cmd.Args)

	for _, name := range env.Cmd.Args {
		if err := pl.GamepadPress(name); err != nil {
			return platforms.CmdResult{}, err
		}
		time.Sleep(100 * time.Millisecond)
	}

	return platforms.CmdResult{}, nil
}

func insertCoin(pl platforms.Platform, env platforms.CmdEnv, key string) (platforms.CmdResult, error) {
	var amount int

	if len(env.Cmd.Args) == 0 || env.Cmd.Args[0] != "" {
		amount = 1
	} else {
		var err error
		amount, err = strconv.Atoi(env.Cmd.Args[0])
		if err != nil {
			return platforms.CmdResult{}, err
		}
	}

	for i := 0; i < amount; i++ {
		_ = pl.KeyboardPress(key)
		time.Sleep(100 * time.Millisecond)
	}

	return platforms.CmdResult{}, nil
}

func cmdCoinP1(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msgf("inserting coin for player 1: %v", env.Cmd.Args)
	return insertCoin(pl, env, "5")
}

func cmdCoinP2(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msgf("inserting coin for player 2: %v", env.Cmd.Args)
	return insertCoin(pl, env, "6")
}
