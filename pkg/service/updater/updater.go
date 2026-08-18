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
	"regexp"
	"runtime"
	"sync/atomic"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/tlsroots"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/restart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/rs/zerolog/log"
)

const updateURL = "https://updates.zaparoo.org/"

var (
	ErrDevelopmentVersion = errors.New("update check skipped for development version")
	ErrUpdateInProgress   = errors.New("update already in progress")
	applyMu               syncutil.Mutex
	applyInProgress       atomic.Bool
)

// Options describes the device an update is being resolved for.
type Options struct {
	UserDB     UpdateBackupper
	PlatformID string
	Channel    string
	DataDir    string
}

type Result struct {
	CurrentVersion  string
	LatestVersion   string
	ReleaseNotes    string
	UpdateAvailable bool
}

// session is one update operation's updater and the transport backing it.
type session struct {
	updater *selfupdate.Updater
	source  *verifiedSource
	// close releases the transport's pooled connections. Callers must defer it:
	// each operation builds a fresh transport, so without it the keep-alive
	// connections and their goroutines outlive the operation until the idle
	// timeout expires.
	close func()
	repo  selfupdate.Repository
}

func makeUpdater(opts Options) (*session, error) {
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
		transport.CloseIdleConnections()
		return nil, fmt.Errorf("creating updater: %w", err)
	}

	return &session{
		updater: updater,
		source:  source,
		repo:    selfupdate.NewRepositorySlug("ZaparooProject", "zaparoo-core"),
		close:   transport.CloseIdleConnections,
	}, nil
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

	s, err := makeUpdater(opts)
	if err != nil {
		return nil, err
	}
	defer s.close()

	release, found, err := s.updater.DetectLatest(ctx, s.repo)
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
	if !applyInProgress.CompareAndSwap(false, true) {
		return "", ErrUpdateInProgress
	}
	defer applyInProgress.Store(false)

	if opts.UserDB == nil {
		return "", errors.New("applying an update needs the user database")
	}
	if opts.DataDir == "" {
		return "", errors.New("applying an update needs the platform data directory")
	}

	applyMu.Lock()
	defer applyMu.Unlock()

	if err := ensureNoPendingUpdate(opts.DataDir); err != nil {
		return "", err
	}

	s, err := makeUpdater(opts)
	if err != nil {
		return "", err
	}
	defer s.close()

	release, found, err := s.updater.DetectLatest(ctx, s.repo)
	if err != nil {
		return "", fmt.Errorf("detecting the release to apply: %w", err)
	}
	if !found || !release.GreaterThan(config.AppVersion) {
		return "", fmt.Errorf("%w: running %s", ErrNotAnUpgrade, config.AppVersion)
	}

	manifestRelease, err := s.source.releaseForVersion(release.Version())
	if err != nil {
		return "", err
	}
	targetPath, err := restart.BinaryPath()
	if err != nil {
		return "", fmt.Errorf("resolving the binary to update: %w", err)
	}
	staged, err := Stage(ctx, &StageOptions{
		Release:        manifestRelease,
		PlatformID:     opts.PlatformID,
		Arch:           runtime.GOARCH,
		OS:             runtime.GOOS,
		TargetPath:     targetPath,
		StagingRoot:    stagingRootFor(opts.DataDir),
		CurrentVersion: config.AppVersion,
	})
	if err != nil {
		return "", fmt.Errorf("staging update: %w", err)
	}

	if err := installStaged(ctx, &installOptions{
		Staged:             staged,
		UserDB:             opts.UserDB,
		TargetPath:         targetPath,
		DataDir:            opts.DataDir,
		PreviousVersion:    config.AppVersion,
		PlatformID:         opts.PlatformID,
		ManifestGeneration: s.source.manifestGeneration(),
		Trigger:            triggerManual,
	}); err != nil {
		return "", fmt.Errorf("installing update: %w", err)
	}

	return staged.Version, nil
}

func ensureNoPendingUpdate(dataDir string) error {
	markerMu.Lock()
	defer markerMu.Unlock()

	m, err := loadMarker(stateDirFor(dataDir))
	if err != nil {
		return fmt.Errorf("checking for an unresolved update: %w", err)
	}
	if m != nil {
		return fmt.Errorf("an update to %s is still unresolved", m.TargetVersion)
	}
	return nil
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
