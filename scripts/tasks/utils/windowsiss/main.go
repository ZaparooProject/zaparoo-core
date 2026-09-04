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

package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

type InnoSetupData struct {
	Version                string
	OutputVersion          string
	Arch                   string
	ArchitecturesAllowed   string
	ArchitecturesInstall64 string
	Year                   string
	// ViGEmMsi is the MSI the ViGEmBus bootstrapper unpacks for this
	// architecture, or empty when the driver is not offered. The vendor ships
	// only x86 and x64 packages, so an ARM64 installer leaves the task out
	// entirely rather than offering one that cannot work.
	ViGEmMsi string
}

// vigemMsiForArch names the ViGEmBus MSI to install on a given build
// architecture. The bootstrapper unpacks both, so the installer picks rather
// than relying on the bootstrapper's own detection.
func vigemMsiForArch(arch string) string {
	switch arch {
	case "amd64":
		return "ViGEmBus.x64.msi"
	case "386":
		return "ViGEmBus.msi"
	default:
		return ""
	}
}

func main() {
	version := flag.String("version", "", "Version number")
	arch := flag.String("arch", "", "Architecture (386, amd64, or arm64)")
	flag.Parse()

	if *version == "" || *arch == "" {
		_, _ = fmt.Fprint(os.Stderr, "Error: version and arch are required\n")
		os.Exit(1)
	}

	outputVersion := *version
	// Inno Setup VersionInfoVersion requires a valid numeric version (e.g., 1.2.3)
	// Fall back to 0.0.0 for non-semver versions like "dev" or pre-release versions
	if strings.Contains(*version, "-") || !isValidSemver(*version) {
		*version = "0.0.0"
	}

	var archAllowed, archInstall string
	switch *arch {
	case "amd64":
		archAllowed = "x64compatible"
		archInstall = "x64compatible"
	case "arm64":
		archAllowed = "arm64"
		archInstall = "arm64"
	case "386":
		archAllowed = "x86compatible"
		archInstall = "" // 32-bit doesn't need special install mode
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Error: unsupported architecture: %s\n", *arch)
		os.Exit(1)
	}

	data := InnoSetupData{
		Version:                *version,
		OutputVersion:          outputVersion,
		Arch:                   *arch,
		ArchitecturesAllowed:   archAllowed,
		ArchitecturesInstall64: archInstall,
		Year:                   strconv.Itoa(time.Now().Year()),
		ViGEmMsi:               vigemMsiForArch(*arch),
	}

	tmpl, err := template.ParseFiles("cmd/windows/setup.iss.tmpl")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error parsing template: %v\n", err)
		os.Exit(1)
	}

	if err := generateSetupFile(tmpl, data, *arch); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error generating setup file: %v\n", err)
		os.Exit(1)
	}
}

//nolint:gocritic // single-use parameter in generator
func generateSetupFile(
	tmpl *template.Template,
	data InnoSetupData,
	arch string,
) error {
	//nolint:gosec // Safe: creates installer script in build environment with controlled path
	outFile, err := os.Create("_build/windows_" + arch + "/setup.iss")
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}
	defer func(outFile *os.File) {
		_ = outFile.Close()
	}(outFile)

	if err := tmpl.Execute(outFile, data); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	return nil
}

// isValidSemver checks if a version string starts with a digit (basic semver check).
// Returns false for versions like "dev", "latest", etc.
func isValidSemver(version string) bool {
	if version == "" {
		return false
	}
	return version[0] >= '0' && version[0] <= '9'
}
