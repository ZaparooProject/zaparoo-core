//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildStartCommand(t *testing.T) {
	t.Parallel()

	msg, err := buildStartCommand("snes", snesGame)
	require.NoError(t, err)
	assert.Equal(t, "START|snes|"+snesGame, string(msg))

	for _, bad := range []string{"/roms/a|b.sfc", "/roms/a\nb.sfc", "/roms/a\x00b.sfc", ""} {
		_, err = buildStartCommand("snes", bad)
		require.Error(t, err, "%q", bad)
	}
	_, err = buildStartCommand("sn|es", snesGame)
	require.Error(t, err)

	_, err = buildStartCommand("snes", "/"+strings.Repeat("a", maxCommandSize))
	require.Error(t, err, "longer than the ES receive buffer")
}

// fakeES listens like EmulationStation's command thread and hands each
// datagram to the test.
func fakeES(t *testing.T) (addr string, frames <-chan string) {
	t.Helper()
	conn, err := (&net.ListenConfig{}).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	out := make(chan string, 4)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, _, readErr := conn.ReadFrom(buf)
			if readErr != nil {
				close(out)
				return
			}
			out <- string(buf[:n])
		}
	}()
	return conn.LocalAddr().String(), out
}

func TestLaunchGame_SendsStartAndWaitsForES(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, snesGame, "rom")
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "Old", snesGame))
	p, rec, _ := newTestPlatform(t, fs)
	p.refresh()

	addr, frames := fakeES(t)
	p.commandAddr = addr
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	go func() {
		frame := <-frames
		if frame != "START|snes|"+snesGame {
			t.Errorf("unexpected frame %q", frame)
			return
		}
		// ES ends the old run before the new one starts.
		for _, st := range []string{
			stateText("endgame", "snes", "Old", snesGame),
			stateText("rungame", "snes", "Super Mario World", snesGame),
		} {
			if writeErr := afero.WriteFile(fs, stateFile, []byte(st), 0o644); writeErr != nil {
				t.Error(writeErr)
				return
			}
			p.poke()
		}
	}()

	proc, err := p.launchGame(cfg, snesGame, nil)
	require.NoError(t, err)
	assert.Nil(t, proc)
	media := rec.get()
	require.NotNil(t, media)
	assert.Equal(t, snesGame, media.Path)
	assert.Equal(t, "Super Mario World", media.Name)
}

func TestLaunchGame_TimesOutWhenESIgnoresIt(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	writeFile(t, fs, snesGame, "rom")
	p, rec, clock := newTestPlatform(t, fs)
	addr, frames := fakeES(t)
	p.commandAddr = addr
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	errs := make(chan error, 1)
	go func() {
		_, launchErr := p.launchGame(cfg, snesGame, nil)
		errs <- launchErr
	}()
	<-frames
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, clock.BlockUntilContext(ctx, 2))
	clock.Advance(launchConfirmTimeout)

	select {
	case launchErr := <-errs:
		require.Error(t, launchErr)
		assert.Contains(t, launchErr.Error(), "did not start the game")
	case <-ctx.Done():
		t.Fatal("launch did not time out")
	}
	assert.Nil(t, rec.get())
}

func TestLaunchGame_RejectsBadPaths(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	require.NoError(t, fs.MkdirAll("/recalbox/share/roms/snes/dir.sfc", 0o755))
	writeFile(t, fs, "/recalbox/share/roms/240ptestsuite/a.k7", "rom")
	writeFile(t, fs, "/recalbox/share/roms/other/a.sfc", "rom")
	writeFile(t, fs, "/recalbox/share/roms/snes/a|b.sfc", "rom")
	p, _, _ := newTestPlatform(t, fs)
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)

	for _, path := range []string{
		"snes/a.sfc",
		"/recalbox/share/roms/snes/missing.sfc",
		"/recalbox/share/roms/snes/dir.sfc",
		"/recalbox/share/roms/other/a.sfc",
		"/recalbox/share/roms/snes/a|b.sfc",
	} {
		_, err := p.launchGame(cfg, path, nil)
		assert.Error(t, err, path)
	}
}

func TestLaunchGame_NoSystemList(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	writeFile(t, fs, snesGame, "rom")
	p, _, _ := newTestPlatform(t, fs)
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	_, err = p.launchGame(cfg, snesGame, nil)
	require.Error(t, err)
}

func TestSendStart_SpacesCommandsPastESRepeatGuard(t *testing.T) {
	t.Parallel()
	p, _, clock := newTestPlatform(t, newTestFs(t))
	addr, frames := fakeES(t)
	p.commandAddr = addr
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := p.sendStart(ctx, []byte("START|snes|/a"))
	require.NoError(t, err)
	assert.Equal(t, "START|snes|/a", <-frames)

	sent := make(chan error, 1)
	go func() {
		_, sendErr := p.sendStart(ctx, []byte("START|snes|/b"))
		sent <- sendErr
	}()
	require.NoError(t, clock.BlockUntilContext(ctx, 1))
	select {
	case frame := <-frames:
		t.Fatalf("second START sent inside the repeat guard: %q", frame)
	default:
	}
	clock.Advance(startSpacing)
	require.NoError(t, <-sent)
	assert.Equal(t, "START|snes|/b", <-frames)
}

func TestLaunchGame_IgnoresIndexRootsESDoesNotList(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	rom := "/media/extra/snes/a.sfc"
	writeFile(t, fs, rom, "rom")
	p, _, _ := newTestPlatform(t, fs)
	addr, frames := fakeES(t)
	p.commandAddr = addr
	cfg, err := helpers.NewTestConfig(helpers.NewMemoryFS(), t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cfg.LoadTOML("[launchers]\nindex_root = [\"/media/extra\"]\n"))

	errs := make(chan error, 1)
	go func() {
		_, launchErr := p.launchGame(cfg, rom, nil)
		errs <- launchErr
	}()
	select {
	case launchErr := <-errs:
		require.ErrorContains(t, launchErr, "no Recalbox system holds")
	case frame := <-frames:
		t.Fatalf("sent %q for a ROM outside ES's roots", frame)
	case <-time.After(5 * time.Second):
		t.Fatal("launch did not fail")
	}
}
