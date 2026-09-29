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

package android

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
)

const (
	maxLaunchDefinitionBytes = 16384
	maxLaunchExtensions      = 64
	maxLaunchExtras          = 16
	maxRepairBytes           = 1024

	// StrategyFilesystemPath hands the target a transient filesystem path.
	StrategyFilesystemPath = "filesystem_path"
	// StrategyContentURI hands the target a content URI with a read grant.
	StrategyContentURI = "content_uri"
	// StrategyApp starts an installed app with no media at all. The optional
	// literal extras select one profile-defined variant of it.
	StrategyApp = "app"

	actionMain = "android.intent.action.MAIN"
	actionView = "android.intent.action.VIEW"

	extraSourceMedia = "media"
	// extraSourceLiteral carries a constant the profile chose, never anything
	// derived from media or the device.
	extraSourceLiteral = "literal"
	maxLiteralBytes    = 128
)

// ErrLaunchDefinition reports a launch definition that is malformed or asks
// for a capability this platform does not support.
var ErrLaunchDefinition = errors.New("invalid or unsupported launch definition")

var (
	dottedNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
	extraNamePattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
	extensionPattern  = regexp.MustCompile(`^\.[a-z0-9]{1,15}$`)
)

// LaunchExtra binds one typed intent extra to a host capability, not a string
// template. Extras are string unless a package's own intent contract needs
// otherwise; adding a type for general use requires host negotiation.
type LaunchExtra struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Source string `json:"source"`
	Suffix string `json:"suffix,omitempty"`
	Value  string `json:"value,omitempty"`
}

// LaunchDefinition is binding data: which component to start and how media
// reaches it. Version 1 carries a transient filesystem path. Version 2 carries
// a content URI with an explicit read grant and ClipData. A host must never
// silently substitute one strategy for the other.
type LaunchDefinition struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Variant       string `json:"variant,omitempty"`
	System        string `json:"system"`
	Package       string `json:"package"`
	Activity      string `json:"activity"`
	Action        string `json:"action"`
	Strategy      string `json:"strategy"`
	StorageAccess string `json:"storageAccess"`
	Repair        string `json:"repair"`
	DataSource    string `json:"dataSource,omitempty"`
	// Data is an Intent data URI, set only at dispatch from a file the
	// definition itself carries no reference to. Reserved for the one
	// package whose exported activity reads its target from Intent data
	// (scummVMPackage); every other definition must leave it empty.
	Data         string        `json:"data,omitempty"`
	Extensions   []string      `json:"extensions"`
	Extras       []LaunchExtra `json:"extras,omitempty"`
	Version      int           `json:"version"`
	MaxTargetSDK int           `json:"maxTargetSdk,omitempty"`
	GrantReadURI bool          `json:"grantReadUri,omitempty"`
	ClipData     bool          `json:"clipData,omitempty"`
}

// copy returns a definition that shares nothing with the receiver, so a caller
// outside Core cannot reach the catalog entry it came from. A plain value copy
// would still share the Extensions and Extras backing arrays.
func (d *LaunchDefinition) copy() LaunchDefinition {
	duplicate := *d
	duplicate.Extensions = slices.Clone(d.Extensions)
	duplicate.Extras = slices.Clone(d.Extras)
	return duplicate
}

// parseLaunchDefinition rejects unknown fields, trailing payloads and
// unsupported capabilities.
func parseLaunchDefinition(data []byte) (LaunchDefinition, error) {
	if len(data) > maxLaunchDefinitionBytes {
		return LaunchDefinition{}, ErrLaunchDefinition
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var definition LaunchDefinition
	if err := decoder.Decode(&definition); err != nil {
		return LaunchDefinition{}, ErrLaunchDefinition
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return LaunchDefinition{}, ErrLaunchDefinition
	}
	if err := definition.Validate(); err != nil {
		return LaunchDefinition{}, err
	}
	return definition, nil
}

// Validate reports ErrLaunchDefinition unless every field is bounded and the
// definition uses exactly one supported strategy.
func (d *LaunchDefinition) Validate() error {
	if !validDottedName(d.ID) || !validDottedName(d.Package) || !validDottedName(d.Activity) ||
		!d.validExtensionCount() || len(d.Extensions) > maxLaunchExtensions || len(d.Extras) > maxLaunchExtras ||
		d.Repair == "" || len(d.Repair) > maxRepairBytes || strings.ContainsAny(d.Repair, "\x00\r\n") {
		return ErrLaunchDefinition
	}
	if !d.validStrategy() {
		return ErrLaunchDefinition
	}
	system, err := systemdefs.LookupSystem(d.System)
	if err != nil || system.ID != d.System {
		return ErrLaunchDefinition
	}
	seenExtensions := make(map[string]struct{}, len(d.Extensions))
	for _, value := range d.Extensions {
		if _, seen := seenExtensions[value]; seen || !extensionPattern.MatchString(value) {
			return ErrLaunchDefinition
		}
		seenExtensions[value] = struct{}{}
	}
	return d.validateExtras()
}

// validExtensionCount requires extensions for a media strategy and forbids
// them for an app, which never matches a file.
func (d *LaunchDefinition) validExtensionCount() bool {
	if d.Version == 3 {
		return len(d.Extensions) == 0
	}
	return len(d.Extensions) > 0
}

func (d *LaunchDefinition) validStrategy() bool {
	switch d.Version {
	case 1:
		return d.Action == actionMain && d.Strategy == StrategyFilesystemPath &&
			d.StorageAccess == "legacy_read" && d.MaxTargetSDK >= 1 && d.MaxTargetSDK <= 28 &&
			d.DataSource == "" && !d.GrantReadURI && !d.ClipData && len(d.Extras) > 0
	case 2:
		return d.Action == actionView && d.Strategy == StrategyContentURI &&
			d.StorageAccess == "none" && d.MaxTargetSDK == 0 && d.GrantReadURI && d.ClipData &&
			(d.DataSource == "" || d.DataSource == extraSourceMedia)
	case 3:
		return d.validAppAction() && d.Strategy == StrategyApp &&
			d.StorageAccess == "none" && d.MaxTargetSDK == 0 && d.DataSource == "" &&
			!d.GrantReadURI && !d.ClipData &&
			validCatalogText(d.Name) && d.validVariant() && d.validData()
	default:
		return false
	}
}

// validAppAction allows the two ordinary app actions for every package, and
// one custom action reserved for the single package whose exported activity
// needs it.
func (d *LaunchDefinition) validAppAction() bool {
	if d.Action == actionMain || d.Action == actionView {
		return true
	}
	return d.Action == gameNativeAction && d.Package == gameNativePackage
}

// validData allows an Intent data URI only for the one package whose
// exported activity reads its target from it; every other definition must
// carry none.
func (d *LaunchDefinition) validData() bool {
	if d.Data == "" {
		return true
	}
	return d.Package == scummVMPackage && validCatalogText(d.Data) && len(d.Data) <= maxScummVMDataBytes
}

// validVariant allows no variant, or one that matches the identity grammar so
// a definition can never describe a variant no path could name.
func (d *LaunchDefinition) validVariant() bool {
	return d.Variant == "" || variantPattern.MatchString(d.Variant)
}

// validateExtras requires the media to reach the target exactly once.
func (d *LaunchDefinition) validateExtras() error {
	seen := make(map[string]struct{}, len(d.Extras))
	media := 0
	if d.DataSource == extraSourceMedia {
		media++
	}
	wantMedia := 1
	if d.Version == 3 {
		wantMedia = 0
	}
	for _, extra := range d.Extras {
		// An int extra exists only for the one package whose intent contract
		// needs one; every other definition's extras stay string.
		validType := extra.Type == "string" || (extra.Type == "int" && d.Package == gameNativePackage)
		if _, duplicate := seen[extra.Name]; duplicate || !extraNamePattern.MatchString(extra.Name) || !validType {
			return ErrLaunchDefinition
		}
		seen[extra.Name] = struct{}{}
		if d.Version == 2 && extra.Source != extraSourceMedia {
			return ErrLaunchDefinition
		}
		if d.Version == 3 && extra.Source != extraSourceLiteral {
			return ErrLaunchDefinition
		}
		switch extra.Source {
		case extraSourceLiteral:
			if d.Version != 3 || extra.Suffix != "" || extra.Value == "" ||
				len(extra.Value) > maxLiteralBytes || !validCatalogText(extra.Value) {
				return ErrLaunchDefinition
			}
		case extraSourceMedia:
			media++
			if extra.Suffix != "" {
				return ErrLaunchDefinition
			}
		case "application_apk":
			if extra.Suffix != "" {
				return ErrLaunchDefinition
			}
		case "application_data", "application_external_files", "external_storage":
			if !validSuffix(extra.Suffix) {
				return ErrLaunchDefinition
			}
		default:
			return ErrLaunchDefinition
		}
	}
	if media != wantMedia {
		return ErrLaunchDefinition
	}
	return nil
}

func validDottedName(value string) bool {
	return len(value) <= 255 && dottedNamePattern.MatchString(value)
}

func validSuffix(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 1024 || strings.Contains(value, `\`) {
		return false
	}
	for component := range strings.SplitSeq(value, "/") {
		if !virtualpath.ValidSegment(component) {
			return false
		}
	}
	return true
}
