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

package methods

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio/trackmeta"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func copyTrackFixture(t *testing.T, name, dir string) string {
	t.Helper()
	fixture := filepath.Join("..", "..", "audio", "trackmeta", "testdata", name)
	data, err := os.ReadFile(fixture) //nolint:gosec // test fixture
	require.NoError(t, err)
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600)) //nolint:gosec // test path from t.TempDir()
	return path
}

// activeImageEnv sets up a slot playing trackPath through native audio.
func activeImageEnv(t *testing.T, slot, trackPath, launcherID, params string) requests.RequestEnv {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)
	drainNotifications(t, ns)

	media := models.NewActiveMedia("Audio", "Audio", trackPath, "Track", launcherID)
	if slot == mediaslot.Background {
		st.SetBackgroundMedia(media)
	} else {
		st.SetActiveMedia(media)
	}
	return requests.RequestEnv{
		Context:         context.Background(),
		Platform:        pl,
		State:           st,
		PlaybackManager: newStubPlaybackManager(slot, audio.PlaybackState{Path: trackPath, Playing: true}),
		Params:          json.RawMessage(params),
	}
}

func requireClientError(t *testing.T, err error, contains string) {
	t.Helper()
	require.Error(t, err)
	var clientErr *models.ClientError
	require.ErrorAs(t, err, &clientErr, "%v is a client error", err)
	assert.Contains(t, err.Error(), contains)
}

func TestHandleMediaActiveImage_EmbeddedInline(t *testing.T) {
	t.Parallel()
	track := copyTrackFixture(t, "tagged.mp3", t.TempDir())
	env := activeImageEnv(t, mediaslot.Background, track, platforms.NativeAudioLauncherID, `{"slot":"bg"}`)

	result, err := HandleMediaActiveImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "audio:embedded", resp.TypeTag)
	assert.Equal(t, "image/png", resp.ContentType)
	assert.Equal(t, "inline", resp.Delivery)
	require.NotNil(t, resp.Extension)
	assert.Equal(t, "png", *resp.Extension)
	data, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(data, []byte("\x89PNG")))
}

func TestHandleMediaActiveImage_FolderArtResized(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	track := copyTrackFixture(t, "tagged.ogg", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cover.PNG"), testPNG(t, 300), 0o600))
	env := activeImageEnv(t, mediaslot.Primary, track, platforms.NativeAudioLauncherID, `{"maxSize":64}`)

	result, err := HandleMediaActiveImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "audio:folder", resp.TypeTag)
	data, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	assert.LessOrEqual(t, max(cfg.Width, cfg.Height), 256, "the art is resized to a size tier")
}

// Not parallel: it installs the process-wide thumbnail cache.
func TestHandleMediaActiveImage_LocalPathIsCached(t *testing.T) {
	cache := newMemThumbCache()
	installThumbCache(t, cache)

	dir := t.TempDir()
	track := copyTrackFixture(t, "tagged.ogg", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "folder.png"), testPNG(t, 300), 0o600))
	params := `{"maxSize":128,"delivery":"localPath"}`
	env := activeImageEnv(t, mediaslot.Primary, track, platforms.NativeAudioLauncherID, params)

	result, err := HandleMediaActiveImage(env)
	require.NoError(t, err)
	first, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "localPath", first.Delivery)
	assert.Equal(t, "audio:folder", first.TypeTag)
	require.NotEmpty(t, first.LocalPath)

	reads := 0
	readTrackPicture = func(path string, maxBytes int64) (*trackmeta.Picture, error) {
		reads++
		return trackmeta.ReadPicture(path, maxBytes)
	}
	t.Cleanup(func() { readTrackPicture = trackmeta.ReadPicture })

	result, err = HandleMediaActiveImage(env)
	require.NoError(t, err)
	second, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, first.LocalPath, second.LocalPath)
	assert.Equal(t, "audio:folder", second.TypeTag)
	assert.Zero(t, reads, "a cached thumbnail is served without reading the track again")
}

func TestHandleMediaActiveImage_ClientErrors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	tagged := copyTrackFixture(t, "tagged.mp3", dir)
	noArtDir := t.TempDir()
	noArt := copyTrackFixture(t, "tagged.ogg", noArtDir)

	t.Run("nothing playing", func(t *testing.T) {
		t.Parallel()
		env := activeImageEnv(t, mediaslot.Primary, tagged, platforms.NativeAudioLauncherID, `{"slot":"background"}`)
		_, err := HandleMediaActiveImage(env)
		requireClientError(t, err, "no active audio track")
	})
	t.Run("not native audio", func(t *testing.T) {
		t.Parallel()
		env := activeImageEnv(t, mediaslot.Primary, tagged, "retroarch", `{}`)
		_, err := HandleMediaActiveImage(env)
		requireClientError(t, err, "no active audio track")
	})
	t.Run("no art", func(t *testing.T) {
		t.Parallel()
		env := activeImageEnv(t, mediaslot.Primary, noArt, platforms.NativeAudioLauncherID, ``)
		_, err := HandleMediaActiveImage(env)
		requireClientError(t, err, "no cover art")
	})
	t.Run("local path needs a size", func(t *testing.T) {
		t.Parallel()
		env := activeImageEnv(t, mediaslot.Primary, tagged, platforms.NativeAudioLauncherID, `{"delivery":"localPath"}`)
		_, err := HandleMediaActiveImage(env)
		requireClientError(t, err, "maxSize")
	})
	t.Run("clients never name a path", func(t *testing.T) {
		t.Parallel()
		env := activeImageEnv(t, mediaslot.Primary, tagged, platforms.NativeAudioLauncherID, `{"path":"/etc/passwd"}`)
		_, err := HandleMediaActiveImage(env)
		requireClientError(t, err, "invalid params")
	})
	t.Run("bad slot", func(t *testing.T) {
		t.Parallel()
		env := activeImageEnv(t, mediaslot.Primary, tagged, platforms.NativeAudioLauncherID, `{"slot":"side"}`)
		_, err := HandleMediaActiveImage(env)
		requireClientError(t, err, "invalid slot")
	})
}
