package state

import (
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/rs/zerolog/log"
)

type State struct {
	mu              sync.RWMutex
	activeCard      tokens.Token // TODO: rename activeToken
	lastScanned     tokens.Token
	stopService     bool // TODO: make a channel outside state
	disableLauncher bool
	platform        platforms.Platform
	readers         map[string]readers.Reader
	softwareToken   *tokens.Token
	wroteToken      *tokens.Token
	Notifications   chan<- models.Notification
}

func NewState(platform platforms.Platform) (*State, <-chan models.Notification) {
	ns := make(chan models.Notification)
	return &State{
		platform:      platform,
		readers:       make(map[string]readers.Reader),
		Notifications: ns,
	}, ns
}

func (s *State) SetActiveCard(card tokens.Token) {
	s.mu.Lock()

	if utils.TokensEqual(&s.activeCard, &card) {
		// ignore duplicate scans
		s.mu.Unlock()
		return
	}

	s.activeCard = card
	if !s.activeCard.ScanTime.IsZero() {
		s.lastScanned = card
	}

	s.Notifications <- models.Notification{
		Method: models.TokensActive,
		Params: models.TokenResponse{
			Type:     card.Type,
			UID:      card.UID,
			Text:     card.Text,
			Data:     card.Data,
			ScanTime: card.ScanTime,
		},
	}
	s.mu.Unlock()
}

func (s *State) GetActiveCard() tokens.Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeCard
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
}

func (s *State) ShouldStopService() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopService
}

func (s *State) DisableLauncher() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disableLauncher = true
}

func (s *State) EnableLauncher() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disableLauncher = false
}

func (s *State) IsLauncherDisabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.disableLauncher
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
	s.Notifications <- models.Notification{
		Method: models.ReadersConnected,
		Params: device,
	}
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
	s.Notifications <- models.Notification{
		Method: models.ReadersDisconnected,
		Params: device,
	}
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
