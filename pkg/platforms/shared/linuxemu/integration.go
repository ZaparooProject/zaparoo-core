//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package linuxemu provides reusable emulator discovery and launchers for
// Linux-family platforms.
package linuxemu

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/gamescope"
	sharedretroarch "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
)

const flatpakDiscoveryTimeout = 2 * time.Second

// Options parameterizes platform-specific Linux emulator behavior.
type Options struct {
	RetroArch            *sharedretroarch.Options
	GameMode             *gamescope.Manager
	LaunchEnv            func() []string
	IsFlatpakInstalled   func(string) bool
	LookPath             func(string) (string, error)
	HomeDir              string
	IncludeRetroArch     bool
	IncludeStandalone    bool
	IncludeProviderDecks bool
}

// NewOptions returns defaults suitable for a desktop Linux platform.
//
//nolint:gocritic // Value API owns a stable copy.
func NewOptions(homeDir string, retroArch sharedretroarch.Options) Options {
	return Options{
		RetroArch:            &retroArch,
		HomeDir:              homeDir,
		IsFlatpakInstalled:   newFlatpakChecker(),
		LookPath:             exec.LookPath,
		IncludeRetroArch:     true,
		IncludeStandalone:    true,
		IncludeProviderDecks: true,
	}
}

func (o *Options) normalize() {
	if o.HomeDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			o.HomeDir = home
		} else {
			o.HomeDir = os.Getenv("HOME")
		}
	}
	if o.IsFlatpakInstalled == nil {
		o.IsFlatpakInstalled = newFlatpakChecker()
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.HomeDir == "" {
		o.IncludeProviderDecks = false
	}
}

func newFlatpakChecker() func(string) bool {
	var once sync.Once
	installed := make(map[string]struct{})
	return func(appID string) bool {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), flatpakDiscoveryTimeout)
			defer cancel()
			output, err := exec.CommandContext(
				ctx, "flatpak", "list", "--app", "--columns=application",
			).Output()
			if err != nil {
				return
			}
			for line := range strings.SplitSeq(string(output), "\n") {
				if id := strings.TrimSpace(line); id != "" {
					installed[id] = struct{}{}
				}
			}
		})
		_, ok := installed[appID]
		return ok
	}
}

// Launchers discovers emulator integrations whose required applications and
// directories currently exist. IDs already present in existing are suppressed.
func Launchers(cfg *config.Instance, options Options, existing []platforms.Launcher) []platforms.Launcher {
	options.normalize()
	result := make([]platforms.Launcher, 0, 64)
	seenIDs := make([]string, 0, len(existing)+64)
	for i := range existing {
		// A launcher that cannot start anything must not displace one that
		// can. A scan-only custom launcher only widens a system's media
		// directories, so suppressing the real launcher for its ID would
		// leave that system indexed and unlaunchable.
		if existing[i].ScanOnly {
			continue
		}
		seenIDs = append(seenIDs, existing[i].ID)
	}
	appendUnique := func(items ...platforms.Launcher) {
		for i := range items {
			if launcherIDInSlice(seenIDs, items[i].ID) {
				continue
			}
			seenIDs = append(seenIDs, items[i].ID)
			result = append(result, items[i])
		}
	}

	if options.IncludeRetroArch && retroArchAvailable(&options) {
		appendUnique(defaultRetroArchLaunchers(&options)...)
	}
	if options.IncludeStandalone {
		appendUnique(nativeStandaloneLaunchers(&options)...)
	}
	if options.IncludeProviderDecks {
		appendUnique(emuDeckLaunchers(cfg, &options)...)
		appendUnique(retroDECKLaunchers(cfg, &options)...)
	}
	configurePlainESDEScanners(cfg, &options, result)
	return result
}

func launcherIDInSlice(ids []string, id string) bool {
	for i := range ids {
		if strings.EqualFold(ids[i], id) {
			return true
		}
	}
	return false
}

func wrapGameMode(options *Options, launcher *platforms.Launcher) {
	if options.GameMode != nil {
		options.GameMode.WrapLauncher(launcher)
	}
}
