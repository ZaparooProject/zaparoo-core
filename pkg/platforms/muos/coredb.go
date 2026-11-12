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

package muos

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	runtimePickles = iota
	runtimeRetroArch
	runtimeExternal
	runtimeCount
)

//nolint:gochecknoglobals // Static lookup table mirroring the frontend's runtime order.
var runtimeNames = [runtimeCount]string{"pickles", "retroarch", "external"}

//nolint:gochecknoglobals // Static lookup table mirroring the frontend's tag prefixes.
var runtimeTagPrefix = [runtimeCount]string{"mu-", "", "ext-"}

// manifestCore is one core entry in a system manifest.
type manifestCore struct {
	Require  map[string][]string `json:"require"`
	ID       string              `json:"-"`
	Core     string              `json:"core"`
	Governor string              `json:"governor"`
	Control  string              `json:"control"`
	Extra    flexInt             `json:"extra"`
}

// orderedCores keeps manifest cores in file order, which decides the
// fallback default core.
type orderedCores []manifestCore

func (o *orderedCores) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("read cores: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return errors.New("cores is not an object")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("read core id: %w", err)
		}
		id, ok := keyTok.(string)
		if !ok {
			return errors.New("core id is not a string")
		}
		var entry manifestCore
		if err := dec.Decode(&entry); err != nil {
			return fmt.Errorf("decode core %q: %w", id, err)
		}
		entry.ID = id
		*o = append(*o, entry)
	}
	return nil
}

// flexInt accepts a JSON number or a numeric string, as muOS reads these
// fields leniently.
type flexInt int

func (f *flexInt) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*f = 0
		return nil //nolint:nilerr // Non-numeric values read as 0 in the frontend.
	}
	*f = flexInt(n)
	return nil
}

// systemManifest is one system definition from the core manifest tree.
type systemManifest struct {
	Name      string       `json:"-"`
	Catalogue string       `json:"catalogue"`
	Default   string       `json:"default"`
	Runtime   string       `json:"runtime"`
	Governor  string       `json:"governor"`
	Control   string       `json:"control"`
	Friendly  []string     `json:"friendly"`
	Libretro  orderedCores `json:"libretro"`
	External  orderedCores `json:"external"`
	Lookup    flexInt      `json:"lookup"`
}

// coreChoice is a resolved core for a system.
type coreChoice struct {
	ID       string
	Core     string
	Governor string
	Control  string
	Runtime  int
}

func (c coreChoice) tag() string {
	return runtimeTagPrefix[c.Runtime] + c.ID
}

// coreDB holds the merged core manifests and the folder assignment index.
type coreDB struct {
	systems    map[string]*systemManifest
	assign     map[string]string
	friendly   map[string]string
	board      string
	arch       string
	extraShown bool
}

// muosArch returns the architecture name used by manifest requirements.
func muosArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	case "arm":
		return "armhf"
	case "amd64":
		return "x86_64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}

// loadCoreDB reads the manifests in the same order as the frontend, where
// the first definition of a system wins.
func loadCoreDB(fs afero.Fs, arch string) *coreDB {
	db := &coreDB{
		systems:    make(map[string]*systemManifest),
		assign:     make(map[string]string),
		friendly:   make(map[string]string),
		board:      readFirstLine(fs, boardNameFile),
		arch:       arch,
		extraShown: readFirstLine(fs, extraCoresFile) == "1",
	}

	for _, root := range manifestRoots {
		namespaces, err := afero.ReadDir(fs, root)
		if err != nil {
			continue
		}
		for _, ns := range namespaces {
			if !ns.IsDir() || strings.HasPrefix(ns.Name(), ".") {
				continue
			}
			dir := filepath.Join(root, ns.Name())
			files, err := afero.ReadDir(fs, dir)
			if err != nil {
				continue
			}
			for _, f := range files {
				name := f.Name()
				if f.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
					continue
				}
				id := strings.TrimSuffix(name, ".json")
				if id == "" {
					continue
				}
				if _, seen := db.systems[id]; seen {
					continue
				}
				m, err := readManifest(fs, filepath.Join(dir, name))
				if err != nil {
					log.Debug().Err(err).Str("file", filepath.Join(dir, name)).Msg("skipping muOS manifest")
					continue
				}
				m.Name = id
				db.systems[id] = m
			}
		}
	}

	if len(db.systems) == 0 {
		db.loadCoreJSON(fs)
	}

	db.buildFriendly()
	db.loadAssign(fs)
	return db
}

// loadCoreJSON reads the single-file core manifest, keyed by system name.
// Like the frontend's scripts, it uses the first file that parses as a JSON
// object and does not merge the others.
func (db *coreDB) loadCoreJSON(fs afero.Fs) {
	for _, path := range coreJSONFiles {
		data, err := readLimited(fs, path, maxManifestSize)
		if err != nil {
			continue
		}
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(data, &entries); err != nil || entries == nil {
			log.Debug().Err(err).Str("file", path).Msg("skipping muOS core manifest")
			continue
		}
		for id, raw := range entries {
			if id == "" {
				continue
			}
			var m systemManifest
			if err := json.Unmarshal(raw, &m); err != nil {
				log.Debug().Err(err).Str("file", path).Str("system", id).Msg("skipping muOS core manifest entry")
				continue
			}
			m.Name = id
			db.systems[id] = &m
		}
		return
	}
}

func readManifest(fs afero.Fs, path string) (*systemManifest, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestSize))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m systemManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// buildFriendly mirrors how muOS builds assign.json: unique sorted
// "key<TAB>system" pairs where the first pair for a key wins.
func (db *coreDB) buildFriendly() {
	pairs := make([]string, 0, len(db.systems)*4)
	for id, m := range db.systems {
		for _, key := range m.Friendly {
			if key == "" {
				continue
			}
			pairs = append(pairs, key+"\t"+id)
		}
	}
	slices.Sort(pairs)
	pairs = slices.Compact(pairs)
	for _, pair := range pairs {
		key, system, _ := strings.Cut(pair, "\t")
		if _, ok := db.friendly[key]; !ok {
			db.friendly[key] = system
		}
	}
}

func (db *coreDB) loadAssign(fs afero.Fs) {
	data, err := readLimited(fs, assignFile, maxManifestSize)
	if err != nil {
		return
	}
	var assign map[string]string
	if err := json.Unmarshal(data, &assign); err != nil {
		log.Debug().Err(err).Msg("ignoring unreadable muOS assign index")
		return
	}
	db.assign = assign
}

// assignKey normalises a folder name the way the frontend does before
// looking it up in assign.json.
func assignKey(folder string) string {
	key := strings.ToLower(folder)
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '_', '+':
			return -1
		default:
			return r
		}
	}, key)
}

// systemForFolder returns the system a folder name is automatically
// assigned to. The live assign index wins, then the manifests.
func (db *coreDB) systemForFolder(folder string) (string, bool) {
	key := assignKey(folder)
	if key == "" {
		return "", false
	}
	if system, ok := db.assign[key]; ok && system != "" {
		return system, true
	}
	if system, ok := db.friendly[key]; ok {
		return system, true
	}
	return "", false
}

func (db *coreDB) requirementsMet(c *manifestCore) bool {
	for key, values := range c.Require {
		var fact string
		switch key {
		case "device":
			fact = db.board
		case "arch":
			fact = db.arch
		default:
			continue
		}
		if !factInList(fact, values) {
			return false
		}
	}
	return true
}

func factInList(fact string, values []string) bool {
	if fact == "" {
		return false
	}
	for _, v := range values {
		if strings.EqualFold(v, fact) {
			return true
		}
		if strings.EqualFold(fact, "x86_64") && strings.EqualFold(v, "x64") {
			return true
		}
		if strings.EqualFold(fact, "armhf") && strings.EqualFold(v, "arm32") {
			return true
		}
	}
	return false
}

func preferredRuntime(m *systemManifest) int {
	for r, name := range runtimeNames {
		if strings.EqualFold(m.Runtime, name) {
			return r
		}
	}
	return runtimePickles
}

func runtimeInOrder(preferred, step int) int {
	if step == 0 {
		return preferred
	}
	idx := step - 1
	if idx < preferred {
		return idx
	}
	return idx + 1
}

func (m *systemManifest) group(r int) orderedCores {
	if r == runtimeExternal {
		return m.External
	}
	return m.Libretro
}

// defaultCore picks a system's default core the way the frontend does: the
// named default in runtime preference order, then the first usable core.
func (db *coreDB) defaultCore(system string) (coreChoice, bool) {
	m, ok := db.systems[system]
	if !ok {
		return coreChoice{}, false
	}
	preferred := preferredRuntime(m)

	if m.Default != "" {
		for step := range runtimeCount {
			r := runtimeInOrder(preferred, step)
			for i := range m.group(r) {
				c := &m.group(r)[i]
				if c.ID == m.Default && db.requirementsMet(c) {
					return choiceFrom(c, r), true
				}
			}
		}
	}

	for step := range runtimeCount {
		r := runtimeInOrder(preferred, step)
		for i := range m.group(r) {
			c := &m.group(r)[i]
			if !db.requirementsMet(c) {
				continue
			}
			if !db.extraShown && c.Extra != 0 {
				continue
			}
			return choiceFrom(c, r), true
		}
	}
	return coreChoice{}, false
}

func choiceFrom(c *manifestCore, r int) coreChoice {
	return coreChoice{ID: c.ID, Core: c.Core, Governor: c.Governor, Control: c.Control, Runtime: r}
}

func (db *coreDB) catalogue(system string) string {
	if m, ok := db.systems[system]; ok && m.Catalogue != "" {
		return m.Catalogue
	}
	return system
}

func (db *coreDB) lookup(system string) int {
	if m, ok := db.systems[system]; ok {
		return int(m.Lookup)
	}
	return 0
}

func (db *coreDB) systemGovernor(system string) string {
	if m, ok := db.systems[system]; ok {
		return m.Governor
	}
	return ""
}

func (db *coreDB) systemControl(system string) string {
	if m, ok := db.systems[system]; ok {
		return m.Control
	}
	return ""
}

//nolint:gochecknoglobals // Static lookup table mirroring the frontend's CPU profiles.
var cpuProfiles = map[string]string{
	"automatic":        "automatic",
	"powersave":        "powersave",
	"efficient":        "efficient",
	"balanced":         "balanced",
	"responsive":       "responsive",
	"turbo":            "turbo",
	"extreme":          "extreme",
	"performance":      "extreme",
	"low_power":        "powersave",
	"schedutil":        "automatic",
	"ondemand":         "automatic",
	"conservative":     "automatic",
	"interactive":      "automatic",
	"userspace":        "automatic",
	"high_performance": "turbo",
}

// cpuProfileValue converts a manifest governor into the value the
// frontend stores for an automatic assignment.
func cpuProfileValue(name string) string {
	value := strings.ToLower(strings.TrimSpace(name))
	if value == "" || value == "default" {
		return "default"
	}
	if profile, ok := cpuProfiles[value]; ok {
		return profile
	}
	return "automatic"
}
