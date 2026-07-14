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

// Package retroarch provides reusable RetroArch CLI launchers.
package retroarch

import (
	"path/filepath"
	"sync"

	"github.com/spf13/afero"
)

// Profile identifies a hardware class used for core selection.
type Profile string

// RetroArch core-selection profiles.
const (
	ProfileApplianceARM Profile = "appliance-arm"
	ProfileDesktop      Profile = "desktop"
)

// DownloadPolicy controls how a future downloader may fetch a core.
type DownloadPolicy string

// Core download policies.
const (
	PolicyFree          DownloadPolicy = "free"
	PolicyNonCommercial DownloadPolicy = "non-commercial"
)

// PreflightFunc performs consumer-specific launch validation.
type PreflightFunc func(corePath string) error

// EnvFunc returns launch-time environment overrides.
type EnvFunc func() []string

// Options describes how to invoke RetroArch and where its files live.
type Options struct {
	FS               afero.Fs
	Preflight        PreflightFunc
	CoresDir         string
	ConfigPath       string
	AppendConfigPath string
	LibDir           string
	Home             string
	NetworkCmdAddr   string
	Exec             []string
	VTWrap           []string
	ExtraEnv         []string
	LaunchEnv        EnvFunc
	ExtraArgs        []string
}

// CoreLaunch maps one Core system to one libretro core.
type CoreLaunch struct {
	ID         string
	SystemID   string
	Core       string
	Folders    []string
	Extensions []string
	Scan       bool
}

// CommandSpec is a testable command invocation without ambient environment.
type CommandSpec struct {
	Name string
	Args []string
	Env  []string
}

// CoreDef defines a system's default core and ES-DE folder mapping. An explicit
// empty per-profile core disables the system for that profile.
type CoreDef struct {
	PerProfileCore   map[Profile]string
	PerProfilePolicy map[Profile]DownloadPolicy
	SystemID         string
	DefaultCore      string
	ESFolder         string
	Policy           DownloadPolicy
}

// BuildCommand constructs a RetroArch invocation without starting it.
func BuildCommand(opts Options, c CoreLaunch, mediaPath string) CommandSpec { //nolint:gocritic // Public value API.
	return buildCommandWithCore(&opts, c.Core, mediaPath)
}

// MemoizePreflight ensures a shared runtime dependency check runs once per
// launcher catalog while per-core filesystem checks remain independent.
func MemoizePreflight(preflight PreflightFunc) PreflightFunc {
	if preflight == nil {
		return nil
	}

	var once sync.Once
	var result error
	return func(corePath string) error {
		once.Do(func() { result = preflight(corePath) })
		return result
	}
}

func buildCommandWithCore(opts *Options, core, mediaPath string) CommandSpec {
	env := append([]string(nil), opts.ExtraEnv...)
	if opts.Home != "" {
		env = append(env, "HOME="+opts.Home)
	}
	if opts.LibDir != "" {
		env = append(env, "LD_LIBRARY_PATH="+opts.LibDir)
	}
	if len(opts.Exec) == 0 {
		return CommandSpec{Env: env}
	}

	argv := make([]string, 0,
		len(opts.VTWrap)+len(opts.Exec)+len(opts.ExtraArgs)+7)
	argv = append(argv, opts.VTWrap...)
	argv = append(argv, opts.Exec...)
	argv = append(argv, opts.ExtraArgs...)
	if opts.ConfigPath != "" {
		argv = append(argv, "--config", opts.ConfigPath)
	}
	if opts.AppendConfigPath != "" {
		argv = append(argv, "--appendconfig", opts.AppendConfigPath)
	}
	argv = append(argv, "-L", filepath.Join(opts.CoresDir, core), mediaPath)

	return CommandSpec{
		Name: argv[0],
		Args: append([]string(nil), argv[1:]...),
		Env:  env,
	}
}

func cloneCoreLaunch(c *CoreLaunch) CoreLaunch {
	cloned := *c
	cloned.Folders = append([]string(nil), c.Folders...)
	cloned.Extensions = append([]string(nil), c.Extensions...)
	return cloned
}

func cloneOptions(opts *Options) Options {
	cloned := *opts
	cloned.Exec = append([]string(nil), opts.Exec...)
	cloned.VTWrap = append([]string(nil), opts.VTWrap...)
	cloned.ExtraEnv = append([]string(nil), opts.ExtraEnv...)
	cloned.ExtraArgs = append([]string(nil), opts.ExtraArgs...)
	return cloned
}
