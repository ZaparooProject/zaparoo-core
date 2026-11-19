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

package romm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeServerURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ in, want string }{
		{in: "romm.local:8080", want: "http://romm.local:8080"},
		{in: " https://Romm.example/ ", want: "https://Romm.example"},
		{in: "HTTP://host/sub/path//", want: "http://host/sub/path"},
		{in: "http://192.168.1.5:3000", want: "http://192.168.1.5:3000"},
		{in: "https://host/with%20space/", want: "https://host/with%20space"},
	} {
		got, err := NormalizeServerURL(tc.in)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
	}
	for _, in := range []string{
		"", "ftp://host", "http://user:pw@host", "http://host/?q=1", "http://host/#frag", "http://",
	} {
		_, err := NormalizeServerURL(in)
		require.Error(t, err, in)
	}
}

func TestRomsFollowsPages(t *testing.T) {
	t.Parallel()

	roms := make([]Rom, 0, romPageSize*2+7)
	for i := range romPageSize*2 + 7 {
		roms = append(roms, singleFileRom(i+1, 1, "nes", fmt.Sprintf("Game %d.nes", i)))
	}
	f := newFakeServer(t, nil, roms)
	c, err := NewClient(f.URL)
	require.NoError(t, err)

	got, err := c.Roms(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, got, len(roms))
	assert.Equal(t, int32(3), f.romCalls.Load())
}

func TestContentURLEscapesOnce(t *testing.T) {
	t.Parallel()

	c, err := NewClient("http://romm.local/base")
	require.NoError(t, err)
	assert.Equal(t, "http://romm.local/base/api/roms/12/content/Game%20%28USA%29%20%5B%21%5D%3F.nes?purpose=play",
		c.ContentURL(12, "Game (USA) [!]?.nes"))
}

func TestAssetURL(t *testing.T) {
	t.Parallel()

	c, err := NewClient("http://romm.local")
	require.NoError(t, err)

	got, ok := c.AssetURL("roms/1/cover/big.png?ts=123")
	require.True(t, ok)
	assert.Equal(t, "http://romm.local/assets/romm/resources/roms/1/cover/big.png", got)

	got, ok = c.AssetURL("/assets/romm/resources/roms/1/shot.jpg")
	require.True(t, ok)
	assert.Equal(t, "http://romm.local/assets/romm/resources/roms/1/shot.jpg", got)

	for _, bad := range []string{"", "http://evil/x.png", "//evil/x.png", "roms/../../x", `roms\x`, "a\x00b"} {
		_, ok = c.AssetURL(bad)
		assert.False(t, ok, "%q", bad)
	}
}

func TestFetchStaysOnServer(t *testing.T) {
	t.Parallel()

	f := newFakeServer(t, nil, nil)
	f.content["/assets/romm/resources/a.png"] = []byte("png")
	c, err := NewClient(f.URL)
	require.NoError(t, err)

	data, err := c.Fetch(context.Background(), f.URL+"/assets/romm/resources/a.png", 10)
	require.NoError(t, err)
	assert.Equal(t, "png", string(data))

	_, err = c.Fetch(context.Background(), f.URL+"/assets/romm/resources/a.png", 2)
	require.Error(t, err, "over the size limit")
	_, err = c.Fetch(context.Background(), "http://elsewhere.example/a.png", 10)
	require.Error(t, err)
	_, err = c.Fetch(context.Background(), f.URL+"/assets/romm/resources/missing.png", 10)
	require.ErrorIs(t, err, ErrNotFound)
}

// A redirect is followed only while it stays on the configured server.
func TestFetchRefusesRedirectOffServer(t *testing.T) {
	t.Parallel()

	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("elsewhere"))
	}))
	t.Cleanup(other.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/assets/romm/resources/off.png":
			http.Redirect(w, r, other.URL+"/x.png", http.StatusFound)
		case "/assets/romm/resources/on.png":
			http.Redirect(w, r, "/assets/romm/resources/real.png", http.StatusFound)
		default:
			_, _ = w.Write([]byte("png"))
		}
	}))
	t.Cleanup(server.Close)
	c, err := NewClient(server.URL)
	require.NoError(t, err)

	data, err := c.Fetch(context.Background(), server.URL+"/assets/romm/resources/on.png", 10)
	require.NoError(t, err)
	assert.Equal(t, "png", string(data))

	_, err = c.Fetch(context.Background(), server.URL+"/assets/romm/resources/off.png", 10)
	require.ErrorContains(t, err, "redirected off the server")
}

func TestCheckRedirect(t *testing.T) {
	t.Parallel()

	c, err := NewClient("http://romm.local:8080/base")
	require.NoError(t, err)
	for target, ok := range map[string]bool{
		"http://romm.local:8080/base/api/roms": true,
		"https://romm.local/base/api/roms":     true,
		"http://romm.local:8080/other":         false,
		"http://evil.example:8080/base/api":    false,
		"http://sub.romm.local:8080/base/api":  false,
	} {
		req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
		require.NoError(t, reqErr)
		assert.Equal(t, ok, c.checkRedirect(req, nil) == nil, target)
	}

	secure, err := NewClient("https://romm.local")
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://romm.local/api/roms", http.NoBody)
	require.NoError(t, err)
	require.Error(t, secure.checkRedirect(req, nil), "https never drops to http")
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, "https://romm.local/api/roms", http.NoBody)
	require.NoError(t, err)
	require.Error(t, secure.checkRedirect(req, make([]*http.Request, maxRedirects)))
}

func TestClientSendsStoredToken(t *testing.T) {
	t.Parallel()

	f := newFakeServer(t, []Platform{{ID: 1, Slug: "nes"}}, nil)
	f.withToken(t, "tok")
	c, err := NewClient(f.URL)
	require.NoError(t, err)
	list, err := c.Platforms(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 1)
}

func TestServerErrorsAreUnreachable(t *testing.T) {
	t.Parallel()

	f := newFakeServer(t, nil, nil)
	f.failAll.Store(true)
	c, err := NewClient(f.URL)
	require.NoError(t, err)
	_, err = c.Platforms(context.Background())
	assert.True(t, IsUnreachable(err))

	f.Close()
	_, err = c.Platforms(context.Background())
	assert.True(t, IsUnreachable(err))
}

func TestSystemForPlatform(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		slug, fsSlug, want string
		ok                 bool
	}{
		{slug: "snes", want: systemdefs.SystemSNES, ok: true},
		{slug: "PSX", want: systemdefs.SystemPSX, ok: true},
		{slug: "unknown-thing", fsSlug: "genesis", want: systemdefs.SystemGenesis, ok: true},
		{slug: "custom", fsSlug: "NES", want: systemdefs.SystemNES, ok: true},
		{slug: "not-a-platform", fsSlug: "also-not"},
		{slug: "", fsSlug: ""},
	} {
		got, ok := SystemForPlatform(tt.slug, tt.fsSlug)
		assert.Equal(t, tt.ok, ok, "%s/%s", tt.slug, tt.fsSlug)
		assert.Equal(t, tt.want, got, "%s/%s", tt.slug, tt.fsSlug)
	}
}

// Every mapped system must exist, or a ROM indexes under a system Core
// cannot launch.
func TestSlugSystemsExist(t *testing.T) {
	t.Parallel()

	for slug, id := range slugSystems {
		_, err := systemdefs.GetSystem(id)
		assert.NoError(t, err, "slug %s maps to unknown system %s", slug, id)
	}
}
