// Package vdfbinary parses Valve's binary VDF format.
//
// This is a vendored and modified version of github.com/TimDeve/valve-vdf-binary
// Licensed under MIT. See LICENSE file in this directory.
package vdfbinary

import (
	"errors"
	"io"
	"sort"
	"strconv"
)

// Shortcut represents a Steam non-Steam game shortcut.
// Fields are ordered for optimal memory alignment.
type Shortcut struct {
	AppName  string
	Exe      string
	Icon     string
	StartDir string
	Tags     []string
	AppID    uint32
	IsHidden bool
}

// ParseShortcuts parses Steam's shortcuts.vdf binary format.
// This is a modified version that treats tags, icon, and IsHidden as optional
// fields to handle shortcuts created by third-party tools like EmuDeck/Lutris.
func ParseShortcuts(buf io.Reader) ([]Shortcut, error) {
	vdf, err := Parse(buf)
	if err != nil {
		return []Shortcut{}, err
	}

	shortcutsMap, ok := vdf.GetMap("shortcuts")
	if !ok {
		return []Shortcut{}, errors.New("could not find 'shortcuts' in parsed vdf")
	}

	// Collect the keys actually present and sort them by numeric value, rather
	// than assuming a contiguous 0..N-1 sequence — third-party tools (EmuDeck,
	// Lutris) can leave gaps or non-numeric keys. The original key strings are
	// preserved for lookup so non-canonical numeric keys (e.g. "01") still match.
	keys := make([]string, 0, len(shortcutsMap))
	for k := range shortcutsMap {
		if _, err := strconv.Atoi(k); err != nil {
			continue // skip non-numeric keys defensively
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.Atoi(keys[i])
		b, _ := strconv.Atoi(keys[j])
		return a < b
	})

	shortcuts := make([]Shortcut, 0, len(keys))

	for _, key := range keys {
		s := shortcutsMap[key]

		appID, ok := s.GetUint("appid")
		if !ok {
			return []Shortcut{}, errors.New("could not get key 'appid' for one of the shortcuts")
		}

		appName, ok := s.GetString("appname")
		if !ok {
			return []Shortcut{}, errors.New("could not get key 'appname' for one of the shortcuts")
		}

		exe, ok := s.GetString("exe")
		if !ok {
			return []Shortcut{}, errors.New("could not get key 'exe' for one of the shortcuts")
		}

		startDir, ok := s.GetString("startdir")
		if !ok {
			return []Shortcut{}, errors.New("could not get key 'startdir' for one of the shortcuts")
		}

		// icon is optional - some shortcuts don't have an icon set
		icon, _ := s.GetString("icon")

		// ishidden is optional - defaults to false if not present
		isHidden, _ := s.GetBool("ishidden")

		// tags is optional - shortcuts from EmuDeck/Lutris may not have tags
		var tags []string
		if tagsMap, ok := s.GetMap("tags"); ok {
			for j := range len(tagsMap) {
				tagKey := strconv.Itoa(j)
				t, ok := tagsMap[tagKey]
				if !ok {
					break
				}
				ts, ok := t.AsString()
				if !ok {
					continue
				}
				tags = append(tags, ts)
			}
		}

		shortcuts = append(shortcuts, Shortcut{
			AppID:    appID,
			AppName:  appName,
			Exe:      exe,
			Icon:     icon,
			IsHidden: isHidden,
			StartDir: startDir,
			Tags:     tags,
		})
	}

	return shortcuts, nil
}
