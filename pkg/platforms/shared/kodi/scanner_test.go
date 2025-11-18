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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockKodiClient is a mock implementation of KodiClient interface for testing
type MockKodiClient struct {
	mock.Mock
}

func (m *MockKodiClient) LaunchFile(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock LaunchFile error: %w", err)
	}
	return nil
}

func (m *MockKodiClient) LaunchMovie(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock LaunchMovie error: %w", err)
	}
	return nil
}

func (m *MockKodiClient) LaunchTVEpisode(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock LaunchTVEpisode error: %w", err)
	}
	return nil
}

func (m *MockKodiClient) Stop() error {
	args := m.Called()
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock Stop error: %w", err)
	}
	return nil
}

func (m *MockKodiClient) Quit(ctx context.Context) error {
	args := m.Called(ctx)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock Quit error: %w", err)
	}
	return nil
}

func (m *MockKodiClient) GetActivePlayers(ctx context.Context) ([]Player, error) {
	args := m.Called(ctx)
	if players, ok := args.Get(0).([]Player); ok {
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

func (m *MockKodiClient) GetPlayerItem(ctx context.Context, playerID int) (*PlayerItem, error) {
	args := m.Called(ctx, playerID)
	if err := args.Error(1); err != nil {
		return nil, fmt.Errorf("mock GetPlayerItem error: %w", err)
	}
	if args.Get(0) == nil {
		return nil, errors.New("mock GetPlayerItem: no item configured")
	}
	item, ok := args.Get(0).(*PlayerItem)
	if !ok {
		return nil, errors.New("mock GetPlayerItem: type assertion failed")
	}
	return item, nil
}

func (m *MockKodiClient) GetMovies(ctx context.Context) ([]Movie, error) {
	args := m.Called(ctx)
	if movies, ok := args.Get(0).([]Movie); ok {
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

func (m *MockKodiClient) GetTVShows(ctx context.Context) ([]TVShow, error) {
	args := m.Called(ctx)
	if shows, ok := args.Get(0).([]TVShow); ok {
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

func (m *MockKodiClient) GetEpisodes(ctx context.Context, tvShowID int) ([]Episode, error) {
	args := m.Called(ctx, tvShowID)
	if episodes, ok := args.Get(0).([]Episode); ok {
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

func (m *MockKodiClient) GetURL() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockKodiClient) SetURL(url string) {
	m.Called(url)
}

func (m *MockKodiClient) APIRequest(ctx context.Context, method APIMethod, params any) (json.RawMessage, error) {
	args := m.Called(ctx, method, params)
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

func (m *MockKodiClient) LaunchSong(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock LaunchSong failed: %w", err)
	}
	return nil
}

func (m *MockKodiClient) LaunchAlbum(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock LaunchAlbum failed: %w", err)
	}
	return nil
}

func (m *MockKodiClient) LaunchArtist(path string) error {
	args := m.Called(path)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("mock LaunchArtist failed: %w", err)
	}
	return nil
}

func (m *MockKodiClient) LaunchTVShow(path string) error {
	args := m.Called(path)
	return args.Error(0) //nolint:wrapcheck // Mock implementation, error wrapping not needed
}

func (m *MockKodiClient) GetSongs(ctx context.Context) ([]Song, error) {
	args := m.Called(ctx)
	if songs, ok := args.Get(0).([]Song); ok {
		return songs, args.Error(1) //nolint:wrapcheck // Mock implementation, error wrapping not needed
	}
	return nil, args.Error(1) //nolint:wrapcheck // Mock implementation, error wrapping not needed
}

func (m *MockKodiClient) GetAlbums(ctx context.Context) ([]Album, error) {
	args := m.Called(ctx)
	if albums, ok := args.Get(0).([]Album); ok {
		return albums, args.Error(1) //nolint:wrapcheck // Mock implementation, error wrapping not needed
	}
	return nil, args.Error(1) //nolint:wrapcheck // Mock implementation, error wrapping not needed
}

func (m *MockKodiClient) GetArtists(ctx context.Context) ([]Artist, error) {
	args := m.Called(ctx)
	if artists, ok := args.Get(0).([]Artist); ok {
		return artists, args.Error(1) //nolint:wrapcheck // Mock implementation, error wrapping not needed
	}
	return nil, args.Error(1) //nolint:wrapcheck // Mock implementation, error wrapping not needed
}

func TestScanMovies(t *testing.T) {
	t.Parallel()
	// Create mock client
	mockClient := new(MockKodiClient)

	// Mock data
	expectedMovies := []Movie{
		{ID: 1, Label: "The Matrix"},
		{ID: 2, Label: "Blade Runner"},
	}

	// Set up mock expectation
	mockClient.On("GetMovies", mock.Anything).Return(expectedMovies, nil)

	// Execute function
	cfg := &config.Instance{}
	results, err := ScanMovies(context.Background(), mockClient, cfg, "", []platforms.ScanResult{})

	// Assert results
	require.NoError(t, err)
	assert.Len(t, results, 2)

	assert.Equal(t, "The Matrix", results[0].Name)
	assert.Equal(t, "kodi-movie://1/The%20Matrix", results[0].Path)

	assert.Equal(t, "Blade Runner", results[1].Name)
	assert.Equal(t, "kodi-movie://2/Blade%20Runner", results[1].Path)

	// Verify mock was called
	mockClient.AssertExpectations(t)
}

func TestScanTV(t *testing.T) {
	t.Parallel()
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
	mockClient.On("GetTVShows", mock.Anything).Return(expectedTVShows, nil)
	mockClient.On("GetEpisodes", mock.Anything, 1).Return(expectedEpisodesBB, nil)
	mockClient.On("GetEpisodes", mock.Anything, 2).Return(expectedEpisodesWire, nil)

	// Execute function
	cfg := &config.Instance{}
	results, err := ScanTV(context.Background(), mockClient, cfg, "", []platforms.ScanResult{})

	// Assert results
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Check Breaking Bad episodes
	assert.Equal(t, "Breaking Bad - Pilot", results[0].Name)
	assert.Equal(t, "kodi-episode://101/Breaking%20Bad%20-%20Pilot", results[0].Path)

	assert.Equal(t, "Breaking Bad - Cat's in the Bag...", results[1].Name)
	assert.Equal(t, "kodi-episode://102/Breaking%20Bad%20-%20Cat%27s%20in%20the%20Bag...", results[1].Path)

	// Check The Wire episode
	assert.Equal(t, "The Wire - The Target", results[2].Name)
	assert.Equal(t, "kodi-episode://201/The%20Wire%20-%20The%20Target", results[2].Path)

	// Verify all mocks were called
	mockClient.AssertExpectations(t)
}

func TestScanSongs(t *testing.T) {
	t.Parallel()
	// Create mock client
	mockClient := new(MockKodiClient)

	// Mock songs data
	expectedSongs := []Song{
		{ID: 123, Label: "Bohemian Rhapsody", Artist: "Queen", Album: "A Night at the Opera", AlbumID: 456, Duration: 355},
		{ID: 124, Label: "Stairway to Heaven", Artist: "Led Zeppelin", Album: "Led Zeppelin IV", AlbumID: 457, Duration: 482},
	}

	// Set up mock expectation
	mockClient.On("GetSongs", mock.Anything).Return(expectedSongs, nil)

	// Execute function
	cfg := &config.Instance{}
	results, err := ScanSongs(context.Background(), mockClient, cfg, "", []platforms.ScanResult{})

	// Assert results
	require.NoError(t, err)
	assert.Len(t, results, 2)

	assert.Equal(t, "Queen - A Night at the Opera - Bohemian Rhapsody", results[0].Name)
	assert.Equal(t, "kodi-song://123/Queen%20-%20A%20Night%20at%20the%20Opera%20-%20Bohemian%20Rhapsody", results[0].Path)

	assert.Equal(t, "Led Zeppelin - Led Zeppelin IV - Stairway to Heaven", results[1].Name)
	assert.Equal(t, "kodi-song://124/Led%20Zeppelin%20-%20Led%20Zeppelin%20IV%20-%20Stairway%20to%20Heaven", results[1].Path)

	// Verify mock was called
	mockClient.AssertExpectations(t)
}

func TestScanArtists(t *testing.T) {
	t.Parallel()
	// Create mock client
	mockClient := new(MockKodiClient)

	// Mock artists data - includes "Various Artists" that should be filtered
	expectedArtists := []Artist{
		{ID: 1, Label: "Queen"},
		{ID: 2, Label: "Led Zeppelin"},
		{ID: 3, Label: "Various Artists"}, // Should be filtered out
		{ID: 4, Label: "Various"},         // Should be filtered out
		{ID: 5, Label: "Pink Floyd"},
	}

	// Set up mock expectation
	mockClient.On("GetArtists", mock.Anything).Return(expectedArtists, nil)

	// Execute function
	cfg := &config.Instance{}
	results, err := ScanArtists(context.Background(), mockClient, cfg, "", []platforms.ScanResult{})

	// Assert results - should exclude "Various Artists" and "Various"
	require.NoError(t, err)
	assert.Len(t, results, 3) // Only Queen, Led Zeppelin, and Pink Floyd

	assert.Equal(t, "Queen", results[0].Name)
	assert.Equal(t, "kodi-artist://1/Queen", results[0].Path)

	assert.Equal(t, "Led Zeppelin", results[1].Name)
	assert.Equal(t, "kodi-artist://2/Led%20Zeppelin", results[1].Path)

	assert.Equal(t, "Pink Floyd", results[2].Name)
	assert.Equal(t, "kodi-artist://5/Pink%20Floyd", results[2].Path)

	// Verify mock was called
	mockClient.AssertExpectations(t)
}

func TestScanTVShows(t *testing.T) {
	t.Parallel()
	// Create mock client
	mockClient := new(MockKodiClient)

	// Mock TV shows data
	expectedTVShows := []TVShow{
		{ID: 1, Label: "Breaking Bad"},
		{ID: 2, Label: "The Wire"},
		{ID: 3, Label: "Better Call Saul"},
	}

	// Set up mock expectation
	mockClient.On("GetTVShows", mock.Anything).Return(expectedTVShows, nil)

	// Execute function
	cfg := &config.Instance{}
	results, err := ScanTVShows(context.Background(), mockClient, cfg, "", []platforms.ScanResult{})

	// Assert results
	require.NoError(t, err)
	assert.Len(t, results, 3)

	assert.Equal(t, "Breaking Bad", results[0].Name)
	assert.Equal(t, "kodi-show://1/Breaking%20Bad", results[0].Path)

	assert.Equal(t, "The Wire", results[1].Name)
	assert.Equal(t, "kodi-show://2/The%20Wire", results[1].Path)

	assert.Equal(t, "Better Call Saul", results[2].Name)
	assert.Equal(t, "kodi-show://3/Better%20Call%20Saul", results[2].Path)

	// Verify mock was called
	mockClient.AssertExpectations(t)
}
