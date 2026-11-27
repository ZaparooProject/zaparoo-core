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
	"bytes"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/gen2brain/malgo"
	"github.com/spf13/afero"
)

// OutputDeviceAuto selects the first USB sound card, or the system default
// when none is plugged in.
const OutputDeviceAuto = "auto"

// asoundDir is where Linux lists sound cards. Only USB cards have a usbid file.
const asoundDir = "/proc/asound"

// SetOutputDeviceSource registers fn to name the playback device all audio
// goes to (see config.Instance.AudioDevice). It is asked each time the device
// opens, so a config change or a newly plugged speaker applies to the next
// sound. It is called at service startup.
func SetOutputDeviceSource(fn func() string) {
	globalDevice.setOutputDeviceSource(fn)
}

func (d *sharedDevice) setOutputDeviceSource(fn func() string) {
	if fn == nil {
		d.outputDevice.Store(nil)
		return
	}
	d.outputDevice.Store(&fn)
}

func (d *sharedDevice) outputDeviceSetting() string {
	fn := d.outputDevice.Load()
	if fn == nil {
		return ""
	}
	return strings.TrimSpace((*fn)())
}

// playbackCandidate is an enumerated playback device.
type playbackCandidate struct {
	name   string
	alsaID string
	id     malgo.DeviceID
}

func playbackCandidates(infos []malgo.DeviceInfo) []playbackCandidate {
	out := make([]playbackCandidate, 0, len(infos))
	for i := range infos {
		raw := infos[i].ID[:]
		if end := bytes.IndexByte(raw, 0); end >= 0 {
			raw = raw[:end]
		}
		out = append(out, playbackCandidate{
			name:   infos[i].Name(),
			alsaID: string(raw),
			id:     infos[i].ID,
		})
	}
	return out
}

func candidateNames(devices []playbackCandidate) []string {
	names := make([]string, len(devices))
	for i := range devices {
		names[i] = devices[i].name
	}
	return names
}

// alsaCardNumber returns the card number of a miniaudio ALSA device ID such as
// ":1,0". IDs from other backends are not in that form.
func alsaCardNumber(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, ":")
	if !ok {
		return 0, false
	}
	card, _, ok := strings.Cut(rest, ",")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(card)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// selectPlaybackDevice picks the device for setting. It returns nil when the
// system default should be used, and a description of the choice for the log.
func selectPlaybackDevice(
	setting string,
	devices []playbackCandidate,
	usbCards []int,
) (id *malgo.DeviceID, reason string) {
	if setting == "" {
		return nil, "system default"
	}
	if strings.EqualFold(setting, OutputDeviceAuto) {
		for i := range devices {
			card, ok := alsaCardNumber(devices[i].alsaID)
			if ok && slices.Contains(usbCards, card) {
				chosen := devices[i].id
				return &chosen, "USB sound card " + devices[i].name
			}
		}
		return nil, "no USB sound card, using system default"
	}
	want := strings.ToLower(setting)
	for i := range devices {
		if strings.Contains(strings.ToLower(devices[i].name), want) {
			chosen := devices[i].id
			return &chosen, "device " + devices[i].name
		}
	}
	return nil, ""
}

// usbSoundCards returns the numbers of the USB sound cards Linux lists, in
// ascending order. It returns nothing on other systems.
func usbSoundCards(fs afero.Fs) []int {
	matches, err := afero.Glob(fs, filepath.Join(asoundDir, "card*", "usbid"))
	if err != nil {
		return nil
	}
	cards := make([]int, 0, len(matches))
	for _, m := range matches {
		dir := filepath.Base(filepath.Dir(m))
		n, err := strconv.Atoi(strings.TrimPrefix(dir, "card"))
		if err != nil || n < 0 {
			continue
		}
		cards = append(cards, n)
	}
	slices.Sort(cards)
	return cards
}
