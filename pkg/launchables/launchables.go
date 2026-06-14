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

// Package launchables defines virtual launch targets exposed through existing
// system and media API shapes without inserting synthetic system definitions.
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

// LaunchFunc executes a code-defined virtual target. The path argument is the
// zaparoo:// URI that selected this launchable.
type LaunchFunc func(*config.Instance, platforms.Platform, string, *platforms.LaunchOptions) (*os.Process, error)

// VirtualSystem is a launch-only entry returned from the systems endpoint.
// ID is encoded into zaparoo:// URIs and must be globally unique.
type VirtualSystem struct {
	Launch      LaunchFunc
	Name        string
	Category    string
	PlatformIDs []string
	ID          uuid.UUID
}

// VirtualMedia is a single launch-only media-shaped entry attached to a real
// Zaparoo system and indexed into MediaDB as a virtual URI path.
type VirtualMedia struct {
	Launch      LaunchFunc
	SystemID    string
	Name        string
	PlatformIDs []string
	ID          uuid.UUID
}

// Registry contains all code-defined virtual launch targets.
type Registry struct {
	byID    map[uuid.UUID]string
	systems []VirtualSystem
	media   []VirtualMedia
}

// SystemDefinitions is the central code-defined list of launch-only systems.
var SystemDefinitions = []VirtualSystem{}

// MediaDefinitions is the central code-defined list of virtual media items.
var MediaDefinitions = []VirtualMedia{}

// DefaultRegistry is used by API handlers, indexing, and virtual launchers.
var DefaultRegistry = MustNewRegistry(SystemDefinitions, MediaDefinitions)

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

// NewRegistry validates and indexes virtual launch definitions.
func NewRegistry(systems []VirtualSystem, media []VirtualMedia) (*Registry, error) {
	r := &Registry{
		byID:    make(map[uuid.UUID]string, len(systems)+len(media)),
		systems: append([]VirtualSystem(nil), systems...),
		media:   append([]VirtualMedia(nil), media...),
	}

	for i := range r.systems {
		entry := &r.systems[i]
		if err := validateCommon(entry.ID, entry.Name, entry.Launch); err != nil {
			return nil, fmt.Errorf("virtual system %q: %w", entry.Name, err)
		}
		if entry.Category == "" {
			return nil, fmt.Errorf("virtual system %q: category is required", entry.Name)
		}
		if err := r.addID(entry.ID, "system", entry.Name); err != nil {
			return nil, err
		}
	}

	for i := range r.media {
		entry := &r.media[i]
		if err := validateCommon(entry.ID, entry.Name, entry.Launch); err != nil {
			return nil, fmt.Errorf("virtual media %q: %w", entry.Name, err)
		}
		if _, err := systemdefs.GetSystem(entry.SystemID); err != nil {
			return nil, fmt.Errorf("virtual media %q: invalid system %q: %w", entry.Name, entry.SystemID, err)
		}
		if err := r.addID(entry.ID, "media", entry.Name); err != nil {
			return nil, err
		}
	}

	return r, nil
}

// MustNewRegistry panics when registry definitions are invalid.
func MustNewRegistry(systems []VirtualSystem, media []VirtualMedia) *Registry {
	r, err := NewRegistry(systems, media)
	if err != nil {
		panic(err)
	}
	return r
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

func (r *Registry) addID(id uuid.UUID, typ, name string) error {
	label := typ + ":" + name
	if existing, ok := r.byID[id]; ok {
		return fmt.Errorf("duplicate launchable id %s for %s and %s", EncodeID(id), existing, label)
	}
	r.byID[id] = label
	return nil
}

// Systems returns virtual systems available for this platform.
func (r *Registry) Systems(pl platforms.Platform) []VirtualSystem {
	if len(r.systems) == 0 {
		return nil
	}
	platformID := platformID(pl)
	out := make([]VirtualSystem, 0, len(r.systems))
	for i := range r.systems {
		entry := &r.systems[i]
		if platformMatches(platformID, entry.PlatformIDs) {
			out = append(out, *entry)
		}
	}
	return out
}

// Media returns virtual media available for this platform.
func (r *Registry) Media(pl platforms.Platform) []VirtualMedia {
	if len(r.media) == 0 {
		return nil
	}
	platformID := platformID(pl)
	out := make([]VirtualMedia, 0, len(r.media))
	for i := range r.media {
		entry := &r.media[i]
		if platformMatches(platformID, entry.PlatformIDs) {
			out = append(out, *entry)
		}
	}
	return out
}

// MediaForSystem returns virtual media for a real Zaparoo system ID.
func (r *Registry) MediaForSystem(pl platforms.Platform, systemID string) []VirtualMedia {
	media := r.Media(pl)
	out := make([]VirtualMedia, 0, len(media))
	for i := range media {
		if media[i].SystemID == systemID {
			out = append(out, media[i])
		}
	}
	return out
}

// Launchers returns scheme launchers that execute code-defined launchables.
func (r *Registry) Launchers(pl platforms.Platform) []platforms.Launcher {
	if len(r.systems) == 0 && len(r.media) == 0 {
		return nil
	}
	platformID := platformID(pl)
	launchers := make([]platforms.Launcher, 0, len(r.systems)+len(r.media))
	for i := range r.systems {
		entry := &r.systems[i]
		if platformMatches(platformID, entry.PlatformIDs) {
			launchers = append(launchers, systemLauncher(pl, entry))
		}
	}
	for i := range r.media {
		entry := &r.media[i]
		if platformMatches(platformID, entry.PlatformIDs) {
			launchers = append(launchers, mediaLauncher(pl, entry))
		}
	}
	return launchers
}

func systemLauncher(pl platforms.Platform, entry *VirtualSystem) platforms.Launcher {
	return platforms.Launcher{
		ID:      launcherID(entry.ID),
		Schemes: []string{Scheme},
		Test:    pathMatchesID(entry.ID),
		Launch:  launchWith(pl, entry.ID, entry.Launch),
	}
}

func mediaLauncher(pl platforms.Platform, entry *VirtualMedia) platforms.Launcher {
	return platforms.Launcher{
		ID:       launcherID(entry.ID),
		SystemID: entry.SystemID,
		Schemes:  []string{Scheme},
		Test:     pathMatchesID(entry.ID),
		Launch:   launchWith(pl, entry.ID, entry.Launch),
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
	pl platforms.Platform,
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
		return launch(cfg, pl, path, opts)
	}
}

func platformID(pl platforms.Platform) string {
	if pl == nil {
		return ""
	}
	return pl.ID()
}

func platformMatches(platformID string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, id := range allowed {
		if strings.EqualFold(platformID, id) {
			return true
		}
	}
	return false
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
