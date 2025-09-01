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

package mocks

import (
	"encoding/json"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/kodi"
	"github.com/stretchr/testify/mock"
)

// MockKodiClient is a mock implementation of the KodiClient interface
// for use in tests. It provides all the standard testify/mock functionality.
type MockKodiClient struct {
	mock.Mock
}

// Ensure MockKodiClient implements KodiClient at compile time
var _ kodi.KodiClient = (*MockKodiClient)(nil)

// LaunchFile mocks launching a file in Kodi
func (m *MockKodiClient) LaunchFile(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock LaunchFile error: %w", err)
	}
	return nil
}

// LaunchMovie mocks launching a movie in Kodi
func (m *MockKodiClient) LaunchMovie(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock LaunchMovie error: %w", err)
	}
	return nil
}

// LaunchTVEpisode mocks launching a TV episode in Kodi
func (m *MockKodiClient) LaunchTVEpisode(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock LaunchTVEpisode error: %w", err)
	}
	return nil
}

// Stop mocks stopping all active players in Kodi
func (m *MockKodiClient) Stop() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock Stop error: %w", err)
	}
	return nil
}

// GetActivePlayers mocks retrieving all active players in Kodi
func (m *MockKodiClient) GetActivePlayers() ([]kodi.Player, error) {
	args := m.Called()
	if players, ok := args.Get(0).([]kodi.Player); ok {
		if err := args.Error(1); err != nil {
			return nil, fmt.Errorf("mock GetActivePlayers error: %w", err)
		}
		return players, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock GetActivePlayers error: %w", err)
	}
	return nil, nil
}

// GetMovies mocks retrieving all movies from Kodi's library
func (m *MockKodiClient) GetMovies() ([]kodi.Movie, error) {
	args := m.Called()
	if movies, ok := args.Get(0).([]kodi.Movie); ok {
		if err := args.Error(1); err != nil {
			return nil, fmt.Errorf("mock GetMovies error: %w", err)
		}
		return movies, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock GetMovies error: %w", err)
	}
	return nil, nil
}

// GetTVShows mocks retrieving all TV shows from Kodi's library
func (m *MockKodiClient) GetTVShows() ([]kodi.TVShow, error) {
	args := m.Called()
	if shows, ok := args.Get(0).([]kodi.TVShow); ok {
		if err := args.Error(1); err != nil {
			return nil, fmt.Errorf("mock GetTVShows error: %w", err)
		}
		return shows, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock GetTVShows error: %w", err)
	}
	return nil, nil
}

// GetEpisodes mocks retrieving all episodes for a specific TV show from Kodi's library
func (m *MockKodiClient) GetEpisodes(tvShowID int) ([]kodi.Episode, error) {
	args := m.Called(tvShowID)
	if episodes, ok := args.Get(0).([]kodi.Episode); ok {
		if err := args.Error(1); err != nil {
			return nil, fmt.Errorf("mock GetEpisodes error: %w", err)
		}
		return episodes, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock GetEpisodes error: %w", err)
	}
	return nil, nil
}

// GetURL mocks returning the current Kodi API URL
func (m *MockKodiClient) GetURL() string {
	args := m.Called()
	return args.String(0)
}

// SetURL mocks setting the Kodi API URL
func (m *MockKodiClient) SetURL(url string) {
	m.Called(url)
}

// APIRequest mocks making a raw JSON-RPC request to Kodi API
func (m *MockKodiClient) APIRequest(method kodi.APIMethod, params any) (json.RawMessage, error) {
	args := m.Called(method, params)
	if result, ok := args.Get(0).(json.RawMessage); ok {
		if err := args.Error(1); err != nil {
			return nil, fmt.Errorf("mock APIRequest error: %w", err)
		}
		return result, nil
	}
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock APIRequest error: %w", err)
	}
	return nil, nil
}

// SetupBasicMock configures the mock with common expectations
// for standard test scenarios
func (m *MockKodiClient) SetupBasicMock() {
	// Setup common successful responses
	m.On("LaunchFile", mock.AnythingOfType("string")).Return(nil).Maybe()
	m.On("LaunchMovie", mock.AnythingOfType("string")).Return(nil).Maybe()
	m.On("LaunchTVEpisode", mock.AnythingOfType("string")).Return(nil).Maybe()
	m.On("Stop").Return(nil).Maybe()
	m.On("GetActivePlayers").Return([]kodi.Player{}, nil).Maybe()
	m.On("GetMovies").Return([]kodi.Movie{}, nil).Maybe()
	m.On("GetTVShows").Return([]kodi.TVShow{}, nil).Maybe()
	m.On("GetEpisodes", mock.AnythingOfType("int")).Return([]kodi.Episode{}, nil).Maybe()
	m.On("GetURL").Return("http://localhost:8080/jsonrpc").Maybe()
	m.On("SetURL", mock.AnythingOfType("string")).Return().Maybe()
}

// NewMockKodiClient creates a new mock Kodi client with basic setup
func NewMockKodiClient() *MockKodiClient {
	client := &MockKodiClient{}
	client.SetupBasicMock()
	return client
}
