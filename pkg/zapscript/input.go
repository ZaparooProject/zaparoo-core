package zapscript

import (
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils/linuxinput/keyboardmap"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
)

// DEPRECATED
func cmdKey(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, fmt.Errorf("input.key cannot be run from a remote source")
	}
	legacyCode, err := strconv.Atoi(env.Args)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("invalid legacy key code: %s", env.Args)
	}
	code := keyboardmap.GetLegacyKey(legacyCode)
	if code == "" {
		return platforms.CmdResult{}, fmt.Errorf("invalid legacy key code: %s", env.Args)
	}
	return platforms.CmdResult{}, pl.KeyboardPress(code)
}

// converts a string to a list of key symbols. long names are named inside
// curly braces and characters can be escaped with a backslash
func readKeys(keys string) ([]string, error) {
	var names []string
	inEscape := false
	inName := false
	var name string

	for _, c := range keys {
		if inEscape {
			name += string(c)
			inEscape = false
			continue
		}

		if c == '\\' {
			inEscape = true
			continue
		}

		if c == '{' {
			if inName {
				return nil, fmt.Errorf("unexpected {")
			}

			inName = true
			continue
		}

		if c == '}' {
			if !inName {
				return nil, fmt.Errorf("unexpected }")
			}

			names = append(names, name)
			name = ""
			inName = false
			continue
		}

		if inName {
			name += string(c)
		} else {
			names = append(names, string(c))
		}
	}

	if inName {
		return nil, fmt.Errorf("missing }")
	}

	return names, nil
}

func cmdKeyboard(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, fmt.Errorf("command cannot be run from a remote source")
	}

	log.Info().Msgf("keyboard input: %s", env.Args)

	// TODO: stuff like adjust delay, only press, etc.
	//	     basically a filled out mini macro language for key presses

	names, err := readKeys(env.Args)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	for _, name := range names {
		if err := pl.KeyboardPress(name); err != nil {
			return platforms.CmdResult{}, err
		}
		time.Sleep(100 * time.Millisecond)
	}

	return platforms.CmdResult{}, nil
}

func cmdGamepad(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if env.Unsafe {
		return platforms.CmdResult{}, fmt.Errorf("command cannot be run from a remote source")
	}

	log.Info().Msgf("gamepad input: %s", env.Args)

	names, err := readKeys(env.Args)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	for _, name := range names {
		if err := pl.GamepadPress(name); err != nil {
			return platforms.CmdResult{}, err
		}
		time.Sleep(100 * time.Millisecond)
	}

	return platforms.CmdResult{}, nil
}

func insertCoin(pl platforms.Platform, env platforms.CmdEnv, key string) (platforms.CmdResult, error) {
	amount, err := strconv.Atoi(env.Args)
	if err != nil {
		return platforms.CmdResult{}, err
	}

	for i := 0; i < amount; i++ {
		_ = pl.KeyboardPress(key)
		time.Sleep(100 * time.Millisecond)
	}

	return platforms.CmdResult{}, nil
}

func cmdCoinP1(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msgf("inserting coin for player 1: %s", env.Args)
	return insertCoin(pl, env, "5")
}

func cmdCoinP2(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msgf("inserting coin for player 2: %s", env.Args)
	return insertCoin(pl, env, "6")
}
