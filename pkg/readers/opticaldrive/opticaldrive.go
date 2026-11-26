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
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/cdrom"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/gameid"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	TokenType           = "disc"
	IDSourceUUID        = "uuid"
	IDSourceLabel       = "label"
	IDSourceMerged      = "merged"
	MergedIDSeparator   = "/"
	defaultPollInterval = time.Second
)

// FSChecker checks filesystem paths used by the optical drive reader.
type FSChecker interface {
	Stat(path string) (os.FileInfo, error)
	ReadDir(path string) ([]os.DirEntry, error)
}

type discIdentifier interface {
	Identify(ctx context.Context, devicePath string) (discIdentity, error)
}

// DefaultFSChecker uses os.Stat for filesystem checks.
type DefaultFSChecker struct{}

func (DefaultFSChecker) Stat(path string) (os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat failed: %w", err)
	}
	return info, nil
}

func (DefaultFSChecker) ReadDir(path string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read dir failed: %w", err)
	}
	return entries, nil
}

type contextReaderAtCloser interface {
	contextReaderAt
	Close() error
}

type defaultDiscIdentifier struct{}

var openDiscDeviceReader = openDiscDevice

func (defaultDiscIdentifier) Identify(ctx context.Context, devicePath string) (discIdentity, error) {
	reader, err := openDiscDeviceReader(devicePath)
	if err != nil {
		return discIdentity{}, err
	}
	defer func() {
		_ = reader.Close()
	}()

	identity, found, err := readISO9660IdentityContext(ctx, reader)
	if err != nil {
		return discIdentity{}, err
	}
	if !found {
		return discIdentity{}, errISO9660IdentityNotFound
	}
	return identity, nil
}

type FileReader struct {
	fsChecker         FSChecker
	discIdentifier    discIdentifier
	gameIDProbe       func(path string) []readers.ScanProperty
	watcher           driveWatcher
	inspector         discInspector
	fs                afero.Fs
	cfg               *config.Instance
	device            config.ReadersConnect
	path              string
	sysBlockPath      string
	devPath           string
	wg                sync.WaitGroup
	mu                syncutil.RWMutex
	pollInterval      time.Duration
	defaultEnabled    bool
	defaultAutoDetect bool
	polling           bool
}

func NewReader(cfg *config.Instance) *FileReader {
	return NewReaderWithDefaults(cfg, true, true)
}

func NewReaderWithDefaults(cfg *config.Instance, defaultEnabled, defaultAutoDetect bool) *FileReader {
	return &FileReader{
		cfg:               cfg,
		defaultEnabled:    defaultEnabled,
		defaultAutoDetect: defaultAutoDetect,
		fsChecker:         DefaultFSChecker{},
		discIdentifier:    defaultDiscIdentifier{},
		gameIDProbe:       identifyGameIDProperties,
		watcher:           cdromWatcher{},
		inspector:         cdromInspector{},
		fs:                afero.NewOsFs(),
		sysBlockPath:      filepath.Join(string(filepath.Separator), "sys", "block"),
		devPath:           filepath.Join(string(filepath.Separator), "dev"),
	}
}

func (r *FileReader) Metadata() readers.DriverMetadata {
	return readers.DriverMetadata{
		ID:                "opticaldrive",
		DefaultEnabled:    r.defaultEnabled,
		DefaultAutoDetect: r.defaultAutoDetect,
		Description:       "Optical drive CD/DVD reader",
		PathKind:          readers.PathKindDevice,
		IDSources:         []string{IDSourceUUID, IDSourceLabel, IDSourceMerged},
		CanAutoDetect:     true,
	}
}

func (*FileReader) IDs() []string {
	return []string{"opticaldrive", "optical_drive"}
}

func resolveTokenID(uuid, label, idSource string) string {
	switch idSource {
	case IDSourceUUID:
		return uuid
	case IDSourceLabel:
		return label
	case IDSourceMerged:
		if uuid == "" || label == "" {
			return ""
		}
		return uuid + MergedIDSeparator + label
	default:
		if uuid == "" || label == "" {
			return ""
		}
		return uuid + MergedIDSeparator + label
	}
}

func scanPropertiesEqual(a, b []readers.ScanProperty) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func discTokenFilesEqual(a, b discTokenFile) bool {
	return a.State == b.State && bytes.Equal(a.Data, b.Data)
}

func cloneDiscTokenFile(tokenFile discTokenFile) discTokenFile {
	return discTokenFile{
		Data:  bytes.Clone(tokenFile.Data),
		State: tokenFile.State,
	}
}

func effectiveDiscTokenFile(
	current, previous discTokenFile,
	hasProbed bool,
	identityErr error,
) discTokenFile {
	if identityErr != nil {
		return discTokenFile{State: discTokenFileAbsent}
	}
	if current.State == discTokenFileUnknown {
		if hasProbed {
			return cloneDiscTokenFile(previous)
		}
		return discTokenFile{State: discTokenFileAbsent}
	}
	return current
}

func (r *FileReader) Open(
	device config.ReadersConnect,
	iq chan<- readers.Scan,
	_ readers.OpenOpts,
) error {
	log.Info().Msgf("opening optical drive reader: %s", device.ConnectionString())
	if !readers.MatchesDriverID(r.IDs(), device.Driver) {
		return errors.New("invalid reader id: " + device.Driver)
	}

	path := device.Path

	if !filepath.IsAbs(path) {
		return errors.New("invalid device path, must be absolute")
	}

	parent := filepath.Dir(path)
	if parent == "" {
		return errors.New("invalid device path")
	}

	if _, err := r.fsChecker.Stat(parent); err != nil {
		return fmt.Errorf("failed to stat device parent directory: %w", err)
	}

	r.device = device
	r.path = path
	r.mu.Lock()
	r.polling = true
	r.mu.Unlock()

	interval := r.pollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		st := &pollState{}
		defer r.removeCover(st)

		for {
			r.mu.RLock()
			polling := r.polling
			r.mu.RUnlock()
			if !polling {
				break
			}
			time.Sleep(interval)
			r.poll(st, iq)
		}
	}()

	return nil
}

// pollState is what the poll loop remembers between ticks.
type pollState struct {
	token                *tokens.Token
	tokenProperties      []readers.ScanProperty
	lastUUID             string
	lastLabel            string
	lastIdentityErr      string
	lastKind             string
	lastSystem           string
	lastTOC              string
	coverDir             string
	activeTokenFile      discTokenFile
	lastTokenFile        discTokenFile
	probeAttempts        int
	hasProbed            bool
	discProbed           bool
	statusUnsupportedLog bool
}

// forget drops what was learnt about the disc, so the next probe treats
// whatever is in the drive as newly inserted.
func (st *pollState) forget() {
	st.lastTokenFile = discTokenFile{}
	st.lastUUID, st.lastLabel, st.lastIdentityErr = "", "", ""
	st.lastKind, st.lastSystem, st.lastTOC = "", "", ""
	st.hasProbed = false
	st.discProbed = false
	st.probeAttempts = 0
}

func (r *FileReader) poll(st *pollState, iq chan<- readers.Scan) {
	// Validate device path to prevent command injection
	if !strings.HasPrefix(r.path, "/dev/") {
		log.Warn().Str("path", r.path).Msg("invalid optical drive device path")
		return
	}

	// Check if device still exists (distinguish hardware error from disc removal)
	if _, err := r.fsChecker.Stat(r.path); err != nil {
		if st.token != nil {
			log.Warn().Err(err).Msg("optical drive device no longer exists - " +
				"sending error signal to keep media running")
			st.token = nil
			st.tokenProperties = nil
			st.activeTokenFile = discTokenFile{}
			st.forget()
			r.removeCover(st)
			iq <- readers.Scan{
				Source:      tokens.SourceReader,
				ReaderID:    r.ReaderID(),
				Token:       nil,
				ReaderError: true,
			}
		}
		return
	}

	check, err := r.watcher.Check(r.path)
	if errors.Is(err, errDriveStatusUnsupported) {
		// Without status reports the disc has to be read every tick to
		// notice it change.
		if !st.statusUnsupportedLog {
			st.statusUnsupportedLog = true
			log.Info().Str("path", r.path).Msg("optical drive does not report status, reading the disc to watch it")
		}
		r.probe(st, iq, true)
		return
	}
	if err != nil {
		log.Debug().Err(err).Str("path", r.path).Msg("optical drive status check failed")
		return
	}

	switch check.Status {
	case cdrom.StatusNoDisc, cdrom.StatusTrayOpen:
		if st.token != nil {
			r.sendRemoval(st, iq)
		}
		st.forget()
		return
	case cdrom.StatusDiscOK:
	default:
		return
	}

	// A change while a probe is still being retried is a new disc too: it
	// starts with its own retries, not what the last disc left.
	if check.Changed && (st.discProbed || st.probeAttempts > 0) {
		log.Debug().Str("path", r.path).Msg("optical drive reports a new disc")
		if st.token != nil {
			r.sendRemoval(st, iq)
		}
		st.forget()
	}

	if !st.discProbed {
		st.probeAttempts++
		complete := r.probe(st, iq, st.probeAttempts >= maxProbeRetries)
		if complete || st.probeAttempts >= maxProbeRetries {
			st.discProbed = true
		}
		return
	}

	if r.cfg != nil && r.cfg.OpticalDriveKeepReading() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if touchErr := r.watcher.Touch(ctx, r.path); touchErr != nil {
			log.Debug().Err(touchErr).Str("path", r.path).Msg("optical drive keep_reading read failed")
		}
		cancel()
	}
}

func (r *FileReader) sendRemoval(st *pollState, iq chan<- readers.Scan) {
	st.token = nil
	st.tokenProperties = nil
	st.activeTokenFile = discTokenFile{}
	r.removeCover(st)
	iq <- readers.Scan{
		Source:   tokens.SourceReader,
		ReaderID: r.ReaderID(),
		Token:    nil,
	}
}

// probe reads the disc and sends a scan when what it holds changed. It
// reports false when a read failed and the disc should be probed again. Such
// a partial read is only sent on the last try: a second scan of the same disc
// is dropped as a duplicate, so what the first one missed could never follow.
func (r *FileReader) probe(st *pollState, iq chan<- readers.Scan, lastTry bool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	media := r.inspector.Inspect(ctx, r.path)
	cancel()

	// An audio CD has no filesystem or game ID to read, and reading for
	// them only fails after the drive seeks.
	audio := media.Kind == readers.DiscKindAudio
	var identity discIdentity
	var identityErr error
	if !audio {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		identity, identityErr = r.discIdentifier.Identify(ctx, r.path)
		cancel()
	}

	// A filesystem whose zaparoo.txt could not be looked up is read again,
	// or a disc that carries one would launch as if it had none.
	complete := media.Complete &&
		(identityErr == nil || errors.Is(identityErr, errISO9660IdentityNotFound)) &&
		(audio || identityErr != nil || identity.TokenFile.State != discTokenFileUnknown)
	if !complete && !lastTry {
		return false
	}

	uuid := strings.TrimSpace(identity.UUID)
	label := strings.TrimSpace(identity.Label)
	identityErrStr := errString(identityErr)
	probeTokenFile := effectiveDiscTokenFile(identity.TokenFile, st.lastTokenFile, st.hasProbed, identityErr)

	toc := media.TOC.Fingerprint()
	probeChanged := !st.hasProbed || uuid != st.lastUUID || label != st.lastLabel ||
		identityErrStr != st.lastIdentityErr || !discTokenFilesEqual(probeTokenFile, st.lastTokenFile) ||
		media.Kind != st.lastKind || media.System != st.lastSystem || toc != st.lastTOC
	st.lastUUID, st.lastLabel, st.lastIdentityErr = uuid, label, identityErrStr
	st.lastKind, st.lastSystem, st.lastTOC = media.Kind, media.System, toc
	st.lastTokenFile = cloneDiscTokenFile(probeTokenFile)
	st.hasProbed = true
	if !probeChanged {
		return complete
	}

	drive := driveName(r.path)
	id := resolveTokenID(uuid, label, r.device.IDSource)
	if id == "" {
		id = fallbackTokenID(&media, drive)
	}
	tokenText := ""
	tokenData := ""
	if probeTokenFile.State == discTokenFilePresent {
		tokenText = strings.TrimSpace(string(probeTokenFile.Data))
		if tokenText != "" {
			tokenData = hex.EncodeToString(probeTokenFile.Data)
		}
	}
	var scanProperties []readers.ScanProperty
	if !audio {
		scanProperties = r.gameIDProbe(r.path)
	}
	scanProperties = append(scanProperties, discProperties(&media, drive)...)
	log.Debug().
		Str("path", r.path).
		Str("uuid", uuid).
		Str("label", label).
		Str("kind", media.Kind).
		Str("system", media.System).
		Str("identityErr", identityErrStr).
		Uint8("tokenFileState", uint8(probeTokenFile.State)).
		Int("tokenFileBytes", len(probeTokenFile.Data)).
		Int("properties", len(scanProperties)).
		Msg("optical media identification probe changed")

	if st.token != nil && len(scanProperties) > 0 && scanPropertiesEqual(scanProperties, st.tokenProperties) &&
		(identityErr != nil || discTokenFilesEqual(probeTokenFile, st.activeTokenFile)) {
		if id == "" || st.token.UID == "" || st.token.UID == id {
			return complete
		}
	}

	if id == "" && tokenText == "" && !identifiesDisc(scanProperties) {
		if st.token != nil {
			log.Debug().
				Err(identityErr).
				Msg("error identifying optical media, removing token")
			r.sendRemoval(st, iq)
		}
		return complete
	}

	if st.token != nil && st.token.UID == id && scanPropertiesEqual(scanProperties, st.tokenProperties) &&
		discTokenFilesEqual(probeTokenFile, st.activeTokenFile) {
		return complete
	}

	r.removeCover(st)
	st.token = &tokens.Token{
		Type:     TokenType,
		ScanTime: time.Now(),
		UID:      id,
		Text:     tokenText,
		Data:     tokenData,
		Source:   tokens.SourceReader,
		ReaderID: r.ReaderID(),
		Metadata: r.discMetadata(st, identity.Sidecar, drive),
	}
	st.tokenProperties = append(st.tokenProperties[:0], scanProperties...)
	st.activeTokenFile = cloneDiscTokenFile(probeTokenFile)

	log.Debug().Msgf("new token: %s", st.token.UID)
	iq <- readers.Scan{
		Source:     tokens.SourceReader,
		ReaderID:   r.ReaderID(),
		Token:      st.token,
		Properties: scanProperties,
	}
	return complete
}

// identifiesDisc reports whether properties say anything about which disc
// is in the drive. The disc's kind and drive alone do not: an unreadable or
// still-settling disc has those too.
func identifiesDisc(properties []readers.ScanProperty) bool {
	for _, p := range properties {
		if p.Name != readers.PropertyDiscKind && p.Name != readers.PropertyDiscDrive {
			return true
		}
	}
	return false
}

// fallbackTokenID names a disc with no ISO9660 identity: an audio CD by its
// table of contents, and a DVD by the drive it is in.
func fallbackTokenID(media *discMedia, drive string) string {
	switch media.Kind {
	case readers.DiscKindAudio:
		if fingerprint := media.TOC.Fingerprint(); fingerprint != "" {
			return "cdda:" + fingerprint
		}
	case readers.DiscKindDVD:
		if drive != "" {
			return "dvd:" + drive
		}
	}
	return ""
}

func discProperties(media *discMedia, drive string) []readers.ScanProperty {
	if media.Kind == "" {
		return nil
	}
	props := []readers.ScanProperty{{Name: readers.PropertyDiscKind, Value: media.Kind}}
	if media.System != "" {
		props = append(props, readers.ScanProperty{Name: readers.PropertyDiscSystem, Value: media.System})
	}
	if drive != "" {
		props = append(props, readers.ScanProperty{Name: readers.PropertyDiscDrive, Value: drive})
	}
	return props
}

// discMetadata turns the disc's zaparoo.json into token metadata scoped to
// launches of the disc itself. Its cover is copied to a temporary directory
// that lives until the disc leaves the drive.
func (r *FileReader) discMetadata(st *pollState, sc *discSidecar, drive string) *tokens.Metadata {
	if sc == nil || drive == "" {
		return nil
	}
	meta := &tokens.Metadata{
		Name:        sc.Fields.Name,
		Description: sc.Fields.Description,
		MediaPrefix: readers.PhysicalDiscScheme + "://" + drive + "/",
	}
	if len(sc.Image) > 0 {
		if _, err := imagefiles.ContentType(sc.Image); err != nil {
			log.Warn().Err(err).Msg("ignoring disc zaparoo.json image")
		} else if imagePath, dir, err := r.writeCover(sc.Image, sc.ImageExt); err != nil {
			log.Warn().Err(err).Msg("failed to copy disc cover")
		} else {
			st.coverDir = dir
			meta.ImagePath = imagePath
			meta.ImageRoot = dir
		}
	}
	if meta.Name == "" && meta.Description == "" && meta.ImagePath == "" {
		return nil
	}
	return meta
}

func (r *FileReader) writeCover(data []byte, ext string) (imagePath, dir string, err error) {
	fs := r.fs
	if fs == nil {
		fs = afero.NewOsFs()
	}
	dir, err = afero.TempDir(fs, "", "zaparoo-disc-")
	if err != nil {
		return "", "", fmt.Errorf("create cover dir: %w", err)
	}
	imagePath = filepath.Join(dir, "cover"+ext)
	if err := afero.WriteFile(fs, imagePath, data, 0o600); err != nil {
		_ = fs.RemoveAll(dir)
		return "", "", fmt.Errorf("write cover: %w", err)
	}
	return imagePath, dir, nil
}

func (r *FileReader) removeCover(st *pollState) {
	if st.coverDir == "" {
		return
	}
	fs := r.fs
	if fs == nil {
		fs = afero.NewOsFs()
	}
	if err := fs.RemoveAll(st.coverDir); err != nil {
		log.Warn().Err(err).Str("dir", st.coverDir).Msg("failed to remove disc cover")
	}
	st.coverDir = ""
}

func (r *FileReader) Close() error {
	r.mu.Lock()
	r.polling = false
	r.mu.Unlock()
	r.wg.Wait()
	return nil
}

func (r *FileReader) Detect(exclude []string) string {
	for _, path := range r.drivePaths() {
		if !opticalPathExcluded(path, exclude) {
			return "opticaldrive:" + path
		}
	}
	return ""
}

// ListPorts lists every optical drive's device node, for a manual
// connection.
func (r *FileReader) ListPorts() ([]readers.Port, error) {
	paths := r.drivePaths()
	ports := make([]readers.Port, 0, len(paths))
	for _, path := range paths {
		ports = append(ports, readers.Port{Path: path})
	}
	return ports, nil
}

// drivePaths lists the device node of each sr* block device that exists.
func (r *FileReader) drivePaths() []string {
	sysBlockPath := r.sysBlockPath
	if sysBlockPath == "" {
		sysBlockPath = filepath.Join(string(filepath.Separator), "sys", "block")
	}
	devPath := r.devPath
	if devPath == "" {
		devPath = filepath.Join(string(filepath.Separator), "dev")
	}

	fsChecker := r.fsChecker
	if fsChecker == nil {
		fsChecker = DefaultFSChecker{}
	}

	entries, err := fsChecker.ReadDir(sysBlockPath)
	if err != nil {
		return nil
	}

	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "sr") {
			continue
		}
		path := filepath.Join(devPath, name)
		if _, statErr := fsChecker.Stat(path); statErr != nil {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func opticalPathExcluded(path string, exclude []string) bool {
	for _, excluded := range exclude {
		_, excludedPath, found := strings.Cut(excluded, ":")
		if found && excludedPath == path {
			return true
		}
		if !found && excluded == path {
			return true
		}
	}
	return false
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func identifyGameIDProperties(path string) []readers.ScanProperty {
	started := time.Now()
	log.Debug().Str("path", path).Msg("live gameid identification started")
	candidates := gameid.IdentifyLiveDisc(path)
	duration := time.Since(started)
	if len(candidates) == 0 {
		log.Debug().Str("path", path).Dur("duration", duration).Msg("live gameid not found")
		return nil
	}

	log.Debug().Str("path", path).Int("candidates", len(candidates)).Dur("duration", duration).
		Msg("live gameid identified")

	out := make([]readers.ScanProperty, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, readers.ScanProperty{
			System: candidate.SystemID,
			Name:   string(tags.TagPropertyGameID),
			Value:  candidate.ID,
		})
	}
	return out
}

func (r *FileReader) Path() string {
	return r.path
}

func (r *FileReader) Connected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.polling
}

func (r *FileReader) Info() string {
	return r.path
}

func (*FileReader) Write(_ string) (*tokens.Token, error) {
	return nil, errors.New("writing not supported on this reader")
}

func (*FileReader) CancelWrite() {
	// no-op, writing not supported
}

func (*FileReader) Capabilities() []readers.Capability {
	return []readers.Capability{readers.CapabilityRemovable}
}

func (*FileReader) OnMediaChange(*models.ActiveMedia) error {
	return nil
}

func (r *FileReader) ReaderID() string {
	return readers.GenerateReaderID(r.Metadata().ID, r.path)
}
