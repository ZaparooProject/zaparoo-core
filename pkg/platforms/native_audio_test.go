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

package platforms

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mediaslot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePlayback is a hand-rolled test double for audio.PlaybackManager.
// It records the last call made and returns configurable errors per method.
type fakePlayback struct {
	playErr   error
	stopErr   error
	pauseErr  error
	resumeErr error
	toggleErr error
	seekErr   error

	lastCall   string
	lastSlot   string
	lastPath   string
	lastOpts   audio.PlaybackOptions
	lastOffset time.Duration
}

func (f *fakePlayback) Play(slot, path string, opts audio.PlaybackOptions) error {
	f.lastCall, f.lastSlot, f.lastPath, f.lastOpts = "play", slot, path, opts
	return f.playErr
}

func (f *fakePlayback) Stop(slot string) error {
	f.lastCall, f.lastSlot = "stop", slot
	return f.stopErr
}

func (f *fakePlayback) Pause(slot string) error {
	f.lastCall, f.lastSlot = "pause", slot
	return f.pauseErr
}

func (f *fakePlayback) Resume(slot string) error {
	f.lastCall, f.lastSlot = "resume", slot
	return f.resumeErr
}

func (f *fakePlayback) TogglePause(slot string) error {
	f.lastCall, f.lastSlot = "toggle", slot
	return f.toggleErr
}

func (f *fakePlayback) Seek(slot string, offset time.Duration) error {
	f.lastCall, f.lastSlot, f.lastOffset = "seek", slot, offset
	return f.seekErr
}

func (*fakePlayback) State(_ string) audio.PlaybackState {
	return audio.PlaybackState{}
}

// --- launchNativeAudio ---

func TestLaunchNativeAudio_NilPlayback(t *testing.T) {
	t.Parallel()
	_, err := launchNativeAudio(nil, nil, nil, filepath.Join("Music", "file.mp3"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestLaunchNativeAudio_BadSlot(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	_, err := launchNativeAudio(
		fp, nil, nil,
		filepath.Join("Music", "file.mp3"),
		&LaunchOptions{Slot: "badvalue"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "normalize native audio slot")
}

func TestLaunchNativeAudio_NilOptsDefaultsPrimary(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	proc, err := launchNativeAudio(fp, nil, nil, filepath.Join("Music", "file.mp3"), nil)
	require.NoError(t, err)
	assert.Nil(t, proc)
	assert.Equal(t, "play", fp.lastCall)
	assert.Equal(t, mediaslot.Primary, fp.lastSlot)
}

func TestLaunchNativeAudio_VolumeFromConfig(t *testing.T) {
	t.Parallel()
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetAudioVolume(50)

	fp := &fakePlayback{}
	_, err = launchNativeAudio(fp, nil, cfg, filepath.Join("Music", "file.mp3"), nil)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, fp.lastOpts.Volume, 1e-9)
}

func TestLaunchNativeAudio_VolumeZeroDefaultsTo1(t *testing.T) {
	t.Parallel()
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	cfg.SetAudioVolume(0)

	fp := &fakePlayback{}
	_, err = launchNativeAudio(fp, nil, cfg, filepath.Join("Music", "file.mp3"), nil)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, fp.lastOpts.Volume, 1e-9)
}

func TestLaunchNativeAudio_NilCfgVolumeDefaultsTo1(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	_, err := launchNativeAudio(fp, nil, nil, filepath.Join("Music", "file.mp3"), nil)
	require.NoError(t, err)
	assert.InDelta(t, 1.0, fp.lastOpts.Volume, 1e-9)
}

func TestLaunchNativeAudio_PlayError(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{playErr: errors.New("device busy")}
	_, err := launchNativeAudio(fp, nil, nil, filepath.Join("Music", "file.mp3"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "play native audio")
	assert.Contains(t, err.Error(), "device busy")
}

func TestLaunchNativeAudio_BackgroundCallsSetter(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	path := filepath.Join("Music", "track.mp3")
	var gotMedia *models.ActiveMedia
	setter := func(m *models.ActiveMedia) { gotMedia = m }

	proc, err := launchNativeAudio(
		fp, setter, nil, path,
		&LaunchOptions{Slot: mediaslot.Background},
	)
	require.NoError(t, err)
	assert.Nil(t, proc)
	require.NotNil(t, gotMedia)
	assert.Equal(t, "Audio", gotMedia.SystemID)
	assert.Equal(t, NativeAudioLauncherID, gotMedia.LauncherID)
	assert.Equal(t, audioDisplayName(path), gotMedia.Name)
	assert.Equal(t, path, gotMedia.Path)
}

func TestLaunchNativeAudio_PrimarySlotSkipsSetter(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	called := false
	setter := func(*models.ActiveMedia) { called = true }

	_, err := launchNativeAudio(
		fp, setter, nil,
		filepath.Join("Music", "track.mp3"),
		&LaunchOptions{Slot: mediaslot.Primary},
	)
	require.NoError(t, err)
	assert.False(t, called)
}

func TestLaunchNativeAudio_ReturnsNilProcess(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	proc, err := launchNativeAudio(fp, nil, nil, filepath.Join("Music", "file.mp3"), nil)
	require.NoError(t, err)
	assert.Nil(t, proc)
}

// --- nativeAudioControl ---

func TestNativeAudioControl_NilPlayback(t *testing.T) {
	t.Parallel()
	fn := nativeAudioControl(nil, ControlTogglePause)
	err := fn(context.Background(), nil, ControlParams{Args: map[string]string{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

func TestNativeAudioControl_BadSlot(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	fn := nativeAudioControl(fp, ControlPause)
	err := fn(context.Background(), nil, ControlParams{
		Args: map[string]string{string(gozapscript.KeySlot): "badslot"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "normalize slot")
}

func TestNativeAudioControl_EmptySlotDefaultsPrimary(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	fn := nativeAudioControl(fp, ControlPause)
	err := fn(context.Background(), nil, ControlParams{Args: map[string]string{}})
	require.NoError(t, err)
	assert.Equal(t, mediaslot.Primary, fp.lastSlot)
}

func TestNativeAudioControl_TogglePause(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{toggleErr: errors.New("toggle err")}
	fn := nativeAudioControl(fp, ControlTogglePause)
	err := fn(context.Background(), nil, ControlParams{Args: map[string]string{}})
	require.Error(t, err)
	assert.Equal(t, "toggle", fp.lastCall)
	assert.Equal(t, "toggle err", err.Error())
}

func TestNativeAudioControl_Pause(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{pauseErr: errors.New("pause err")}
	fn := nativeAudioControl(fp, ControlPause)
	err := fn(context.Background(), nil, ControlParams{Args: map[string]string{}})
	require.Error(t, err)
	assert.Equal(t, "pause", fp.lastCall)
}

func TestNativeAudioControl_Resume(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{resumeErr: errors.New("resume err")}
	fn := nativeAudioControl(fp, ControlResume)
	err := fn(context.Background(), nil, ControlParams{Args: map[string]string{}})
	require.Error(t, err)
	assert.Equal(t, "resume", fp.lastCall)
}

func TestNativeAudioControl_Stop(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{stopErr: errors.New("stop err")}
	fn := nativeAudioControl(fp, ControlStop)
	err := fn(context.Background(), nil, ControlParams{Args: map[string]string{}})
	require.Error(t, err)
	assert.Equal(t, "stop", fp.lastCall)
}

func TestNativeAudioControl_FastForward_Default(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	fn := nativeAudioControl(fp, ControlFastForward)
	err := fn(context.Background(), nil, ControlParams{Args: map[string]string{}})
	require.NoError(t, err)
	assert.Equal(t, "seek", fp.lastCall)
	assert.Equal(t, 10*time.Second, fp.lastOffset)
}

func TestNativeAudioControl_Rewind_Default(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	fn := nativeAudioControl(fp, ControlRewind)
	err := fn(context.Background(), nil, ControlParams{Args: map[string]string{}})
	require.NoError(t, err)
	assert.Equal(t, "seek", fp.lastCall)
	assert.Equal(t, -10*time.Second, fp.lastOffset)
}

func TestNativeAudioControl_FastForward_Custom(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	fn := nativeAudioControl(fp, ControlFastForward)
	err := fn(context.Background(), nil, ControlParams{
		Args: map[string]string{"seconds": "5"},
	})
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, fp.lastOffset)
}

func TestNativeAudioControl_Rewind_Custom(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	fn := nativeAudioControl(fp, ControlRewind)
	err := fn(context.Background(), nil, ControlParams{
		Args: map[string]string{"seconds": "5"},
	})
	require.NoError(t, err)
	assert.Equal(t, -5*time.Second, fp.lastOffset)
}

func TestNativeAudioControl_Unsupported(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	fn := nativeAudioControl(fp, "fly")
	err := fn(context.Background(), nil, ControlParams{Args: map[string]string{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported native audio control")
}

// --- controlSeekDuration ---

func TestControlSeekDuration(t *testing.T) {
	t.Parallel()
	const fallback = 10 * time.Second
	tests := []struct {
		args     map[string]string
		name     string
		fallback time.Duration
		want     time.Duration
	}{
		{
			name:     "empty_uses_fallback",
			args:     map[string]string{},
			fallback: fallback,
			want:     fallback,
		},
		{
			name:     "non_numeric_uses_fallback",
			args:     map[string]string{"seconds": "abc"},
			fallback: fallback,
			want:     fallback,
		},
		{
			name:     "fractional_parsed",
			args:     map[string]string{"seconds": "2.5"},
			fallback: fallback,
			want:     2500 * time.Millisecond,
		},
		{
			name:     "integer_parsed",
			args:     map[string]string{"seconds": "3"},
			fallback: fallback,
			want:     3 * time.Second,
		},
		{
			name:     "whitespace_trimmed",
			args:     map[string]string{"seconds": " 3 "},
			fallback: fallback,
			want:     3 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := controlSeekDuration(tt.args, tt.fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- audioDisplayName ---

// TestAudioDisplayName verifies the helper is self-consistent with tags.ParseTitleFromFilename.
func TestAudioDisplayName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
	}{
		{name: "mp3_file", path: filepath.Join("Music", "01 - Track Name.mp3")},
		{name: "no_extension", path: filepath.Join("Music", "TrackName")},
		{name: "deep_path", path: filepath.Join("a", "b", "c", "Song Title.flac")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := filepath.Base(tt.path)
			stem := strings.TrimSuffix(base, filepath.Ext(base))
			want := tags.ParseTitleFromFilename(stem, false)
			assert.Equal(t, want, audioDisplayName(tt.path))
		})
	}
}

// --- NativeAudioLauncher factory ---

func TestNativeAudioLauncher_Fields(t *testing.T) {
	t.Parallel()
	fp := &fakePlayback{}
	l := NativeAudioLauncher(fp, nil)

	assert.Equal(t, NativeAudioLauncherID, l.ID)
	assert.Equal(t, "Audio", l.SystemID)
	assert.Contains(t, l.Extensions, ".wav")
	assert.Contains(t, l.Extensions, ".mp3")
	assert.Contains(t, l.Extensions, ".ogg")
	assert.Contains(t, l.Extensions, ".flac")

	for _, key := range []string{
		ControlTogglePause,
		ControlPause,
		ControlResume,
		ControlStop,
		ControlFastForward,
		ControlRewind,
	} {
		ctrl, ok := l.Controls[key]
		assert.True(t, ok, "missing control: %s", key)
		assert.NotNil(t, ctrl.Func, "nil Func for control: %s", key)
	}
}
