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

package audio

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/gen2brain/malgo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCandidate(name, alsaID string, tag byte) playbackCandidate {
	var id malgo.DeviceID
	copy(id[:], alsaID)
	id[len(id)-1] = tag
	return playbackCandidate{name: name, alsaID: alsaID, id: id}
}

func TestSelectPlaybackDevice(t *testing.T) {
	t.Parallel()

	mister := []playbackCandidate{
		testCandidate("Dummy, Dummy PCM", ":0,0", 1),
		testCandidate("USB Audio Device, USB Audio", ":1,0", 2),
	}
	pulse := []playbackCandidate{
		testCandidate("Built-in Audio", "alsa_output.pci-0000_00_1f.3", 3),
		testCandidate("USB Headset", "alsa_output.usb-Logitech_Headset", 4),
	}

	tests := []struct {
		name       string
		setting    string
		devices    []playbackCandidate
		usbCards   []int
		wantTag    byte
		wantReason bool
	}{
		{name: "empty uses default", setting: "", devices: mister, usbCards: []int{1}, wantReason: true},
		{
			name: "auto picks USB card", setting: "auto", devices: mister,
			usbCards: []int{1}, wantTag: 2, wantReason: true,
		},
		{
			name: "auto is case-insensitive", setting: "AUTO", devices: mister,
			usbCards: []int{1}, wantTag: 2, wantReason: true,
		},
		{name: "auto without USB card uses default", setting: "auto", devices: mister, wantReason: true},
		{name: "auto ignores non-ALSA IDs", setting: "auto", devices: pulse, usbCards: []int{0, 1}, wantReason: true},
		{name: "name substring", setting: "usb audio", devices: mister, wantTag: 2, wantReason: true},
		{name: "name match on Pulse", setting: "headset", devices: pulse, wantTag: 4, wantReason: true},
		{name: "first match wins", setting: "audio", devices: pulse, wantTag: 3, wantReason: true},
		{name: "no match", setting: "Sound Blaster", devices: mister},
		{name: "no devices", setting: "usb", devices: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, reason := selectPlaybackDevice(tt.setting, tt.devices, tt.usbCards)
			if tt.wantTag == 0 {
				assert.Nil(t, id)
			} else {
				require.NotNil(t, id)
				assert.Equal(t, tt.wantTag, id[len(id)-1])
			}
			assert.Equal(t, tt.wantReason, reason != "")
		})
	}
}

func TestAlsaCardNumber(t *testing.T) {
	t.Parallel()

	for id, want := range map[string]int{":0,0": 0, ":1,0": 1, ":12,3": 12} {
		got, ok := alsaCardNumber(id)
		assert.True(t, ok, id)
		assert.Equal(t, want, got, id)
	}
	for _, id := range []string{"", ":", ":1", ":x,0", ":-1,0", "hw:1,0", "default"} {
		_, ok := alsaCardNumber(id)
		assert.False(t, ok, id)
	}
}

func TestPlaybackCandidatesTrimsALSAID(t *testing.T) {
	t.Parallel()

	var info malgo.DeviceInfo
	copy(info.ID[:], ":1,0")
	got := playbackCandidates([]malgo.DeviceInfo{info})
	require.Len(t, got, 1)
	assert.Equal(t, ":1,0", got[0].alsaID)
	assert.Equal(t, info.ID, got[0].id)
}

func TestUSBSoundCards(t *testing.T) {
	t.Parallel()

	t.Run("lists only cards with usbid", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		for _, card := range []string{"card0", "card2", "card10"} {
			require.NoError(t, fs.MkdirAll(filepath.Join(asoundDir, card), 0o755))
		}
		for _, card := range []string{"card10", "card2"} {
			usbid := filepath.Join(asoundDir, card, "usbid")
			require.NoError(t, afero.WriteFile(fs, usbid, []byte("0d8c:0014\n"), 0o644))
		}
		require.NoError(t, afero.WriteFile(fs, filepath.Join(asoundDir, "cards"), []byte(""), 0o644))
		assert.Equal(t, []int{2, 10}, usbSoundCards(fs))
	})

	t.Run("no asound directory", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, usbSoundCards(afero.NewMemMapFs()))
	})
}

// nullBackendInit has the signature sharedDevice.initContext requires.
func nullBackendInit(
	_ []malgo.Backend,
	cfg malgo.ContextConfig, //nolint:gocritic // signature fixed by sharedDevice.initContext
	logProc malgo.LogProc,
) (*malgo.AllocatedContext, error) {
	ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendNull}, cfg, logProc)
	if err != nil {
		return nil, fmt.Errorf("init null audio backend: %w", err)
	}
	return ctx, nil
}

// TestSharedDeviceOpenOutputDeviceSetting opens a real device on miniaudio's
// null backend with each kind of setting. A setting that matches nothing must
// still open the default output rather than fail the sources.
func TestSharedDeviceOpenOutputDeviceSetting(t *testing.T) {
	t.Parallel()

	for _, setting := range []string{"", "null", "auto", "No Such Speaker"} {
		t.Run(setting, func(t *testing.T) {
			t.Parallel()
			d := newSharedDevice()
			d.initContext = nullBackendInit
			d.fs = afero.NewMemMapFs()
			d.setOutputDeviceSource(func() string { return setting })
			src := &testMixSource{active: true, drained: true, onDrainedCh: make(chan struct{})}
			setTestSources(d, src)
			d.opening = true

			d.open()

			// onDrained runs only from the device callback path; a failed
			// open calls fail() instead and clears the sources.
			select {
			case <-src.onDrainedCh:
			case <-time.After(5 * time.Second):
				t.Fatal("device did not open and drain the source")
			}
			d.closeDevice()
		})
	}
}

func TestSharedDeviceOutputDeviceSettingTrimsAndClears(t *testing.T) {
	t.Parallel()

	d := newSharedDevice()
	assert.Empty(t, d.outputDeviceSetting())
	d.setOutputDeviceSource(func() string { return "  auto \n" })
	assert.Equal(t, "auto", d.outputDeviceSetting())
	d.setOutputDeviceSource(nil)
	assert.Empty(t, d.outputDeviceSetting())
}
