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
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
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

	actionMain = "android.intent.action.MAIN"
	actionView = "android.intent.action.VIEW"

	extraSourceMedia = "media"
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
// template. Only string extras exist; adding a type requires host negotiation.
type LaunchExtra struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Source string `json:"source"`
	Suffix string `json:"suffix,omitempty"`
}

// LaunchDefinition is binding data: which component to start and how media
// reaches it. Version 1 carries a transient filesystem path. Version 2 carries
// a content URI with an explicit read grant and ClipData. A host must never
// silently substitute one strategy for the other.
type LaunchDefinition struct {
	ID            string        `json:"id"`
	System        string        `json:"system"`
	Package       string        `json:"package"`
	Activity      string        `json:"activity"`
	Action        string        `json:"action"`
	Strategy      string        `json:"strategy"`
	StorageAccess string        `json:"storageAccess"`
	Repair        string        `json:"repair"`
	DataSource    string        `json:"dataSource,omitempty"`
	Extensions    []string      `json:"extensions"`
	Extras        []LaunchExtra `json:"extras,omitempty"`
	Version       int           `json:"version"`
	MaxTargetSDK  int           `json:"maxTargetSdk,omitempty"`
	GrantReadURI  bool          `json:"grantReadUri,omitempty"`
	ClipData      bool          `json:"clipData,omitempty"`
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
		len(d.Extensions) == 0 || len(d.Extensions) > maxLaunchExtensions || len(d.Extras) > maxLaunchExtras ||
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
	default:
		return false
	}
}

// validateExtras requires the media to reach the target exactly once.
func (d *LaunchDefinition) validateExtras() error {
	seen := make(map[string]struct{}, len(d.Extras))
	media := 0
	if d.DataSource == extraSourceMedia {
		media++
	}
	for _, extra := range d.Extras {
		if _, duplicate := seen[extra.Name]; duplicate ||
			!extraNamePattern.MatchString(extra.Name) || extra.Type != "string" {
			return ErrLaunchDefinition
		}
		seen[extra.Name] = struct{}{}
		if d.Version == 2 && extra.Source != extraSourceMedia {
			return ErrLaunchDefinition
		}
		switch extra.Source {
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
	if media != 1 {
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
		if !sourcepath.ValidName(component) {
			return false
		}
	}
	return true
}

// Matches reports whether a canonical source identity belongs to this
// definition's system and has one of its extensions. It reads the identity's
// own components and never a host-resolved filesystem path.
func (d *LaunchDefinition) Matches(identity string) bool {
	_, parts, err := sourcepath.Parse(identity)
	if err != nil || len(parts) < 2 {
		return false
	}
	system, err := esde.GetSystemID(parts[0])
	if err != nil {
		alias, lookupErr := systemdefs.LookupSystem(parts[0])
		if lookupErr != nil {
			return false
		}
		system = alias.ID
	}
	return system == d.System && slices.Contains(d.Extensions, strings.ToLower(path.Ext(parts[len(parts)-1])))
}
