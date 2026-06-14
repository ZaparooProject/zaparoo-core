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

// Package launchables defines shared IDs and URI helpers for platform-owned
// virtual launch targets.
package launchables

import (
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/google/uuid"
)

const Scheme = "zaparoo"

const encodedIDLength = 26

const launcherIDPrefix = "ZaparooVirtual-"

var encodedIDRe = regexp.MustCompile(`^[a-z2-7]{26}$`)

var uuidBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// LaunchFunc executes a platform-defined virtual target. The path argument is
// the zaparoo:// URI that selected this launchable.
type LaunchFunc func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error)

// Launchable is implemented by all platform-defined virtual launch targets.
type Launchable interface {
	isLaunchable()
}

// Provider is optionally implemented by platforms with launchable targets.
type Provider interface {
	Launchables(*config.Instance) []Launchable
}

// VirtualSystem is a launch-only entry returned from the systems endpoint.
// ID is encoded into zaparoo:// URIs and must be globally unique.
type VirtualSystem struct {
	Launch LaunchFunc
	// Test reports whether the launchable is available. Nil means always available.
	Test     func(*config.Instance) bool
	Name     string
	Category string
	ID       uuid.UUID
}

// VirtualMedia is a single launch-only media-shaped entry attached to a real
// Zaparoo system and indexed into MediaDB as a virtual URI path.
type VirtualMedia struct {
	Launch LaunchFunc
	// Test reports whether the launchable is available. Nil means always available.
	Test     func(*config.Instance) bool
	SystemID string
	Name     string
	ID       uuid.UUID
}

func (VirtualSystem) isLaunchable() {}
func (VirtualMedia) isLaunchable()  {}

// EncodeID converts a UUID to canonical lowercase RFC 4648 base32 without padding.
func EncodeID(id uuid.UUID) string {
	return strings.ToLower(uuidBase32.EncodeToString(id[:]))
}

// DecodeID decodes a canonical zaparoo URI host ID. Input is case-insensitive.
func DecodeID(s string) (uuid.UUID, error) {
	var id uuid.UUID
	normalized := strings.ToLower(strings.TrimSpace(s))
	if len(normalized) != encodedIDLength || !encodedIDRe.MatchString(normalized) {
		return id, fmt.Errorf("invalid launchable id %q", s)
	}
	raw, err := uuidBase32.DecodeString(strings.ToUpper(normalized))
	if err != nil {
		return id, fmt.Errorf("decode launchable id: %w", err)
	}
	if len(raw) != len(id) {
		return id, fmt.Errorf("decoded launchable id has %d bytes", len(raw))
	}
	copy(id[:], raw)
	return id, nil
}

// ZapScript returns the generated zaparoo URI for a virtual system.
func (s *VirtualSystem) ZapScript() string {
	return makeURI(s.ID, s.Name)
}

// ZapScript returns the generated zaparoo URI for a virtual media item.
func (m *VirtualMedia) ZapScript() string {
	return makeURI(m.ID, m.Name)
}

func makeURI(id uuid.UUID, name string) string {
	return fmt.Sprintf("%s://%s/%s", Scheme, EncodeID(id), url.PathEscape(name))
}

// Launchables returns validated launchables defined by a platform.
func Launchables(cfg *config.Instance, pl platforms.Platform) []Launchable {
	provider, ok := pl.(Provider)
	if !ok {
		return nil
	}
	defs := provider.Launchables(cfg)
	if len(defs) == 0 {
		return nil
	}
	if err := validateLaunchables(defs); err != nil {
		panic(err)
	}
	return filterAvailable(cfg, defs)
}

func filterAvailable(cfg *config.Instance, defs []Launchable) []Launchable {
	out := make([]Launchable, 0, len(defs))
	for i := range defs {
		switch entry := defs[i].(type) {
		case VirtualSystem:
			if entry.Test == nil || entry.Test(cfg) {
				out = append(out, entry)
			}
		case *VirtualSystem:
			if entry.Test == nil || entry.Test(cfg) {
				out = append(out, entry)
			}
		case VirtualMedia:
			if entry.Test == nil || entry.Test(cfg) {
				out = append(out, entry)
			}
		case *VirtualMedia:
			if entry.Test == nil || entry.Test(cfg) {
				out = append(out, entry)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validateLaunchables(defs []Launchable) error {
	seen := make(map[uuid.UUID]string, len(defs))
	for i := range defs {
		switch entry := defs[i].(type) {
		case VirtualSystem:
			if err := validateSystem(entry); err != nil {
				return err
			}
			if err := addID(seen, entry.ID, "system", entry.Name); err != nil {
				return err
			}
		case *VirtualSystem:
			if entry == nil {
				return errors.New("nil virtual system")
			}
			if err := validateSystem(*entry); err != nil {
				return err
			}
			if err := addID(seen, entry.ID, "system", entry.Name); err != nil {
				return err
			}
		case VirtualMedia:
			if err := validateMedia(entry); err != nil {
				return err
			}
			if err := addID(seen, entry.ID, "media", entry.Name); err != nil {
				return err
			}
		case *VirtualMedia:
			if entry == nil {
				return errors.New("nil virtual media")
			}
			if err := validateMedia(*entry); err != nil {
				return err
			}
			if err := addID(seen, entry.ID, "media", entry.Name); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported launchable type %T", defs[i])
		}
	}
	return nil
}

func validateSystem(entry VirtualSystem) error {
	if err := validateCommon(entry.ID, entry.Name, entry.Launch); err != nil {
		return fmt.Errorf("virtual system %q: %w", entry.Name, err)
	}
	if entry.Category == "" {
		return fmt.Errorf("virtual system %q: category is required", entry.Name)
	}
	return nil
}

func validateMedia(entry VirtualMedia) error {
	if err := validateCommon(entry.ID, entry.Name, entry.Launch); err != nil {
		return fmt.Errorf("virtual media %q: %w", entry.Name, err)
	}
	if _, err := systemdefs.GetSystem(entry.SystemID); err != nil {
		return fmt.Errorf("virtual media %q: invalid system %q: %w", entry.Name, entry.SystemID, err)
	}
	return nil
}

func validateCommon(id uuid.UUID, name string, launch LaunchFunc) error {
	if id == uuid.Nil {
		return errors.New("id is required")
	}
	if name == "" {
		return errors.New("name is required")
	}
	if launch == nil {
		return errors.New("launch function is required")
	}
	return nil
}

func addID(seen map[uuid.UUID]string, id uuid.UUID, typ, name string) error {
	label := typ + ":" + name
	if existing, ok := seen[id]; ok {
		return fmt.Errorf("duplicate launchable id %s for %s and %s", EncodeID(id), existing, label)
	}
	seen[id] = label
	return nil
}

// Systems returns virtual systems defined by this platform.
func Systems(cfg *config.Instance, pl platforms.Platform) []VirtualSystem {
	defs := Launchables(cfg, pl)
	if len(defs) == 0 {
		return nil
	}
	out := make([]VirtualSystem, 0, len(defs))
	for i := range defs {
		switch entry := defs[i].(type) {
		case VirtualSystem:
			out = append(out, entry)
		case *VirtualSystem:
			out = append(out, *entry)
		}
	}
	return out
}

// Media returns virtual media defined by this platform.
func Media(cfg *config.Instance, pl platforms.Platform) []VirtualMedia {
	defs := Launchables(cfg, pl)
	if len(defs) == 0 {
		return nil
	}
	out := make([]VirtualMedia, 0, len(defs))
	for i := range defs {
		switch entry := defs[i].(type) {
		case VirtualMedia:
			out = append(out, entry)
		case *VirtualMedia:
			out = append(out, *entry)
		}
	}
	return out
}

// MediaForSystem returns virtual media for a real Zaparoo system ID.
func MediaForSystem(cfg *config.Instance, pl platforms.Platform, systemID string) []VirtualMedia {
	media := Media(cfg, pl)
	out := make([]VirtualMedia, 0, len(media))
	for i := range media {
		if media[i].SystemID == systemID {
			out = append(out, media[i])
		}
	}
	return out
}

// Launchers returns scheme launchers that execute platform-defined launchables.
func Launchers(cfg *config.Instance, pl platforms.Platform) []platforms.Launcher {
	defs := Launchables(cfg, pl)
	if len(defs) == 0 {
		return nil
	}
	launchers := make([]platforms.Launcher, 0, len(defs))
	for i := range defs {
		switch entry := defs[i].(type) {
		case VirtualSystem:
			launchers = append(launchers, systemLauncher(entry))
		case *VirtualSystem:
			launchers = append(launchers, systemLauncher(*entry))
		case VirtualMedia:
			launchers = append(launchers, mediaLauncher(entry))
		case *VirtualMedia:
			launchers = append(launchers, mediaLauncher(*entry))
		}
	}
	return launchers
}

func systemLauncher(entry VirtualSystem) platforms.Launcher {
	return platforms.Launcher{
		ID:      launcherID(entry.ID),
		Schemes: []string{Scheme},
		Test:    pathMatchesID(entry.ID),
		Launch:  launchWith(entry.ID, entry.Launch),
	}
}

func mediaLauncher(entry VirtualMedia) platforms.Launcher {
	return platforms.Launcher{
		ID:       launcherID(entry.ID),
		SystemID: entry.SystemID,
		Schemes:  []string{Scheme},
		Test:     pathMatchesID(entry.ID),
		Launch:   launchWith(entry.ID, entry.Launch),
	}
}

func launcherID(id uuid.UUID) string {
	return launcherIDPrefix + EncodeID(id)
}

func pathMatchesID(id uuid.UUID) func(*config.Instance, string) bool {
	return func(_ *config.Instance, path string) bool {
		pathID, ok, err := ParseURI(path)
		return err == nil && ok && pathID == id
	}
}

func launchWith(
	id uuid.UUID,
	launch LaunchFunc,
) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		pathID, ok, err := ParseURI(path)
		if err != nil {
			return nil, err
		}
		if !ok || pathID != id {
			return nil, fmt.Errorf("zaparoo URI %q does not match launcher %s", path, launcherID(id))
		}
		return launch(cfg, path, opts)
	}
}

// ParseURI extracts the UUID from a zaparoo URI. The path is decorative.
func ParseURI(rawURI string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	u, err := url.Parse(rawURI)
	if err != nil {
		return id, false, fmt.Errorf("parse zaparoo URI: %w", err)
	}
	if !strings.EqualFold(u.Scheme, Scheme) {
		return id, false, nil
	}
	if u.Host == "" {
		return id, true, errors.New("zaparoo URI missing id host")
	}
	id, err = DecodeID(u.Host)
	if err != nil {
		return id, true, err
	}
	return id, true, nil
}

// IsURI reports whether s uses the zaparoo virtual launch URI scheme.
func IsURI(s string) bool {
	u, err := url.Parse(s)
	return err == nil && strings.EqualFold(u.Scheme, Scheme)
}
