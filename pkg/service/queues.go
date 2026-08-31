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

package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/profiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/google/uuid"
	"github.com/mackerelio/go-osstat/uptime"
	"github.com/rs/zerolog/log"
)

const maxLoggedPlaylistItems = 10

type playlistLogEntry struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Slot          string                   `json:"slot"`
	Items         []playlists.PlaylistItem `json:"items"`
	Total         int                      `json:"total"`
	Showing       int                      `json:"showing"`
	Truncated     int                      `json:"truncated,omitempty"`
	Index         int                      `json:"index"`
	Clear         bool                     `json:"clear,omitempty"`
	Loop          bool                     `json:"loop,omitempty"`
	LoopOne       bool                     `json:"loopOne,omitempty"`
	ForceRelaunch bool                     `json:"forceRelaunch,omitempty"`
	Playing       bool                     `json:"playing"`
}

func playlistForLog(pls *playlists.Playlist) any {
	if pls == nil {
		return nil
	}
	showing := min(len(pls.Items), maxLoggedPlaylistItems)
	return playlistLogEntry{
		ID:            pls.ID,
		Name:          pls.Name,
		Slot:          pls.Slot,
		Index:         pls.Index,
		Playing:       pls.Playing,
		Clear:         pls.Clear,
		Loop:          pls.Loop,
		LoopOne:       pls.LoopOne,
		ForceRelaunch: pls.ForceRelaunch,
		Total:         len(pls.Items),
		Showing:       showing,
		Truncated:     len(pls.Items) - showing,
		Items:         pls.Items[:showing],
	}
}

// isExpectedLaunchError reports whether a token-launch error is an expected
// user/operational condition that should be logged at Warn rather than Error
// (keeping it out of Sentry). These are not bugs: a missing file, a playlist
// control command with nothing playing, a double-tap during an active launch,
// a user-supplied system or command that doesn't exist, a script that doesn't
// parse, or a launch refused by configuration or a hook.
func isExpectedLaunchError(err error) bool {
	return errors.Is(err, zapscript.ErrFileNotFound) ||
		errors.Is(err, zapscript.ErrNoPlaylistActive) ||
		errors.Is(err, zapscript.ErrInvalidScript) ||
		errors.Is(err, zapscript.ErrUnknownCommand) ||
		errors.Is(err, zapscript.ErrCommandBlocked) ||
		errors.Is(err, zapscript.ErrExecuteNotAllowed) ||
		errors.Is(err, zapscript.ErrHTTPNotAllowed) ||
		errors.Is(err, zapscript.ErrRemoteSource) ||
		errors.Is(err, state.ErrLaunchInProgress) ||
		errors.Is(err, state.ErrLaunchBlockedByHook) ||
		errors.Is(err, systemdefs.ErrUnknownSystem) ||
		errors.Is(err, state.ErrRunZapScriptDisabled)
}

func runTokenZapScript(
	svc *ServiceContext,
	token tokens.Token, //nolint:gocritic // single-use parameter in service function
	plsc playlists.PlaylistController,
	exprEnv *gozapscript.ArgExprEnv,
	inHookContext bool,
) error {
	return runTokenZapScriptWithContext(
		svc.State.GetContext(),
		svc,
		token,
		plsc,
		exprEnv,
		inHookContext,
	)
}

func runTokenZapScriptWithContext(
	runCtx context.Context,
	svc *ServiceContext,
	token tokens.Token, //nolint:gocritic // single-use parameter in service function
	plsc playlists.PlaylistController,
	exprEnv *gozapscript.ArgExprEnv,
	inHookContext bool,
) error {
	if !svc.State.RunZapScriptEnabled() {
		log.Warn().Msg("ignoring ZapScript, run ZapScript is disabled")
		return state.ErrRunZapScriptDisabled
	}

	originToken := token
	cmds := token.Commands
	if len(cmds) == 0 {
		mappedValue, hasMapping := getMapping(svc.Config, svc.DB, svc.Platform, token)
		if hasMapping {
			log.Info().Msgf("found mapping: %s", mappedValue)
			token.Text = mappedValue
		}

		reader := gozapscript.NewParser(token.Text)
		script, err := reader.ParseScript()
		if err != nil {
			return fmt.Errorf("failed to parse script: %w: %w", zapscript.ErrInvalidScript, err)
		}
		cmds = script.Cmds
		// script.Traits is deliberately dropped here. Traits are resolved once,
		// where the token entered the system, so running a script cannot change
		// the traits of the token running it. Tokens that reach here without
		// going through that step, such as playlist tracks and hook scripts,
		// inherit from the token they came from instead.
	}

	log.Info().Msgf("running script (%d cmds)", len(cmds))

	currentPrimary := plsc.Active
	currentBackground := plsc.Background
	currentPlaylist := plsc.Current
	if currentPlaylist == nil {
		currentPlaylist = currentPrimary
	}

	switch {
	case originToken.ReaderID != "":
		plsc.HoldToken = &originToken
	case token.Source == tokens.SourcePlaylist && currentPrimary != nil:
		plsc.HoldToken = currentPrimary.HoldToken
	default:
		plsc.HoldToken = nil
	}

	for i := 0; i < len(cmds); i++ {
		cmd := cmds[i]

		// Run before_media_start hook; errors block the launch.
		beforeMediaStartScript := svc.Config.LaunchersBeforeMediaStart()
		if shouldRunBeforeMediaStartHook(inHookContext, beforeMediaStartScript, cmd.Name) {
			log.Info().Msgf("running before_media_start hook: %s", beforeMediaStartScript)
			hookPlsc := playlists.PlaylistController{
				Active:     currentPrimary,
				Background: currentBackground,
				Current:    currentPlaylist,
				Queue:      plsc.Queue,
			}
			hookToken := tokens.Token{
				ScanTime: time.Now(),
				Text:     beforeMediaStartScript,
			}
			launching := buildLaunchingContext(cmd)
			hookEnv := zapscript.GetExprEnv(svc.Platform, svc.Config, svc.State, nil, launching)
			hookErr := runTokenZapScriptWithContext(runCtx, svc, hookToken, hookPlsc, &hookEnv, true)
			if hookErrorBlocks(hookErr) {
				return fmt.Errorf("%w: %w", state.ErrLaunchBlockedByHook, hookErr)
			}
		}

		// The outgoing media's before_exit hook for commands that stop media
		// outright. After before_media_start so a hook that blocks the launch
		// also suppresses the exit, and before any lock on the launch path is
		// taken so the script is free to run its own ZapScript. Launch commands
		// run it from the launch path instead, once the launch is known to be
		// going ahead. Failures never abort the launch or stop.
		if shouldRunBeforeExitHook(inHookContext, cmd) {
			outgoingGen, hadMedia := svc.State.ActiveMediaReadyGeneration()
			svc.State.RunBeforeExitHook()
			// A before_exit script can launch media of its own. Running the
			// stop now would kill what the hook just started instead of what
			// the token asked to stop, so skip the command entirely.
			if commandStopsPrimaryMedia(cmd) &&
				svc.State.ActiveMediaReplacedSince(outgoingGen, hadMedia) {
				log.Info().Str("command", cmd.Name).
					Msg("before_exit replaced the outgoing media, skipping stop")
				continue
			}
		}

		mediaReadyGen, _ := svc.State.ActiveMediaReadyGeneration()

		var cmdEnv gozapscript.ArgExprEnv
		if exprEnv != nil {
			cmdEnv = *exprEnv
		} else {
			cmdEnv = zapscript.GetExprEnv(svc.Platform, svc.Config, svc.State, nil, nil)
		}

		if shouldApplyLaunchOverride(&token, inHookContext, cmd.Name) {
			if pending := svc.State.ConsumePendingLaunchOverride(); pending != nil {
				if launchOverrideExpired(pending.CreatedAt) {
					log.Warn().Str("launcher", pending.LauncherID).
						Msg("discarding expired one-shot launch override")
				} else {
					log.Info().Str("launcher", pending.LauncherID).Msg("applying one-shot launch override")
					cmd.AdvArgs = cmd.AdvArgs.With(gozapscript.KeyLauncher, pending.LauncherID)
				}
			}
		}

		if stopErr := stopNativePlaybackBeforePrimaryCommand(svc, cmd, currentPlaylist); stopErr != nil {
			return stopErr
		}
		if pauseErr := pauseBackgroundForPrimaryLaunch(svc, cmd, currentPlaylist); pauseErr != nil {
			return pauseErr
		}

		result, err := zapscript.RunCommand(
			runCtx,
			svc.Platform, svc.Config,
			playlists.PlaylistController{
				Active:     currentPrimary,
				Background: currentBackground,
				Current:    currentPlaylist,
				HoldToken:  plsc.HoldToken,
				Queue:      plsc.Queue,
			},
			token,
			cmd,
			len(cmds),
			i,
			svc.DB,
			zapscript.RunCommandOptions{
				LauncherManager:    svc.State.LauncherManager(),
				AcquireMediaLaunch: svc.State.AcquireMediaLaunch,
				BeforeExit:         beforeExitCallback(svc, inHookContext),
				WaitForMediaReady: func(ctx context.Context) error {
					return waitForMediaReady(ctx, svc, mediaReadyGen)
				},
				PlaybackManager: svc.PlaybackManager,
				UI:              svc.UI,
			},
			&cmdEnv,
		)
		if err != nil {
			return fmt.Errorf("failed to run zapscript command: %w", err)
		}

		// Background slot commands don't disturb primary media, so they must not
		// clear the primary playlist or replace the hold-mode software token.
		primaryMediaChanged := result.MediaChanged && !commandTargetsBackgroundSlot(cmd)

		if primaryMediaChanged && token.Source != tokens.SourcePlaylist {
			log.Debug().Any("token", token).Msg("cmd launch: clearing current playlist")
			select {
			case plsc.Queue <- nil:
			case <-svc.State.GetContext().Done():
				return errors.New("service shutting down")
			}
		}

		if primaryMediaChanged {
			holdToken := &originToken
			if token.Source == tokens.SourcePlaylist {
				holdToken = plsc.HoldToken
			}
			if holdToken != nil && holdToken.ReaderID != "" {
				r, ok := svc.State.GetReader(holdToken.ReaderID)
				if ok && readers.HasCapability(r, readers.CapabilityRemovable) {
					softwareToken := *holdToken
					log.Debug().Msg("media changed, updating hold owner")
					select {
					case svc.LaunchSoftwareQueue <- &softwareToken:
					case <-svc.State.GetContext().Done():
						return errors.New("service shutting down")
					}
				}
			}
		}

		if result.PlaylistChanged {
			resultSlot := mediaslot.Primary
			if result.Playlist != nil && result.Playlist.Slot != "" {
				var slotErr error
				resultSlot, slotErr = mediaslot.Normalize(result.Playlist.Slot)
				if slotErr != nil {
					return fmt.Errorf("normalize playlist slot: %w", slotErr)
				}
			}
			if resultSlot == mediaslot.Background {
				currentBackground = result.Playlist
			} else {
				currentPrimary = result.Playlist
			}
			if currentPlaylist == nil || currentPlaylist.Slot == resultSlot {
				currentPlaylist = result.Playlist
			}
		}

		if result.ProfileSwitch != nil {
			if profileErr := applyProfileSwitch(svc, result.ProfileSwitch); profileErr != nil {
				return profileErr
			}
		}

		if result.PlaytimeExtension != nil {
			if extendErr := applyPlaytimeExtension(svc, result.PlaytimeExtension, &originToken); extendErr != nil {
				return extendErr
			}
		}

		if result.Unsafe {
			log.Warn().Msg("token has been flagged as unsafe")
			token.Unsafe = true
		}

		// if a command results in additional commands to run (like from a
		// remote query) inject them to be run immediately after this command
		if len(result.NewCommands) > 0 {
			log.Info().Msgf("injecting %d new commands", len(result.NewCommands))
			cmds = injectCommands(cmds, i, result.NewCommands)
		}
	}

	return nil
}

// applyProfileSwitch applies a profile switch requested by a ZapScript
// command. This is the physical-scan path, so activation bypasses any
// profile PIN — possession of the card is the authorization.
func applyProfileSwitch(svc *ServiceContext, req *platforms.ProfileSwitchRequest) error {
	if svc.Profiles == nil {
		return errors.New("profiles service not available")
	}
	if req.Clear {
		if err := svc.Profiles.Deactivate(); err != nil {
			return fmt.Errorf("failed to clear active profile: %w", err)
		}
		return nil
	}
	if _, err := svc.Profiles.ActivateBySwitchID(req.SwitchID); err != nil {
		return fmt.Errorf("failed to switch profile: %w", err)
	}
	return nil
}

// tokenForLog returns a copy of a token with any bearer credential removed,
// for log lines that print the whole token.
func tokenForLog(t *tokens.Token) tokens.Token {
	safe := *t
	safe.Text, safe.Data = zapscript.RedactToken(t.Text, t.Data)
	// The completion is a channel handle, not token content: printing it
	// only puts a heap address in the log.
	safe.Completion = nil
	return safe
}

// cardGrantIdempotencyWindow is how long one scanned extension card counts
// as the same grant. It absorbs reader bounce without turning a deliberate
// second tap minutes later into a no-op.
const cardGrantIdempotencyWindow = 10 * time.Second

// applyPlaytimeExtension grants extra playtime from a scanned card. The
// switch ID on the card is a bearer credential, exactly like a profile
// card: resolving it is the authorization. The difference is that a grant
// weakens somebody's limits, so it additionally requires the credential to
// belong to an administrator profile — a member card grants nothing.
//
// The recipient is never named on the card. It is whichever profile is
// governing playtime when the card is scanned, so a card cannot be aimed at
// a different person's session.
func applyPlaytimeExtension(
	svc *ServiceContext,
	req *platforms.PlaytimeExtensionRequest,
	token *tokens.Token,
) error {
	if svc.Profiles == nil {
		return errors.New("profiles service not available")
	}
	if svc.LimitsManager == nil {
		return errors.New("playtime limits not available")
	}

	profile, err := svc.Profiles.VerifyBySwitchID(req.AuthorizerSwitchID)
	if err != nil {
		return fmt.Errorf("unknown profile switch ID: %w", err)
	}
	if profile.Role != profiles.ProfileRoleAdmin {
		return fmt.Errorf("profile %s is not an administrator", profile.ProfileID)
	}

	grant := &playtime.GrantRequest{
		Source:              "reader",
		AuthorizerProfileID: profile.ProfileID,
		Duration:            req.Duration,
	}
	// A tap is one grant. Reader bounce and a token briefly re-seating both
	// re-fire within a second or two, so a short window collapses them,
	// while a deliberate second tap later still grants again (up to the
	// cumulative session cap). The key is built from the card's identity and
	// what it asked for, never from the credential it carries. Without a UID
	// there is nothing to tell two cards apart, so dedup is skipped rather
	// than risk collapsing distinct cards into one grant.
	if token != nil && token.UID != "" {
		grant.IdempotencyKey = fmt.Sprintf("%s|%s|%s", token.UID, req.Mode, req.Duration)
		grant.IdempotencyWindow = cardGrantIdempotencyWindow
	}
	switch req.Mode {
	case models.PlaytimeExtendModeDuration:
		grant.Mode = playtime.GrantModeDuration
	case models.PlaytimeExtendModeToday:
		grant.Mode = playtime.GrantModeToday
	default:
		return fmt.Errorf("%w: %q", playtime.ErrGrantModeInvalid, req.Mode)
	}

	result, err := svc.LimitsManager.Grant(grant)
	if err != nil {
		return fmt.Errorf("failed to extend playtime: %w", err)
	}

	if result.Replayed {
		return nil
	}

	payload := &models.PlaytimeExtendedParams{
		Mode:      string(result.Mode),
		ProfileID: result.RecipientProfileID,
		GrantedBy: result.AuthorizerProfileID,
	}
	if result.Duration > 0 {
		payload.Duration = result.Duration.String()
	}
	if result.SessionExtension > 0 {
		payload.SessionExtension = result.SessionExtension.String()
	}
	if !result.ExpiresAt.IsZero() {
		payload.Expires = result.ExpiresAt.Format(time.RFC3339)
	}
	notifications.PlaytimeExtended(svc.State.Notifications, payload)

	return nil
}

func stopNativePlaybackBeforePrimaryCommand(
	svc *ServiceContext,
	cmd gozapscript.Command,
	activePlaylist *playlists.Playlist,
) error {
	if svc.PlaybackManager == nil {
		return nil
	}
	stopsPrimaryMedia := cmd.Name == gozapscript.ZapScriptCmdStop ||
		cmd.Name == gozapscript.ZapScriptCmdPlaylistStop
	if !zapscript.IsMediaLaunchingCommand(cmd.Name) && !stopsPrimaryMedia {
		return nil
	}

	slot := cmd.AdvArgs.Get(gozapscript.KeySlot)
	if slot == "" && activePlaylist != nil && activePlaylist.Slot != "" {
		slot = activePlaylist.Slot
	}
	normalizedSlot, err := mediaslot.Normalize(slot)
	if err != nil {
		return fmt.Errorf("normalize media slot: %w", err)
	}
	if normalizedSlot != mediaslot.Primary {
		return nil
	}

	media := svc.State.ActiveMedia()
	if media == nil || media.LauncherID != platforms.NativeAudioLauncherID {
		return nil
	}
	if err := svc.PlaybackManager.Stop(mediaslot.Primary); err != nil {
		return fmt.Errorf("stop native audio before primary command: %w", err)
	}
	svc.State.SetActiveMedia(nil)
	return nil
}

func pauseBackgroundForPrimaryLaunch(
	svc *ServiceContext,
	cmd gozapscript.Command,
	activePlaylist *playlists.Playlist,
) error {
	if svc.PlaybackManager == nil {
		return nil
	}
	if !svc.Config.AudioPauseOnLaunch() {
		return nil
	}
	if !zapscript.IsMediaLaunchingCommand(cmd.Name) {
		return nil
	}

	slot := cmd.AdvArgs.Get(gozapscript.KeySlot)
	if slot == "" && activePlaylist != nil && activePlaylist.Slot != "" {
		slot = activePlaylist.Slot
	}
	normalizedSlot, err := mediaslot.Normalize(slot)
	if err != nil {
		return fmt.Errorf("normalize media slot: %w", err)
	}
	if normalizedSlot != mediaslot.Primary {
		return nil
	}

	if !svc.PlaybackManager.State(mediaslot.Background).Playing {
		return nil
	}

	if err := svc.PlaybackManager.Pause(mediaslot.Background); err != nil {
		return fmt.Errorf("pause background audio before primary launch: %w", err)
	}
	svc.State.SetBackgroundAutoPaused(true)
	return nil
}

func launchPlaylistMedia(
	svc *ServiceContext,
	pls *playlists.Playlist,
	player audio.Player,
) {
	t := tokens.Token{
		Text:     pls.Current().ZapScript,
		ScanTime: time.Now(),
		Source:   tokens.SourcePlaylist,
	}
	plsc := playlists.PlaylistController{
		Active:     svc.State.GetActivePlaylist(),
		Background: svc.State.GetBackgroundPlaylist(),
		Current:    pls,
		HoldToken:  pls.HoldToken,
		Queue:      svc.PlaylistQueue,
	}
	if pls.Slot == mediaslot.Background {
		plsc.Background = pls
	} else {
		plsc.Active = pls
	}

	err := runTokenZapScript(svc, t, plsc, nil, false)
	// ErrRunZapScriptDisabled already logged its own Warn inside
	// runTokenZapScriptWithContext; treat it as the prior silent-no-op
	// success, not a launch failure, so a disabled setting doesn't play a
	// fail sound or record a failed history entry.
	disabled := errors.Is(err, state.ErrRunZapScriptDisabled)
	if err != nil && !disabled {
		if isExpectedLaunchError(err) {
			log.Warn().Err(err).Msgf("error launching token")
		} else {
			log.Error().Err(err).Msgf("error launching token")
		}
		path, enabled := svc.Config.FailSoundPath(helpers.DataDir(svc.Platform))
		helpers.PlayConfiguredSound(player, path, enabled, assets.FailSound, "fail")
	}

	if pls.Slot == mediaslot.Background {
		return
	}

	now := time.Now()
	systemUptime, uptimeErr := uptime.Get()
	if uptimeErr != nil {
		log.Warn().Err(uptimeErr).Msg("failed to get system uptime for history entry, using 0")
		systemUptime = 0
	}
	monotonicStart := int64(systemUptime.Seconds())

	// Never store a bearer credential: history is readable by every client.
	historyText, historyData := zapscript.RedactToken(t.Text, t.Data)

	he := database.HistoryEntry{
		ID:             uuid.New().String(),
		Time:           t.ScanTime,
		Type:           t.Type,
		TokenID:        t.UID,
		TokenValue:     historyText,
		TokenData:      historyData,
		ClockReliable:  helpers.IsClockReliable(now),
		BootUUID:       svc.State.BootUUID(),
		MonotonicStart: monotonicStart,
		CreatedAt:      now,
	}
	he.Success = err == nil || disabled
	err = svc.DB.UserDB.AddHistory(&he)
	if err != nil {
		log.Error().Err(err).Msgf("error adding history")
	}
}

func handlePlaylist(
	svc *ServiceContext,
	pls *playlists.Playlist,
	player audio.Player,
) {
	slot := mediaslot.Primary
	if pls != nil && pls.Slot != "" {
		var err error
		slot, err = mediaslot.Normalize(pls.Slot)
		if err != nil {
			log.Warn().Err(err).Str("slot", pls.Slot).Msg("ignoring playlist update with invalid slot")
			return
		}
	}
	activePlaylist := svc.State.GetActivePlaylist()
	if slot == mediaslot.Background {
		activePlaylist = svc.State.GetBackgroundPlaylist()
	}

	switch {
	case pls == nil || pls.Clear:
		// request to clear playlist
		if activePlaylist != nil {
			log.Info().Str("slot", slot).Msg("clearing playlist")
		}
		if slot == mediaslot.Background {
			if svc.PlaybackManager != nil {
				if err := svc.PlaybackManager.Stop(mediaslot.Background); err != nil {
					log.Warn().Err(err).Msg("failed to stop background playlist playback")
				}
			}
			svc.State.SetBackgroundPlaylist(nil)
			svc.State.SetBackgroundMedia(nil)
		} else {
			svc.State.SetActivePlaylist(nil)
		}
		return
	case activePlaylist == nil:
		// new playlist loaded
		if pls.Slot == "" {
			pls.Slot = slot
		}
		if slot == mediaslot.Background {
			svc.State.SetBackgroundPlaylist(pls)
		} else {
			svc.State.SetActivePlaylist(pls)
		}
		if pls.Playing {
			log.Info().Any("pls", playlistForLog(pls)).Msg("setting new playlist, launching token")
			if slot == mediaslot.Background {
				svc.State.SetBackgroundAutoPaused(false)
			}
			if svc.BackgroundWG != nil {
				svc.BackgroundWG.Add(1)
			}
			go func() {
				if svc.BackgroundWG != nil {
					defer svc.BackgroundWG.Done()
				}
				launchPlaylistMedia(svc, pls, player)
			}()
		} else {
			log.Info().Any("pls", playlistForLog(pls)).Msg("setting new playlist")
		}
		return
	default:
		// active playlist updated
		if !pls.ForceRelaunch && !playlistNeedsUpdate(pls, activePlaylist) {
			log.Debug().Msg("playlist current token unchanged, skipping")
			return
		}

		if pls.Slot == "" {
			pls.Slot = slot
		}
		if slot == mediaslot.Background {
			svc.State.SetBackgroundPlaylist(pls)
		} else {
			svc.State.SetActivePlaylist(pls)
		}
		if pls.Playing {
			if !activePlaylist.Playing && pls.Current() == activePlaylist.Current() &&
				svc.PlaybackManager != nil && svc.PlaybackManager.State(slot).Path != "" {
				log.Info().Any("pls", playlistForLog(pls)).Str("slot", slot).Msg("resuming playlist playback")
				if slot == mediaslot.Background {
					svc.State.SetBackgroundAutoPaused(false)
				}
				if err := svc.PlaybackManager.Resume(slot); err != nil {
					log.Warn().Err(err).Str("slot", slot).Msg("failed to resume playlist playback")
				}
				return
			}
			log.Info().Any("pls", playlistForLog(pls)).Msg("updating playlist, launching token")
			if slot == mediaslot.Background {
				svc.State.SetBackgroundAutoPaused(false)
			}
			if svc.BackgroundWG != nil {
				svc.BackgroundWG.Add(1)
			}
			go func() {
				if svc.BackgroundWG != nil {
					defer svc.BackgroundWG.Done()
				}
				launchPlaylistMedia(svc, pls, player)
			}()
		} else {
			if svc.PlaybackManager != nil && svc.PlaybackManager.State(slot).Path != "" {
				if err := svc.PlaybackManager.Pause(slot); err != nil {
					log.Warn().Err(err).Str("slot", slot).Msg("failed to pause playlist playback")
				}
			}
			log.Info().Any("pls", playlistForLog(pls)).Msg("updating playlist")
		}
		return
	}
}

var (
	errEmptyToken     = errors.New("empty token")
	errLaunchPanicked = errors.New("token launch panicked")
)

func processTokenQueue(
	svc *ServiceContext,
	itq <-chan tokens.Token,
	limitsManager *playtime.LimitsManager,
	player audio.Player,
) {
	for {
		select {
		case pls := <-svc.PlaylistQueue:
			handlePlaylist(svc, pls, player)
		case t := <-itq:
			// TODO: change this channel to send a token pointer or something
			handleQueuedToken(svc, t, limitsManager, player)
		case <-svc.State.GetContext().Done():
			log.Debug().Msg("exiting service worker via context cancellation")
			return
		}
	}
}

// handleQueuedToken runs preflight for one queued token and either rejects it
// or hands it to a launch goroutine. Every path completes t.Completion exactly
// once: preflight rejections complete here, everything else completes in
// launchQueuedToken. The worker itself never waits on execution.
func handleQueuedToken(
	svc *ServiceContext,
	t tokens.Token, //nolint:gocritic // single-use parameter in service function
	limitsManager *playtime.LimitsManager,
	player audio.Player,
) {
	if t.ScanTime.IsZero() {
		// ignore empty tokens
		t.Completion.Complete(errEmptyToken)
		return
	}

	log.Info().Msgf("processing token: %v", tokenForLog(&t))

	if err := svc.Platform.ScanHook(&t); err != nil {
		log.Error().Err(err).Msgf("error writing tmp scan result")
	}

	now := time.Now()
	systemUptime, uptimeErr := uptime.Get()
	if uptimeErr != nil {
		log.Warn().Err(uptimeErr).Msg("failed to get system uptime for history entry, using 0")
		systemUptime = 0
	}
	monotonicStart := int64(systemUptime.Seconds())

	// Never store a bearer credential: history is readable by every client.
	historyText, historyData := zapscript.RedactToken(t.Text, t.Data)

	he := database.HistoryEntry{
		ID:             uuid.New().String(),
		Time:           t.ScanTime,
		Type:           t.Type,
		TokenID:        t.UID,
		TokenValue:     historyText,
		TokenData:      historyData,
		ClockReliable:  helpers.IsClockReliable(now),
		BootUUID:       svc.State.BootUUID(),
		MonotonicStart: monotonicStart,
		CreatedAt:      now,
	}

	mappedValue, hasMapping := getMapping(svc.Config, svc.DB, svc.Platform, t)
	scriptText := t.Text
	if hasMapping {
		scriptText = mappedValue
	}

	reader := gozapscript.NewParser(scriptText)
	script, parseErr := reader.ParseScript()
	if parseErr != nil {
		log.Debug().Err(parseErr).Msg("failed to parse script for playtime check")
		// Continue anyway - the error will be caught in runTokenZapScript
	}

	// The one place a token's traits are settled. Every token carrying script
	// text passes through here, and everything downstream reads this set
	// rather than deriving its own, so a token's traits cannot change once it
	// starts running.
	if parseErr == nil {
		t.Traits = tokens.ResolveTraits(script.Traits)
		if !t.Traits.IsEmpty() {
			log.Info().Strs("traits", t.Traits.Names()).Msg("token declares traits")
		}
	}

	if parseErr != nil || shouldPlayScanSuccessSound(&script) {
		path, enabled := svc.Config.SuccessSoundPath(helpers.DataDir(svc.Platform))
		helpers.PlayConfiguredSound(player, path, enabled, assets.SuccessSound, "success")
	}

	if parseErr == nil {
		switch preflight := handleNextActionPreflight(svc, &t, &script); preflight {
		case nextActionArmed:
			he.Success = true
			if histErr := svc.DB.UserDB.AddHistory(&he); histErr != nil {
				log.Error().Err(histErr).Msgf("error adding history")
			}
			// Arming the next action is this token's whole job.
			t.Completion.Complete(nil)
			return
		case nextActionInvalid, nextActionBlocked:
			he.Success = false
			if histErr := svc.DB.UserDB.AddHistory(&he); histErr != nil {
				log.Error().Err(histErr).Msgf("error adding history")
			}
			path, enabled := svc.Config.FailSoundPath(helpers.DataDir(svc.Platform))
			helpers.PlayConfiguredSound(player, path, enabled, assets.FailSound, "fail")
			if preflight == nextActionBlocked {
				t.Completion.Complete(fmt.Errorf("%w: %s", zapscript.ErrCommandBlocked, script.Cmds[0].Name))
			} else {
				t.Completion.Complete(state.ErrInvalidNextAction)
			}
			return
		case nextActionNone:
		}
	}

	// Check if any command in the script launches media
	hasMediaLaunchCmd := parseErr == nil && scriptHasMediaLaunchingCommand(&script)

	// When require_for_launch is enabled, media launches are blocked
	// until a profile is active (profile switch commands still run —
	// scanning a profile card is how the device gets unparked). A
	// combo card that switches profile before launching passes: the
	// switch activates a profile before the launch command runs, or
	// fails and aborts the whole script.
	if hasMediaLaunchCmd && svc.Config.ProfilesRequireForLaunch() &&
		svc.State.ActiveProfile() == nil && !scriptActivatesProfileBeforeLaunch(&script) {
		log.Warn().Msg("profiles: launch blocked, no active profile and require_for_launch is set")

		path, enabled := svc.Config.FailSoundPath(helpers.DataDir(svc.Platform))
		helpers.PlayConfiguredSound(player, path, enabled, assets.FailSound, "fail")

		he.Success = false
		if histErr := svc.DB.UserDB.AddHistory(&he); histErr != nil {
			log.Error().Err(histErr).Msgf("error adding history")
		}

		t.Completion.Complete(state.ErrLaunchRequiresProfile)
		return
	}

	// Only check playtime limits if the script contains media-launching commands
	if hasMediaLaunchCmd {
		if limitReason, limitErr := limitsManager.CheckBeforeLaunch(); limitErr != nil {
			log.Warn().Err(limitErr).Msg("playtime: launch blocked by limit")

			if limitReason != "" {
				notifications.PlaytimeLimitReached(svc.State.Notifications, models.PlaytimeLimitReachedParams{
					Reason: limitReason,
				})

				path, enabled := svc.Config.LimitSoundPath(helpers.DataDir(svc.Platform))
				helpers.PlayConfiguredSound(player, path, enabled, assets.LimitSound, "limit")
			}

			he.Success = false
			if histErr := svc.DB.UserDB.AddHistory(&he); histErr != nil {
				log.Error().Err(histErr).Msgf("error adding history")
			}

			t.Completion.Complete(limitErr)
			return
		}
	} else {
		log.Debug().Msg("script contains no media-launching commands, bypassing playtime limit check")
	}

	// launch tokens in a separate thread
	if svc.BackgroundWG != nil {
		svc.BackgroundWG.Add(1)
	}
	go launchQueuedToken(svc, t, &he, player)
}

// launchQueuedToken executes a token's ZapScript and records the outcome. The
// completion is delivered as soon as execution finishes so a waiting caller
// is not held behind sound playback or the history write.
func launchQueuedToken(
	svc *ServiceContext,
	t tokens.Token, //nolint:gocritic // single-use parameter in service function
	he *database.HistoryEntry,
	player audio.Player,
) {
	if svc.BackgroundWG != nil {
		defer svc.BackgroundWG.Done()
	}
	defer func() {
		// Execution panics are already turned into errors; this only guards
		// the bookkeeping that follows so it cannot take the process down.
		if r := recover(); r != nil {
			log.Error().Any("panic", r).Msg("recovered panic in token launch")
		}
	}()

	plsc := playlists.PlaylistController{
		Active:     svc.State.GetActivePlaylist(),
		Background: svc.State.GetBackgroundPlaylist(),
		Queue:      svc.PlaylistQueue,
	}

	err := runTokenZapScriptRecovering(svc, t, plsc)

	// ErrRunZapScriptDisabled already logged its own Warn inside
	// runTokenZapScriptWithContext; treat it as the prior
	// silent-no-op success, not a launch failure, so a disabled
	// setting doesn't play a fail sound or record a failed
	// history entry.
	disabled := errors.Is(err, state.ErrRunZapScriptDisabled)
	failed := err != nil && !disabled
	if failed {
		if isExpectedLaunchError(err) {
			log.Warn().Err(err).Msgf("error launching token")
		} else {
			log.Error().Err(err).Msgf("error launching token")
		}
	}

	he.Success = err == nil || disabled
	if histErr := svc.DB.UserDB.AddHistory(he); histErr != nil {
		log.Error().Err(histErr).Msgf("error adding history")
	}

	// Complete once history is durable: a caller that waited for this
	// result can immediately read the run back from tokens.history. The
	// fail sound follows, so the caller never waits on audio.
	t.Completion.Complete(err)

	if failed {
		path, enabled := svc.Config.FailSoundPath(helpers.DataDir(svc.Platform))
		helpers.PlayConfiguredSound(player, path, enabled, assets.FailSound, "fail")
	}
}

// runTokenZapScriptRecovering runs the token's ZapScript and converts a panic
// into an error, so the token is still completed and recorded in history and
// the worker keeps serving the queue.
func runTokenZapScriptRecovering(
	svc *ServiceContext,
	t tokens.Token, //nolint:gocritic // single-use parameter in service function
	plsc playlists.PlaylistController,
) error {
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Any("panic", r).Msg("recovered panic in token launch")
				err = fmt.Errorf("%w: %v", errLaunchPanicked, r)
			}
		}()
		err = runTokenZapScript(svc, t, plsc, nil, false)
	}()
	return err
}
