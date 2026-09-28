/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package service

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/rs/zerolog/log"
)

// failedStateShutdownTimeout bounds the HTTP shutdown when leaving the failed
// state. Nothing else is running, so this only has to outlast in-flight page
// requests.
const failedStateShutdownTimeout = 30 * time.Second

// startupFailureError is a startup error that Core can survive. It carries the
// pieces needed to stay alive and explain itself: the bound listener, the
// service state that owns the shutdown context, and the wording to show.
//
// It is an error so it travels the existing return path, which matters because
// the updater gets first refusal on a failed start and must keep doing so.
//
// Every failure that ends startup after the listener is bound becomes one of
// these, whether or not Core will stay up for it, because the listener has to
// be given back either way. startingAgainCanHelp is the only thing that
// decides which happens, so headline and detail are supplied everywhere rather
// than at whichever call sites currently qualify.
type startupFailureError struct {
	startup      *api.StartupServer
	state        *state.State
	platform     platforms.Platform
	err          error
	headline     string
	detail       string
	stopPlatform bool
}

func (f *startupFailureError) Error() string {
	return f.err.Error()
}

func (f *startupFailureError) Unwrap() error {
	return f.err
}

func newStartupFailure(
	pl platforms.Platform,
	st *state.State,
	startup *api.StartupServer,
	stopPlatform bool,
	headline, detail string,
	err error,
) error {
	return &startupFailureError{
		platform:     pl,
		state:        st,
		startup:      startup,
		stopPlatform: stopPlatform,
		headline:     headline,
		detail:       detail,
		err:          err,
	}
}

// startingAgainCanHelp reports whether a supervisor restarting this process is
// a plausible recovery for the failure.
//
// Only the schema-ahead refusal is certain that it is not: the file on disk is
// the same on the next attempt. A data directory whose mount is not up yet, a
// busy file, a platform dependency still starting — those do come good on
// their own, and the service units retry every five seconds until they do.
// Holding the port to show a page instead would trade a recovery that works
// for one nobody is watching.
//
// pkg/cli's ExitCodeFor answers the same question for the exit status and is
// keyed off the same sentinel. It cannot be called from here, because pkg/cli
// imports this package.
func (f *startupFailureError) startingAgainCanHelp() bool {
	return !errors.Is(f.err, database.ErrSchemaAhead)
}

// release gives the bound listener back when Core is going to exit after all.
// The listener was taken before the databases opened, so nothing else frees it
// on a path that ends startup.
func (f *startupFailureError) release() {
	if f.stopPlatform {
		if stopErr := f.platform.Stop(); stopErr != nil {
			log.Warn().Err(stopErr).Msg("error stopping platform after a failed start")
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), failedStateShutdownTimeout)
	defer cancel()
	if shutdownErr := f.startup.Shutdown(shutdownCtx); shutdownErr != nil {
		log.Warn().Err(shutdownErr).Msg("error shutting down startup server")
	}

	f.state.StopService()
}

// enter keeps the process alive after a startup failure a person has to
// resolve. The listener is already bound, so the startup page and /health can
// say what happened. Nothing else runs: no readers, no token processing, no
// launching, no database.
//
// Exiting instead is what produced both halves of the reported problem — a
// supervisor restarting every five seconds and burying the one line that
// explains it, or on MiSTer, where nothing supervises the service at all, a
// process that is simply gone by the time anyone looks.
func (f *startupFailureError) enter() (*StartResult, error) {
	// Before the copy, not after it: the copy is the file support asks for, so
	// it has to contain the explanation. The interrupted-backup path does not
	// log on its own, so without this that one failure reaches storage with
	// nothing in the log saying what happened.
	log.Error().Err(f.err).Msg("startup failed; staying up to report it")

	logPath := helpers.PersistLog(f.platform)
	f.startup.SetFailed(f.headline, f.detail, logPath)

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-f.state.GetContext().Done()

		if f.stopPlatform {
			if stopErr := f.platform.Stop(); stopErr != nil {
				log.Warn().Err(stopErr).Msg("error stopping platform from failed state")
			}
		}

		shutdownCtx, cancel := context.WithTimeout(context.Background(), failedStateShutdownTimeout)
		defer cancel()
		if shutdownErr := f.startup.Shutdown(shutdownCtx); shutdownErr != nil {
			log.Warn().Err(shutdownErr).Msg("error shutting down startup server")
		}
		log.Info().Msg("failed state stopped")
	}()

	stopContext := func(ctx context.Context) error {
		f.state.StopService()
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return &StartResult{
		Stop:             func() error { return stopContext(context.Background()) },
		StopContext:      stopContext,
		Done:             done,
		RestartRequested: func() bool { return false },
		Err:              func() error { return nil },
	}, nil
}

// describeDatabaseStartupFailure turns a database open failure into wording a
// user can act on.
//
// The schema-ahead case is the one worth wording carefully. It happens when
// somebody replaces the installed binary with an older one by hand, which is
// how testers move between a beta and a stable release, and the refusal is
// deliberate: the media database is rebuilt in the same situation because a
// reindex reconstructs it, while history, mappings and profiles have no such
// source. What was missing was any way for a user to learn that, and what to
// do next.
func describeDatabaseStartupFailure(pl platforms.Platform, err error) (headline, detail string) {
	if !errors.Is(err, database.ErrSchemaAhead) {
		return "Zaparoo could not start", "Zaparoo could not open its databases."
	}

	headline = "Zaparoo cannot open your saved data"

	wroteIt, known := userdb.SchemaProvenance(
		filepath.Join(helpers.DataDir(pl), config.UserDbFile),
	)
	if !known {
		// Databases last written by a build from before the version was
		// recorded cannot name it, so say the part that is still true.
		return headline, "Your history, mappings and profiles were upgraded by a newer version " +
			"of Zaparoo than the one installed here (v" + config.AppVersion + "), and an older " +
			"version cannot read them. Reinstalling the newer version will start Zaparoo again. " +
			"Nothing has been changed or deleted."
	}

	return headline, "Your history, mappings and profiles were upgraded by Zaparoo v" + wroteIt +
		", and this version (v" + config.AppVersion + ") cannot read them. Reinstall v" + wroteIt +
		" to start Zaparoo again. Nothing has been changed or deleted."
}
