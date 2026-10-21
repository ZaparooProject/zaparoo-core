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

// Package mgl generates MiSTer MGL documents without platform runtime dependencies.
package mgl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/mister/catalog"
)

func escapeAttribute(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return replacer.Replace(value)
}

// Option sets one OSD option by the labels the core shows for it. Main applies
// it after the core's settings load, so an older Main that predates the tag
// skips it and the launch carries on without the option.
type Option struct {
	Name  string
	Value string
	// Page names the OSD sub-page holding the option, for a label that is not
	// unique across pages.
	Page string
	// Default applies the option only when the core has no saved settings yet,
	// so a choice the user already made wins.
	Default bool
}

// Setting is an element added to a generated MGL after the media: an OSD option
// or a reset.
type Setting interface {
	element() string
}

// Reset holds the core's reset button after the media is loaded, for a machine
// that must start the media from a fresh boot.
type Reset struct {
	// Delay is the seconds to wait before the reset, Hold how long it is held.
	Delay int
	Hold  int
}

func (r Reset) element() string {
	return fmt.Sprintf("\t<reset delay=\"%d\" hold=\"%d\"/>\n", r.Delay, r.Hold)
}

func (o Option) element() string {
	var b strings.Builder
	b.WriteString("\t<option")
	fmt.Fprintf(&b, " name=\"%s\" value=\"%s\"", escapeAttribute(o.Name), escapeAttribute(o.Value))
	if o.Page != "" {
		fmt.Fprintf(&b, " page=\"%s\"", escapeAttribute(o.Page))
	}
	if o.Default {
		b.WriteString(` default="1"`)
	}
	b.WriteString("/>\n")
	return b.String()
}

func optionElements(settings []Setting) string {
	var b strings.Builder
	for _, setting := range settings {
		if o, ok := setting.(Option); ok && (o.Name == "" || o.Value == "") {
			continue
		}
		b.WriteString(setting.element())
	}
	return b.String()
}

// Generate creates an MGL document for a core and optional media path.
// Override is trusted MGL fragment content produced by a platform hook.
func Generate(core *catalog.Core, rbfPath, mediaPath, override string, settings ...Setting) (string, error) {
	if core == nil {
		return "", errors.New("no core supplied for MGL generation")
	}

	result := fmt.Sprintf("<mistergamedescription>\n\t<rbf>%s</rbf>\n", escapeAttribute(rbfPath))

	if core.SetName != "" {
		sameDir := ""
		if core.SetNameSameDir {
			sameDir = " same_dir=\"1\""
		}
		result += fmt.Sprintf("\t<setname%s>%s</setname>\n", sameDir, escapeAttribute(core.SetName))
	}

	switch {
	case mediaPath == "":
		result += optionElements(settings)
		result += "</mistergamedescription>"
		return result, nil
	case override != "":
		result += override
		result += optionElements(settings)
		result += "</mistergamedescription>"
		return result, nil
	}

	// Media that launches directly comes back as catalog.ErrLaunchesDirectly,
	// so reaching Generate with it means a caller picked the wrong launch path.
	params, err := catalog.PathToMGLDef(core, mediaPath)
	if err != nil {
		return "", fmt.Errorf("failed to get MGL definition: %w", err)
	}

	result += fmt.Sprintf(
		"\t<file delay=\"%d\" type=%q index=\"%d\" path=\"../../../../..%s\"/>\n",
		params.Delay, params.Method, params.Index, escapeAttribute(mediaPath),
	)

	if params.ResetDelay > 0 {
		result += fmt.Sprintf(
			"\t<reset delay=\"%d\" hold=\"%d\"/>\n",
			params.ResetDelay,
			params.ResetHold,
		)
	}

	result += optionElements(settings)
	result += "</mistergamedescription>"
	return result, nil
}
