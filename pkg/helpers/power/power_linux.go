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
	"strings"

	"github.com/spf13/afero"
)

// sysfsRoot is where the kernel exposes every power supply it knows about,
// one directory per supply.
var sysfsRoot = filepath.Join(string(filepath.Separator), "sys", "class", "power_supply")

// Read reports the device's power state from the kernel's power-supply
// directory.
func Read() (Status, error) {
	return statusFrom(afero.NewOsFs(), sysfsRoot)
}

// statusFrom resolves the four states the updater distinguishes from a
// power-supply tree.
//
// A device with no battery directory is mains-powered hardware such as a
// MiSTer, which is the common case and always safe to install on. Everything
// else needs a real reading: a battery whose charge cannot be read is reported
// as unknown rather than assumed full, because the cost of being wrong is a
// device that loses power mid-install.
func statusFrom(fs afero.Fs, root string) (Status, error) {
	entries, err := afero.ReadDir(fs, root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// No power-supply class at all. Kernels built without it are on
			// hardware that has nothing to report.
			return Status{Source: SourceNoBattery}, nil
		}
		return Status{Source: SourceUnknown}, err //nolint:wrapcheck // caller logs the sysfs error as-is
	}

	var (
		batteries  []string
		externalOn bool
		unreadable bool
	)
	for _, entry := range entries {
		name := entry.Name()
		dir := filepath.Join(root, name)
		supplyType, ok := readSupplyField(fs, dir, "type")
		if !ok || supplyType == "" {
			unreadable = true
			continue
		}
		switch supplyType {
		case "Battery":
			// A wireless mouse or controller is a battery the kernel knows
			// about and the device does not run on. The kernel marks those
			// "Device"; a battery the whole machine runs on is "System" or
			// says nothing at all.
			scope, _ := readSupplyField(fs, dir, "scope")
			if scope == "Device" {
				continue
			}
			batteries = append(batteries, dir)
		case "Mains", "USB", "USB_PD", "USB_PD_DRP", "BrickID", "Wireless":
			online, _ := readSupplyField(fs, dir, "online")
			if online == "1" {
				externalOn = true
			}
		}
	}

	if len(batteries) == 0 {
		if unreadable && !externalOn {
			return Status{Source: SourceUnknown}, nil
		}
		return Status{Source: SourceNoBattery}, nil
	}
	if externalOn {
		return Status{Source: SourceExternal}, nil
	}

	// A battery reporting Charging or Full is on external power even when no
	// mains supply announced itself, which is how some handhelds wire USB-C.
	for _, dir := range batteries {
		status, _ := readSupplyField(fs, dir, "status")
		switch status {
		case "Charging", "Full":
			return Status{Source: SourceExternal}, nil
		}
	}

	// Several batteries means several readings. The lowest is the one that
	// decides when the device dies.
	lowest := -1
	for _, dir := range batteries {
		percent, ok := readCapacity(fs, dir)
		if !ok {
			unreadable = true
			continue
		}
		if lowest < 0 || percent < lowest {
			lowest = percent
		}
	}
	if unreadable {
		return Status{Source: SourceUnknown}, nil
	}
	if lowest < 0 {
		return Status{Source: SourceUnknown}, nil
	}
	return Status{Source: SourceBattery, Percent: lowest}, nil
}

func readSupplyField(fs afero.Fs, dir, name string) (string, bool) {
	data, err := afero.ReadFile(fs, filepath.Join(dir, name))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

func readCapacity(fs afero.Fs, dir string) (int, bool) {
	raw, ok := readSupplyField(fs, dir, "capacity")
	if !ok || raw == "" {
		return 0, false
	}
	percent, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	// The kernel is supposed to report 0-100 but some drivers report junk on
	// a battery they cannot talk to, and junk must not read as a full battery.
	if percent < 0 || percent > 100 {
		return 0, false
	}
	return percent, true
}
