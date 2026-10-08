//go:build linux && !android

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

package power

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/afero"
)

// ReadDetail reports every battery the device runs on from the kernel's
// power-supply directory.
func ReadDetail() (Detail, error) {
	return detailFrom(afero.NewOsFs(), sysfsRoot)
}

func detailFrom(fs afero.Fs, root string) (Detail, error) {
	entries, err := afero.ReadDir(fs, root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return summarize(nil, false), nil
		}
		return Detail{}, err //nolint:wrapcheck // caller logs the sysfs error as-is
	}

	var (
		batteries []Battery
		external  bool
	)
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		supplyType, _ := readSupplyField(fs, dir, "type")
		switch supplyType {
		case "Battery":
			// Controllers and mice report a battery of their own, marked
			// "Device"; those are not what the machine runs on.
			if scope, _ := readSupplyField(fs, dir, "scope"); scope == "Device" {
				continue
			}
			if present, ok := readSupplyField(fs, dir, "present"); ok && present == "0" {
				continue
			}
			batteries = append(batteries, batteryFrom(fs, dir, entry.Name()))
		case "Mains", "USB", "USB_PD", "USB_PD_DRP", "BrickID", "Wireless":
			if online, _ := readSupplyField(fs, dir, "online"); online == "1" {
				external = true
			}
		}
	}
	return summarize(batteries, external), nil
}

func batteryFrom(fs afero.Fs, dir, name string) Battery {
	battery := Battery{ID: name}
	if percent, ok := readCapacity(fs, dir); ok {
		battery.Percent = &percent
	}
	status, _ := readSupplyField(fs, dir, "status")
	switch status {
	case "Charging":
		battery.State = ChargeCharging
	case "Discharging":
		battery.State = ChargeDischarging
	case "Full":
		battery.State = ChargeFull
	case "Not charging":
		battery.State = ChargeNotCharging
	}
	if battery.State == ChargeDischarging {
		battery.TimeRemaining = timeToEmpty(fs, dir)
	}
	return battery
}

// timeToEmpty prefers the kernel's own estimate and otherwise derives one from
// the remaining energy or charge and the present draw, which is all most
// handheld fuel gauges expose.
func timeToEmpty(fs afero.Fs, dir string) time.Duration {
	if seconds, ok := readPositive(fs, dir, "time_to_empty_now"); ok {
		return time.Duration(seconds) * time.Second
	}
	pairs := [][2]string{{"energy_now", "power_now"}, {"charge_now", "current_now"}}
	for _, pair := range pairs {
		remaining, ok := readPositive(fs, dir, pair[0])
		if !ok {
			continue
		}
		draw, ok := readPositive(fs, dir, pair[1])
		if !ok {
			continue
		}
		hours := float64(remaining) / float64(draw)
		// A gauge reporting a near-zero draw produces an estimate of weeks,
		// which is noise rather than a reading.
		if hours > 0 && hours < 100 {
			return time.Duration(hours * float64(time.Hour)).Truncate(time.Second)
		}
	}
	return 0
}

func readPositive(fs afero.Fs, dir, name string) (int64, bool) {
	raw, ok := readSupplyField(fs, dir, name)
	if !ok || raw == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}
