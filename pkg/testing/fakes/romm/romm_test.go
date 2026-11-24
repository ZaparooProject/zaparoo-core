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

package romm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	coreromm "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/romm"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakectl"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/internal/fakehttp"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/romm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	return root
}

func start(t *testing.T, opts romm.Options) (*romm.Server, *coreromm.Client) { //nolint:gocritic // Mirrors New.
	t.Helper()
	s := romm.New(opts)
	require.NoError(t, s.Err())
	u, err := s.Start("127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	assert.Equal(t, u, s.URL())
	c, err := coreromm.NewClient(u)
	require.NoError(t, err)
	return s, c
}

func TestLibraryThroughCoreClient(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"snes/Super Game (USA).sfc":      "snes-rom",
		"snes/Hacks/Other Game (EU).smc": "hack",
		"snes/gamelist.xml":              "<gameList/>",
		"snes/media/covers/x.sfc":        "not a rom",
		"nes/Plumber (USA).nes":          "nes",
		"megadrive/Hedgehog (USA).md":    "md",
		"notasystem/file.bin":            "x",
		".hidden/file.sfc":               "x",
	})
	s, c := start(t, romm.Options{LibraryRoot: root})
	ctx := context.Background()

	systems, err := coreromm.ServerSystems(ctx, c)
	require.NoError(t, err)
	assert.Equal(t, []string{systemdefs.SystemGenesis, systemdefs.SystemNES, systemdefs.SystemSNES}, systems)

	tests := []struct {
		system string
		names  []string
	}{
		{systemdefs.SystemSNES, []string{"Other Game (EU).smc", "Super Game (USA).sfc"}},
		{systemdefs.SystemNES, []string{"Plumber (USA).nes"}},
		{systemdefs.SystemGenesis, []string{"Hedgehog (USA).md"}},
		{systemdefs.SystemPSX, nil},
	}
	for _, tt := range tests {
		roms, romsErr := coreromm.SystemRoms(ctx, c, tt.system)
		require.NoError(t, romsErr, tt.system)
		var names []string
		for i := range roms {
			names = append(names, roms[i].FSName)
		}
		assert.ElementsMatch(t, tt.names, names, tt.system)
	}

	roms, err := coreromm.SystemRoms(ctx, c, systemdefs.SystemSNES)
	require.NoError(t, err)
	var game coreromm.Rom
	for i := range roms {
		if roms[i].FSName == "Super Game (USA).sfc" {
			game = roms[i]
		}
	}
	require.NotZero(t, game.ID)
	assert.Equal(t, "Super Game (USA)", game.Name)
	assert.Equal(t, "snes", game.PlatformSlug)

	detail, err := c.Rom(ctx, game.ID)
	require.NoError(t, err)
	assert.Equal(t, game.FSName, detail.FSName)

	layout, err := coreromm.LayoutFor(detail)
	require.NoError(t, err)
	body, err := c.Fetch(ctx, c.ContentURL(game.ID, layout.Name), 1<<20)
	require.NoError(t, err)
	assert.Equal(t, "snes-rom", string(body))

	coverURL, ok := c.AssetURL(detail.PathCoverLarge)
	require.True(t, ok)
	cover, err := c.Fetch(ctx, coverURL, 1<<20)
	require.NoError(t, err)
	_, err = png.Decode(bytes.NewReader(cover))
	require.NoError(t, err, "generated covers are real PNGs")

	_, err = c.Fetch(ctx, c.ContentURL(game.ID, "wrong.sfc"), 1<<20)
	require.ErrorIs(t, err, coreromm.ErrNotFound)
	_, err = c.Rom(ctx, 9999)
	require.ErrorIs(t, err, coreromm.ErrNotFound)

	assert.NotEmpty(t, s.Requests())
}

func TestPaging(t *testing.T) {
	t.Parallel()
	roms := make([]coreromm.Rom, 0, 1203)
	for i := range 1203 {
		name := fmt.Sprintf("Game %04d.gba", i)
		roms = append(roms, coreromm.Rom{
			ID: i + 1, PlatformID: 1, PlatformSlug: "gba", FSName: name,
			Files: []coreromm.RomFile{{ID: i + 1, FileName: name}},
		})
	}
	s, c := start(t, romm.Options{Platforms: []coreromm.Platform{{ID: 1, Slug: "gba"}}, Roms: roms})
	got, err := coreromm.SystemRoms(context.Background(), c, systemdefs.SystemGBA)
	require.NoError(t, err)
	assert.Len(t, got, 1203)
	pages := 0
	for _, r := range s.Requests() {
		if r.Path == "/api/roms" {
			pages++
		}
	}
	assert.Equal(t, 3, pages, "500 per page")
}

func TestFixedCatalogueAndFaults(t *testing.T) {
	t.Parallel()
	rom := coreromm.Rom{
		ID: 7, PlatformID: 1, PlatformSlug: "nes", Name: "Game", FSName: "Game.nes",
		Files: []coreromm.RomFile{{ID: 70, FileName: "Game.nes", FileSizeBytes: 3}},
	}
	s, c := start(t, romm.Options{
		Platforms: []coreromm.Platform{{ID: 1, Slug: "nes", Name: "NES"}},
		Roms:      []coreromm.Rom{rom},
		Content:   map[string][]byte{"7/Game.nes": []byte("abc")},
		Assets:    map[string][]byte{"/assets/romm/resources/a.png": []byte("png")},
	})
	ctx := context.Background()

	tests := []struct {
		check func(t *testing.T, err error)
		fault *fakehttp.Fault
		name  string
	}{
		{name: "no fault", check: func(t *testing.T, err error) { require.NoError(t, err) }},
		{
			name: "503", fault: &fakehttp.Fault{Status: http.StatusServiceUnavailable},
			check: func(t *testing.T, err error) { assert.True(t, coreromm.IsUnreachable(err), err) },
		},
		{
			name: "401", fault: &fakehttp.Fault{Status: http.StatusUnauthorized},
			check: func(t *testing.T, err error) { require.ErrorIs(t, err, coreromm.ErrUnauthorized) },
		},
		{
			name: "fault on another path", fault: &fakehttp.Fault{Status: 502, PathPrefix: "/assets/"},
			check: func(t *testing.T, err error) { require.NoError(t, err) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fault != nil {
				require.NoError(t, s.SetFault(*tt.fault))
				defer s.ClearFault()
			}
			_, err := c.Platforms(ctx)
			tt.check(t, err)
		})
	}

	body, err := c.Fetch(ctx, c.ContentURL(7, "Game.nes"), 10)
	require.NoError(t, err)
	assert.Equal(t, "abc", string(body))
	assetURL, ok := c.AssetURL("a.png")
	require.True(t, ok)
	body, err = c.Fetch(ctx, assetURL, 10)
	require.NoError(t, err)
	assert.Equal(t, "png", string(body))

	require.NoError(t, s.SetFault(fakehttp.Fault{DelayMS: 2000}))
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = c.Platforms(short)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	s.ClearFault()
}

func TestToken(t *testing.T) {
	t.Parallel()
	_, c := start(t, romm.Options{Token: "secret"})
	_, err := c.Platforms(context.Background())
	require.ErrorIs(t, err, coreromm.ErrUnauthorized)

	t.Cleanup(config.AddAuthEntryForTesting(c.BaseURL(), config.CredentialEntry{Bearer: "secret"}))
	_, err = c.Platforms(context.Background())
	require.NoError(t, err)
}

func TestDeviceLink(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	req := coreromm.DeviceLinkRequest{Identifier: "dev-1", Name: "Sim", Platform: "sim", Version: "1"}

	tests := []struct {
		decide  func(s *romm.Server, code string)
		wantErr error
		name    string
		token   string
		auto    bool
	}{
		{name: "approve", decide: func(s *romm.Server, code string) { s.ApproveLink(code) }},
		{
			name: "approve with fixed token", token: "fixed",
			decide: func(s *romm.Server, _ string) { s.ApproveLink("") },
		},
		{name: "auto approve", auto: true},
		{name: "deny", decide: func(s *romm.Server, code string) { s.DenyLink(code) }, wantErr: coreromm.ErrLinkDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, c := start(t, romm.Options{Token: tt.token, AutoApproveLinks: tt.auto})
			link, err := c.StartDeviceLink(ctx, req)
			require.NoError(t, err)
			assert.Equal(t, c.BaseURL()+"/pair/device", link.VerificationURL)
			assert.Contains(t, link.VerificationURLComplete, "user_code=")
			assert.Positive(t, link.Interval)

			if !tt.auto {
				_, err = c.PollDeviceToken(ctx, link.DeviceCode)
				require.ErrorIs(t, err, coreromm.ErrLinkPending)
				tt.decide(s, link.UserCode)
			}
			token, err := c.PollDeviceToken(ctx, link.DeviceCode)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, s.Token(), token)
			if tt.token != "" {
				assert.Equal(t, tt.token, token)
			}

			_, err = c.PollDeviceToken(ctx, link.DeviceCode)
			require.ErrorIs(t, err, coreromm.ErrLinkExpired, "a link is used once")

			_, err = c.Platforms(ctx)
			require.ErrorIs(t, err, coreromm.ErrUnauthorized, "the linked token is now required")
			t.Cleanup(config.AddAuthEntryForTesting(c.BaseURL(), config.CredentialEntry{Bearer: token}))
			_, err = c.Platforms(ctx)
			require.NoError(t, err)
		})
	}

	_, c := start(t, romm.Options{})
	_, err := c.PollDeviceToken(ctx, "unknown")
	require.ErrorIs(t, err, coreromm.ErrLinkExpired)
}

func TestControlAndCLI(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{"snes/A.sfc": "a"})
	s := romm.New(romm.Options{LibraryRoot: root})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	call := fakehttp.HandlerCaller(s.ControlHandler())
	ctx := context.Background()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/platforms", http.NoBody)
	require.NoError(t, err)
	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	tests := []struct {
		check   func(t *testing.T, out string)
		name    string
		wantErr string
		args    []string
	}{
		{name: "requests", args: []string{"requests"}, check: func(t *testing.T, out string) {
			var reqs []fakectl.Request
			require.NoError(t, json.Unmarshal([]byte(out), &reqs))
			require.Len(t, reqs, 1)
			assert.Equal(t, "/api/platforms", reqs[0].Path)
		}},
		{name: "requests clear", args: []string{"requests", "clear"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"ok":true}`, out)
			assert.Empty(t, s.Requests())
		}},
		{name: "platforms", args: []string{"platforms"}, check: func(t *testing.T, out string) {
			assert.Contains(t, out, `"slug":"snes"`)
		}},
		{name: "roms", args: []string{"roms"}, check: func(t *testing.T, out string) {
			assert.Contains(t, out, `"fs_name":"A.sfc"`)
		}},
		{name: "fault set", args: []string{"fault", "set", "503"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"fault":{"status":503}}`, out)
		}},
		{name: "fault show", args: []string{"fault"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"fault":{"status":503}}`, out)
		}},
		{name: "fault delay", args: []string{"fault", "delay", "10", "/api/"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"fault":{"delay_ms":10,"path_prefix":"/api/"}}`, out)
		}},
		{name: "fault clear", args: []string{"fault", "clear"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"ok":true}`, out)
			_, hasFault := faultOf(t, call)
			assert.False(t, hasFault)
		}},
		{name: "bad fault", args: []string{"fault", "set", "42"}, wantErr: "not an HTTP status"},
		{name: "token set", args: []string{"token", "set", "abc"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"token":"abc"}`, out)
			assert.Equal(t, "abc", s.Token())
		}},
		{name: "token clear", args: []string{"token", "clear"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"token":""}`, out)
		}},
		{name: "links empty", args: []string{"links"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `[]`, out)
		}},
		{name: "approve nothing", args: []string{"links", "approve"}, wantErr: "status 404"},
		{name: "reload", args: []string{"reload"}, check: func(t *testing.T, out string) {
			assert.JSONEq(t, `{"platforms":1,"roms":1}`, out)
		}},
		{name: "unknown", args: []string{"nope"}, wantErr: "unknown command"},
		{name: "none", args: nil, wantErr: "no command"},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		cliErr := romm.CLI(ctx, tt.args, call, &out)
		if tt.wantErr != "" {
			require.ErrorContains(t, cliErr, tt.wantErr, tt.name)
			continue
		}
		require.NoError(t, err, tt.name)
		tt.check(t, strings.TrimSpace(out.String()))
	}

	// A link made over HTTP shows up in the control API and can be approved there.
	c, err := coreromm.NewClient(ts.URL)
	require.NoError(t, err)
	link, err := c.StartDeviceLink(ctx, coreromm.DeviceLinkRequest{Identifier: "d", Name: "n"})
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, romm.CLI(ctx, []string{"links", "approve", link.UserCode}, call, &out))
	assert.JSONEq(t, `{"changed":1}`, out.String())
	assert.Equal(t, romm.LinkApproved, s.Links()[0].Status)
}

func faultOf(t *testing.T, call fakectl.Caller) (fakehttp.Fault, bool) {
	t.Helper()
	out, err := call(context.Background(), http.MethodGet, "fault", nil)
	require.NoError(t, err)
	var got struct {
		Fault *fakehttp.Fault `json:"fault"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	if got.Fault == nil {
		return fakehttp.Fault{}, false
	}
	return *got.Fault, true
}

func TestPlatformSlug(t *testing.T) {
	t.Parallel()
	tests := []struct{ folder, system, want string }{
		{"snes", systemdefs.SystemSNES, "snes"},
		{"megadrive", systemdefs.SystemGenesis, "genesis"},
		{"nes", systemdefs.SystemNES, "nes"},
	}
	for _, tt := range tests {
		got := romm.PlatformSlug(tt.folder, tt.system)
		assert.Equal(t, tt.want, got, tt.folder)
		id, ok := coreromm.SystemForPlatform(got, tt.folder)
		assert.True(t, ok)
		assert.Equal(t, tt.system, id)
	}
}

func TestMissingLibrary(t *testing.T) {
	t.Parallel()
	s := romm.New(romm.Options{LibraryRoot: filepath.Join(t.TempDir(), "missing")})
	require.Error(t, s.Err())
	assert.Empty(t, s.Roms())
	assert.NoError(t, s.Close(), "closing a server never started is fine")
}
