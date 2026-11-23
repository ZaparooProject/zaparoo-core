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

// Package fakeset starts the offline network fakes a simulated Core uses and
// points a config at them. The sim binary and the in-process test harness
// share it so both wire the fakes the same way.
package fakeset

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/romm"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/libretro"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/logupload"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/online"
	rommfake "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/romm"
)

// Fake names.
const (
	Libretro = "libretro"
	RomM     = "romm"
	Logs     = "logs"
	Online   = "online"
)

// Unreachable is where Configure points a service that has no fake running:
// a loopback port nothing listens on, so a sim never reaches a real server.
const Unreachable = "http://127.0.0.1:9"

// Names lists every fake, in start order.
var Names = []string{Libretro, RomM, Logs, Online}

// Options says where the fakes find their data.
type Options struct {
	// ROMs is the sim ROM tree the libretro and RomM fakes serve.
	ROMs string
	// StateDir is where the Online fake keeps its state; empty keeps it in
	// memory.
	StateDir string
}

// Set is a group of running fakes.
type Set struct {
	// URLs maps each running fake to its base URL.
	URLs map[string]string
	// Mounts maps "/fakes/<name>/" to each fake's control handler.
	Mounts map[string]http.Handler

	Libretro  *libretro.Server
	RomM      *rommfake.Server
	LogUpload *logupload.Server
	Online    *online.Server

	closes []func() error
}

// Validate reports an unknown fake name.
func Validate(names []string) error {
	for _, n := range names {
		if !slices.Contains(Names, n) {
			return fmt.Errorf("unknown fake %q (have %s)", n, strings.Join(Names, ", "))
		}
	}
	return nil
}

// Start starts the named fakes on loopback ports.
func Start(names []string, opts Options) (*Set, error) {
	if err := Validate(names); err != nil {
		return nil, err
	}
	s := &Set{URLs: map[string]string{}, Mounts: map[string]http.Handler{}}
	type fake interface {
		Start(addr string) (string, error)
		Close() error
		ControlHandler() http.Handler
	}
	for _, name := range Names {
		if !slices.Contains(names, name) {
			continue
		}
		var srv fake
		switch name {
		case Libretro:
			s.Libretro = libretro.New(libretro.Options{LibraryRoot: opts.ROMs})
			srv = s.Libretro
		case RomM:
			s.RomM = rommfake.New(rommfake.Options{LibraryRoot: opts.ROMs, AutoApproveLinks: true})
			srv = s.RomM
		case Logs:
			s.LogUpload = logupload.New(logupload.Options{})
			srv = s.LogUpload
		case Online:
			stateDir := ""
			if opts.StateDir != "" {
				stateDir = filepath.Join(opts.StateDir, "online")
			}
			s.Online = online.New(online.Options{StateDir: stateDir})
			srv = s.Online
		}
		url, err := srv.Start("127.0.0.1:0")
		if err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("start %s fake: %w", name, err)
		}
		s.closes = append(s.closes, srv.Close)
		s.URLs[name] = url
		s.Mounts["/fakes/"+name+"/"] = srv.ControlHandler()
	}
	return s, nil
}

// Configure points cfg at the running fakes and saves them to its
// config.toml, so a settings.update or settings.reload that reads the file
// back keeps them. Online, the libretro thumbnail server and log upload point
// at Unreachable when their fake is not running, so the instance stays
// offline. To override a URL after this call, set it and save the config
// again. The returned function undoes the process-wide log upload URL; call
// it when the instance stops.
func (s *Set) Configure(cfg *config.Instance) (restore func(), err error) {
	restore = func() {}
	onlineURL, ok := s.URLs[Online]
	if !ok {
		onlineURL = Unreachable
	}
	if err := cfg.SetOnlineBaseURL(onlineURL); err != nil {
		return restore, fmt.Errorf("set online url: %w", err)
	}
	libretroURL, ok := s.URLs[Libretro]
	if !ok {
		libretroURL = Unreachable
	}
	cfg.SetScraperLibretroThumbnailsBaseURL(libretroURL)
	if u, found := s.URLs[RomM]; found {
		cfg.SetLauncherServerURL(romm.LauncherID, u)
	}
	logsURL, ok := s.URLs[Logs]
	if !ok {
		logsURL = Unreachable
	}
	restore = helpers.SetLogUploadURL(logsURL)
	if err := cfg.Save(); err != nil {
		return restore, fmt.Errorf("save fake urls: %w", err)
	}
	return restore, nil
}

// Close stops every fake.
func (s *Set) Close() error {
	errs := make([]error, 0, len(s.closes))
	for i := len(s.closes) - 1; i >= 0; i-- {
		errs = append(errs, s.closes[i]())
	}
	s.closes = nil
	return errors.Join(errs...)
}
