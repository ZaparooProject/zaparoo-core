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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/catalog"
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

// Generate creates an MGL document for a core and optional media path.
// Override is trusted MGL fragment content produced by a platform hook.
func Generate(core *catalog.Core, rbfPath, mediaPath, override string) (string, error) {
	if core == nil {
		return "", errors.New("no core supplied for MGL generation")
	}

	result := fmt.Sprintf("<mistergamedescription>\n\t<rbf>%s</rbf>\n", rbfPath)

	if core.SetName != "" {
		sameDir := ""
		if core.SetNameSameDir {
			sameDir = " same_dir=\"1\""
		}
		result += fmt.Sprintf("\t<setname%s>%s</setname>\n", sameDir, escapeAttribute(core.SetName))
	}

	switch {
	case mediaPath == "":
		result += "</mistergamedescription>"
		return result, nil
	case override != "":
		result += override
		result += "</mistergamedescription>"
		return result, nil
	}

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

	result += "</mistergamedescription>"
	return result, nil
}
