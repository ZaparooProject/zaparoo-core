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

package kodi

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockKodiClient is a mock implementation of KodiClient interface for testing
type MockKodiClient struct {
	mock.Mock
}

func (m *MockKodiClient) LaunchFile(path string) error {
	args := m.Called(path)
	return args.Error(0)
}

func (m *MockKodiClient) LaunchMovie(path string) error {
	args := m.Called(path)
	return args.Error(0)
}

func (m *MockKodiClient) LaunchTVEpisode(path string) error {
	args := m.Called(path)
	return args.Error(0)
}

func (m *MockKodiClient) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockKodiClient) GetActivePlayers() ([]Player, error) {
	args := m.Called()
	return args.Get(0).([]Player), args.Error(1)
}

func (m *MockKodiClient) GetMovies() ([]Movie, error) {
	args := m.Called()
	return args.Get(0).([]Movie), args.Error(1)
}

func (m *MockKodiClient) GetTVShows() ([]TVShow, error) {
	args := m.Called()
	return args.Get(0).([]TVShow), args.Error(1)
}

func (m *MockKodiClient) GetEpisodes(tvShowID int) ([]Episode, error) {
	args := m.Called(tvShowID)
	return args.Get(0).([]Episode), args.Error(1)
}

func (m *MockKodiClient) GetURL() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockKodiClient) SetURL(url string) {
	m.Called(url)
}

func (m *MockKodiClient) APIRequest(method APIMethod, params any) (json.RawMessage, error) {
	args := m.Called(method, params)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func TestScanMovies(t *testing.T) {
	// Create mock client
	mockClient := new(MockKodiClient)

	// Mock data
	expectedMovies := []Movie{
		{ID: 1, Label: "The Matrix"},
		{ID: 2, Label: "Blade Runner"},
	}

	// Set up mock expectation
	mockClient.On("GetMovies").Return(expectedMovies, nil)

	// Execute function
	cfg := &config.Instance{}
	results, err := ScanMovies(mockClient, cfg, "", []platforms.ScanResult{})

	// Assert results
	assert.NoError(t, err)
	assert.Len(t, results, 2)

	assert.Equal(t, "The Matrix", results[0].Name)
	assert.Equal(t, "kodi-movie://1/The Matrix", results[0].Path)

	assert.Equal(t, "Blade Runner", results[1].Name)
	assert.Equal(t, "kodi-movie://2/Blade Runner", results[1].Path)

	// Verify mock was called
	mockClient.AssertExpectations(t)
}

func TestScanTV(t *testing.T) {
	// Create mock client
	mockClient := new(MockKodiClient)

	// Mock TV shows data
	expectedTVShows := []TVShow{
		{ID: 1, Label: "Breaking Bad"},
		{ID: 2, Label: "The Wire"},
	}

	// Mock episodes data for Breaking Bad
	expectedEpisodesBB := []Episode{
		{ID: 101, Label: "Pilot", Season: 1, Episode: 1},
		{ID: 102, Label: "Cat's in the Bag...", Season: 1, Episode: 2},
	}

	// Mock episodes data for The Wire
	expectedEpisodesWire := []Episode{
		{ID: 201, Label: "The Target", Season: 1, Episode: 1},
	}

	// Set up mock expectations
	mockClient.On("GetTVShows").Return(expectedTVShows, nil)
	mockClient.On("GetEpisodes", 1).Return(expectedEpisodesBB, nil)
	mockClient.On("GetEpisodes", 2).Return(expectedEpisodesWire, nil)

	// Execute function
	cfg := &config.Instance{}
	results, err := ScanTV(mockClient, cfg, "", []platforms.ScanResult{})

	// Assert results
	assert.NoError(t, err)
	assert.Len(t, results, 3)

	// Check Breaking Bad episodes
	assert.Equal(t, "Breaking Bad - Pilot", results[0].Name)
	assert.Equal(t, "kodi-episode://101/Breaking Bad - Pilot", results[0].Path)

	assert.Equal(t, "Breaking Bad - Cat's in the Bag...", results[1].Name)
	assert.Equal(t, "kodi-episode://102/Breaking Bad - Cat's in the Bag...", results[1].Path)

	// Check The Wire episode
	assert.Equal(t, "The Wire - The Target", results[2].Name)
	assert.Equal(t, "kodi-episode://201/The Wire - The Target", results[2].Path)

	// Verify all mocks were called
	mockClient.AssertExpectations(t)
}

