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

package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/tlsroots"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/rs/zerolog/log"
)

const updateURL = "https://updates.zaparoo.org/"

var ErrDevelopmentVersion = errors.New("update check skipped for development version")

// Options describes the device an update is being resolved for.
type Options struct {
	// PlatformID is the platform half of the archive name.
	PlatformID string
	// Channel is stable or beta.
	Channel string
	// DataDir is the platform data directory. The generation watermark and the
	// verified manifest cache live in a subdirectory of it. An empty value
	// disables both: checks still work and are still verified, they just lose
	// replay protection and re-download the manifest each time.
	DataDir string
}

type Result struct {
	CurrentVersion  string
	LatestVersion   string
	ReleaseNotes    string
	UpdateAvailable bool
}

func makeUpdater(opts Options) (*selfupdate.Updater, selfupdate.Repository, error) {
	// tlsroots hands back a transport this updater owns outright, so setting the
	// header timeout here does not affect anything else in the process.
	transport := tlsroots.Transport(nil)
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	key := &keyRef{}

	source := &verifiedSource{
		baseURL:    updateURL,
		transport:  transport,
		stateDir:   stateDirFor(opts.DataDir),
		platformID: opts.PlatformID,
		goarch:     runtime.GOARCH,
		key:        key,
		verify:     otameta.Verify,
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:     source,
		Validator:  newSignedChecksumValidator(key),
		Filters:    []string{assetFilter(opts.PlatformID, runtime.GOARCH)},
		Prerelease: opts.Channel == config.UpdateChannelBeta,
	})
	if err != nil {
		return nil, selfupdate.RepositorySlug{}, fmt.Errorf("creating updater: %w", err)
	}

	repo := selfupdate.NewRepositorySlug("ZaparooProject", "zaparoo-core")
	return updater, repo, nil
}

// assetFilter is the pattern go-selfupdate matches asset names against. The
// source has already reduced each release to the one archive that belongs to
// this device, so this is a backstop rather than the selection. The trailing
// separator is what it adds: without it "linux_arm" prefix-matches an
// "linux_arm64" archive.
func assetFilter(platformID, goarch string) string {
	return fmt.Sprintf("^zaparoo-%s_%s-", regexp.QuoteMeta(platformID), regexp.QuoteMeta(goarch))
}

func Check(ctx context.Context, opts Options) (*Result, error) {
	if config.IsDevelopmentVersion() {
		return nil, ErrDevelopmentVersion
	}

	updater, repo, err := makeUpdater(opts)
	if err != nil {
		return nil, err
	}

	release, found, err := updater.DetectLatest(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("detecting latest release: %w", err)
	}

	result := &Result{
		CurrentVersion: config.AppVersion,
	}

	if found {
		result.LatestVersion = release.Version()
		result.UpdateAvailable = release.GreaterThan(config.AppVersion)
		result.ReleaseNotes = release.ReleaseNotes
	}

	return result, nil
}

func Apply(ctx context.Context, opts Options) (string, error) {
	if config.IsDevelopmentVersion() {
		return "", ErrDevelopmentVersion
	}

	u, repo, err := makeUpdater(opts)
	if err != nil {
		return "", err
	}

	// When running as a daemon subprocess, the binary is a temp copy and
	// os.Executable() would point to it. ZAPAROO_APP holds the path to
	// the original binary that should be updated instead.
	var release *selfupdate.Release
	if appPath := os.Getenv(config.AppEnv); appPath != "" {
		release, err = u.UpdateCommand(ctx, appPath, config.AppVersion, repo)
	} else {
		release, err = u.UpdateSelf(ctx, config.AppVersion, repo)
	}
	if err != nil {
		return "", fmt.Errorf("applying update: %w", err)
	}

	return release.Version(), nil
}

// CheckFn is the signature for a function that checks for updates.
type CheckFn func(ctx context.Context, opts Options) (*Result, error)

// CheckAndNotify checks for updates and posts an inbox message if one is
// available. Intended to be called as a fire-and-forget goroutine on startup.
func CheckAndNotify(
	ctx context.Context,
	cfg *config.Instance,
	opts Options,
	inboxSvc *inbox.Service,
	waitFn func(context.Context, int) bool,
	checkFn CheckFn,
	managedInstall bool,
) {
	if !cfg.AutoUpdate(!managedInstall) {
		log.Debug().Msg("auto-update disabled, skipping update check")
		return
	}

	if !waitFn(ctx, 30) {
		log.Warn().Msg("no internet connectivity, skipping update check")
		return
	}
	if ctx.Err() != nil {
		return
	}

	opts.Channel = cfg.UpdateChannel()
	result, err := checkFn(ctx, opts)
	if errors.Is(err, ErrDevelopmentVersion) {
		log.Debug().Msg("development version, skipping update check")
		return
	}
	if err != nil {
		log.Warn().Err(err).Msg("update check failed")
		return
	}

	if !result.UpdateAvailable {
		log.Debug().
			Str("current", result.CurrentVersion).
			Str("latest", result.LatestVersion).
			Msg("no update available")
		return
	}
	if ctx.Err() != nil {
		return
	}

	log.Info().
		Str("current", result.CurrentVersion).
		Str("latest", result.LatestVersion).
		Msg("update available")

	title := fmt.Sprintf("Zaparoo %s is available", result.LatestVersion)
	body := fmt.Sprintf(
		"Currently on %s. Use the App or TUI to update.",
		result.CurrentVersion,
	)

	if err := inboxSvc.Add(
		title,
		inbox.WithBody(body),
		inbox.WithCategory(inbox.CategoryUpdateAvailable),
		inbox.WithSeverity(inbox.SeverityInfo),
	); err != nil {
		log.Error().Err(err).Msg("failed to add update inbox message")
	}
}
