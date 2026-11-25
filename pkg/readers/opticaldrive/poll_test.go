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

//go:build linux

package opticaldrive

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/cdrom"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedWatcher reports whatever status the test sets.
type scriptedWatcher struct {
	err     error
	check   driveCheck
	touches int
}

func (w *scriptedWatcher) Check(string) (driveCheck, error) {
	check := w.check
	w.check.Changed = false
	return check, w.err
}

func (w *scriptedWatcher) Touch(context.Context, string) error {
	w.touches++
	return nil
}

type countingInspector struct {
	media discMedia
	calls int
}

func (c *countingInspector) Inspect(context.Context, string) discMedia {
	c.calls++
	return c.media
}

type pollFixture struct {
	reader    *FileReader
	watcher   *scriptedWatcher
	inspector *countingInspector
	st        *pollState
	iq        chan readers.Scan
	identity  discIdentity
	identify  int
}

func newPollFixture(t *testing.T, cfg *config.Instance) *pollFixture {
	t.Helper()
	if cfg == nil {
		cfg = &config.Instance{}
	}
	f := &pollFixture{
		watcher:   &scriptedWatcher{},
		inspector: &countingInspector{media: discMedia{Kind: readers.DiscKindData, Complete: true}},
		identity:  discIdentity{UUID: "2001-01-01-00-00-00-00", Label: "GAME"},
		st:        &pollState{},
		iq:        make(chan readers.Scan, 16),
	}
	f.reader = newTestReader(cfg)
	f.reader.fsChecker = &mockFSChecker{}
	f.reader.watcher = f.watcher
	f.reader.inspector = f.inspector
	f.reader.fs = afero.NewMemMapFs()
	f.reader.discIdentifier = &mockDiscIdentifier{
		identifyFunc: func(context.Context, string) (discIdentity, error) {
			f.identify++
			return f.identity, nil
		},
	}
	f.reader.path = "/dev/sr0"
	f.reader.device = config.ReadersConnect{Driver: "opticaldrive", Path: "/dev/sr0"}
	return f
}

func (f *pollFixture) tick(status cdrom.Status) {
	f.watcher.check.Status = status
	f.reader.poll(f.st, f.iq)
}

func (f *pollFixture) scans() []readers.Scan {
	var out []readers.Scan
	for {
		select {
		case scan := <-f.iq:
			out = append(out, scan)
		default:
			return out
		}
	}
}

func TestPollReadsTheDiscOnlyOncePerInsert(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)

	f.tick(cdrom.StatusNoDisc)
	f.tick(cdrom.StatusNotReady)
	assert.Zero(t, f.identify, "an empty or spinning-up drive must not be read")
	assert.Empty(t, f.scans())

	for range 10 {
		f.tick(cdrom.StatusDiscOK)
	}
	assert.Equal(t, 1, f.identify, "a disc is read once, not every poll")
	assert.Equal(t, 1, f.inspector.calls)
	scans := f.scans()
	require.Len(t, scans, 1)
	require.NotNil(t, scans[0].Token)
	assert.Equal(t, "2001-01-01-00-00-00-00/GAME", scans[0].Token.UID)
	assert.Zero(t, f.watcher.touches, "keep_reading is off by default")

	f.tick(cdrom.StatusTrayOpen)
	scans = f.scans()
	require.Len(t, scans, 1)
	assert.Nil(t, scans[0].Token, "opening the tray removes the token")
	assert.False(t, scans[0].ReaderError)

	f.tick(cdrom.StatusDiscOK)
	assert.Equal(t, 2, f.identify)
	scans = f.scans()
	require.Len(t, scans, 1)
	require.NotNil(t, scans[0].Token, "reinserting the same disc scans it again")
}

func TestPollMediaChangedRescansWithoutTrayStatus(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)
	f.tick(cdrom.StatusDiscOK)
	require.Len(t, f.scans(), 1)

	// The disc was swapped between two polls, so the tray was never seen
	// open, but the drive reports the media changed.
	f.watcher.check.Changed = true
	f.tick(cdrom.StatusDiscOK)

	scans := f.scans()
	require.Len(t, scans, 2)
	assert.Nil(t, scans[0].Token)
	require.NotNil(t, scans[1].Token)
	assert.Equal(t, 2, f.identify)
}

func TestPollRetriesAnIncompleteProbe(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)
	f.inspector.media = discMedia{}

	for range maxProbeRetries + 5 {
		f.tick(cdrom.StatusDiscOK)
	}
	assert.Equal(t, maxProbeRetries, f.inspector.calls, "an unreadable disc is retried a bounded number of times")
}

func TestPollKeepReadingTouchesTheDisc(t *testing.T) {
	t.Parallel()
	on := true
	cfg, err := config.NewConfig(t.TempDir(), config.Values{Readers: config.Readers{
		Drivers: map[string]config.DriverConfig{"opticaldrive": {KeepReading: &on}},
	}})
	require.NoError(t, err)
	f := newPollFixture(t, cfg)

	for range 4 {
		f.tick(cdrom.StatusDiscOK)
	}
	assert.Equal(t, 1, f.identify)
	assert.Equal(t, 3, f.watcher.touches, "every poll after the probe reads one sector")
}

func TestPollFallsBackToReadingWithoutStatus(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)
	f.watcher.err = errDriveStatusUnsupported

	for range 3 {
		f.tick(cdrom.StatusUnknown)
	}
	assert.Equal(t, 3, f.identify)
	require.Len(t, f.scans(), 1, "an unchanged disc is scanned once")
}

func TestPollAudioDiscScan(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)
	f.identity = discIdentity{}
	f.reader.discIdentifier = &mockDiscIdentifier{
		identifyFunc: func(context.Context, string) (discIdentity, error) {
			return discIdentity{}, errISO9660IdentityNotFound
		},
	}
	toc := cdrom.TOC{
		Tracks:  []cdrom.Track{{Number: 1, Start: 0, End: 15000}, {Number: 2, Start: 15000, End: 30000}},
		LeadOut: 30000,
	}
	f.inspector.media = discMedia{Kind: readers.DiscKindAudio, TOC: toc, Complete: true}

	f.tick(cdrom.StatusDiscOK)
	f.tick(cdrom.StatusDiscOK)

	scans := f.scans()
	require.Len(t, scans, 1)
	require.NotNil(t, scans[0].Token)
	assert.Equal(t, "cdda:"+toc.Fingerprint(), scans[0].Token.UID)
	assert.Equal(t, []readers.ScanProperty{
		{Name: readers.PropertyDiscKind, Value: readers.DiscKindAudio},
		{Name: readers.PropertyDiscDrive, Value: "sr0"},
	}, scans[0].Properties)
	assert.Equal(t, 1, f.inspector.calls)
}

func TestPollDataDiscCarriesItsSystem(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)
	f.inspector.media = discMedia{Kind: readers.DiscKindData, System: systemdefs.System3DO, Complete: true}

	f.tick(cdrom.StatusDiscOK)

	scans := f.scans()
	require.Len(t, scans, 1)
	assert.Contains(t, scans[0].Properties,
		readers.ScanProperty{Name: readers.PropertyDiscSystem, Value: systemdefs.System3DO})
}

func TestPollDiscSidecarCoverLivesUntilEject(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR" +
		"\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	f.identity.Sidecar = &discSidecar{Image: png, ImageExt: ".png"}
	f.identity.Sidecar.Fields.Name = "Disc Game"

	f.tick(cdrom.StatusDiscOK)
	scans := f.scans()
	require.Len(t, scans, 1)
	meta := scans[0].Token.Metadata
	require.NotNil(t, meta)
	assert.Equal(t, "Disc Game", meta.Name)
	assert.Equal(t, "physicaldisc://sr0/", meta.MediaPrefix)
	require.NotEmpty(t, meta.ImagePath)
	data, err := afero.ReadFile(f.reader.fs, meta.ImagePath)
	require.NoError(t, err)
	assert.Equal(t, png, data)

	f.tick(cdrom.StatusNoDisc)
	exists, err := afero.Exists(f.reader.fs, meta.ImageRoot)
	require.NoError(t, err)
	assert.False(t, exists, "the copied cover is removed with the disc")
}

func TestPollDiscSidecarRejectsANonImage(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)
	f.identity.Sidecar = &discSidecar{Image: []byte("not an image"), ImageExt: ".png"}
	f.identity.Sidecar.Fields.Name = "Disc Game"

	f.tick(cdrom.StatusDiscOK)
	scans := f.scans()
	require.Len(t, scans, 1)
	meta := scans[0].Token.Metadata
	require.NotNil(t, meta)
	assert.Empty(t, meta.ImagePath)
	assert.Empty(t, meta.ImageRoot)
}

func TestDriveName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "sr0", driveName("/dev/sr0"))
	assert.Equal(t, "sr12", driveName("/dev/sr12"))
	assert.Empty(t, driveName("/dev/cdrom"))
	assert.Empty(t, driveName("/dev/sr0;rm"))
}

func TestPollAudioDiscSkipsFilesystemAndGameIDReads(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)
	f.inspector.media = discMedia{
		Kind:     readers.DiscKindAudio,
		TOC:      cdrom.TOC{Tracks: []cdrom.Track{{Number: 1, Start: 0, End: 9000}}},
		Complete: true,
	}
	gameID := 0
	f.reader.gameIDProbe = func(string) []readers.ScanProperty {
		gameID++
		return nil
	}

	f.tick(cdrom.StatusDiscOK)

	assert.Zero(t, f.identify, "an audio CD has no filesystem to read")
	assert.Zero(t, gameID, "an audio CD has no game ID to read")
	scans := f.scans()
	require.Len(t, scans, 1)
	require.NotNil(t, scans[0].Token)
	assert.Contains(t, scans[0].Token.UID, "cdda:")
	assert.Contains(t, scans[0].Properties,
		readers.ScanProperty{Name: readers.PropertyDiscKind, Value: readers.DiscKindAudio})
}

func TestPollUnidentifiedDataDiscSendsNoToken(t *testing.T) {
	t.Parallel()
	f := newPollFixture(t, nil)
	f.identity = discIdentity{}
	f.reader.discIdentifier = &mockDiscIdentifier{
		identifyFunc: func(context.Context, string) (discIdentity, error) {
			return discIdentity{}, errISO9660IdentityNotFound
		},
	}
	f.reader.gameIDProbe = func(string) []readers.ScanProperty { return nil }

	f.tick(cdrom.StatusDiscOK)

	assert.Empty(t, f.scans(), "a disc with nothing to name it by is not a token")
}
