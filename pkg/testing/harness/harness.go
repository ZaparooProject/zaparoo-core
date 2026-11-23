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

// Package harness boots a complete Zaparoo Core service on the sim platform
// inside a test process: real service, API, reader pipeline, ZapScript,
// launch lifecycle and databases, with simulated readers, sim launchers that
// run invisible child processes, generated ROM trees and offline network
// fakes. Nothing reaches the host's Zaparoo directories, its audio device,
// its readers or the network beyond loopback.
//
// Only one Core runtime may run in a process, so instances are serialized:
// New and Start block until the previous instance has closed.
package harness

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/sim"
	simreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/sim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakeset"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/simfixtures"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// runtimeMu serializes instances: the service keeps process-wide state such
// as the launcher cache and the logger.
// A plain mutex: an instance holds it for its whole life, which the deadlock
// detector would report as a stuck lock.
var runtimeMu sync.Mutex //nolint:forbidigo // held for an instance lifetime

// DefaultReader is the sim reader an instance plugs in when Options.Readers
// is nil.
const DefaultReader = "sim0"

// StartTimeout bounds booting an instance, including fixture generation,
// reader connection and the optional index run.
var StartTimeout = 2 * time.Minute

// Options configures an instance.
type Options struct {
	// Fixtures, if set, generates a ROM tree under the sim root before boot.
	// Its Root is filled in.
	Fixtures *simfixtures.Options
	// Values adjusts the config written on first boot.
	Values func(*config.Values)
	// Config adjusts the loaded config before the service starts. The config
	// is saved afterwards, so its changes survive settings.update and
	// settings.reload.
	Config func(*config.Instance) error
	// Root is the sim root. Empty makes a short temporary directory that Close
	// removes; a given root is kept.
	Root string
	// Readers are the sim readers plugged in at boot. Nil means DefaultReader;
	// an empty non-nil slice plugs in none.
	Readers []string
	// Fakes are the offline network fakes to start, by fakeset name.
	Fakes []string
	// Index runs a real media.generate once the service is up.
	Index bool
	// NoWaitReaders skips waiting for Core to open the boot readers.
	NoWaitReaders bool
}

// Instance is a running sim Core.
type Instance struct {
	Platform *sim.Platform
	Config   *config.Instance
	API      *sim.API
	Control  *sim.Client
	Fakes    *fakeset.Set
	Audio    *Recorder
	Fixtures *simfixtures.Report
	Events   *EventLog

	closeErr   error
	result     *service.StartResult
	cancel     context.CancelFunc
	Layout     sim.Layout
	Root       string
	readers    []string
	restore    []func()
	closeOnce  sync.Once
	removeRoot bool
}

// New starts an instance for a test and closes it when the test ends. It
// fails the test if the instance cannot start.
func New(t testing.TB, opts *Options) *Instance {
	t.Helper()
	in, err := Start(context.Background(), opts)
	if err != nil {
		t.Fatalf("harness: %v", err)
	}
	t.Cleanup(func() {
		if err := in.Close(); err != nil {
			t.Errorf("harness: close: %v", err)
		}
	})
	return in
}

// Start starts an instance. The caller must Close it.
func Start(ctx context.Context, opts *Options) (in *Instance, err error) {
	runtimeMu.Lock() //nolint:forbidigo // see runtimeMu
	in = &Instance{Audio: NewRecorder(), Events: newEventLog()}
	defer func() {
		if err != nil {
			_ = in.Close()
		}
	}()
	in.restore = append(in.restore, runtimeMu.Unlock) //nolint:forbidigo // see runtimeMu

	ctx, cancel := context.WithTimeout(ctx, StartTimeout)
	defer cancel()

	root := opts.Root
	if root == "" {
		// Short, so the API and control sockets fit the Unix path limit.
		if root, err = os.MkdirTemp("", "zsim"); err != nil {
			return in, fmt.Errorf("make sim root: %w", err)
		}
		in.removeRoot = true
	}
	in.Root = root
	if in.Layout, err = sim.NewLayout(root); err != nil {
		return in, err //nolint:wrapcheck // self-describing
	}
	if rootErr := in.Layout.CheckRoot(); rootErr != nil {
		return in, rootErr //nolint:wrapcheck // self-describing
	}
	if rootErr := in.Layout.Ensure(); rootErr != nil {
		return in, rootErr //nolint:wrapcheck // self-describing
	}

	if opts.Fixtures != nil {
		fx := *opts.Fixtures
		fx.Root = in.Layout.ROMs
		if in.Fixtures, err = simfixtures.Generate(ctx, fx); err != nil {
			return in, fmt.Errorf("generate fixtures: %w", err)
		}
	}

	// Before anything can open an audio device: an instance never makes sound.
	audio.UseSilentOutput()

	if in.Fakes, err = fakeset.Start(opts.Fakes, fakeset.Options{ROMs: in.Layout.ROMs}); err != nil {
		return in, err //nolint:wrapcheck // self-describing
	}

	in.readers = opts.Readers
	if in.readers == nil {
		in.readers = []string{DefaultReader}
	}
	hub := simreader.NewHub()
	for _, name := range in.readers {
		if _, err = hub.Add(name, simreader.DeviceOptions{}); err != nil {
			return in, fmt.Errorf("add reader %s: %w", name, err)
		}
	}
	if in.Platform, err = sim.New(&sim.Options{Layout: in.Layout, Hub: hub, Mounts: in.Fakes.Mounts}); err != nil {
		return in, err //nolint:wrapcheck // self-describing
	}
	in.Control = sim.NewHandlerClient(in.Platform.ControlHandler())

	if err = helpers.EnsureDirectories(in.Platform); err != nil {
		return in, fmt.Errorf("create directories: %w", err)
	}
	defaults := in.Layout.Defaults(sim.DefaultsOptions{})
	if opts.Values != nil {
		opts.Values(&defaults)
	}
	if in.Config, err = config.NewConfig(in.Layout.Config, defaults); err != nil {
		return in, fmt.Errorf("load config: %w", err)
	}
	restoreFakes, err := in.Fakes.Configure(in.Config)
	in.restore = append(in.restore, restoreFakes)
	if err != nil {
		return in, err //nolint:wrapcheck // self-describing
	}
	if opts.Config != nil {
		if err = opts.Config(in.Config); err != nil {
			return in, fmt.Errorf("configure: %w", err)
		}
	}
	// settings.update and settings.reload read config.toml back in.
	if err = in.Config.Save(); err != nil {
		return in, fmt.Errorf("save config: %w", err)
	}

	// The service owns a process-wide launcher cache and logger.
	prevCache := helpers.GlobalLauncherCache
	helpers.GlobalLauncherCache = &helpers.LauncherCache{}
	prevLevel := zerolog.GlobalLevel()
	in.restore = append(in.restore, func() {
		helpers.GlobalLauncherCache = prevCache
		zerolog.SetGlobalLevel(prevLevel)
		_ = helpers.CloseLogging()
	})

	socket := filepath.Join(in.Layout.Temp, "api.sock")
	ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", socket)
	if err != nil {
		return in, fmt.Errorf("listen on api socket: %w", err)
	}
	runCtx, runCancel := context.WithCancel(context.Background())
	in.cancel = runCancel
	// Core's Unix listener always requires an API key.
	key := uuid.NewString()
	in.result, err = service.StartEmbedded(in.Platform, in.Config, service.EmbeddedOptions{
		Context:  runCtx,
		Listener: ln,
		Audio:    in.Audio,
		APIKeys:  func() []string { return []string{key} },
	})
	if err != nil {
		return in, fmt.Errorf("start service: %w", err)
	}
	in.API = sim.NewUnixAPI(socket, key)

	if eventsErr := in.Events.start(runCtx, in.API); eventsErr != nil {
		return in, eventsErr
	}
	if !opts.NoWaitReaders {
		if waitErr := in.WaitReaders(ctx, in.readers...); waitErr != nil {
			return in, waitErr
		}
	}
	if opts.Index {
		if _, err = in.API.Index(ctx, nil); err != nil {
			return in, fmt.Errorf("index: %w", err)
		}
	}
	return in, nil
}

// WaitReaders waits until Core has opened each named sim reader.
func (in *Instance) WaitReaders(ctx context.Context, names ...string) error {
	for {
		open := map[string]bool{}
		for _, st := range in.Platform.Hub().List() {
			open[st.Name] = st.Open
		}
		missing := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return open[n] })
		if len(missing) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for readers %v: %w", missing, ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// Reader returns the named sim reader device, or the first boot reader when
// name is empty.
func (in *Instance) Reader(name string) (*simreader.Device, error) {
	if name == "" {
		if len(in.readers) == 0 {
			return nil, errors.New("no sim reader")
		}
		name = in.readers[0]
	}
	return in.Platform.Hub().Device(name) //nolint:wrapcheck // self-describing
}

// Close stops the service and every fake, kills any child process the sim
// started and removes a temporary root. It is safe to call more than once.
func (in *Instance) Close() error {
	in.closeOnce.Do(func() {
		var errs []error
		if in.result != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			errs = append(errs, in.result.StopContext(ctx))
			cancel()
			select {
			case <-in.result.Done:
			case <-time.After(30 * time.Second):
				errs = append(errs, errors.New("service did not stop"))
			}
		}
		if in.cancel != nil {
			in.cancel()
		}
		if in.Events != nil {
			in.Events.waitDone()
		}
		if in.Platform != nil {
			_ = in.Platform.Stop()
		}
		if in.Fakes != nil {
			errs = append(errs, in.Fakes.Close())
		}
		for i := len(in.restore) - 1; i >= 0; i-- {
			in.restore[i]()
		}
		if in.removeRoot && in.Root != "" {
			errs = append(errs, os.RemoveAll(in.Root))
		}
		in.closeErr = errors.Join(errs...)
	})
	return in.closeErr
}
