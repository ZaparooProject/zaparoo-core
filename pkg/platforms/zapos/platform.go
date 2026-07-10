//go:build linux

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package zapos implements the Zaparoo OS appliance platform.
package zapos

import (
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
)

const retroArchNetworkAddr = "127.0.0.1:55355"

// Platform implements Zaparoo OS support.
type Platform struct {
	*linuxbase.Base
}

// NewPlatform creates a Zaparoo OS platform.
func NewPlatform() *Platform {
	return &Platform{Base: linuxbase.NewBase(platformids.ZapOS)}
}

// Settings returns appliance-specific persistent paths.
func (*Platform) Settings() platforms.Settings {
	dataDir := userdataPath("data", config.AppName)
	return platforms.Settings{
		ConfigDir: userdataPath("config", config.AppName),
		DataDir:   dataDir,
		LogDir:    filepath.Join(dataDir, config.LogsDir),
		TempDir:   filepath.Join(os.TempDir(), config.AppName),
	}
}

// RootDirs returns configured roots or the appliance media root.
func (*Platform) RootDirs(cfg *config.Instance) []string {
	if roots := cfg.IndexRoots(); len(roots) > 0 {
		return roots
	}
	return []string{userdataPath(config.MediaDir)}
}

// SupportedReaders returns the standard Linux reader set.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

// LaunchMedia launches media through the shared Linux implementation.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
	opts *platforms.LaunchOptions,
) error {
	return p.Base.LaunchMedia(cfg, path, launcher, db, opts, p) //nolint:wrapcheck // Base error already has context.
}

// Launchers returns custom launchers followed by curated appliance launchers.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	builtIn := retroarch.NewLaunchers(
		applianceRetroArchOptions(),
		retroarch.CoreLaunches(retroarch.ProfileApplianceARM),
	)
	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), builtIn...)
}

func applianceRetroArchOptions() retroarch.Options {
	pack := userdataPath("runtime", "retroarch")
	return retroarch.Options{
		Exec:           []string{filepath.Join(pack, "retroarch")},
		VTWrap:         []string{"openvt", "-c", "2", "-s", "-w", "--"},
		CoresDir:       filepath.Join(pack, "cores"),
		ConfigPath:     filepath.Join(pack, "retroarch.cfg"),
		LibDir:         filepath.Join(pack, "lib"),
		Home:           userdataPath("home"),
		ExtraArgs:      []string{"-v", "--log-file", userdataPath("ra.log")},
		NetworkCmdAddr: retroArchNetworkAddr,
	}
}

func userdataPath(elements ...string) string {
	parts := make([]string, 0, len(elements)+2)
	parts = append(parts, string(filepath.Separator), "userdata")
	parts = append(parts, elements...)
	return filepath.Join(parts...)
}
