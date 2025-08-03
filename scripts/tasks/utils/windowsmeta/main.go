// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-only
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
	"flag"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"
)

type TemplateData struct {
	Version string
	Year    int
}

func main() {
	version := flag.String("version", "", "Version number")
	flag.Parse()

	if *version == "" {
		_, _ = fmt.Fprint(os.Stderr, "Error: version is required\n")
		os.Exit(1)
	}

	if strings.Contains(*version, "-") {
		*version = "0.0.0"
	}

	data := TemplateData{
		Version: *version,
		Year:    time.Now().Year(),
	}

	tmpl, err := template.ParseFiles("cmd/windows/winres/winres.json.tmpl")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error parsing template: %v\n", err)
		os.Exit(1)
	}

	if err := generateWinresFile(tmpl, data); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error generating winres file: %v\n", err)
		os.Exit(1)
	}
}

func generateWinresFile(tmpl *template.Template, data TemplateData) error {
	outFile, err := os.Create("cmd/windows/winres/winres.json")
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}
	defer func(outFile *os.File) {
		_ = outFile.Close()
	}(outFile)

	if err := tmpl.Execute(outFile, data); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	return nil
}
