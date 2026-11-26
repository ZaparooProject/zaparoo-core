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

package methods

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/scanmode"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/rs/zerolog/log"
)

func HandleReaderWrite(
	params json.RawMessage,
	allReaders []readers.Reader,
	lastScanned *tokens.Token,
	setWroteToken func(*tokens.Token),
	setWriteActive ...func(string, bool),
) (any, error) {
	var p models.ReaderWriteParams
	if err := validation.ValidateAndUnmarshal(params, &p); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}

	var writeActivity func(string, bool)
	if len(setWriteActive) > 0 {
		writeActivity = setWriteActive[0]
	}

	var r readers.Reader
	var err error

	if p.ReaderID != nil && *p.ReaderID != "" {
		r, err = readers.SelectWriterStrict(allReaders, *p.ReaderID)
	} else {
		var prefs []string
		// TODO: remove IsZero check when lastScanned is token pointer in state
		if lastScanned != nil && !lastScanned.ScanTime.IsZero() && lastScanned.ReaderID != "" {
			prefs = []string{lastScanned.ReaderID}
		}
		r, err = readers.SelectWriterPreferred(allReaders, prefs)
	}
	if err != nil {
		if errors.Is(err, readers.ErrNoWritableReader) {
			return nil, fmt.Errorf("failed to select writer: %w", models.QuietClientErr(err))
		}
		return nil, models.ClientErrf("failed to select writer: %w", err)
	}
	readerID := r.ReaderID()
	if writeActivity != nil {
		writeActivity(readerID, true)
		defer writeActivity(readerID, false)
	}

	t, err := r.Write(p.Text)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			log.Debug().Err(err).Msg("tag write cancelled")
		case errors.Is(err, readers.ErrTagNotDetected), errors.Is(err, readers.ErrUnsupportedTagType),
			errors.Is(err, readers.ErrReaderDisconnected), errors.Is(err, readers.ErrTagRemoved):
			// Expected user conditions (no tag presented, unsupported tag,
			// reader unplugged, tag pulled away mid-write); keep these out of
			// Sentry. Hardware write failures stay at Error.
			log.Warn().Err(err).Msg("error writing to reader")
		default:
			log.Error().Err(err).Msg("error writing to reader")
		}
		return nil, fmt.Errorf("%w", models.QuietClientErr(errors.New("error writing to reader")))
	}

	if t != nil {
		t.ReaderID = readerID
		setWroteToken(t)
	}

	return NoContent{}, nil
}

func HandleReaderWriteCancel(
	params json.RawMessage,
	allReaders []readers.Reader,
) (any, error) {
	// Params are optional: no params (or null) cancels on every write-capable
	// reader, the same as an empty object.
	var p models.ReaderWriteCancelParams
	if len(params) > 0 && !bytes.Equal(bytes.TrimSpace(params), []byte("null")) {
		if err := validation.ValidateAndUnmarshal(params, &p); err != nil {
			return nil, models.ClientErrf("invalid params: %w", err)
		}
	}

	if p.ReaderID != nil && *p.ReaderID != "" {
		r, err := readers.SelectWriterStrict(allReaders, *p.ReaderID)
		if err != nil {
			return nil, models.ClientErrf("failed to select reader: %w", err)
		}
		r.CancelWrite()
	} else {
		writeCapable := readers.FilterByCapability(allReaders, readers.CapabilityWrite)
		for _, r := range writeCapable {
			r.CancelWrite()
		}
	}

	return NoContent{}, nil
}

func HandleReaders(cfg *config.Instance, st *state.State, allReaders []readers.Reader) (any, error) {
	readerInfos := make([]models.ReaderInfo, 0, len(allReaders))

	var connects []config.ReadersConnect
	connectPaths := make(map[string]struct{})
	for _, entry := range cfg.Readers().Connect {
		if entry.IsEnabled() {
			connects = append(connects, entry)
			connectPaths[entry.Path] = struct{}{}
		}
	}
	connectedPaths := make(map[string]struct{})

	for _, r := range allReaders {
		if r == nil {
			continue
		}

		capabilities := r.Capabilities()
		capabilityStrings := make([]string, len(capabilities))
		for i, capability := range capabilities {
			capabilityStrings[i] = string(capability)
		}

		source := models.ReaderSourceAuto
		if _, ok := connectPaths[r.Path()]; ok {
			source = models.ReaderSourceConnect
		}
		connectedPaths[r.Path()] = struct{}{}

		readerInfo := models.ReaderInfo{
			ID:           r.Path(), // Legacy field, prefer ReaderID for stable identification
			ReaderID:     r.ReaderID(),
			Driver:       r.Metadata().ID,
			Info:         r.Info(),
			ScanMode:     cfg.ScanModeForReader(readers.DriverIDs(r), r.Path()),
			Path:         r.Path(),
			Source:       source,
			Connected:    r.Connected(),
			Capabilities: capabilityStrings,
		}

		readerInfos = append(readerInfos, readerInfo)
	}

	// Configured connections that did not open are listed too, with the
	// reason, so a client can show a connection the user added and why it
	// is not working.
	for i := range connects {
		entry := connects[i]
		if _, ok := connectedPaths[entry.Path]; ok {
			continue
		}
		connectedPaths[entry.Path] = struct{}{}
		readerInfos = append(readerInfos, models.ReaderInfo{
			ID:           entry.Path,
			Driver:       entry.Driver,
			Path:         entry.Path,
			Source:       models.ReaderSourceConnect,
			ScanMode:     cfg.ScanModeForReader([]string{entry.Driver}, entry.Path),
			Error:        st.ReaderConnectError(entry.ConnectionString()),
			Capabilities: []string{},
		})
	}

	response := models.ReadersResponse{
		Readers: readerInfos,
	}

	// The token that owns hold-mode exit, reported by reader and effective
	// policy only so diagnostics never expose what is written on the token.
	// The mode comes from the same resolver the exit path uses, including its
	// rule for an owner whose reader has since disconnected, so this never
	// reports a policy other than the one removal will apply.
	if owner := st.GetSoftwareToken(); owner != nil {
		response.HoldOwnerReaderID = owner.ReaderID
		response.HoldScanMode = scanmode.ForTokenAfterRemoval(cfg, st, owner)
	}

	return response, nil
}

// HandleReadersDrivers lists the reader drivers Core supports on this
// platform, with what a client needs to offer them in a setup screen.
//
//nolint:gocritic // single-use parameter in API handler
func HandleReadersDrivers(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received readers drivers request")

	drivers := make([]models.DriverInfo, 0)
	seen := make(map[string]struct{})
	forEachSupportedReader(env.Platform, env.Config, func(r readers.Reader) {
		metadata := r.Metadata()
		if _, ok := seen[metadata.ID]; ok {
			return
		}
		seen[metadata.ID] = struct{}{}
		pathKind := metadata.PathKind
		if pathKind == "" {
			pathKind = readers.PathKindNone
		}
		idSources := metadata.IDSources
		if idSources == nil {
			idSources = []string{}
		}
		drivers = append(drivers, models.DriverInfo{
			ID:                metadata.ID,
			Description:       metadata.Description,
			PathKind:          string(pathKind),
			IDSources:         idSources,
			DefaultEnabled:    metadata.DefaultEnabled,
			DefaultAutoDetect: metadata.DefaultAutoDetect,
			CanAutoDetect:     metadata.CanAutoDetect,
			Display:           slices.Contains(r.Capabilities(), readers.CapabilityDisplay),
		})
	})

	return models.ReadersDriversResponse{Drivers: drivers}, nil
}

// HandleReadersPorts lists the paths a manual connection for a driver could
// use. A driver whose path is free text, or that takes none, has no list.
//
//nolint:gocritic // single-use parameter in API handler
func HandleReadersPorts(env requests.RequestEnv) (any, error) {
	log.Info().Msg("received readers ports request")

	var params models.ReadersPortsParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		log.Warn().Err(err).Msg("invalid params")
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	want := readers.NormalizeDriverID(params.Driver)

	var (
		found bool
		ports []readers.Port
		err   error
	)
	forEachSupportedReader(env.Platform, env.Config, func(r readers.Reader) {
		if found || readers.NormalizeDriverID(r.Metadata().ID) != want {
			return
		}
		found = true
		ports, err = listReaderPorts(r)
	})
	if !found {
		return nil, models.ClientErrf("unknown driver: %s", params.Driver)
	}
	if err != nil {
		return nil, fmt.Errorf("list ports for %s: %w", params.Driver, err)
	}

	resp := models.ReadersPortsResponse{Ports: make([]models.PortInfo, 0, len(ports))}
	for _, p := range ports {
		resp.Ports = append(resp.Ports, models.PortInfo{Path: p.Path, Description: p.Description})
	}
	return resp, nil
}

func listReaderPorts(r readers.Reader) ([]readers.Port, error) {
	if lister, ok := r.(readers.PortLister); ok {
		ports, err := lister.ListPorts()
		if err != nil {
			return nil, fmt.Errorf("list ports: %w", err)
		}
		return ports, nil
	}
	if r.Metadata().PathKind != readers.PathKindSerial {
		return nil, nil
	}
	paths, err := helpers.GetSerialDeviceList()
	if err != nil {
		return nil, fmt.Errorf("list serial devices: %w", err)
	}
	ports := make([]readers.Port, 0, len(paths))
	for _, p := range paths {
		ports = append(ports, readers.Port{Path: p})
	}
	return ports, nil
}

// forEachSupportedReader calls fn with a fresh instance of each reader the
// platform supports, closing every instance afterwards. The instances are
// never opened.
func forEachSupportedReader(pl platforms.Platform, cfg *config.Instance, fn func(readers.Reader)) {
	if pl == nil {
		return
	}
	for _, r := range pl.SupportedReaders(cfg) {
		if r == nil {
			continue
		}
		fn(r)
		if err := r.Close(); err != nil {
			log.Debug().Err(err).Msg("error closing unused reader")
		}
	}
}
