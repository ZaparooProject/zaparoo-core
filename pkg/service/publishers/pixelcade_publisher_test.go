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

package publishers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPixelCadePublisher_Defaults(t *testing.T) {
	t.Parallel()

	pub := NewPixelCadePublisher("192.168.1.50", 0, "", "", nil)

	assert.Equal(t, "http://192.168.1.50:8080", pub.baseURL)
	assert.Equal(t, PixelCadeModeStream, pub.mode)
	assert.Equal(t, PixelCadeOnStopBlank, pub.onStop)
	assert.Nil(t, pub.filter)
}

func TestNewPixelCadePublisher_CustomValues(t *testing.T) {
	t.Parallel()

	pub := NewPixelCadePublisher(
		"10.0.0.5", 9090, PixelCadeModeWrite, PixelCadeOnStopMarquee,
		[]string{"media.started"},
	)

	assert.Equal(t, "http://10.0.0.5:9090", pub.baseURL)
	assert.Equal(t, PixelCadeModeWrite, pub.mode)
	assert.Equal(t, PixelCadeOnStopMarquee, pub.onStop)
	assert.Equal(t, []string{"media.started"}, pub.filter)
}

func TestPixelCadePublisher_Start_EmptyHost(t *testing.T) {
	t.Parallel()

	t.Run("default port", func(t *testing.T) {
		t.Parallel()
		pub := NewPixelCadePublisher("", 0, "", "", nil)
		err := pub.Start(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "host is required")
	})

	t.Run("custom port", func(t *testing.T) {
		t.Parallel()
		pub := NewPixelCadePublisher("", 9090, "", "", nil)
		err := pub.Start(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "host is required")
	})
}

func TestPixelCadePublisher_Start_Success(t *testing.T) {
	t.Parallel()

	pub := NewPixelCadePublisher("192.168.1.50", 0, "", "", nil)
	err := pub.Start(context.Background())

	require.NoError(t, err)
	pub.Stop()
}

func TestPixelCadePublisher_Publish_MediaStarted(t *testing.T) {
	t.Parallel()

	var receivedURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)
	pub := NewPixelCadePublisher(host, port, PixelCadeModeStream, PixelCadeOnStopNone, nil)
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	params, err := json.Marshal(models.MediaStartedParams{
		SystemID:   "NES",
		SystemName: "Nintendo Entertainment System",
		MediaName:  "Super Mario Bros",
		MediaPath:  "/roms/nes/smb.nes",
	})
	require.NoError(t, err)

	err = pub.Publish(models.Notification{
		Method: models.NotificationStarted,
		Params: params,
	})

	require.NoError(t, err)
	assert.Equal(t, "/arcade/stream/nes/smb?event=GameStart", receivedURI)
}

func TestPixelCadePublisher_Publish_MediaStarted_WriteMode(t *testing.T) {
	t.Parallel()

	var receivedURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)
	pub := NewPixelCadePublisher(host, port, PixelCadeModeWrite, PixelCadeOnStopNone, nil)
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	params, err := json.Marshal(models.MediaStartedParams{
		SystemID:   "Genesis",
		SystemName: "Sega Genesis",
		MediaName:  "Sonic The Hedgehog",
		MediaPath:  "/roms/genesis/sonic.bin",
	})
	require.NoError(t, err)

	err = pub.Publish(models.Notification{
		Method: models.NotificationStarted,
		Params: params,
	})

	require.NoError(t, err)
	assert.Equal(t, "/arcade/write/genesis/sonic?event=GameStart", receivedURI)
}

func TestPixelCadePublisher_Publish_MediaStarted_PathWithoutExtension(t *testing.T) {
	t.Parallel()

	var receivedURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)
	pub := NewPixelCadePublisher(host, port, PixelCadeModeStream, PixelCadeOnStopNone, nil)
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	params, err := json.Marshal(models.MediaStartedParams{
		SystemID:   "ScummVM",
		SystemName: "ScummVM",
		MediaName:  "Day of the Tentacle",
		MediaPath:  "scummvm://games/dayoftentacle",
	})
	require.NoError(t, err)

	err = pub.Publish(models.Notification{
		Method: models.NotificationStarted,
		Params: params,
	})

	require.NoError(t, err)
	assert.Equal(t, "/arcade/stream/scummvm/dayoftentacle?event=GameStart", receivedURI)
}

func TestPixelCadePublisher_Publish_MediaStopped_Blank(t *testing.T) {
	t.Parallel()

	var receivedURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)
	pub := NewPixelCadePublisher(host, port, PixelCadeModeStream, PixelCadeOnStopBlank, nil)
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	err := pub.Publish(models.Notification{
		Method: models.NotificationStopped,
		Params: json.RawMessage(`{}`),
	})

	require.NoError(t, err)
	assert.Equal(t, "/arcade/stream/black/dummy", receivedURI)
}

func TestPixelCadePublisher_Publish_MediaStopped_Marquee(t *testing.T) {
	t.Parallel()

	var receivedURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURI = r.RequestURI
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)
	pub := NewPixelCadePublisher(host, port, PixelCadeModeStream, PixelCadeOnStopMarquee, nil)
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	err := pub.Publish(models.Notification{
		Method: models.NotificationStopped,
		Params: json.RawMessage(`{}`),
	})

	require.NoError(t, err)
	assert.Equal(t, "/arcade/write/marquee/dummy", receivedURI)
}

func TestPixelCadePublisher_Publish_MediaStopped_None(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)
	pub := NewPixelCadePublisher(host, port, PixelCadeModeStream, PixelCadeOnStopNone, nil)
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	err := pub.Publish(models.Notification{
		Method: models.NotificationStopped,
		Params: json.RawMessage(`{}`),
	})

	require.NoError(t, err)
	assert.Equal(t, int32(0), requestCount.Load())
}

func TestPixelCadePublisher_Publish_FilteredOut(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)
	pub := NewPixelCadePublisher(host, port, PixelCadeModeStream, PixelCadeOnStopBlank, []string{"media.started"})
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	// media.stopped is not in the filter, should be ignored
	err := pub.Publish(models.Notification{
		Method: models.NotificationStopped,
		Params: json.RawMessage(`{}`),
	})

	require.NoError(t, err)
	assert.Equal(t, int32(0), requestCount.Load())
}

func TestPixelCadePublisher_Publish_UnhandledMethod(t *testing.T) {
	t.Parallel()

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)
	pub := NewPixelCadePublisher(host, port, PixelCadeModeStream, PixelCadeOnStopBlank, nil)
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	err := pub.Publish(models.Notification{
		Method: "tokens.added",
		Params: json.RawMessage(`{"uid": "test"}`),
	})

	require.NoError(t, err)
	assert.Equal(t, int32(0), requestCount.Load())
}

func TestPixelCadePublisher_Publish_ServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	host, port := splitHostPort(t, server.URL)
	pub := NewPixelCadePublisher(host, port, PixelCadeModeStream, PixelCadeOnStopNone, nil)
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	params, err := json.Marshal(models.MediaStartedParams{
		SystemID:   "NES",
		SystemName: "NES",
		MediaName:  "test",
		MediaPath:  "/test",
	})
	require.NoError(t, err)

	err = pub.Publish(models.Notification{
		Method: models.NotificationStarted,
		Params: params,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status 500")
}

func TestPixelCadePublisher_Publish_ConnectionError(t *testing.T) {
	t.Parallel()

	pub := NewPixelCadePublisher("127.0.0.1", 1, PixelCadeModeStream, PixelCadeOnStopNone, nil)
	require.NoError(t, pub.Start(context.Background()))
	defer pub.Stop()

	params, err := json.Marshal(models.MediaStartedParams{
		SystemID:   "NES",
		SystemName: "NES",
		MediaName:  "test",
		MediaPath:  "/test",
	})
	require.NoError(t, err)

	err = pub.Publish(models.Notification{
		Method: models.NotificationStarted,
		Params: params,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request to")
}

func TestPixelCadePublisher_Stop(t *testing.T) {
	t.Parallel()

	pub := NewPixelCadePublisher("192.168.1.50", 0, "", "", nil)
	require.NoError(t, pub.Start(context.Background()))

	pub.Stop()

	// Context should be cancelled after stop
	assert.Error(t, pub.ctx.Err())
}

func TestPixelCadeConsoleName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		systemID string
		want     string
	}{
		{"NES", "nes"},
		{"SNES", "snes"},
		{"Nintendo64", "n64"},
		{"GameCube", "nintendo_gamecube"},
		{"Genesis", "genesis"},
		{"MasterSystem", "mastersystem"},
		{"Dreamcast", "dreamcast"},
		{"PSX", "psx"},
		{"Atari2600", "atari2600"},
		{"Arcade", "mame"},
		{"CPS2", "capcom_play_system_ii"},
		{"NeoGeo", "neogeo"},
		{"TurboGrafx16", "pcengine"},
		{"C64", "c64"},
		{"Amiga", "amiga"},
		// Unmapped system falls back to lowercase
		{"UnknownSystem", "unknownsystem"},
	}

	for _, tt := range tests {
		t.Run(tt.systemID, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, pixelCadeConsoleName(tt.systemID))
		})
	}
}

func TestPixelCadeMediaIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mediaPath string
		want      string
	}{
		{name: "posix path", mediaPath: "/roms/nes/Super Mario Bros.nes", want: "Super Mario Bros"},
		{name: "windows path", mediaPath: `C:\ROMs\MAME\4dwarrio.zip`, want: "4dwarrio"},
		{name: "virtual path", mediaPath: "scummvm://games/dayoftentacle", want: "dayoftentacle"},
		{name: "no extension", mediaPath: "/roms/ports/Celeste", want: "Celeste"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, pixelCadeMediaIdentifier(tt.mediaPath))
		})
	}
}

// splitHostPort extracts host and port from an httptest.Server URL.
func splitHostPort(t *testing.T, rawURL string) (host string, port int) {
	t.Helper()

	// URL is like "http://127.0.0.1:12345"
	rawURL = strings.TrimPrefix(rawURL, "http://")
	parts := strings.SplitN(rawURL, ":", 2)
	require.Len(t, parts, 2)

	_, err := fmt.Sscanf(parts[1], "%d", &port)
	require.NoError(t, err)

	host = parts[0]
	return host, port
}
