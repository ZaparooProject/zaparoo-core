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

package main

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedPlatformImports are the platform packages this tool may import: both
// hold data and nothing else.
var allowedPlatformImports = map[string]bool{
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload":    true,
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/batocera/payload": true,
}

// This tool runs on whatever machine assembles the release archives, and Go
// compiles a whole package to use one symbol from it. Importing a platform
// package therefore drags in the readers stack and its libnfc pkg-config
// lookup, which that machine has no reason to satisfy — and the break only
// shows up during a release, because nothing else runs this tool.
func TestNoPlatformPackageImports(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading this package's directory: %v", err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		for _, spec := range file.Imports {
			path, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("reading an import path in %s: %v", name, unquoteErr)
			}
			if !strings.Contains(path, "/pkg/platforms/") || allowedPlatformImports[path] {
				continue
			}
			t.Errorf("%s imports %s: a platform package compiles the readers stack, "+
				"which needs libnfc headers the archive machine does not have. "+
				"Move whatever is needed into a package that holds only data.", name, path)
		}
	}
}
