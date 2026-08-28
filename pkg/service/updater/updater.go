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
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/tlsroots"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/restart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	updateURL = "https://updates.zaparoo.org/"

	// updateOwner and updateRepo name the path the manifest is published under.
	updateOwner = "ZaparooProject"
	updateRepo  = "zaparoo-core"
)

var (
	ErrDevelopmentVersion    = errors.New("update check skipped for development version")
	ErrUpdateInProgress      = errors.New("update already in progress")
	errAutoInstallIneligible = errors.New("automatic update install is not eligible")
	applyMu                  syncutil.Mutex
	applyInProgress          atomic.Bool
	eligibilityPreflight     platformPreflightCache
)

type platformPreflightCache struct {
	results map[string]error
	mu      syncutil.Mutex
}

func (c *platformPreflightCache) check(fs afero.Fs, goos, targetPath string) error {
	key := goos + "\x00" + targetPath
	c.mu.Lock()
	defer c.mu.Unlock()

	if result, ok := c.results[key]; ok {
		return result
	}
	result := preflightPlatform(fs, goos, targetPath)
	if c.results == nil {
		c.results = make(map[string]error)
	}
	c.results[key] = result
	return result
}

// Eligibility says whether this device can take an OTA update at all, ahead of
// any question about whether one is available.
const (
	// EligibilityEligible means OTA updates work here.
	EligibilityEligible = "eligible"
	// EligibilityDevelopment means this is a build from source, which has no
	// release to compare itself against.
	EligibilityDevelopment = "development"
	// EligibilityManaged means a package manager owns this install and should
	// be the one updating it.
	EligibilityManaged = "managed"
	// EligibilityUnsupported means this install cannot replace its own binary,
	// so the update has to come from wherever it was installed from. On Windows
	// that is an install directory this process cannot write to.
	EligibilityUnsupported = "unsupported"
)

// Options describes the device an update is being resolved for.
//
//nolint:govet // Field order groups updater dependencies and device settings.
type Options struct {
	UserDB UpdateBackupper
	// Progress is called as the update moves through its stages, when the
	// caller wants to follow along. Nil reports nothing.
	Progress ProgressFn
	// PreQuiesce runs at the last moment an install can still be called off.
	// The second power check goes here.
	PreQuiesce func(context.Context) error
	// Gate is what the device is busy with. A check uses it to report what
	// would stop an update going ahead right now; nil reports nothing.
	Gate       *GateDeps
	Payload    []updatepayload.File
	PlatformID string
	Channel    string
	DataDir    string
	// DeviceID is this device's identifier, used to work out whether a staged
	// rollout has reached it yet. Empty means only fully released versions
	// count as rolled out.
	DeviceID string
	// Mode is who asked. It decides how the install is recorded and, for
	// automatic installs, that nothing may be forced.
	Mode Mode
	// Managed says a package manager owns this install, which the check
	// reports as the reason OTA updates do not apply here.
	Managed bool
}

// OutcomeReport is how the last update finished, for a client that was not
// connected when it happened. The confirm and rollback stages run on the boot
// after the restart, before any client is back, so this is the only way they
// are ever seen.
type OutcomeReport struct {
	At          time.Time `json:"at"`
	Outcome     string    `json:"outcome"`
	FromVersion string    `json:"fromVersion,omitempty"`
	ToVersion   string    `json:"toVersion,omitempty"`
	Detail      string    `json:"detail,omitempty"`
}

type Result struct {
	CheckedAt        time.Time
	DeferredSince    time.Time
	LastResult       *OutcomeReport
	Eligibility      string
	ReleaseNotes     string
	Channel          string
	LatestVersion    string
	DeferredReason   string
	BlockedReason    string
	BlockedMessage   string
	CurrentVersion   string
	UpdateAvailable  bool
	RolloutHeld      bool
	BlockedForceable bool
}

// session is one update operation's source and the transport backing it.
type session struct {
	source *verifiedSource
	// close releases the transport's pooled connections. Callers must defer it:
	// each operation builds a fresh transport, so without it the keep-alive
	// connections and their goroutines outlive the operation until the idle
	// timeout expires.
	close func()
}

func makeUpdater(opts Options) (*session, error) { //nolint:gocritic // hugeParam
	// tlsroots hands back a transport this updater owns outright, so setting the
	// header timeout here does not affect anything else in the process.
	transport := tlsroots.Transport(nil)
	transport.ResponseHeaderTimeout = responseHeaderTimeout

	return &session{
		source: &verifiedSource{
			baseURL:    updateURL,
			transport:  transport,
			stateDir:   stateDirFor(opts.DataDir),
			platformID: opts.PlatformID,
			goarch:     runtime.GOARCH,
			verify:     otameta.Verify,
		},
		close: transport.CloseIdleConnections,
	}, nil
}

// latestRelease fetches the verified manifest and returns the release this
// device should be offered, or nil when there is none.
func (s *session) latestRelease(ctx context.Context, channel string) (*otameta.Release, error) {
	if err := s.source.load(ctx, updateOwner, updateRepo); err != nil {
		return nil, fmt.Errorf("reading update metadata: %w", err)
	}
	release, err := s.source.selectRelease(channel)
	if err != nil {
		return nil, fmt.Errorf("selecting the latest release: %w", err)
	}
	return release, nil
}

// newerThanCurrent reports whether version is an upgrade from the running
// build. A version neither side can read is not an upgrade: the decision that
// replaces the binary is not one to make on a guess.
func newerThanCurrent(version string) (bool, error) {
	candidate, err := semver.NewVersion(version)
	if err != nil {
		return false, fmt.Errorf("reading the offered version %q: %w", version, err)
	}
	current, err := semver.NewVersion(config.AppVersion)
	if err != nil {
		return false, fmt.Errorf("reading the running version %q: %w", config.AppVersion, err)
	}
	return candidate.GreaterThan(current), nil
}

func Check(ctx context.Context, opts Options) (*Result, error) { //nolint:gocritic // hugeParam
	if config.IsDevelopmentVersion() {
		return nil, ErrDevelopmentVersion
	}

	s, err := makeUpdater(opts)
	if err != nil {
		return nil, err
	}
	defer s.close()

	release, err := s.latestRelease(ctx, opts.Channel)
	if err != nil {
		return nil, err
	}

	stateDir := stateDirFor(opts.DataDir)
	result := &Result{
		CurrentVersion: config.AppVersion,
		Channel:        opts.Channel,
		Eligibility:    eligibilityFor(&opts),
		CheckedAt:      time.Now().UTC(),
		LastResult:     lastOutcome(stateDir),
	}

	if release == nil {
		return result, nil
	}

	version := otameta.VersionFromTag(release.TagName)
	upgrade, err := newerThanCurrent(version)
	if err != nil {
		return nil, err
	}
	result.LatestVersion = version
	if err := clearDeferralForRelease(stateDir, result.LatestVersion); err != nil {
		log.Warn().Err(err).Msg("could not clear deferral for a superseded update")
	}
	result.UpdateAvailable = upgrade
	result.ReleaseNotes = release.ReleaseNotes

	if result.UpdateAvailable {
		result.RolloutHeld = rolloutHeld(opts.DeviceID, release)
		noteGate(ctx, &opts, result, stateDir, version)
		if deferral := peekDeferral(stateDir, version); deferral != nil {
			result.DeferredReason = deferral.Reason
			result.DeferredSince = deferral.Since
		}
	}

	return result, nil
}

func clearDeferralForRelease(stateDir, version string) error {
	deferral := peekDeferralState(stateDir)
	if deferral == nil || deferral.Version == version {
		return nil
	}
	return clearDeferral(stateDir, deferral.Version)
}

// noteGate records what is currently in the way of installing a version. A
// signal that will pass on its own — someone playing a game, a busy API — also
// starts the clock an automatic install eventually runs out of patience with,
// so it is written to disk; the rest is only reported.
//
// This only ever reports, so the gate is asked with nothing to acquire: a check
// must not stop the user launching something while it answers.
func noteGate(ctx context.Context, opts *Options, result *Result, stateDir, version string) {
	if opts.Gate == nil {
		return
	}
	reporting := *opts.Gate
	reporting.AcquireRestore = nil
	reporting.AcquireMediaGate = nil
	decision, err := CanApplyUpdate(ctx, &reporting, ModeManual, false)
	if err != nil {
		log.Warn().Err(err).Msg("could not read what is in the way of an update")
		return
	}
	decision.Release()
	if decision.OK {
		return
	}
	result.BlockedReason = decision.Reason
	result.BlockedMessage = decision.Message
	result.BlockedForceable = decision.Forceable
	if !decision.Expires {
		return
	}
	now := time.Now().UTC()
	if reporting.Now != nil {
		now = reporting.Now().UTC()
	}
	if err := recordDeferralAt(stateDir, version, decision.Reason, now); err != nil {
		log.Warn().Err(err).Msg("could not record why an update is waiting")
	}
}

// installAdvice says how this device gets a new release. A package manager
// reconciles the files it owns against its own index, so an update installed
// behind its back is undone the next time it runs — pointing someone at an
// update button there would waste their time and confuse them when the version
// went backwards.
func installAdvice(platformID string, managed bool) string {
	if !managed {
		return "Use the App or TUI to update."
	}
	switch platformID {
	case platformids.Mister:
		return "Run update_all to install it."
	case platformids.Batocera:
		return "Install it through the Batocera package manager."
	default:
		return "Your package manager installs updates on this device."
	}
}

// eligibilityFor says whether OTA updates apply to this install at all.
func eligibilityFor(opts *Options) string {
	switch {
	case config.IsDevelopmentVersion():
		return EligibilityDevelopment
	case eligibilityPreflight.check(afero.NewOsFs(), runtime.GOOS, currentBinaryPath()) != nil:
		// Checked before Managed because this one is a refusal Apply enforces,
		// while Managed only says the package manager should be doing it.
		return EligibilityUnsupported
	case opts.Managed:
		return EligibilityManaged
	default:
		return EligibilityEligible
	}
}

// currentBinaryPath resolves the executable an update would replace, or an
// empty string when it cannot be resolved. Eligibility is only advice, so a
// path this build cannot work out is left for Apply to report properly.
func currentBinaryPath() string {
	path, err := restart.BinaryPath()
	if err != nil {
		log.Debug().Err(err).Msg("could not resolve the binary an update would replace")
		return ""
	}
	return path
}

// rolloutHeld reports whether the selected release's staged rollout has not
// reached this device yet. Using the selected release matters when channels
// contain semver-equal tags with different rollout percentages.
func autoInstallReleaseAllowed(opts *Options, release *otameta.Release) error {
	if opts == nil || opts.Mode != ModeAuto {
		return nil
	}
	if opts.Managed {
		return fmt.Errorf("%w: install is managed by a package manager", errAutoInstallIneligible)
	}
	if rolloutHeld(opts.DeviceID, release) {
		return fmt.Errorf("%w: release is not rolled out to this device", errAutoInstallIneligible)
	}
	if version := releaseVersion(release); rolledBackHere(opts.DataDir, version) {
		return fmt.Errorf("%w: %s already failed to start on this device",
			errAutoInstallIneligible, version)
	}
	return nil
}

func releaseVersion(release *otameta.Release) string {
	if release == nil {
		return ""
	}
	return otameta.VersionFromTag(release.TagName)
}

// rolledBackHere reports whether this exact version was already installed here
// and had to be rolled back.
//
// Nothing else declines it. Without this an automatic install repeats the whole
// download, snapshot, swap and restore on every check for as long as the bad
// release stays published, which on a device whose owner is not watching is a
// loop nobody sees: the inbox keeps one row per category, so the tenth failure
// looks exactly like the first.
//
// Only automatic installs are declined. A person asking for it again is asking
// on purpose, and a later version is a different release that has not failed.
func rolledBackHere(dataDir, version string) bool {
	if dataDir == "" || version == "" {
		return false
	}
	last := lastOutcome(stateDirFor(dataDir))
	if last == nil {
		return false
	}
	return last.Outcome == string(outcomeRolledBack) && last.ToVersion == version
}

// PreviouslyRolledBack reports the same refusal as the one Apply enforces, from
// a check result a caller already has. Apply is the authority; this lets a
// scheduler skip the work instead of arranging an install that will be refused.
func PreviouslyRolledBack(result *Result) bool {
	return result != nil && result.LastResult != nil &&
		result.LastResult.Outcome == string(outcomeRolledBack) &&
		result.LastResult.ToVersion == result.LatestVersion
}

func rolloutHeld(deviceID string, release *otameta.Release) bool {
	if release == nil {
		return false
	}
	return !RolloutEligible(deviceID, release.TagName, release.Rollout)
}

// lastOutcome reports how the previous update finished, whether or not it has
// already been shown. A client asking now was not necessarily the client that
// saw it the first time.
func lastOutcome(dir string) *OutcomeReport {
	stateMu.Lock()
	defer stateMu.Unlock()

	st := loadState(dir)
	if st.LastResult == nil {
		return nil
	}
	return &OutcomeReport{
		At:          st.LastResult.At,
		Outcome:     string(st.LastResult.Outcome),
		FromVersion: st.LastResult.FromVersion,
		ToVersion:   st.LastResult.ToVersion,
		Detail:      st.LastResult.Detail,
	}
}

func Apply(ctx context.Context, opts Options) (string, error) { //nolint:gocritic // hugeParam
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

	trigger := triggerManual
	if opts.Mode == ModeAuto {
		trigger = triggerAuto
	}
	report := newProgressReporter(opts.Progress, trigger)
	// Anything that stops the update from here on is worth telling the client
	// about, whatever stage it happened at.
	fail := func(err error) (string, error) {
		report.failed(err)
		return "", err
	}

	if err := ensureNoPendingUpdate(opts.DataDir); err != nil {
		return fail(err)
	}

	s, err := makeUpdater(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	report.stage(ProgressChecking)
	manifestRelease, err := s.latestRelease(ctx, opts.Channel)
	if err != nil {
		return fail(err)
	}
	if manifestRelease == nil {
		return fail(fmt.Errorf("%w: running %s", ErrNotAnUpgrade, config.AppVersion))
	}
	version := otameta.VersionFromTag(manifestRelease.TagName)
	upgrade, err := newerThanCurrent(version)
	if err != nil {
		return fail(err)
	}
	if !upgrade {
		return fail(fmt.Errorf("%w: running %s", ErrNotAnUpgrade, config.AppVersion))
	}
	if eligibilityErr := autoInstallReleaseAllowed(&opts, manifestRelease); eligibilityErr != nil {
		return fail(eligibilityErr)
	}
	report.setVersion(version)

	targetPath, err := restart.BinaryPath()
	if err != nil {
		return fail(fmt.Errorf("resolving the binary to update: %w", err))
	}
	// Checked here rather than at the top of Apply so that a device already on
	// the newest version is told that, instead of being told its platform is
	// unsupported. Everything below this point costs the user something the
	// install can never spend well.
	if platformErr := preflightPlatform(afero.NewOsFs(), runtime.GOOS, targetPath); platformErr != nil {
		return fail(platformErr)
	}
	stagingRoot := stagingRootFor(opts.DataDir)
	if spaceErr := preflightSpace(&opts, manifestRelease, targetPath, stagingRoot); spaceErr != nil {
		return fail(spaceErr)
	}
	stageOpts := &StageOptions{
		Release:        manifestRelease,
		PlatformID:     opts.PlatformID,
		Arch:           runtime.GOARCH,
		OS:             runtime.GOOS,
		TargetPath:     targetPath,
		StagingRoot:    stagingRoot,
		CurrentVersion: config.AppVersion,
		progress:       report,
	}
	stageOpts.payload = updatePayloadFiles(&opts)
	staged, err := Stage(ctx, stageOpts)
	if err != nil {
		return fail(fmt.Errorf("staging update: %w", err))
	}

	if err := installStaged(ctx, &installOptions{
		Staged:             staged,
		UserDB:             opts.UserDB,
		TargetPath:         targetPath,
		DataDir:            opts.DataDir,
		PreviousVersion:    config.AppVersion,
		PlatformID:         opts.PlatformID,
		ManifestGeneration: s.source.manifestGeneration(),
		Trigger:            trigger,
		PreQuiesce:         opts.PreQuiesce,
		progress:           report,
	}); err != nil {
		return fail(fmt.Errorf("installing update: %w", err))
	}

	// The version on offer has been taken, so nothing is waiting on it any
	// more. A failure to say so is not worth failing an installed update over.
	if err := clearDeferral(stateDirFor(opts.DataDir), staged.Version); err != nil {
		log.Warn().Err(err).Msg("could not clear the recorded update deferral")
	}

	report.stage(ProgressRestarting)
	return staged.Version, nil
}

// preflightSpace sizes the update from the verified manifest and refuses one
// that cannot fit. The asset lookup here is a size lookup only: Stage repeats it
// along with the version and upgrade-floor checks, so a selection failure is
// left for Stage to report with its own error rather than reported twice.
func updatePayloadFiles(opts *Options) []updatepayload.File {
	if opts == nil || opts.Managed {
		return nil
	}
	return append([]updatepayload.File(nil), opts.Payload...)
}

func preflightSpace(opts *Options, release *otameta.Release, targetPath, stagingRoot string) error {
	asset, err := otameta.SelectAsset(release, opts.PlatformID, runtime.GOARCH)
	if err != nil {
		return nil //nolint:nilerr // Stage reports this properly a moment later
	}
	payloadSize := int64(0)
	if len(updatePayloadFiles(opts)) > 0 {
		payloadSize = maxStagedPayloadBytes
	}
	return checkFreeSpace(&spaceNeeds{
		archiveSize: asset.Size,
		payloadSize: payloadSize,
		targetPath:  targetPath,
		stagingRoot: stagingRoot,
		userDBPath:  filepath.Join(opts.DataDir, config.UserDbFile),
	})
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

// CheckAndNotify checks for updates and posts a version-deduplicated inbox
// message when one is available. The service scheduler calls it periodically.
func CheckAndNotify(
	ctx context.Context,
	cfg *config.Instance,
	opts Options, //nolint:gocritic // hugeParam
	inboxSvc *inbox.Service,
	waitFn func(context.Context, int) bool,
	checkFn CheckFn,
	managedInstall bool,
) {
	if !cfg.UpdateCheck() {
		log.Debug().Msg("update checking is off, skipping update check")
		return
	}

	if !waitFn(ctx, 30) {
		log.Warn().Msg("no internet connectivity, skipping update check")
		if err := recordScheduledCheck(stateDirFor(opts.DataDir), false); err != nil {
			log.Warn().Err(err).Msg("could not record failed scheduled update check")
		}
		return
	}
	if ctx.Err() != nil {
		return
	}

	opts.Channel = cfg.UpdateChannel()
	opts.DeviceID = cfg.DeviceID()
	opts.Managed = managedInstall
	result, err := checkFn(ctx, opts)
	if errors.Is(err, ErrDevelopmentVersion) {
		log.Debug().Msg("development version, skipping update check")
		return
	}
	if err != nil {
		log.Warn().Err(err).Msg("update check failed")
		if stateErr := recordScheduledCheck(stateDirFor(opts.DataDir), false); stateErr != nil {
			log.Warn().Err(stateErr).Msg("could not record failed scheduled update check")
		}
		return
	}
	if stateErr := recordScheduledCheck(stateDirFor(opts.DataDir), true); stateErr != nil {
		log.Warn().Err(stateErr).Msg("could not record successful scheduled update check")
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
	// A release still rolling out has not reached this device. Announcing it
	// anyway would have everyone install on the first day, which is the one
	// thing a staged rollout exists to prevent. Asking for it by hand still
	// works, and update.check says why it is being held back.
	if result.RolloutHeld {
		log.Debug().
			Str("latest", result.LatestVersion).
			Msg("update is not rolled out to this device yet")
		return
	}

	log.Info().
		Str("current", result.CurrentVersion).
		Str("latest", result.LatestVersion).
		Msg("update available")

	stateDir := stateDirFor(opts.DataDir)
	if lastOfferedVersion(stateDir) == result.LatestVersion {
		return
	}
	title := fmt.Sprintf("Zaparoo %s is available", result.LatestVersion)
	body := fmt.Sprintf(
		"Currently on %s. %s",
		result.CurrentVersion,
		installAdvice(opts.PlatformID, managedInstall),
	)

	if err := inboxSvc.Add(
		title,
		inbox.WithBody(body),
		inbox.WithCategory(inbox.CategoryUpdateAvailable),
		inbox.WithSeverity(inbox.SeverityInfo),
	); err != nil {
		log.Error().Err(err).Msg("failed to add update inbox message")
		return
	}
	if err := recordOfferedVersion(stateDir, result.LatestVersion); err != nil {
		log.Warn().Err(err).Msg("could not record offered update version")
	}
}
