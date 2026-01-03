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

package state

import (
	"context"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

// State holds the runtime state of the Zaparoo service.
//
// LOCKING RULES: The mu mutex protects all mutable fields. To prevent deadlocks:
//   - Never send to channels while holding the lock (notifications, callbacks)
//   - Never call external methods (reader.OnMediaChange, hooks) while holding the lock
//   - Pattern: lock → modify state → copy needed data → unlock → send notifications
//
// See SetActiveCard, SetActiveMedia, SetReader, RemoveReader for examples.
type State struct {
	platform         platforms.Platform
	ctx              context.Context
	activePlaylist   *playlists.Playlist
	softwareToken    *tokens.Token
	wroteToken       *tokens.Token
	readers          map[string]readers.Reader
	ctxCancelFunc    context.CancelFunc
	activeMedia      *models.ActiveMedia
	onMediaStartHook func(*models.ActiveMedia)
	launcherManager  *LauncherManager
	Notifications    chan<- models.Notification
	inbox            *inbox.Service
	bootUUID         string
	lastScanned      tokens.Token
	activeToken      tokens.Token
	mu               syncutil.RWMutex
	stopService      bool
	runZapScript     bool
}

func NewState(platform platforms.Platform, bootUUID string) (state *State, notificationCh <-chan models.Notification) {
	// Buffer size of 500 provides headroom for high-volume events (e.g., MediaIndexing)
	// without dropping critical user-facing notifications (tokens, readers, media state)
	ns := make(chan models.Notification, 500)
	ctx, ctxCancelFunc := context.WithCancel(context.Background())
	return &State{
		runZapScript:    true,
		platform:        platform,
		readers:         make(map[string]readers.Reader),
		Notifications:   ns,
		ctx:             ctx,
		ctxCancelFunc:   ctxCancelFunc,
		launcherManager: NewLauncherManager(),
		bootUUID:        bootUUID,
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

	// Prepare notification payload inside lock, send outside
	var payload *models.TokenResponse
	if !s.activeToken.ScanTime.IsZero() {
		s.lastScanned = card
		payload = &models.TokenResponse{
			Type:     card.Type,
			UID:      card.UID,
			Text:     card.Text,
			Data:     card.Data,
			ScanTime: card.ScanTime,
		}
	}

	s.mu.Unlock()

	// Send notification outside lock to prevent deadlock
	if payload != nil {
		notifications.TokensAdded(s.Notifications, *payload)
	} else {
		notifications.TokensRemoved(s.Notifications)
	}
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

func (s *State) SetOnMediaStartHook(hook func(*models.ActiveMedia)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onMediaStartHook = hook
}

// GetReader returns the Reader for a given ReaderID.
func (s *State) GetReader(readerID string) (readers.Reader, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.readers[readerID]
	return r, ok
}

// SetReader registers a reader using its ReaderID as the key.
// If a reader with the same ReaderID exists, it is closed first.
func (s *State) SetReader(reader readers.Reader) {
	readerID := reader.ReaderID()

	s.mu.Lock()

	existing, ok := s.readers[readerID]
	if ok && existing != nil {
		err := existing.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing existing reader")
		}
	}

	s.readers[readerID] = reader

	// Prepare payload inside lock
	payload := models.ReaderResponse{
		Connected: true,
		Driver:    reader.Metadata().ID,
		Path:      reader.Path(),
	}

	s.mu.Unlock()

	// Send notification outside lock to prevent deadlock
	notifications.ReadersAdded(s.Notifications, payload)
}

// RemoveReader removes a reader by its ReaderID and closes it.
func (s *State) RemoveReader(readerID string) {
	s.mu.Lock()

	r, ok := s.readers[readerID]
	var driverID, path string
	if ok && r != nil {
		driverID = r.Metadata().ID
		path = r.Path()
		err := r.Close()
		if err != nil {
			log.Warn().Err(err).Msg("error closing reader")
		}
	}
	delete(s.readers, readerID)

	// Prepare payload inside lock
	payload := models.ReaderResponse{
		Connected: false,
		Driver:    driverID,
		Path:      path,
	}

	s.mu.Unlock()

	// Send notification outside lock to prevent deadlock
	notifications.ReadersRemoved(s.Notifications, payload)
}

// ListReaders returns all registered Reader instances.
func (s *State) ListReaders() []readers.Reader {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rs := make([]readers.Reader, 0, len(s.readers))
	for _, r := range s.readers {
		rs = append(rs, r)
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
	s.mu.Lock()

	// Read oldMedia inside lock to prevent race condition where another
	// goroutine modifies activeMedia between our read and lock acquisition
	oldMedia := s.activeMedia

	if oldMedia == nil && media == nil {
		s.mu.Unlock()
		return
	}

	// Capture hook reference inside lock
	hook := s.onMediaStartHook

	if media == nil {
		// media has stopped
		s.activeMedia = media
		s.mu.Unlock()

		// Send notifications outside lock to prevent deadlock
		notifications.MediaStopped(s.Notifications)
		s.notifyDisplayReaders(media)
		return
	}

	if oldMedia == nil {
		// media has started
		s.activeMedia = media
		s.mu.Unlock()

		// Send notifications outside lock to prevent deadlock
		notifications.MediaStarted(s.Notifications, models.MediaStartedParams{
			SystemID:   media.SystemID,
			SystemName: media.SystemName,
			MediaName:  media.Name,
			MediaPath:  media.Path,
		})
		s.notifyDisplayReaders(media)

		// Execute OnMediaStart hook if set
		if hook != nil {
			go hook(media)
		}
		return
	}

	if !oldMedia.Equal(media) {
		// media has changed
		s.activeMedia = media
		s.mu.Unlock()

		// Send notifications outside lock to prevent deadlock
		notifications.MediaStopped(s.Notifications)
		notifications.MediaStarted(s.Notifications, models.MediaStartedParams{
			SystemID:   media.SystemID,
			SystemName: media.SystemName,
			MediaName:  media.Name,
			MediaPath:  media.Path,
		})
		s.notifyDisplayReaders(media)

		// Execute OnMediaStart hook if set (new media started)
		if hook != nil {
			go hook(media)
		}
		return
	}

	// No changes
	s.mu.Unlock()
}

// notifyDisplayReaders calls OnMediaChange for all readers with display capability.
// This method acquires a read lock to copy the readers list, then releases the lock
// before making external calls to prevent deadlocks.
//
// Note: Because we release the lock before calling OnMediaChange, a reader could be
// closed by RemoveReader concurrently. We use defer/recover to handle any panics
// from calling methods on closed readers.
func (s *State) notifyDisplayReaders(media *models.ActiveMedia) {
	// Copy readers list inside lock to avoid holding lock during external calls
	s.mu.RLock()
	readersToNotify := make([]readers.Reader, 0, len(s.readers))
	for _, r := range s.readers {
		if r != nil {
			readersToNotify = append(readersToNotify, r)
		}
	}
	s.mu.RUnlock()

	// Call OnMediaChange outside lock to prevent deadlock
	for _, reader := range readersToNotify {
		s.safeNotifyReader(reader, media)
	}
}

// safeNotifyReader calls OnMediaChange on a reader with panic recovery.
// This handles the case where a reader is closed concurrently by RemoveReader.
//
// TODO: The proper fix is to make all Reader.OnMediaChange implementations
// close-safe (return ErrReaderClosed instead of panicking). Then we can remove
// the panic recovery entirely. The Connected() check narrows the race window
// but doesn't eliminate it.
func (*State) safeNotifyReader(reader readers.Reader, media *models.ActiveMedia) {
	// Skip readers that are already disconnected (fast path)
	if !reader.Connected() {
		return
	}

	defer func() {
		if r := recover(); r != nil {
			log.Error().Interface("panic", r).Msg("panic in reader OnMediaChange (reader may have been closed)")
		}
	}()

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

func (s *State) GetContext() context.Context {
	return s.ctx
}

func (s *State) LauncherManager() *LauncherManager {
	return s.launcherManager
}

func (s *State) BootUUID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bootUUID
}

// SetInbox sets the inbox service. Called during service startup after database is ready.
func (s *State) SetInbox(svc *inbox.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inbox = svc
}

// Inbox returns the inbox service for adding system notifications.
func (s *State) Inbox() *inbox.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inbox
}
