//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const snesGame = "/recalbox/share/roms/snes/Super Mario World (USA).sfc"

func TestParseState(t *testing.T) {
	t.Parallel()

	st, ok := parseState([]byte(stateText("rungame", "snes", "Super Mario World", snesGame)))
	require.True(t, ok)
	assert.Equal(t, esState{Action: "rungame", SystemID: "snes", Game: "Super Mario World", GamePath: snesGame}, st)

	crlf := strings.ReplaceAll(stateText("RunGame", "snes", "Game", snesGame), "\n", "\r\n")
	st, ok = parseState([]byte(crlf))
	require.True(t, ok)
	assert.Equal(t, "rungame", st.Action)
	assert.Equal(t, snesGame, st.GamePath)

	_, ok = parseState([]byte("Action=rungame\nSystemId=snes\nGamePath=" + snesGame + "\n"))
	assert.False(t, ok, "a file without State= is still being written")

	_, ok = parseState([]byte("State=playing\n"))
	assert.False(t, ok, "no action")

	_, ok = parseState([]byte("Game=" + strings.Repeat("x", maxStateSize+1) + "\nAction=rungame\nState=playing\n"))
	assert.False(t, ok, "oversized line")
}

func TestReadState_RejectsOversizedFile(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, _, _ := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "x", snesGame)+strings.Repeat("#\n", maxStateSize))
	_, ok := p.readState()
	assert.False(t, ok)
}

func TestTracker_GameStartAndEnd(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, _ := newTestPlatform(t, fs)

	writeFile(t, fs, stateFile, stateText("systembrowsing", "snes", "", ""))
	p.refresh()
	assert.Nil(t, rec.get())

	writeFile(t, fs, stateFile, stateText("rungame", "snes", "Super Mario World", snesGame))
	p.refresh()
	media := rec.get()
	require.NotNil(t, media)
	assert.Equal(t, systemdefs.SystemSNES, media.SystemID)
	assert.Equal(t, "SNES", media.LauncherID)
	assert.Equal(t, snesGame, media.Path)
	assert.Equal(t, "Super Mario World", media.Name)

	writeFile(t, fs, stateFile, stateText("scrapgame", "snes", "Super Mario World", snesGame))
	p.refresh()
	assert.Same(t, media, rec.get(), "scraper events do not end the game")

	writeFile(t, fs, stateFile, stateText("endgame", "snes", "Super Mario World", snesGame))
	p.refresh()
	assert.Nil(t, rec.get())
}

func TestTracker_SameEventIsAppliedOnce(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, _ := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("gamelistbrowsing", "snes", "", ""))
	p.refresh()
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "Game", snesGame))
	p.refresh()
	p.refresh()
	assert.Len(t, rec.sets, 1)
}

func TestTracker_EndDoesNotClearOtherMedia(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, _ := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("gamelistbrowsing", "snes", "", ""))
	p.refresh()
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "Game", snesGame))
	p.refresh()

	other := models.NewActiveMedia(systemdefs.SystemNES, "NES", "/other.nes", "Other", "NES")
	rec.set(other)
	writeFile(t, fs, stateFile, stateText("endgame", "snes", "Game", snesGame))
	p.refresh()
	assert.Same(t, other, rec.get())
}

func TestTracker_StaleGameIgnoredAtStartup(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, _ := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "Game", snesGame))
	p.refresh()
	assert.Nil(t, rec.get(), "no emulator is running, so the file is left over")
}

func TestTracker_RunningGameAdoptedAtStartup(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	addRunningGame(t, fs)
	p, rec, _ := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "Game", snesGame))
	p.refresh()
	require.NotNil(t, rec.get())
	assert.Equal(t, snesGame, rec.get().Path)
}

func TestTracker_UnknownSystemFallsBackToFolder(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, _ := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("gamelistbrowsing", "", "", ""))
	p.refresh()

	path := "/recalbox/share/roms/segacd/Sonic CD.chd"
	writeFile(t, fs, stateFile, stateText("rungame", "", "", path))
	p.refresh()
	media := rec.get()
	require.NotNil(t, media)
	assert.Equal(t, systemdefs.SystemMegaCD, media.SystemID)
	assert.Equal(t, "Sonic CD", media.Name)

	writeFile(t, fs, stateFile, stateText("endgame", "", "", path))
	p.refresh()
	writeFile(t, fs, stateFile, stateText("rungame", "240ptestsuite", "x", "/recalbox/share/roms/240ptestsuite/x.k7"))
	p.refresh()
	assert.Nil(t, rec.get(), "unmapped systems publish nothing")
}

func TestTracker_IgnoresRelativeGamePath(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, _ := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("gamelistbrowsing", "", "", ""))
	p.refresh()
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "x", "snes/x.sfc"))
	p.refresh()
	assert.Nil(t, rec.get())
}

func TestTrack_EventTriggersRead(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, _ := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("gamelistbrowsing", "snes", "", ""))

	events := make(chan func(), 1)
	p.subscribe = func(ctx context.Context, onEvent func()) {
		events <- onEvent
		<-ctx.Done()
	}
	// The first read treats a running game as stale, so the game must start
	// only after the tracker has recorded the browsing state.
	firstRead := p.changed
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.track(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	onEvent := <-events
	<-firstRead
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "Game", snesGame))
	onEvent()

	select {
	case media := <-rec.sets:
		require.NotNil(t, media)
		assert.Equal(t, snesGame, media.Path)
	case <-time.After(5 * time.Second):
		t.Fatal("event did not trigger a state read")
	}
}

func TestTrack_PollsWithoutEvents(t *testing.T) {
	t.Parallel()
	fs := newTestFs(t)
	p, rec, clock := newTestPlatform(t, fs)
	writeFile(t, fs, stateFile, stateText("gamelistbrowsing", "snes", "", ""))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.track(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.NoError(t, clock.BlockUntilContext(ctx, 1))
	writeFile(t, fs, stateFile, stateText("rungame", "snes", "Game", snesGame))
	clock.Advance(statePollEvery)

	select {
	case media := <-rec.sets:
		require.NotNil(t, media)
	case <-time.After(5 * time.Second):
		t.Fatal("poll did not read the state file")
	}
}
