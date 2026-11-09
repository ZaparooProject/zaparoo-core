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

package zapscript

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mp3Frame is one silent MPEG-1 Layer III frame header and padding, enough
// for a sniffer to see an MP3 sync word.
var mp3Frame = append([]byte{0xFF, 0xFB, 0x90, 0x64}, make([]byte, 413)...)

const browserLauncherID = "WebBrowser"

func streamServer(t *testing.T, contentType string, body []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", contentType)
		for range 64 {
			if _, err := w.Write(body); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// icyServer answers like a Shoutcast v1 server, whose status line is
// "ICY 200 OK" rather than HTTP.
func icyServer(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				r := bufio.NewReader(c)
				for {
					line, readErr := r.ReadString('\n')
					if readErr != nil || line == "\r\n" || line == "\n" {
						break
					}
				}
				_, _ = c.Write([]byte("ICY 200 OK\r\nicy-name:Test Station\r\ncontent-type:audio/mpeg\r\n\r\n"))
				for range 64 {
					if _, writeErr := c.Write(mp3Frame); writeErr != nil {
						return
					}
				}
			}(conn)
		}
	}()
	return "http://" + ln.Addr().String() + "/stream"
}

func routerLaunchers() []platforms.Launcher {
	return []platforms.Launcher{
		platforms.NativeAudioLauncher(nil, nil, nil, nil),
		{ID: browserLauncherID, Schemes: []string{"http", "https"}},
	}
}

func runRouterLaunch(t *testing.T, link string, adv map[string]string, wantLauncher string, wantStream bool) {
	t.Helper()
	mockPlatform := mocks.NewMockPlatform()
	cfg := &config.Instance{}
	mockPlatform.On("Launchers", cfg).Return(routerLaunchers())
	mockPlatform.On("RootDirs", cfg).Return([]string{t.TempDir()}).Maybe()
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: t.TempDir()}).Maybe()
	mockPlatform.On("LaunchMedia", cfg, link,
		mock.MatchedBy(func(l *platforms.Launcher) bool {
			if wantLauncher == "" {
				return l == nil || l.ID != platforms.NativeAudioLauncherID
			}
			return l != nil && l.ID == wantLauncher
		}),
		mock.Anything,
		mock.MatchedBy(func(opts *platforms.LaunchOptions) bool {
			if wantStream {
				return opts != nil && opts.AudioStream != nil
			}
			return opts == nil || opts.AudioStream == nil
		}),
	).Return(nil)

	env := platforms.CmdEnv{
		Cmd: zapscript.Command{
			Name:    "launch",
			Args:    []string{link},
			AdvArgs: zapscript.NewAdvArgs(adv),
		},
		Cfg:         cfg,
		LauncherCtx: t.Context(),
	}
	result, err := cmdLaunchWithFS(afero.NewMemMapFs(), mockPlatform, env)
	require.NoError(t, err)
	assert.True(t, result.MediaChanged)
	mockPlatform.AssertExpectations(t)
}

func TestCmdLaunch_StreamRouter(t *testing.T) {
	t.Parallel()

	mp3Srv, _ := streamServer(t, "audio/mpeg", mp3Frame)
	adtsFrame := append([]byte{0xFF, 0xF1, 0x50, 0x80, 0x02, 0x1F, 0xFC}, make([]byte, 9)...)
	aacSrv, _ := streamServer(t, "audio/aacp", adtsFrame)
	hlsSrv, _ := streamServer(t, "application/vnd.apple.mpegurl",
		[]byte("#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-MEDIA-SEQUENCE:1\n#EXTINF:6,\nseg1.aac\n"))
	plsSrv, _ := streamServer(t, "audio/x-scpls",
		[]byte("[playlist]\nFile1=http://127.0.0.1:1/live\nNumberOfEntries=1\n"))
	sniffSrv, _ := streamServer(t, "application/octet-stream", mp3Frame)
	htmlSrv, _ := streamServer(t, "text/html; charset=utf-8", []byte("<!doctype html><html></html>"))

	offline := httptest.NewServer(http.NotFoundHandler())
	offlineURL := offline.URL + "/stream"
	offline.Close()

	const audioID = platforms.NativeAudioLauncherID
	tests := []struct {
		name         string
		link         string
		wantLauncher string
		wantStream   bool
	}{
		{name: "audio mime", link: mp3Srv.URL + "/live", wantLauncher: audioID, wantStream: true},
		{name: "aac mime", link: aacSrv.URL + "/live", wantLauncher: audioID, wantStream: true},
		{name: "shoutcast v1 status line", link: icyServer(t), wantLauncher: audioID, wantStream: true},
		{name: "hls playlist", link: hlsSrv.URL + "/live.m3u8", wantLauncher: audioID, wantStream: true},
		{name: "pls playlist", link: plsSrv.URL + "/listen.pls", wantLauncher: audioID, wantStream: true},
		{name: "generic mime sniffed as audio", link: sniffSrv.URL + "/live", wantLauncher: audioID, wantStream: true},
		{name: "html page launches as before", link: htmlSrv.URL + "/page"},
		{name: "unreachable host launches as before", link: offlineURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runRouterLaunch(t, tt.link, map[string]string{}, tt.wantLauncher, tt.wantStream)
		})
	}
}

func TestCmdLaunch_StreamRouterExplicitLauncherSkipsProbe(t *testing.T) {
	t.Parallel()

	srv, hits := streamServer(t, "audio/mpeg", mp3Frame)
	link := srv.URL + "/live"
	runRouterLaunch(t, link, map[string]string{"launcher": browserLauncherID}, browserLauncherID, false)
	assert.Zero(t, hits.Load(), "an explicit launcher must not probe the URL")
}

// Plain http is accepted for audio streams, while public ZapScript links
// stay https-only.
func TestStreamRouter_PlainHTTPOnlyForAudio(t *testing.T) {
	t.Parallel()

	srv, _ := streamServer(t, "audio/mpeg", mp3Frame)
	link := srv.URL + "/live"

	public, err := url.Parse("http://radio.example.com/live")
	require.NoError(t, err)
	require.Error(t, validateZapLinkURL(public), "public ZapScript must stay https-only")

	probe := probeAudioStream(t.Context(), link)
	require.NotNil(t, probe, "plain http audio must be accepted")
	probe.Close()
}
