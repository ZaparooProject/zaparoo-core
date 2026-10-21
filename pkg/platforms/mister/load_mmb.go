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

package mister

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mmb"
	"github.com/spf13/afero"
)

const (
	// mmbBootSettle covers the core mounting the image, the machine
	// autostarting into the image's own menu and the menu drawing its list.
	mmbBootSettle = 12 * time.Second
)

var errNoSuchDisk = errors.New("no such disk in the image")

// mmbLeaveMenu is the input that takes a machine out of the menu its image
// autostarts, to the BASIC prompt. The Electron's menu exits on f0, which is
// Func (the Caps Lock key) with 0.
var mmbLeaveMenu = map[string]string{
	systemdefs.SystemAcornElectron: chord("capslock", "0"),
}

// mmbTitles names the disks of an image for use in paths. A title shared by
// several disks carries the disk number so each path still names one disk.
func mmbTitles(disks []mmb.Disk) []string {
	counts := make(map[string]int, len(disks))
	for _, d := range disks {
		counts[strings.ToLower(d.Title)]++
	}
	names := make([]string, len(disks))
	for i, d := range disks {
		if counts[strings.ToLower(d.Title)] > 1 {
			names[i] = fmt.Sprintf("%s (%d)", d.Title, d.Number)
		} else {
			names[i] = d.Title
		}
	}
	return names
}

func listMMB(fs afero.Fs, image string) []string {
	disks, err := mmb.ReadCatalog(fs, image)
	if err != nil {
		return nil
	}
	return mmbTitles(disks)
}

// findMMBDisk resolves the name of a path within an image to a disk: by the
// title as listed, then by title alone, then by a trailing disk number.
func findMMBDisk(disks []mmb.Disk, name string) (mmb.Disk, bool) {
	for i, listed := range mmbTitles(disks) {
		if strings.EqualFold(listed, name) {
			return disks[i], true
		}
	}
	for _, d := range disks {
		if strings.EqualFold(d.Title, name) {
			return d, true
		}
	}
	if open := strings.LastIndex(name, " ("); open > 0 && strings.HasSuffix(name, ")") {
		number, err := strconv.Atoi(name[open+2 : len(name)-1])
		if err == nil {
			for _, d := range disks {
				if d.Number == number && strings.EqualFold(d.Title, name[:open]) {
					return d, true
				}
			}
		}
	}
	return mmb.Disk{}, false
}

// mmbPlanner boots a disk of an MMB image with MMFS: the machine autostarts the
// image's menu, which is left for the BASIC prompt, where *DBOOT loads the disk
// by number.
func mmbPlanner(fs afero.Fs, systemID string) loadPlanner {
	return func(_ *config.Instance, path string) (loadPlan, bool, error) {
		image, inner, ok := splitImagePath(fs, path)
		if !ok {
			return loadPlan{}, false, nil
		}
		disks, err := mmb.ReadCatalog(fs, image)
		if err != nil {
			return loadPlan{}, false, fmt.Errorf("read %s: %w", image, err)
		}
		disk, found := findMMBDisk(disks, inner)
		if !found {
			return loadPlan{}, false, fmt.Errorf("%w: %s", errNoSuchDisk, inner)
		}
		var options []mgls.Setting
		if systemID == systemdefs.SystemBBCMicro {
			// Without the autostart the machine waits at the BASIC prompt.
			options = []mgls.Setting{mgls.Option{Name: "Autostart", Value: "No"}}
		}
		return loadPlan{
			options: options,
			target:  image,
			layout:  acornLayout(),
			settle:  mmbBootSettle,
			steps:   mmbSteps(systemID, disk.Number),
		}, true, nil
	}
}

// mmbSteps boots a disk once the machine is at a prompt: the Electron leaves
// the menu its image autostarts, the BBC already waits there.
func mmbSteps(systemID string, number int) []loadStep {
	boot := fmt.Sprintf("*DBOOT %d{enter}", number)
	if leave, ok := mmbLeaveMenu[systemID]; ok {
		return []loadStep{typeText(leave), typeAfter(time.Second, boot)}
	}
	return []loadStep{typeText(boot)}
}
