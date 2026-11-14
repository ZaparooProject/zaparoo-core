// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package zapscript

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	apimodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

// ExecuteTimeout is the maximum duration for execute commands.
const ExecuteTimeout = 2 * time.Second

var (
	// ErrExecuteTimeout is returned when an execute command is killed for
	// running past ExecuteTimeout. It deliberately does not wrap
	// context.DeadlineExceeded: the user's command overran, the run was not
	// cancelled.
	ErrExecuteTimeout = errors.New("execute command timed out")
	// ErrExecuteExitStatus is returned when an execute command runs and exits
	// with a non-zero status: the user's program failed, not Core.
	ErrExecuteExitStatus = errors.New("execute command exited with an error")
)

//nolint:gocritic // single-use parameter in command handler
func cmdEcho(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	log.Info().Msg(strings.Join(env.Cmd.Args, ", "))
	return platforms.CmdResult{}, nil
}

//nolint:gocritic // unused parameter required by interface
func cmdStop(pl platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	slot, _, err := explicitCommandSlot(&env)
	if err != nil {
		return platforms.CmdResult{}, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
	}
	if slot == mediaslot.Background {
		// Background audio plays alongside the primary slot and has no core,
		// window or process of its own. Stopping it must never go near the
		// platform's stop or return-to-menu: on MiSTer that reloads the menu
		// core and ends whatever game is running, tracked or not. Closing the
		// background slot stops its playback and any playlist driving it.
		log.Info().Msg("stopping background media")
		clearUpdate := &playlists.Playlist{Slot: mediaslot.Background, Clear: true}
		if err := queuePlaylistUpdate(&env, clearUpdate); err != nil {
			return platforms.CmdResult{}, err
		}
		return platforms.CmdResult{PlaylistChanged: true}, nil
	}

	log.Info().Msg("stopping media")
	cancelStop := env.RecordStop(apimodels.MediaStopReasonStopped)
	if err := pl.ReturnToMenu(); err != nil {
		cancelStop()
		return platforms.CmdResult{
			MediaChanged: true,
		}, fmt.Errorf("failed to return to menu: %w", err)
	}
	return platforms.CmdResult{
		MediaChanged: true,
	}, nil
}

// parseMacroDuration parses a duration string used in **delay and {delay:N}.
// A plain integer is treated as milliseconds; any other string is passed to
// time.ParseDuration so "1s", "500ms", and "1m30s" are all accepted.
func parseMacroDuration(s string) (time.Duration, error) {
	if ms, err := strconv.Atoi(s); err == nil {
		return time.Duration(ms) * time.Millisecond, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return d, nil
}

// ErrDelaySuperseded is returned by a delay that a newer scan cut short. The
// script it belonged to ends there, quietly: it was replaced, not failed.
var ErrDelaySuperseded = errors.New("delay cut short by a newer scan")

// mergeDone returns a context that ends when either a or b does. A nil a is
// treated as one that never ends.
func mergeDone(a, b context.Context) (context.Context, context.CancelFunc) {
	if a == nil {
		a = context.Background()
	}
	ctx, cancel := context.WithCancel(a)
	stop := context.AfterFunc(b, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// waitForScanDelay waits out a timed delay, which ends early with the service
// or when a newer scan supersedes the one that started it.
func waitForScanDelay(env *platforms.CmdEnv, delay time.Duration) error {
	ctx := env.ServiceCtx
	if env.ScanCtx != nil {
		var stop context.CancelFunc
		ctx, stop = mergeDone(ctx, env.ScanCtx)
		defer stop()
	}
	err := waitForDelay(ctx, delay)
	if err != nil && env.ScanCtx != nil && env.ScanCtx.Err() != nil &&
		(env.ServiceCtx == nil || env.ServiceCtx.Err() == nil) {
		return ErrDelaySuperseded
	}
	return err
}

func waitForDelay(ctx context.Context, delay time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

//nolint:gocritic // single-use parameter in command handler
func cmdDelay(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	amount, err := strconv.Atoi(env.Cmd.Args[0])
	if err == nil {
		log.Info().Msgf("delaying for: %d", amount)
		return platforms.CmdResult{}, waitForScanDelay(&env, time.Duration(amount)*time.Millisecond)
	}

	switch env.Cmd.Args[0] {
	case "media_ready":
		if env.WaitForMediaReady == nil {
			return platforms.CmdResult{}, errors.New("media ready wait is not available")
		}
		ctx := env.LauncherCtx
		if ctx == nil {
			ctx = env.ServiceCtx
		}
		if ctx == nil {
			ctx = context.Background()
		}
		if waitErr := env.WaitForMediaReady(ctx); waitErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("wait for media ready: %w", waitErr)
		}
		return platforms.CmdResult{}, nil
	default:
		d, durErr := time.ParseDuration(env.Cmd.Args[0])
		if durErr != nil {
			return platforms.CmdResult{}, fmt.Errorf("invalid delay target %q: %w", env.Cmd.Args[0], durErr)
		}
		log.Info().Msgf("delaying for: %v", d)
		return platforms.CmdResult{}, waitForScanDelay(&env, d)
	}
}

//nolint:gocritic // single-use parameter in command handler
func cmdExecute(_ platforms.Platform, env platforms.CmdEnv) (platforms.CmdResult, error) {
	if len(env.Cmd.Args) == 0 {
		return platforms.CmdResult{}, ErrArgCount
	}

	execStr := env.Cmd.Args[0]

	if env.Unsafe {
		return platforms.CmdResult{}, ErrRemoteSource
	} else if env.Source != tokens.SourceControl && !env.Cfg.IsExecuteAllowed(execStr) {
		return platforms.CmdResult{}, fmt.Errorf("%w: %s", ErrExecuteNotAllowed, execStr)
	}

	tokenArgs, splitErr := helpers.SplitCommand(execStr)
	if splitErr != nil {
		return platforms.CmdResult{}, fmt.Errorf("failed to parse execute command: %w", splitErr)
	}
	if len(tokenArgs) == 0 {
		return platforms.CmdResult{}, errors.New("execute command is empty")
	}

	log.Debug().Str("command", execStr).Strs("argv", tokenArgs).Msg("executing zapscript command")

	ctx, cancel := context.WithTimeout(context.Background(), ExecuteTimeout)
	defer cancel()

	//nolint:gosec // Config-defined controls are trusted executable config; other sources require IsExecuteAllowed.
	execCmd := exec.CommandContext(ctx, tokenArgs[0], tokenArgs[1:]...)

	var stderr bytes.Buffer
	execCmd.Stderr = &stderr

	if env.ExprEnv != nil {
		envJSON, err := json.Marshal(env.ExprEnv)
		if err != nil {
			log.Warn().Err(err).Msg("failed to marshal expression env to JSON")
		} else {
			execCmd.Env = append(os.Environ(), "ZAPAROO_ENVIRONMENT="+string(envJSON))
		}
	}

	if err := command.RunInJob(execCmd); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			log.Debug().Str("stderr", stderrStr).Msg("execute command stderr")
		}
		return platforms.CmdResult{}, executeError(ctx.Err(), err)
	}
	return platforms.CmdResult{}, nil
}

// executeError classifies a failed execute command from its timeout
// context's error and the error running it.
func executeError(ctxErr, runErr error) error {
	var exitErr *exec.ExitError
	exited := errors.As(runErr, &exitErr)
	// A process that never started has no exit status, so a failure to start
	// stays a fault even when the deadline has also passed.
	ran := exited || runErr == nil || errors.Is(runErr, context.DeadlineExceeded)
	if errors.Is(ctxErr, context.DeadlineExceeded) && ran {
		if runErr == nil {
			return fmt.Errorf("%w after %v", ErrExecuteTimeout, ExecuteTimeout)
		}
		// The cause is kept as text: wrapping a context error would class
		// the timeout as a cancellation.
		return fmt.Errorf("%w after %v: %s", ErrExecuteTimeout, ExecuteTimeout, runErr.Error())
	}
	if exited {
		return fmt.Errorf("%w: %w", ErrExecuteExitStatus, runErr)
	}
	return fmt.Errorf("failed to execute command: %w", runErr)
}
