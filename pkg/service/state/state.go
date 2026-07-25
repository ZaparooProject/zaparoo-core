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
	"errors"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	backupcoordinator "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup/coordinator"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	uievents "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/events"
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
type PendingLaunchOverride struct {
	CreatedAt  time.Time
	LauncherID string
	Source     tokens.Token
}

type PendingWrite struct {
	CreatedAt time.Time
	Payload   string
	Source    tokens.Token
}

type State struct {
	platform              platforms.Platform
	ctx                   context.Context
	ctxCancelFunc         context.CancelFunc
	softwareToken         *tokens.Token
	wroteToken            *tokens.Token
	pendingLaunchOverride *PendingLaunchOverride
	pendingWrite          *PendingWrite
	readers               map[string]readers.Reader
	Notifications         chan<- models.Notification
	activeMedia           *models.ActiveMedia
	backgroundMedia       *models.ActiveMedia
	activeProfile         *models.ActiveProfile
	activePlaylist        *playlists.Playlist
	backgroundPlaylist    *playlists.Playlist
	activeMediaReadyCh    chan struct{}
	inbox                 *inbox.Service
	onMediaStartHook      func(*models.ActiveMedia, uint64)
	onMediaStopHook       func()
	launcherManager       *LauncherManager
	uiEvents              *uievents.Service
	backupCoordinator     *backupcoordinator.Coordinator
	bootUUID              string
	lastScanned           tokens.Token
	activeToken           tokens.Token
	activeMediaReadyGen   uint64
	mediaLaunchAccesses   int
	mu                    syncutil.RWMutex
	mediaRestoreMu        syncutil.RWMutex
	activeMediaReady      bool
	stopService           bool
	restartRequested      bool
	restorePendingRestart bool
	runZapScript          bool
	backgroundAutoPaused  bool
}

func NewState(platform platforms.Platform, bootUUID string) (state *State, notificationCh <-chan models.Notification) {
	// Buffer size of 500 provides headroom for high-volume events (e.g., MediaIndexing)
	// without dropping critical user-facing notifications (tokens, readers, media state)
	ns := make(chan models.Notification, 500)
	ctx, ctxCancelFunc := context.WithCancel(context.Background())
	state = &State{
		runZapScript:      true,
		platform:          platform,
		readers:           make(map[string]readers.Reader),
		Notifications:     ns,
		ctx:               ctx,
		ctxCancelFunc:     ctxCancelFunc,
		launcherManager:   NewLauncherManager(),
		backupCoordinator: backupcoordinator.New(),
		bootUUID:          bootUUID,
	}
	// Terminal backup.state event: clients showing a paused/throttled backup
	// banner clear it when the operation's lease is released.
	state.backupCoordinator.SetOnWriteFinished(func(kind backupcoordinator.OperationKind) {
		notifications.BackupState(ns, models.BackupStateNotification{
			Operation: string(kind),
			Finished:  true,
		})
	})
	return state, ns
}

func (s *State) BackupCoordinator() *backupcoordinator.Coordinator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backupCoordinator
}

// SetUIEvents stores the process-wide UI event service.
func (s *State) SetUIEvents(service *uievents.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uiEvents = service
}

// UIEvents returns the process-wide UI event service.
func (s *State) UIEvents() *uievents.Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.uiEvents
}

func (s *State) SetActiveCard(card tokens.Token) { //nolint:gocritic // single-use parameter in state setter
	s.mu.Lock()

	if helpers.TokensEqual(&s.activeToken, &card) && card.ScanTime.IsZero() {
		// ignore duplicate removals
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

// SetActiveProfile sets or clears (nil) the device's active profile and
// broadcasts a profiles.active notification. The snapshot is stored by
// value internally so callers cannot mutate state through the pointer.
func (s *State) SetActiveProfile(profile *models.ActiveProfile) {
	s.mu.Lock()

	if profile == nil && s.activeProfile == nil {
		// ignore duplicate deactivations
		s.mu.Unlock()
		return
	}

	var stored *models.ActiveProfile
	if profile != nil {
		profileCopy := *profile
		stored = &profileCopy
	}
	s.activeProfile = stored

	// Prepare notification payload inside lock, send outside
	var payload *models.ActiveProfile
	if stored != nil {
		payloadCopy := *stored
		payload = &payloadCopy
	}

	s.mu.Unlock()

	notifications.ProfilesActiveChanged(s.Notifications, models.ProfilesActiveNotification{Profile: payload})
}

// ActiveProfile returns a copy of the device's active profile snapshot, or
// nil when no profile is active.
func (s *State) ActiveProfile() *models.ActiveProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeProfile == nil {
		return nil
	}
	profileCopy := *s.activeProfile
	return &profileCopy
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

// RestartService signals the service to shut down and restart with the new
// binary. Used after applying an update for graceful restart instead of
// os.Exit.
func (s *State) RestartService() {
	s.mu.Lock()
	s.restartRequested = true
	s.stopService = true
	s.mu.Unlock()
	s.ctxCancelFunc()
}

// RestartRequested returns true if the service shutdown was triggered by a
// restart request (e.g. after applying an update).
func (s *State) RestartRequested() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.restartRequested
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

func (s *State) SetOnMediaStartHook(hook func(*models.ActiveMedia, uint64)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onMediaStartHook = hook
}

func (s *State) SetOnMediaStopHook(hook func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onMediaStopHook = hook
}

func (s *State) BackgroundAutoPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backgroundAutoPaused
}

func (s *State) SetBackgroundAutoPaused(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.backgroundAutoPaused = v
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

func (s *State) SetPendingLaunchOverride(pending *PendingLaunchOverride) {
	s.mu.Lock()
	s.pendingLaunchOverride = pending
	s.mu.Unlock()
}

func (s *State) GetPendingLaunchOverride() *PendingLaunchOverride {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingLaunchOverride
}

func (s *State) ConsumePendingLaunchOverride() *PendingLaunchOverride {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pendingLaunchOverride
	s.pendingLaunchOverride = nil
	return pending
}

func (s *State) SetPendingWrite(pending *PendingWrite) {
	s.mu.Lock()
	s.pendingWrite = pending
	s.mu.Unlock()
}

func (s *State) GetPendingWrite() *PendingWrite {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pendingWrite
}

func (s *State) ConsumePendingWrite() *PendingWrite {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pendingWrite
	s.pendingWrite = nil
	return pending
}

func (s *State) ClearPendingWrite() {
	s.mu.Lock()
	s.pendingWrite = nil
	s.mu.Unlock()
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

func (s *State) GetBackgroundPlaylist() *playlists.Playlist {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backgroundPlaylist
}

func (s *State) SetBackgroundPlaylist(playlist *playlists.Playlist) {
	s.mu.Lock()
	s.backgroundPlaylist = playlist
	s.mu.Unlock()
}

var (
	ErrNoActiveMedia          = errors.New("no active media")
	ErrActiveMediaChanged     = errors.New("active media changed")
	ErrRestoreInProgress      = errors.New("backup restore is in progress")
	ErrMediaLaunchInProgress  = errors.New("media launch is in progress")
	ErrRestoreRestartRequired = errors.New("backup restore restart is pending")
)

func (s *State) restoreAccessAfterLock() (func(), error) {
	s.mu.RLock()
	pendingRestart := s.restorePendingRestart
	s.mu.RUnlock()
	if pendingRestart {
		s.mediaRestoreMu.RUnlock()
		return nil, ErrRestoreRestartRequired
	}
	return s.mediaRestoreMu.RUnlock, nil
}

func (s *State) TryAcquireRestoreAccess() (func(), error) {
	if !s.mediaRestoreMu.TryRLock() {
		return nil, ErrRestoreInProgress
	}
	return s.restoreAccessAfterLock()
}

func (s *State) AcquireRestoreAccess() (func(), error) {
	s.mediaRestoreMu.RLock()
	return s.restoreAccessAfterLock()
}

func (s *State) AcquireMediaLaunch() (func(), error) {
	release, err := s.TryAcquireRestoreAccess()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.mediaLaunchAccesses++
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.mediaLaunchAccesses--
		s.mu.Unlock()
		release()
	}, nil
}

func (s *State) BeginRestoreGate() (func(bool), error) {
	if !s.mediaRestoreMu.TryLock() {
		return nil, ErrMediaLaunchInProgress
	}
	s.mu.RLock()
	pendingRestart := s.restorePendingRestart
	s.mu.RUnlock()
	if pendingRestart {
		s.mediaRestoreMu.Unlock()
		return nil, ErrRestoreRestartRequired
	}
	return func(success bool) {
		if success {
			s.mu.Lock()
			s.restorePendingRestart = true
			s.mu.Unlock()
		}
		s.mediaRestoreMu.Unlock()
	}, nil
}

func (s *State) ActiveMedia() *models.ActiveMedia {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeMedia
}

func (s *State) BackgroundMedia() *models.ActiveMedia {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backgroundMedia
}

func (s *State) ActiveMediaReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeMedia != nil && s.activeMediaReady
}

func (s *State) ActiveMediaReadyGeneration() (uint64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.activeMedia == nil {
		return 0, false
	}
	return s.activeMediaReadyGen, true
}

func (s *State) WaitForActiveMediaReady(ctx context.Context, expectedGen uint64) error {
	for {
		s.mu.RLock()
		if s.activeMedia == nil {
			s.mu.RUnlock()
			return ErrNoActiveMedia
		}
		if s.activeMediaReadyGen != expectedGen {
			s.mu.RUnlock()
			return ErrActiveMediaChanged
		}
		if s.activeMediaReady {
			s.mu.RUnlock()
			return nil
		}
		readyCh := s.activeMediaReadyCh
		s.mu.RUnlock()

		select {
		case <-readyCh:
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *State) MarkActiveMediaReady(gen uint64) {
	var readyCh chan struct{}

	s.mu.Lock()
	if s.activeMedia == nil || s.activeMediaReadyGen != gen || s.activeMediaReady {
		s.mu.Unlock()
		return
	}
	s.activeMediaReady = true
	readyCh = s.activeMediaReadyCh
	s.activeMediaReadyCh = nil
	s.mu.Unlock()

	close(readyCh)
}

func (s *State) SetActiveMedia(media *models.ActiveMedia) {
	if media != nil {
		s.mu.RLock()
		launchAccessHeld := s.mediaLaunchAccesses > 0
		s.mu.RUnlock()
		if launchAccessHeld {
			s.updateActiveMediaState(media)
			return
		}
		release, err := s.TryAcquireRestoreAccess()
		if errors.Is(err, ErrRestoreInProgress) {
			s.backupCoordinator.CancelRestore()
			release, err = s.AcquireRestoreAccess()
		}
		if err != nil {
			log.Warn().Err(err).Msg("active media update rejected during backup restore")
			return
		}
		defer release()
	}

	s.updateActiveMediaState(media)
}

func (s *State) updateActiveMediaState(media *models.ActiveMedia) {
	s.mu.Lock()

	// Read oldMedia inside lock to prevent race condition where another
	// goroutine modifies activeMedia between our read and lock acquisition
	oldMedia := s.activeMedia

	if oldMedia == nil && media == nil {
		s.mu.Unlock()
		return
	}

	// Capture hook references inside lock
	hook := s.onMediaStartHook
	stopHook := s.onMediaStopHook

	if media == nil {
		// media has stopped
		readyCh := s.activeMediaReadyCh
		s.activeMedia = media
		s.activeMediaReady = false
		s.activeMediaReadyCh = nil
		s.activeMediaReadyGen++
		s.mu.Unlock()

		if readyCh != nil {
			close(readyCh)
		}

		// Send notifications outside lock to prevent deadlock
		stoppedParams := buildMediaStoppedParams(oldMedia, mediaslot.Primary)
		notifications.MediaStopped(s.Notifications, &stoppedParams)
		s.notifyDisplayReaders(media)
		// Run the hook synchronously (outside the lock) so callers observe its
		// effects before their next step — the launch path stops native audio and
		// then decides whether to pause background music, which must happen after
		// any auto-resume the hook performs.
		if stopHook != nil {
			stopHook()
		}
		return
	}

	media.Slot = mediaslot.Primary
	if oldMedia == nil {
		// media has started
		s.activeMedia = media
		s.activeMediaReady = false
		s.activeMediaReadyGen++
		gen := s.activeMediaReadyGen
		s.activeMediaReadyCh = make(chan struct{})
		s.mu.Unlock()

		// Send notifications outside lock to prevent deadlock
		notifications.MediaStarted(s.Notifications, models.MediaStartedParams{
			SystemID:   media.SystemID,
			SystemName: media.SystemName,
			MediaName:  media.Name,
			MediaPath:  media.Path,
			Slot:       mediaslot.Primary,
		})
		s.notifyDisplayReaders(media)

		// Execute OnMediaStart hook if set
		if hook != nil {
			go hook(media, gen)
		}
		return
	}

	if !oldMedia.Equal(media) {
		// media has changed
		readyCh := s.activeMediaReadyCh
		s.activeMedia = media
		s.activeMediaReady = false
		s.activeMediaReadyGen++
		gen := s.activeMediaReadyGen
		s.activeMediaReadyCh = make(chan struct{})
		s.mu.Unlock()

		if readyCh != nil {
			close(readyCh)
		}

		// Send notifications outside lock to prevent deadlock
		changedStoppedParams := buildMediaStoppedParams(oldMedia, mediaslot.Primary)
		notifications.MediaStopped(s.Notifications, &changedStoppedParams)
		notifications.MediaStarted(s.Notifications, models.MediaStartedParams{
			SystemID:   media.SystemID,
			SystemName: media.SystemName,
			MediaName:  media.Name,
			MediaPath:  media.Path,
			Slot:       mediaslot.Primary,
		})
		s.notifyDisplayReaders(media)

		// Execute OnMediaStart hook if set (new media started)
		if hook != nil {
			go hook(media, gen)
		}
		return
	}

	// No changes
	s.mu.Unlock()
}

func (s *State) SetBackgroundMedia(media *models.ActiveMedia) {
	s.mu.Lock()
	oldMedia := s.backgroundMedia
	if oldMedia == nil && media == nil {
		s.mu.Unlock()
		return
	}

	if media == nil {
		s.backgroundMedia = nil
		s.mu.Unlock()
		stoppedParams := buildMediaStoppedParams(oldMedia, mediaslot.Background)
		notifications.MediaStopped(s.Notifications, &stoppedParams)
		return
	}

	media.Slot = mediaslot.Background
	if oldMedia == nil {
		s.backgroundMedia = media
		s.mu.Unlock()
		notifications.MediaStarted(s.Notifications, models.MediaStartedParams{
			SystemID:   media.SystemID,
			SystemName: media.SystemName,
			MediaName:  media.Name,
			MediaPath:  media.Path,
			Slot:       mediaslot.Background,
		})
		return
	}

	if !oldMedia.Equal(media) {
		s.backgroundMedia = media
		s.mu.Unlock()
		stoppedParams := buildMediaStoppedParams(oldMedia, mediaslot.Background)
		notifications.MediaStopped(s.Notifications, &stoppedParams)
		notifications.MediaStarted(s.Notifications, models.MediaStartedParams{
			SystemID:   media.SystemID,
			SystemName: media.SystemName,
			MediaName:  media.Name,
			MediaPath:  media.Path,
			Slot:       mediaslot.Background,
		})
		return
	}

	s.mu.Unlock()
}

func buildMediaStoppedParams(media *models.ActiveMedia, slot string) models.MediaStoppedParams {
	elapsed := max(0, int(time.Since(media.Started).Seconds()))
	return models.MediaStoppedParams{
		SystemID:   media.SystemID,
		SystemName: media.SystemName,
		MediaName:  media.Name,
		MediaPath:  media.Path,
		LauncherID: media.LauncherID,
		Slot:       slot,
		Elapsed:    elapsed,
	}
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
