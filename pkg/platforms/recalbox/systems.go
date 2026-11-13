//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package recalbox

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/spf13/afero"
)

const (
	// templateSystemList ships with the OS; userSystemList holds the user's
	// overrides, matched to template entries by uuid.
	templateSystemList = "/recalbox/share_init/system/.emulationstation/systemlist.xml"
	userSystemList     = "/recalbox/share/system/.emulationstation/systemlist.xml"

	rootPlaceholder     = "%ROOT%"
	favoritesSystemName = "favorites"
	maxSystemListSize   = 16 << 20
	portsLauncherID     = "Ports"
)

// recalboxSystems maps Recalbox system names that the shared ES map lacks or
// names differently. Ports each get their own system and folder under ports/.
//
//nolint:gochecknoglobals // Package-level configuration
var recalboxSystems = map[string]esde.SystemInfo{
	"64dd":           {SystemID: systemdefs.SystemNintendo64, LauncherID: "64DD"},
	"amiga600":       {SystemID: systemdefs.SystemAmiga, LauncherID: "Amiga600"},
	"bbcmicro":       {SystemID: systemdefs.SystemBBCMicro},
	"bk":             {SystemID: systemdefs.SystemBK0011M},
	"cassettevision": {SystemID: systemdefs.SystemCassetteVision},
	"dragon":         {SystemID: systemdefs.SystemDragon32},
	"exl100":         {SystemID: systemdefs.SystemEXL100},
	"fba":            {SystemID: systemdefs.SystemArcade, LauncherID: "FBA"},
	"naomigd":        {SystemID: systemdefs.SystemNAOMI, LauncherID: "NAOMIGD"},
	"p2000t":         {SystemID: systemdefs.SystemP2000T},
	"palm":           {SystemID: systemdefs.SystemPalmOS},
	"pcv2":           {SystemID: systemdefs.SystemPocketChallengeV2},
	"segacd":         {SystemID: systemdefs.SystemMegaCD},
	"stv":            {SystemID: systemdefs.SystemSegaSTV},
	"tamagotchi":     {SystemID: systemdefs.SystemTamagotchi},
	"ti994a":         {SystemID: systemdefs.SystemTI994A},
	"trs80coco":      {SystemID: systemdefs.SystemCoCo2},
	"vg5000":         {SystemID: systemdefs.SystemVG5000},
	"vic20":          {SystemID: systemdefs.SystemVIC20},
	"zmachine":       {SystemID: systemdefs.SystemZMachine},

	"2048":                  {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"caesar3":               {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"dinothawr":             {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"dungeoncrawlstonesoup": {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"flashback":             {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"minecraft":             {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"outrun":                {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"princeofpersia":        {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"rickdangerous":         {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"startrekvoyager":       {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"themehospital":         {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"thepowdertoy":          {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"tombraider":            {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"vvvvvv":                {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
	"wolfenstein3d":         {SystemID: systemdefs.SystemPC, LauncherID: portsLauncherID},
}

type rbDescriptor struct {
	Path       string `xml:"path,attr"`
	Extensions string `xml:"extensions,attr"`
}

type rbSystem struct {
	UUID       string       `xml:"uuid,attr"`
	Name       string       `xml:"name,attr"`
	FullName   string       `xml:"fullname,attr"`
	Descriptor rbDescriptor `xml:"descriptor"`
}

type rbSystemList struct {
	Systems []rbSystem `xml:"system"`
}

// folder is the system's ROM folder: relative to each ROM root when the
// descriptor uses %ROOT%, absolute otherwise, or "" when unusable.
func (s *rbSystem) folder() string {
	p := strings.TrimSpace(s.Descriptor.Path)
	if rest, ok := strings.CutPrefix(p, rootPlaceholder); ok {
		rest = strings.TrimLeft(rest, "/")
		if rest == "" {
			return ""
		}
		clean := filepath.Clean(rest)
		if clean == ".." || strings.HasPrefix(clean, "../") {
			return ""
		}
		return clean
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return ""
}

// extensions returns the descriptor's extensions, lowercased with a leading
// dot, falling back to the mapping's list.
func (s *rbSystem) extensions(info esde.SystemInfo) []string {
	fields := strings.FieldsFunc(s.Descriptor.Extensions, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	})
	exts := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ToLower(f)
		if !strings.HasPrefix(f, ".") || len(f) < 2 {
			continue
		}
		exts = append(exts, f)
	}
	if len(exts) == 0 {
		return info.Extensions
	}
	return exts
}

// lookupSystem maps a Recalbox system name to Zaparoo.
func lookupSystem(name string) (esde.SystemInfo, bool) {
	name = strings.ToLower(name)
	if info, ok := recalboxSystems[name]; ok {
		if len(info.Extensions) == 0 {
			if shared, sharedOK := esde.LookupByFolderName(name); sharedOK {
				info.Extensions = shared.Extensions
			}
		}
		return info, true
	}
	return esde.LookupByFolderName(name)
}

func readSystemList(fsys afero.Fs, path string) ([]rbSystem, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxSystemListSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxSystemListSize {
		return nil, fmt.Errorf("%s is too large", path)
	}
	var list rbSystemList
	if err := xml.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return list.Systems, nil
}

// loadSystems reads the template system list and applies the user's list on
// top, the same way EmulationStation merges them.
func loadSystems(fsys afero.Fs) ([]rbSystem, error) {
	base, err := readSystemList(fsys, templateSystemList)
	if err != nil {
		return nil, err
	}
	user, err := readSystemList(fsys, userSystemList)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	result := make([]rbSystem, 0, len(base)+len(user))
	byUUID := make(map[string]int, len(base)+len(user))
	for _, list := range [][]rbSystem{base, user} {
		for i := range list {
			sys := list[i]
			if sys.Name == "" || sys.UUID == "" || sys.Name == favoritesSystemName {
				continue
			}
			if at, ok := byUUID[sys.UUID]; ok {
				result[at] = sys
				continue
			}
			byUUID[sys.UUID] = len(result)
			result = append(result, sys)
		}
	}
	return result, nil
}

// systemForPath finds the system whose ROM folder holds path, preferring the
// deepest folder so ports/<game> wins over a broader match.
func systemForPath(systems []rbSystem, path string, roots []string) (*rbSystem, error) {
	var best *rbSystem
	bestLen := -1
	for i := range systems {
		sys := &systems[i]
		folder := sys.folder()
		if folder == "" {
			continue
		}
		dirs := []string{folder}
		if !filepath.IsAbs(folder) {
			dirs = dirs[:0]
			for _, root := range roots {
				dirs = append(dirs, filepath.Join(root, folder))
			}
		}
		for _, dir := range dirs {
			if strings.HasPrefix(path, dir+string(filepath.Separator)) && len(dir) > bestLen {
				best = sys
				bestLen = len(dir)
			}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no Recalbox system holds %s", path)
	}
	return best, nil
}
