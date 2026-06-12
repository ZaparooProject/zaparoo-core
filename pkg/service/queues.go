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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/google/uuid"
	"github.com/mackerelio/go-osstat/uptime"
	"github.com/rs/zerolog/log"
)

func runTokenZapScript(
	svc *ServiceContext,
	token tokens.Token, //nolint:gocritic // single-use parameter in service function
	plsc playlists.PlaylistController,
	exprEnv *gozapscript.ArgExprEnv,
	inHookContext bool,
) error {
	if !svc.State.RunZapScriptEnabled() {
		log.Warn().Msg("ignoring ZapScript, run ZapScript is disabled")
		return nil
	}

	mappedValue, hasMapping := getMapping(svc.Config, svc.DB, svc.Platform, token)
	if hasMapping {
		log.Info().Msgf("found mapping: %s", mappedValue)
		token.Text = mappedValue
	}

	reader := gozapscript.NewParser(token.Text)
	script, err := reader.ParseScript()
	if err != nil {
		return fmt.Errorf("failed to parse script: %w", err)
	}

	log.Info().Msgf("running script (%d cmds)", len(script.Cmds))

	pls := plsc.Active

	cmds := script.Cmds
	for i := 0; i < len(cmds); i++ {
		cmd := cmds[i]

		// Run before_media_start hook; errors block the launch.
		beforeMediaStartScript := svc.Config.LaunchersBeforeMediaStart()
		if shouldRunBeforeMediaStartHook(inHookContext, beforeMediaStartScript, cmd.Name) {
			log.Info().Msgf("running before_media_start hook: %s", beforeMediaStartScript)
			hookPlsc := playlists.PlaylistController{
				Active:     pls,
				Background: plsc.Background,
				Queue:      plsc.Queue,
			}
			hookToken := tokens.Token{
				ScanTime: time.Now(),
				Text:     beforeMediaStartScript,
			}
			launching := buildLaunchingContext(cmd)
			hookEnv := zapscript.GetExprEnv(svc.Platform, svc.Config, svc.State, nil, launching)
			hookErr := runTokenZapScript(svc, hookToken, hookPlsc, &hookEnv, true)
			if hookErr != nil {
				return fmt.Errorf("before_media_start hook blocked launch: %w", hookErr)
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
				log.Info().Str("launcher", pending.LauncherID).Msg("applying one-shot launch override")
				cmd.AdvArgs = cmd.AdvArgs.With(gozapscript.KeyLauncher, pending.LauncherID)
			}
		}

		if stopErr := stopNativePlaybackBeforePrimaryCommand(svc, cmd, pls); stopErr != nil {
			return stopErr
		}
		if pauseErr := pauseBackgroundForPrimaryLaunch(svc, cmd, pls); pauseErr != nil {
			return pauseErr
		}

		result, err := zapscript.RunCommand(
			svc.State.GetContext(),
			svc.Platform, svc.Config,
			playlists.PlaylistController{
				Active:     pls,
				Background: plsc.Background,
				Queue:      plsc.Queue,
			},
			token,
			cmd,
			len(script.Cmds),
			i,
			svc.DB,
			svc.State.LauncherManager(),
			func(ctx context.Context) error { return waitForMediaReady(ctx, svc, mediaReadyGen) },
			&cmdEnv,
		)
		if err != nil {
			return fmt.Errorf("failed to run zapscript command: %w", err)
		}

		if result.MediaChanged && token.Source != tokens.SourcePlaylist {
			log.Debug().Any("token", token).Msg("cmd launch: clearing current playlist")
			select {
			case plsc.Queue <- nil:
			case <-svc.State.GetContext().Done():
				return errors.New("service shutting down")
			}
		}

		if result.MediaChanged && token.ReaderID != "" {
			r, ok := svc.State.GetReader(token.ReaderID)
			if ok && readers.HasCapability(r, readers.CapabilityRemovable) {
				log.Debug().Any("token", token).Msg("media changed, updating software token")
				select {
				case svc.LaunchSoftwareQueue <- &token:
				case <-svc.State.GetContext().Done():
					return errors.New("service shutting down")
				}
			}
		}

		if result.PlaylistChanged {
			pls = result.Playlist
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

func stopNativePlaybackBeforePrimaryCommand(
	svc *ServiceContext,
	cmd gozapscript.Command,
	activePlaylist *playlists.Playlist,
) error {
	if svc.PlaybackManager == nil {
		return nil
	}
	stopsPrimaryMedia := cmd.Name == gozapscript.ZapScriptCmdStop ||
		cmd.Name == gozapscript.ZapScriptCmdPlaylistStop ||
		cmd.Name == gozapscript.ZapScriptCmdPlaylistPause
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
	activePlaylist *playlists.Playlist,
	player audio.Player,
) {
	t := tokens.Token{
		Text:     pls.Current().ZapScript,
		ScanTime: time.Now(),
		Source:   tokens.SourcePlaylist,
	}
	plsc := playlists.PlaylistController{
		Active:     activePlaylist,
		Background: svc.State.GetBackgroundPlaylist(),
		Queue:      svc.PlaylistQueue,
	}
	if pls.Slot == mediaslot.Background {
		plsc.Active = pls
		plsc.Background = pls
	}

	err := runTokenZapScript(svc, t, plsc, nil, false)
	if err != nil {
		log.Error().Err(err).Msgf("error launching token")
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

	he := database.HistoryEntry{
		ID:             uuid.New().String(),
		Time:           t.ScanTime,
		Type:           t.Type,
		TokenID:        t.UID,
		TokenValue:     t.Text,
		TokenData:      t.Data,
		ClockReliable:  helpers.IsClockReliable(now),
		BootUUID:       svc.State.BootUUID(),
		MonotonicStart: monotonicStart,
		CreatedAt:      now,
	}
	he.Success = err == nil
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
			log.Info().Any("pls", pls).Msg("setting new playlist, launching token")
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
				launchPlaylistMedia(svc, pls, activePlaylist, player)
			}()
		} else {
			log.Info().Any("pls", pls).Msg("setting new playlist")
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
			log.Info().Any("pls", pls).Msg("updating playlist, launching token")
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
				launchPlaylistMedia(svc, pls, activePlaylist, player)
			}()
		} else {
			if slot == mediaslot.Background && svc.PlaybackManager != nil {
				if err := svc.PlaybackManager.Pause(mediaslot.Background); err != nil {
					log.Warn().Err(err).Msg("failed to pause background playlist playback")
				}
			}
			log.Info().Any("pls", pls).Msg("updating playlist")
		}
		return
	}
}

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
			continue
		case t := <-itq:
			// TODO: change this channel to send a token pointer or something
			if t.ScanTime.IsZero() {
				// ignore empty tokens
				continue
			}

			log.Info().Msgf("processing token: %v", t)

			err := svc.Platform.ScanHook(&t)
			if err != nil {
				log.Error().Err(err).Msgf("error writing tmp scan result")
			}

			now := time.Now()
			systemUptime, uptimeErr := uptime.Get()
			if uptimeErr != nil {
				log.Warn().Err(uptimeErr).Msg("failed to get system uptime for history entry, using 0")
				systemUptime = 0
			}
			monotonicStart := int64(systemUptime.Seconds())

			he := database.HistoryEntry{
				ID:             uuid.New().String(),
				Time:           t.ScanTime,
				Type:           t.Type,
				TokenID:        t.UID,
				TokenValue:     t.Text,
				TokenData:      t.Data,
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

			if parseErr != nil || shouldPlayScanSuccessSound(&script) {
				path, enabled := svc.Config.SuccessSoundPath(helpers.DataDir(svc.Platform))
				helpers.PlayConfiguredSound(player, path, enabled, assets.SuccessSound, "success")
			}

			if parseErr == nil {
				switch handleNextActionPreflight(svc, &t, &script) {
				case nextActionArmed:
					he.Success = true
					if histErr := svc.DB.UserDB.AddHistory(&he); histErr != nil {
						log.Error().Err(histErr).Msgf("error adding history")
					}
					continue
				case nextActionInvalid:
					he.Success = false
					if histErr := svc.DB.UserDB.AddHistory(&he); histErr != nil {
						log.Error().Err(histErr).Msgf("error adding history")
					}
					path, enabled := svc.Config.FailSoundPath(helpers.DataDir(svc.Platform))
					helpers.PlayConfiguredSound(player, path, enabled, assets.FailSound, "fail")
					continue
				case nextActionNone:
				}
			}

			// Check if any command in the script launches media
			hasMediaLaunchCmd := parseErr == nil && scriptHasMediaLaunchingCommand(&script)

			// Only check playtime limits if the script contains media-launching commands
			if hasMediaLaunchCmd {
				if limitErr := limitsManager.CheckBeforeLaunch(); limitErr != nil {
					log.Warn().Err(limitErr).Msg("playtime: launch blocked by daily limit")

					// Send playtime limit notification
					notifications.PlaytimeLimitReached(svc.State.Notifications, models.PlaytimeLimitReachedParams{
						Reason: models.PlaytimeLimitReasonDaily,
					})

					path, enabled := svc.Config.LimitSoundPath(helpers.DataDir(svc.Platform))
					helpers.PlayConfiguredSound(player, path, enabled, assets.LimitSound, "limit")

					he.Success = false
					if histErr := svc.DB.UserDB.AddHistory(&he); histErr != nil {
						log.Error().Err(histErr).Msgf("error adding history")
					}

					// Skip launch
					continue
				}
			} else {
				log.Debug().Msg("script contains no media-launching commands, bypassing playtime limit check")
			}

			// launch tokens in a separate thread
			if svc.BackgroundWG != nil {
				svc.BackgroundWG.Add(1)
			}
			go func() {
				if svc.BackgroundWG != nil {
					defer svc.BackgroundWG.Done()
				}
				defer func() {
					if r := recover(); r != nil {
						log.Error().Any("panic", r).Msg("recovered panic in token launch")
					}
				}()

				plsc := playlists.PlaylistController{
					Active:     svc.State.GetActivePlaylist(),
					Background: svc.State.GetBackgroundPlaylist(),
					Queue:      svc.PlaylistQueue,
				}

				err = runTokenZapScript(svc, t, plsc, nil, false)
				if err != nil {
					if errors.Is(err, zapscript.ErrFileNotFound) {
						log.Warn().Err(err).Msgf("error launching token")
					} else {
						log.Error().Err(err).Msgf("error launching token")
					}
				}

				if err != nil {
					path, enabled := svc.Config.FailSoundPath(helpers.DataDir(svc.Platform))
					helpers.PlayConfiguredSound(player, path, enabled, assets.FailSound, "fail")
				}

				he.Success = err == nil
				err = svc.DB.UserDB.AddHistory(&he)
				if err != nil {
					log.Error().Err(err).Msgf("error adding history")
				}
			}()
		case <-svc.State.GetContext().Done():
			log.Debug().Msg("exiting service worker via context cancellation")
			return
		}
	}
}
