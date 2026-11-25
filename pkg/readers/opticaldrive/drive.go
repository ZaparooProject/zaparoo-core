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

package opticaldrive

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/cdrom"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/rs/zerolog/log"
)

// maxProbeRetries is how many polls a disc whose probe hit a read error is
// probed again, for drives that report a disc before it is readable.
const maxProbeRetries = 5

var (
	errDriveStatusUnsupported = errors.New("drive does not report its status")
	driveNamePattern          = regexp.MustCompile(`^sr\d{1,3}$`)
)

// driveName returns the device name of a drive path such as /dev/sr0, or ""
// when it is not a SCSI CD-ROM device name.
func driveName(path string) string {
	name := filepath.Base(path)
	if !driveNamePattern.MatchString(name) {
		return ""
	}
	return name
}

func openDiscDevice(path string) (contextReaderAtCloser, error) {
	drive, err := cdrom.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open optical device: %w", err)
	}
	return drive, nil
}

// driveCheck is what a drive says about itself without the disc being read.
type driveCheck struct {
	Status  cdrom.Status
	Changed bool
}

// driveWatcher asks a drive for its state. Check never reads the disc, so it
// makes no noise and does not disturb another program streaming from it.
type driveWatcher interface {
	Check(path string) (driveCheck, error)
	// Touch reads one sector, for the keep_reading setting.
	Touch(ctx context.Context, path string) error
}

// discMedia is what a disc is, read once after it changes.
type discMedia struct {
	Kind     string
	System   string
	TOC      cdrom.TOC
	Complete bool
}

// discInspector reads the table of contents and boot sectors of a disc.
type discInspector interface {
	Inspect(ctx context.Context, path string) discMedia
}

var openDrive = cdrom.Open

type cdromWatcher struct{}

func (cdromWatcher) Check(path string) (driveCheck, error) {
	drive, err := openDrive(path)
	if err != nil {
		if errors.Is(err, cdrom.ErrUnsupported) {
			return driveCheck{}, errDriveStatusUnsupported
		}
		return driveCheck{}, fmt.Errorf("open drive: %w", err)
	}
	defer func() { _ = drive.Close() }()

	status, err := drive.Status()
	if err != nil {
		if cdrom.IsUnsupportedRequest(err) {
			return driveCheck{}, errDriveStatusUnsupported
		}
		return driveCheck{}, fmt.Errorf("drive status: %w", err)
	}
	if status == cdrom.StatusUnknown {
		return driveCheck{}, errDriveStatusUnsupported
	}
	check := driveCheck{Status: status}
	if status == cdrom.StatusDiscOK {
		changed, changedErr := drive.MediaChanged()
		switch {
		case changedErr == nil:
			check.Changed = changed
		case !cdrom.IsUnsupportedRequest(changedErr):
			// Reading it as no change would miss a disc swapped since the
			// last poll; the next poll asks again.
			return driveCheck{}, fmt.Errorf("drive media changed: %w", changedErr)
		}
	}
	return check, nil
}

func (cdromWatcher) Touch(ctx context.Context, path string) error {
	drive, err := openDrive(path)
	if err != nil {
		return fmt.Errorf("open drive: %w", err)
	}
	defer func() { _ = drive.Close() }()
	buf := make([]byte, cdrom.DataSectorSize)
	if _, err := drive.ReadAtContext(ctx, buf, iso9660SuperblockOffset); err != nil {
		return fmt.Errorf("read drive: %w", err)
	}
	return nil
}

type cdromInspector struct{}

func (cdromInspector) Inspect(ctx context.Context, path string) discMedia {
	drive, err := openDrive(path)
	if err != nil {
		return discMedia{Complete: errors.Is(err, cdrom.ErrUnsupported)}
	}
	defer func() { _ = drive.Close() }()

	if drive.IsDVD() {
		return discMedia{Kind: readers.DiscKindDVD, System: systemdefs.SystemDVDPlayer, Complete: true}
	}
	toc, err := drive.ReadTOC()
	if err != nil {
		log.Debug().Err(err).Str("path", path).Msg("optical drive: no table of contents")
		return discMedia{}
	}
	media := discMedia{TOC: toc, Complete: true}
	if toc.IsAudio() {
		media.Kind = readers.DiscKindAudio
		return media
	}
	media.Kind = readers.DiscKindData
	media.System, media.Complete = identifyDiscSystem(ctx, toc, drive.ReadDataSector)
	return media
}
