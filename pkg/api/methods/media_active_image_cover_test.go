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
	"encoding/binary"
	"encoding/json"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coverEnv publishes a game on a drive whose token declares cover, a file
// written under the drive root with data.
func coverEnv(t *testing.T, cover string, data []byte, params string) (env requests.RequestEnv, coverPath string) {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)
	drainNotifications(t, ns)

	root := t.TempDir()
	coverPath = filepath.Join(root, cover)
	require.NoError(t, os.WriteFile(coverPath, data, 0o600))
	game := filepath.Join(root, "game.sfc")
	require.NoError(t, os.WriteFile(game, []byte("rom"), 0o600))

	st.SetActiveCard(tokens.Token{
		Type:     "externaldrive",
		Text:     "game.sfc",
		ScanTime: time.Now(),
		PathRoot: root,
		Metadata: &tokens.Metadata{Name: "Drive Game", ImagePath: coverPath},
	})
	st.SetActiveMedia(models.NewActiveMedia("SNES", "Super Nintendo", game, "game", "SNES"))
	return requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		State:    st,
		Params:   json.RawMessage(params),
	}, coverPath
}

func testJPEG(t *testing.T, side int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, side, side)), nil))
	return buf.Bytes()
}

// hugeJPEG is a valid JPEG whose header declares a frame far past the pixel
// limit, so only a header check stands between it and a large allocation.
func hugeJPEG(t *testing.T) []byte {
	t.Helper()
	data := testJPEG(t, 8)
	sof := bytes.Index(data, []byte{0xff, 0xc0})
	require.Positive(t, sof)
	binary.BigEndian.PutUint16(data[sof+5:], 5000)
	binary.BigEndian.PutUint16(data[sof+7:], 5000)
	return data
}

func TestHandleMediaActiveImage_DriveCoverInline(t *testing.T) {
	t.Parallel()
	png := testPNG(t, 32)
	env, _ := coverEnv(t, "cover.png", png, ``)

	result, err := HandleMediaActiveImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "token:cover", resp.TypeTag)
	assert.Equal(t, "image/png", resp.ContentType)
	data, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	assert.Equal(t, png, data)
}

func TestHandleMediaActiveImage_DriveCoverJPEGResized(t *testing.T) {
	t.Parallel()
	env, _ := coverEnv(t, "cover.jpg", testJPEG(t, 300), `{"maxSize":64}`)

	result, err := HandleMediaActiveImage(env)
	require.NoError(t, err)
	resp, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "token:cover", resp.TypeTag)
	data, err := base64.StdEncoding.DecodeString(resp.Data)
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Less(t, cfg.Width, 300, "the cover is scaled down to the requested size tier")
}

func TestHandleMediaActiveImage_DriveCoverLocalPathIsCached(t *testing.T) {
	cache := newMemThumbCache()
	installThumbCache(t, cache)
	env, _ := coverEnv(t, "cover.png", testPNG(t, 300), `{"maxSize":128,"delivery":"localPath"}`)

	result, err := HandleMediaActiveImage(env)
	require.NoError(t, err)
	first, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, "localPath", first.Delivery)
	require.NotEmpty(t, first.LocalPath)

	result, err = HandleMediaActiveImage(env)
	require.NoError(t, err)
	second, ok := result.(models.MediaImageResponse)
	require.True(t, ok)
	assert.Equal(t, first.LocalPath, second.LocalPath)
}

func TestHandleMediaActiveImage_DriveCoverRejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cover    string
		contains string
		data     []byte
	}{
		{
			name: "not an image", cover: "cover.png", data: []byte("not an image at all"),
			contains: "not a PNG or JPEG",
		},
		{
			name: "unsupported format", cover: "cover.png", data: []byte("GIF89a\x01\x00\x01\x00"),
			contains: "not a PNG or JPEG",
		},
		{
			name: "truncated PNG", cover: "cover.png", data: []byte("\x89PNG\r\n\x1a\n"),
			contains: "not a PNG or JPEG",
		},
		{
			name: "oversized dimensions", cover: "cover.jpg", data: hugeJPEG(t),
			contains: "cover art is too large",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env, _ := coverEnv(t, tt.cover, tt.data, ``)
			_, err := HandleMediaActiveImage(env)
			requireClientError(t, err, tt.contains)
		})
	}
}

func TestHandleMediaActiveImage_DriveCoverGone(t *testing.T) {
	t.Parallel()
	env, coverPath := coverEnv(t, "cover.png", testPNG(t, 8), ``)
	require.NoError(t, os.Remove(coverPath))

	_, err := HandleMediaActiveImage(env)
	requireClientError(t, err, "no cover art")
}

func TestHandleMediaActiveImage_DriveCoverSwappedForLink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires Windows privileges")
	}
	env, coverPath := coverEnv(t, "cover.png", testPNG(t, 8), ``)
	outside := filepath.Join(t.TempDir(), "secret.png")
	require.NoError(t, os.WriteFile(outside, testPNG(t, 8), 0o600))
	require.NoError(t, os.Remove(coverPath))
	require.NoError(t, os.Symlink(outside, coverPath))

	_, err := HandleMediaActiveImage(env)
	requireClientError(t, err, "no cover art")
}

func TestHandleMediaActiveImage_DriveCoverTooManyBytes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cover.png")
	data := testPNG(t, 16)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	info, err := os.Lstat(path)
	require.NoError(t, err)

	_, err = readActiveCover(path, info, int64(len(data)-1))
	requireClientError(t, err, "cover art is too large")
}

func TestHandleMediaActiveImage_BackgroundSlotIgnoresDriveCover(t *testing.T) {
	t.Parallel()
	env, _ := coverEnv(t, "cover.png", testPNG(t, 8), `{"slot":"background"}`)

	_, err := HandleMediaActiveImage(env)
	requireClientError(t, err, "no image for the active media")
}
