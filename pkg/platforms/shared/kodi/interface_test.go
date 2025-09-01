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

package kodi_test

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/shared/kodi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockKodiClient is a mock implementation of KodiClient interface
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

func (m *MockKodiClient) GetActivePlayers() ([]kodi.Player, error) {
	args := m.Called()
	return args.Get(0).([]kodi.Player), args.Error(1)
}

func (m *MockKodiClient) GetMovies() ([]kodi.Movie, error) {
	args := m.Called()
	return args.Get(0).([]kodi.Movie), args.Error(1)
}

func (m *MockKodiClient) GetTVShows() ([]kodi.TVShow, error) {
	args := m.Called()
	return args.Get(0).([]kodi.TVShow), args.Error(1)
}

func (m *MockKodiClient) GetEpisodes(tvShowID int) ([]kodi.Episode, error) {
	args := m.Called(tvShowID)
	return args.Get(0).([]kodi.Episode), args.Error(1)
}

func (m *MockKodiClient) GetURL() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockKodiClient) SetURL(url string) {
	m.Called(url)
}

func (m *MockKodiClient) APIRequest(method kodi.APIMethod, params any) (json.RawMessage, error) {
	args := m.Called(method, params)
	return args.Get(0).(json.RawMessage), args.Error(1)
}

func TestKodiClient_CanBeMocked(t *testing.T) {
	t.Parallel()

	// This test drives the creation of the KodiClient interface
	// It ensures we can mock the client for TDD
	mockClient := new(MockKodiClient)

	// Setup expectation
	testPath := "/storage/videos/test.mp4"
	mockClient.On("LaunchFile", testPath).Return(nil)

	// Use the mock as a KodiClient
	var client kodi.KodiClient = mockClient

	// Execute
	err := client.LaunchFile(testPath)

	// Verify
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestNewClient_ReturnsKodiClient(t *testing.T) {
	t.Parallel()

	// This test drives the creation of NewClient function
	// We need to be able to create a real client that implements the interface

	// Simplified API - launcherID parameter removed since it was unused
	// We can't actually test the config loading without more setup,
	// so this test just ensures the constructor exists and returns the interface

	// This will fail until we implement simplified NewClient
	_ = kodi.NewClient(nil)
}

func TestKodiClient_LaunchMovie(t *testing.T) {
	t.Parallel()

	// This test drives the addition of LaunchMovie method
	mockClient := new(MockKodiClient)
	mockClient.On("LaunchMovie", "kodi-movie://123/Test Movie").Return(nil)

	// Use as KodiClient interface
	var client kodi.KodiClient = mockClient

	// Test LaunchMovie method exists and can be called
	err := client.LaunchMovie("kodi-movie://123/Test Movie")
	assert.NoError(t, err)

	mockClient.AssertExpectations(t)
}

func TestKodiClient_LaunchTVEpisode(t *testing.T) {
	t.Parallel()

	// This test drives the addition of LaunchTVEpisode method
	mockClient := new(MockKodiClient)
	mockClient.On("LaunchTVEpisode", "kodi-episode://456/Test Episode").Return(nil)

	// Use as KodiClient interface
	var client kodi.KodiClient = mockClient

	// Test LaunchTVEpisode method exists and can be called
	err := client.LaunchTVEpisode("kodi-episode://456/Test Episode")
	assert.NoError(t, err)

	mockClient.AssertExpectations(t)
}

func TestKodiClient_Stop(t *testing.T) {
	t.Parallel()

	// This test drives the addition of Stop method - critical for fixing the broken implementation
	mockClient := new(MockKodiClient)
	mockClient.On("Stop").Return(nil)

	// Use as KodiClient interface
	var client kodi.KodiClient = mockClient

	// Test Stop method exists and can be called
	err := client.Stop()
	assert.NoError(t, err)

	mockClient.AssertExpectations(t)
}

func TestKodiClient_GetActivePlayers(t *testing.T) {
	t.Parallel()

	// This test drives the addition of GetActivePlayers method
	mockClient := new(MockKodiClient)

	// Mock returning multiple active players
	expectedPlayers := []kodi.Player{
		{ID: 1, Type: "video"},
		{ID: 2, Type: "audio"},
	}
	mockClient.On("GetActivePlayers").Return(expectedPlayers, nil)

	// Use as KodiClient interface
	var client kodi.KodiClient = mockClient

	// Test GetActivePlayers method exists and can be called
	players, err := client.GetActivePlayers()
	assert.NoError(t, err)
	assert.Len(t, players, 2)
	assert.Equal(t, 1, players[0].ID)
	assert.Equal(t, "video", players[0].Type)
	assert.Equal(t, 2, players[1].ID)
	assert.Equal(t, "audio", players[1].Type)

	mockClient.AssertExpectations(t)
}

func TestKodiClient_GetMovies(t *testing.T) {
	t.Parallel()

	// This test drives the addition of GetMovies method
	mockClient := new(MockKodiClient)

	// Mock returning multiple movies from library
	expectedMovies := []kodi.Movie{
		{ID: 123, Label: "The Matrix", File: "/storage/movies/matrix.mkv"},
		{ID: 456, Label: "Inception", File: "/storage/movies/inception.mp4"},
	}
	mockClient.On("GetMovies").Return(expectedMovies, nil)

	// Use as KodiClient interface
	var client kodi.KodiClient = mockClient

	// Test GetMovies method exists and can be called
	movies, err := client.GetMovies()
	assert.NoError(t, err)
	assert.Len(t, movies, 2)
	assert.Equal(t, 123, movies[0].ID)
	assert.Equal(t, "The Matrix", movies[0].Label)
	assert.Equal(t, "/storage/movies/matrix.mkv", movies[0].File)
	assert.Equal(t, 456, movies[1].ID)
	assert.Equal(t, "Inception", movies[1].Label)
	assert.Equal(t, "/storage/movies/inception.mp4", movies[1].File)

	mockClient.AssertExpectations(t)
}

func TestKodiClient_GetTVShows(t *testing.T) {
	t.Parallel()

	// This test drives the addition of GetTVShows method
	mockClient := new(MockKodiClient)

	// Mock returning multiple TV shows from library
	expectedShows := []kodi.TVShow{
		{ID: 789, Label: "Breaking Bad"},
		{ID: 12, Label: "Better Call Saul"},
	}
	mockClient.On("GetTVShows").Return(expectedShows, nil)

	// Use as KodiClient interface
	var client kodi.KodiClient = mockClient

	// Test GetTVShows method exists and can be called
	shows, err := client.GetTVShows()
	assert.NoError(t, err)
	assert.Len(t, shows, 2)
	assert.Equal(t, 789, shows[0].ID)
	assert.Equal(t, "Breaking Bad", shows[0].Label)
	assert.Equal(t, 12, shows[1].ID) // 012 becomes 12 as int
	assert.Equal(t, "Better Call Saul", shows[1].Label)

	mockClient.AssertExpectations(t)
}

func TestKodiClient_GetEpisodes(t *testing.T) {
	t.Parallel()

	// This test drives the addition of GetEpisodes method
	mockClient := new(MockKodiClient)

	// Mock returning episodes for a specific TV show
	tvShowID := 789
	expectedEpisodes := []kodi.Episode{
		{ID: 101, Label: "Pilot", Season: 1, Episode: 1, TVShowID: tvShowID},
		{ID: 102, Label: "Cat's in the Bag...", Season: 1, Episode: 2, TVShowID: tvShowID},
	}
	mockClient.On("GetEpisodes", tvShowID).Return(expectedEpisodes, nil)

	// Use as KodiClient interface
	var client kodi.KodiClient = mockClient

	// Test GetEpisodes method exists and can be called
	episodes, err := client.GetEpisodes(tvShowID)
	assert.NoError(t, err)
	assert.Len(t, episodes, 2)
	assert.Equal(t, 101, episodes[0].ID)
	assert.Equal(t, "Pilot", episodes[0].Label)
	assert.Equal(t, 1, episodes[0].Season)
	assert.Equal(t, 1, episodes[0].Episode)
	assert.Equal(t, tvShowID, episodes[0].TVShowID)
	assert.Equal(t, 102, episodes[1].ID)
	assert.Equal(t, "Cat's in the Bag...", episodes[1].Label)
	assert.Equal(t, 1, episodes[1].Season)
	assert.Equal(t, 2, episodes[1].Episode)
	assert.Equal(t, tvShowID, episodes[1].TVShowID)

	mockClient.AssertExpectations(t)
}

func TestKodiClient_GetURL(t *testing.T) {
	t.Parallel()

	// This test drives the addition of GetURL method
	mockClient := new(MockKodiClient)

	// Mock returning a URL
	expectedURL := "http://localhost:8080/jsonrpc"
	mockClient.On("GetURL").Return(expectedURL)

	// Use as KodiClient interface
	var client kodi.KodiClient = mockClient

	// Test GetURL method exists and can be called
	url := client.GetURL()
	assert.Equal(t, expectedURL, url)

	mockClient.AssertExpectations(t)
}

func TestKodiClient_SetURL(t *testing.T) {
	t.Parallel()

	// This test drives the addition of SetURL method
	mockClient := new(MockKodiClient)

	// Mock SetURL call
	newURL := "http://192.168.1.100:8080/jsonrpc"
	mockClient.On("SetURL", newURL).Return()

	// Use as KodiClient interface
	var client kodi.KodiClient = mockClient

	// Test SetURL method exists and can be called
	client.SetURL(newURL)

	mockClient.AssertExpectations(t)
}
