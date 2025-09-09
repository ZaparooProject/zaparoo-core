// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package state

import (
	"context"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

type State struct {
	platform       platforms.Platform
	ctx            context.Context
	activePlaylist *playlists.Playlist
	softwareToken  *tokens.Token
	wroteToken     *tokens.Token
	readers        map[string]readers.Reader
	ctxCancelFunc  context.CancelFunc
	activeMedia    *models.ActiveMedia
	Notifications  chan<- models.Notification
	lastScanned    tokens.Token
	activeToken    tokens.Token
	mu             sync.RWMutex
	stopService    bool
	runZapScript   bool
}

func NewState(platform platforms.Platform) (state *State, notificationChan <-chan models.Notification) {
	ns := make(chan models.Notification, 100)
	ctx, ctxCancelFunc := context.WithCancel(context.Background())
	return &State{
		runZapScript:  true,
		platform:      platform,
		readers:       make(map[string]readers.Reader),
		Notifications: ns,
		ctx:           ctx,
		ctxCancelFunc: ctxCancelFunc,
	}, ns
}

func (s *State) SetActiveCard(card tokens.Token) { //nolint:gocritic // single-use parameter in state setter
	s.mu.Lock()

	if helpers.TokensEqual(&s.activeToken, &card) {
		// ignore duplicate scans
		s.mu.Unlock()
		return
	}

	s.activeToken = card
	if !s.activeToken.ScanTime.IsZero() {
		s.lastScanned = card
		notifications.TokensAdded(s.Notifications, models.TokenResponse{
			Type:     card.Type,
			UID:      card.UID,
			Text:     card.Text,
			Data:     card.Data,
			ScanTime: card.ScanTime,
		})
	} else {
		notifications.TokensRemoved(s.Notifications)
	}

	s.mu.Unlock()
}

func (s *State) GetActiveCard() tokens.Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeToken
}

func (s *State) GetLastScanned() tokens.Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastScanned
}

func (s *State) StopService() {
	s.mu.Lock()
	s.stopService = true
	s.mu.Unlock()
	s.ctxCancelFunc()
}

func (s *State) SetRunZapScript(run bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runZapScript = run
}

func (s *State) RunZapScriptEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runZapScript
}

func (s *State) GetReader(device string) (readers.Reader, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.readers[device]
	return r, ok
}

func (s *State) SetReader(device string, reader readers.Reader) {
	s.mu.Lock()

	r, ok := s.readers[device]
	if ok {
		err := r.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing reader")
		}
	}
	s.readers[device] = reader

	ps := strings.SplitN(device, ":", 2)
	driver := ps[0]
	var path string
	if len(ps) > 1 {
		path = ps[1]
	}
	notifications.ReadersAdded(s.Notifications, models.ReaderResponse{
		Connected: true,
		Driver:    driver,
		Path:      path,
	})
	s.mu.Unlock()
}

func (s *State) RemoveReader(device string) {
	s.mu.Lock()
	r, ok := s.readers[device]
	if ok && r != nil {
		err := r.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing reader")
		}
	}
	ps := strings.SplitN(device, ":", 2)
	driver := ps[0]
	var path string
	if len(ps) > 1 {
		path = ps[1]
	}
	delete(s.readers, device)
	notifications.ReadersRemoved(s.Notifications, models.ReaderResponse{
		Connected: false,
		Driver:    driver,
		Path:      path,
	})
	s.mu.Unlock()
}

func (s *State) ListReaders() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rs := make([]string, 0, len(s.readers))
	for k := range s.readers {
		rs = append(rs, k)
	}

	return rs
}

func (s *State) SetSoftwareToken(token *tokens.Token) {
	s.mu.Lock()
	s.softwareToken = token
	s.mu.Unlock()
}

func (s *State) GetSoftwareToken() *tokens.Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.softwareToken
}

func (s *State) SetWroteToken(token *tokens.Token) {
	s.mu.Lock()
	s.wroteToken = token
	s.mu.Unlock()
}

func (s *State) GetWroteToken() *tokens.Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wroteToken
}

func (s *State) GetActivePlaylist() *playlists.Playlist {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activePlaylist
}

func (s *State) SetActivePlaylist(playlist *playlists.Playlist) {
	s.mu.Lock()
	s.activePlaylist = playlist
	s.mu.Unlock()
}

func (s *State) ActiveMedia() *models.ActiveMedia {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeMedia
}

func (s *State) SetActiveMedia(media *models.ActiveMedia) {
	oldMedia := s.ActiveMedia()
	s.mu.Lock()
	defer s.mu.Unlock()
	if oldMedia == nil && media == nil {
		return
	}
	if media == nil {
		// media has stopped
		s.activeMedia = media
		notifications.MediaStopped(s.Notifications)
		s.notifyDisplayReaders(media)
		return
	}
	if oldMedia == nil {
		// media has started
		s.activeMedia = media
		notifications.MediaStarted(s.Notifications, models.MediaStartedParams{
			SystemID:   media.SystemID,
			SystemName: media.SystemName,
			MediaName:  media.Name,
			MediaPath:  media.Path,
		})
		s.notifyDisplayReaders(media)
		return
	}
	if !oldMedia.Equal(media) {
		// media has changed
		s.activeMedia = media
		notifications.MediaStopped(s.Notifications)
		notifications.MediaStarted(s.Notifications, models.MediaStartedParams{
			SystemID:   media.SystemID,
			SystemName: media.SystemName,
			MediaName:  media.Name,
			MediaPath:  media.Path,
		})
		s.notifyDisplayReaders(media)
		return
	}
}

// notifyDisplayReaders calls OnMediaChange for all readers with display capability
func (s *State) notifyDisplayReaders(media *models.ActiveMedia) {
	for _, reader := range s.readers {
		if reader == nil {
			continue
		}

		capabilities := reader.Capabilities()
		hasDisplayCapability := false
		for _, cap := range capabilities {
			if cap == readers.CapabilityDisplay {
				hasDisplayCapability = true
				break
			}
		}

		if hasDisplayCapability {
			if err := reader.OnMediaChange(media); err != nil {
				log.Warn().Err(err).Msg("failed to notify display reader of media change")
			}
		}
	}
}

func (s *State) GetContext() context.Context {
	return s.ctx
}
