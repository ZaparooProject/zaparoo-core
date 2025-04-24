package state

import (
	"context"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/notifications"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
)

type State struct {
	mu             sync.RWMutex
	runZapScript   bool
	activeToken    tokens.Token // TODO: make a pointer
	lastScanned    tokens.Token // TODO: make a pointer
	stopService    bool         // ctx used for observers when stopped
	platform       platforms.Platform
	readers        map[string]readers.Reader
	softwareToken  *tokens.Token
	wroteToken     *tokens.Token
	Notifications  chan<- models.Notification // TODO: move outside state
	activePlaylist *playlists.Playlist
	ctx            context.Context
	ctxCancelFunc  context.CancelFunc
	selectedItem   string
}

func NewState(platform platforms.Platform) (*State, <-chan models.Notification) {
	ns := make(chan models.Notification)
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

func (s *State) SetActiveCard(card tokens.Token) {
	s.mu.Lock()

	if utils.TokensEqual(&s.activeToken, &card) {
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

// Deprecated, use <-GetContext().Done() channel instead
func (s *State) ShouldStopService() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopService
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
	notifications.ReadersAdded(s.Notifications, device)
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
	delete(s.readers, device)
	notifications.ReadersRemoved(s.Notifications, device)
	s.mu.Unlock()
}

func (s *State) ListReaders() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rs []string
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

func (s *State) GetSelectedItem() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selectedItem
}

func (s *State) SetSelectedItem(selectedItem string) {
	s.mu.Lock()
	s.selectedItem = selectedItem
	s.mu.Unlock()
}

func (s *State) GetContext() context.Context {
	return s.ctx
}
