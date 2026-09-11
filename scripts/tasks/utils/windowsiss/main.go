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
	"encoding/base64"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode/utf16"
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
	// ViGEmInstallScript is the base64 of the PowerShell that installs the
	// driver, ready for powershell.exe -EncodedCommand.
	ViGEmInstallScript string
}

// vigemInstallScript installs the unpacked driver, and runs elevated.
//
// It exists because the staging directory is writable by an unprivileged
// process, so handing its contents straight to an elevated msiexec would let
// anything running as the user swap the package and have Windows install it
// with administrator rights. A directory only administrators can write is
// therefore created and locked first, the unpacked package copied into it,
// and only then is the package checked and installed: the order matters,
// because a signature checked before the files are out of reach could be
// checked on one package and acted on for another.
//
// The whole unpacked directory is copied, not just the MSI, because the
// package installs files that sit beside it and fails with 1603 without them.
//
// It is passed to powershell.exe with -EncodedCommand rather than written to
// disk, so that the script itself cannot be swapped the same way.
const vigemInstallScript = `
$ErrorActionPreference = 'Stop'
$stage = Join-Path $env:ProgramData 'Zaparoo\vigembus-stage'
$found = Get-ChildItem -LiteralPath $stage -Recurse -Filter '%MSI%' -File -ErrorAction SilentlyContinue |
  Select-Object -First 1
if (-not $found) { exit 3 }
$safeDir = Join-Path $env:SystemRoot ('Temp\zaparoo-vigembus-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $safeDir -Force | Out-Null
$acl = New-Object System.Security.AccessControl.DirectorySecurity
$acl.SetSecurityDescriptorSddlForm('D:PAI(A;OICI;FA;;;BA)(A;OICI;FA;;;SY)')
Set-Acl -LiteralPath $safeDir -AclObject $acl
Copy-Item -Path (Join-Path $found.Directory.FullName '*') -Destination $safeDir -Recurse -Force
$safe = Join-Path $safeDir '%MSI%'
$sig = Get-AuthenticodeSignature -LiteralPath $safe
if ($sig.Status -ne 'Valid' -or $sig.SignerCertificate.Subject -notlike '*Nefarius Software Solutions*') {
  Remove-Item -LiteralPath $safeDir -Recurse -Force
  exit 4
}
$p = Start-Process msiexec.exe -ArgumentList '/i', ('"' + $safe + '"'), '/qn', '/norestart' -Wait -PassThru
Remove-Item -LiteralPath $safeDir -Recurse -Force
exit $p.ExitCode
`

// vigemInstallCommand renders the elevated install script for an
// architecture, encoded the way powershell.exe -EncodedCommand expects.
func vigemInstallCommand(msi string) string {
	if msi == "" {
		return ""
	}
	script := strings.ReplaceAll(vigemInstallScript, "%MSI%", msi)
	return base64.StdEncoding.EncodeToString(utf16LE(script))
}

// utf16LE encodes a string the way -EncodedCommand reads it.
func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(out[i*2:], u)
	}
	return out
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
		ViGEmInstallScript:     vigemInstallCommand(vigemMsiForArch(*arch)),
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
