// Package vdfbinary parses Valve's binary VDF format.
//
// This is a vendored and modified version of github.com/TimDeve/valve-vdf-binary
// Licensed under MIT. See LICENSE file in this directory.
package vdfbinary

import (
	"errors"
	"io"
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

	shortcuts := make([]Shortcut, len(shortcutsMap))

	for i := range shortcuts {
		key := strconv.Itoa(i)

		s, ok := shortcutsMap[key]
		if !ok {
			return []Shortcut{}, errors.New("vdf that should be an array does not have the corresponding index")
		}

		appID, ok := s.GetUint("appid")
		if !ok {
			return []Shortcut{}, errors.New("could not get key 'appid' for one of the shortcuts")
		}

		appName, ok := s.GetString("AppName")
		if !ok {
			return []Shortcut{}, errors.New("could not get key 'AppName' for one of the shortcuts")
		}

		exe, ok := s.GetString("Exe")
		if !ok {
			return []Shortcut{}, errors.New("could not get key 'Exe' for one of the shortcuts")
		}

		startDir, ok := s.GetString("StartDir")
		if !ok {
			return []Shortcut{}, errors.New("could not get key 'StartDir' for one of the shortcuts")
		}

		// icon is optional - some shortcuts don't have an icon set
		icon, _ := s.GetString("icon")

		// IsHidden is optional - defaults to false if not present
		isHidden, _ := s.GetBool("IsHidden")

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

		shortcuts[i] = Shortcut{
			AppID:    appID,
			AppName:  appName,
			Exe:      exe,
			Icon:     icon,
			IsHidden: isHidden,
			StartDir: startDir,
			Tags:     tags,
		}
	}

	return shortcuts, nil
}
