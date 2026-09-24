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

// Credits generates the contributor list and third-party software notices
// embedded in pkg/assets/credits. By default it checks the committed
// third-party data against the current dependencies and fails when it is
// stale; run it with -check=false (task credits) to regenerate.
//
// The component list is every Go module linked into any shipped binary,
// found with "go list -deps" for each release platform and tag set, plus
// the hand-maintained entries in extras.go for code and data that do not
// come from Go modules. License, copying, notice and patent files are
// copied verbatim from each module directory a linked package lives under.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets/credits"
	"github.com/rs/zerolog/log"
)

const (
	mainModule  = "github.com/ZaparooProject/zaparoo-core/v2"
	outputDir   = "pkg/assets/credits"
	readmePath  = "README.md"
	listTimeout = 5 * time.Minute
)

// releaseTags are the build tags every release build uses (Taskfile.dist.yml
// build task, plus nopkgconfig from the zigcc builds).
const releaseTags = "netgo,osusergo,sqlite_omit_load_extension,validator_novalidatefn," +
	"expr_static_methods,nopkgconfig"

// target is one platform and tag set to collect linked packages for.
type target struct {
	goos   string
	goarch string
	tags   string
	cmds   []string
}

func main() {
	check := flag.Bool("check", true, "verify the third-party data without rewriting it")
	flag.Parse()
	root, err := os.Getwd()
	if err != nil {
		log.Fatal().Err(err).Msg("resolve Core root")
	}
	if err := run(root, filepath.Join(root, outputDir), *check); err != nil {
		log.Fatal().Err(err).Msg("credits generation")
	}
}

// run generates the credits data for the Core checkout at root, then either
// checks it against the files in outDir or writes it there.
func run(root, outDir string, check bool) error {
	// Module directories from go list are absolute; so must the root be for
	// modules inside the checkout to be recognized.
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve Core root: %w", err)
	}
	targets, err := releaseTargets(root)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	defer cancel()
	modules, err := linkedModules(ctx, root, targets)
	if err != nil {
		return err
	}
	bundle, err := buildBundle(root, modules)
	if err != nil {
		return err
	}
	encoded, err := credits.EncodeBundle(bundle)
	if err != nil {
		return fmt.Errorf("encoding credits: %w", err)
	}
	componentsPath := filepath.Join(outDir, credits.ComponentsFile)

	if check {
		return checkBundle(componentsPath, bundle)
	}

	contributors, err := readmeContributors(filepath.Join(root, readmePath))
	if err != nil {
		return err
	}
	contributorsJSON, err := json.MarshalIndent(contributors, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding contributors: %w", err)
	}
	if err := os.WriteFile(componentsPath, encoded, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", componentsPath, err)
	}
	contributorsPath := filepath.Join(outDir, credits.ContributorsFile)
	if err := os.WriteFile(contributorsPath, append(contributorsJSON, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", contributorsPath, err)
	}
	log.Info().Int("components", len(bundle.Components)).Int("texts", len(bundle.Texts)).
		Int("contributors", len(contributors)).Msg("credits written")
	return nil
}

// checkBundle compares the committed third-party data with a fresh one. The
// comparison is on the decoded data, so a different gzip implementation does
// not count as a change. Contributors are not checked: the README list they
// come from is updated on main by a bot, and they refresh with "task credits".
func checkBundle(path string, fresh *credits.Bundle) error {
	data, err := os.ReadFile(path) //nolint:gosec // fixed repository path
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	committed, err := credits.DecodeBundle(data)
	if err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	committedJSON, err := json.Marshal(committed)
	if err != nil {
		return fmt.Errorf("encoding committed credits: %w", err)
	}
	freshJSON, err := json.Marshal(fresh)
	if err != nil {
		return fmt.Errorf("encoding current credits: %w", err)
	}
	if !bytes.Equal(committedJSON, freshJSON) {
		return fmt.Errorf("third-party credits are out of date (%s); run \"task credits\" and commit the result: %s",
			path, describeDiff(committed, fresh))
	}
	return nil
}

// describeDiff names the components that were added, removed or changed.
func describeDiff(committed, fresh *credits.Bundle) string {
	summarize := func(bundle *credits.Bundle) map[string]string {
		out := make(map[string]string, len(bundle.Components))
		for _, component := range bundle.Components {
			data, _ := json.Marshal(component)
			for _, file := range component.Files {
				data = append(data, bundle.Texts[file.Text]...)
			}
			out[component.Name] = string(data)
		}
		return out
	}
	before, after := summarize(committed), summarize(fresh)
	var changes []string
	for name, value := range after {
		old, ok := before[name]
		switch {
		case !ok:
			changes = append(changes, "added "+name)
		case old != value:
			changes = append(changes, "changed "+name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			changes = append(changes, "removed "+name)
		}
	}
	sort.Strings(changes)
	if len(changes) == 0 {
		return "license texts changed"
	}
	return strings.Join(changes, ", ")
}

// releaseTargets lists every platform a release is built for, with the
// commands built for it. Windows and macOS each have one command; every
// other command is a Linux build. MiSTer builds also embed the arcade
// database.
func releaseTargets(root string) ([]target, error) {
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		return nil, fmt.Errorf("listing commands: %w", err)
	}
	var linux []string
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "windows" || entry.Name() == "mac" {
			continue
		}
		linux = append(linux, "./cmd/"+entry.Name())
	}
	mister := []string{"./cmd/mister", "./cmd/mistex"}
	return []target{
		{goos: "linux", goarch: "amd64", tags: releaseTags, cmds: linux},
		{goos: "linux", goarch: "arm64", tags: releaseTags, cmds: linux},
		{goos: "linux", goarch: "arm", tags: releaseTags, cmds: linux},
		{goos: "linux", goarch: "arm", tags: releaseTags + ",embed_arcadedb", cmds: mister},
		{goos: "windows", goarch: "amd64", tags: releaseTags, cmds: []string{"./cmd/windows"}},
		{goos: "windows", goarch: "arm64", tags: releaseTags, cmds: []string{"./cmd/windows"}},
		{goos: "windows", goarch: "386", tags: releaseTags, cmds: []string{"./cmd/windows"}},
		{goos: "darwin", goarch: "amd64", tags: releaseTags, cmds: []string{"./cmd/mac"}},
		{goos: "darwin", goarch: "arm64", tags: releaseTags, cmds: []string{"./cmd/mac"}},
	}, nil
}

// listedModule is the part of "go list" module output the generator uses.
type listedModule struct {
	Replace *listedModule
	Path    string
	Version string
	Dir     string
	Main    bool
}

type listedPackage struct {
	Module   *listedModule
	Dir      string
	Standard bool
}

// linkedModule is a third-party module with the directories of its packages
// that end up in a binary.
type linkedModule struct {
	packageDirs map[string]bool
	path        string
	version     string
	dir         string
	replacedBy  string
}

// linkedModules runs "go list -deps" for every target and gathers the
// third-party modules whose packages are linked. Modules that live inside
// this repository (the main module and local replacements) are skipped.
func linkedModules(ctx context.Context, root string, targets []target) (map[string]*linkedModule, error) {
	modules := make(map[string]*linkedModule)
	for _, t := range targets {
		args := append([]string{"list", "-deps", "-json=Dir,Standard,Module", "-tags=" + t.tags}, t.cmds...)
		cmd := exec.CommandContext(ctx, "go", args...) //nolint:gosec // fixed go list arguments
		cmd.Dir = root
		cmd.Env = append(listEnv(), "GOOS="+t.goos, "GOARCH="+t.goarch, "CGO_ENABLED=1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("go list for %s/%s: %w: %s", t.goos, t.goarch, err, stderr.String())
		}
		decoder := json.NewDecoder(bytes.NewReader(out))
		for {
			var pkg listedPackage
			if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return nil, fmt.Errorf("decoding go list output: %w", err)
			}
			if pkg.Standard || pkg.Module == nil || pkg.Module.Main {
				continue
			}
			mod := pkg.Module
			dir, version, replacedBy := mod.Dir, mod.Version, ""
			if mod.Replace != nil {
				dir, version = mod.Replace.Dir, mod.Replace.Version
				if mod.Replace.Version != "" {
					replacedBy = mod.Replace.Path
				}
			}
			if dir == "" {
				return nil, fmt.Errorf("module %s has no directory; run go mod download", mod.Path)
			}
			if inside(root, dir) {
				continue
			}
			entry, ok := modules[mod.Path]
			if !ok {
				entry = &linkedModule{
					path: mod.Path, version: version, dir: dir, replacedBy: replacedBy,
					packageDirs: make(map[string]bool),
				}
				modules[mod.Path] = entry
			}
			entry.packageDirs[pkg.Dir] = true
		}
	}
	return modules, nil
}

// listEnv is the environment for "go list" without the C toolchain settings
// of whatever build runs the generator. Cross builds point CC at a compiler
// for their own target, which has nothing to do with listing other targets.
func listEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS":
			continue
		default:
			env = append(env, entry)
		}
	}
	return env
}

func inside(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// licenseFilePattern matches the files that carry a license, copyright,
// notice or patent grant.
var licenseFilePattern = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|unlicense|patents|copyright)([-._].*)?$`)

// moduleLicenseFiles returns every license file in the directories between
// each linked package and its module root, keyed by path relative to the
// module root. A license kept next to vendored code inside a module is
// included only when a package under it is linked.
func moduleLicenseFiles(mod *linkedModule) (map[string]string, error) {
	files := make(map[string]string)
	visited := make(map[string]bool)
	for pkgDir := range mod.packageDirs {
		for dir := pkgDir; ; dir = filepath.Dir(dir) {
			if !inside(mod.dir, dir) {
				break
			}
			if !visited[dir] {
				visited[dir] = true
				if err := collectLicenseFiles(mod.dir, dir, files); err != nil {
					return nil, err
				}
			}
			if dir == mod.dir {
				break
			}
		}
	}
	return files, nil
}

func collectLicenseFiles(moduleDir, dir string, files map[string]string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !licenseFilePattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path) //nolint:gosec // module cache path from go list
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		rel, err := filepath.Rel(moduleDir, path)
		if err != nil {
			return fmt.Errorf("locating %s: %w", path, err)
		}
		files[filepath.ToSlash(rel)] = normalizeText(string(data))
	}
	return nil
}

// normalizeText makes line endings consistent so a module packed on Windows
// does not produce a different text.
func normalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimRight(text, " \t\n") + "\n"
}

func textID(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// buildBundle turns the linked modules and the hand-maintained extras into
// the embedded bundle.
func buildBundle(root string, modules map[string]*linkedModule) (*credits.Bundle, error) {
	bundle := &credits.Bundle{Texts: make(map[string]string)}
	addFile := func(name, text string) credits.LicenseFile {
		id := textID(text)
		bundle.Texts[id] = text
		return credits.LicenseFile{Name: name, Text: id}
	}

	var unknown []string
	for _, mod := range modules {
		files, err := moduleLicenseFiles(mod)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("module %s has no license file", mod.path)
		}
		names := make([]string, 0, len(files))
		for name := range files {
			names = append(names, name)
		}
		sort.Strings(names)
		component := credits.Component{
			Name:    mod.path,
			Version: mod.version,
			URL:     "https://pkg.go.dev/" + mod.path,
		}
		if mod.replacedBy != "" {
			component.Note = "Built from the fork " + mod.replacedBy + "."
		}
		labels := map[string]bool{}
		for _, name := range names {
			component.Files = append(component.Files, addFile(name, files[name]))
			for _, label := range classifyFile(name, files[name]) {
				labels[label] = true
			}
		}
		component.License = joinLabels(labels)
		if component.License == "" {
			unknown = append(unknown, mod.path)
		}
		bundle.Components = append(bundle.Components, component)
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("cannot identify the license of %s; teach classifyLicense the text",
			strings.Join(unknown, ", "))
	}

	extraComponents, err := extras(root, modules)
	if err != nil {
		return nil, err
	}
	for i := range extraComponents {
		extra := &extraComponents[i]
		component := extra.component
		for _, file := range extra.files {
			component.Files = append(component.Files, addFile(file.name, normalizeText(file.text)))
		}
		bundle.Components = append(bundle.Components, component)
	}
	credits.SortComponents(bundle.Components)
	return bundle, nil
}

func joinLabels(labels map[string]bool) string {
	list := make([]string, 0, len(labels))
	for label := range labels {
		list = append(list, label)
	}
	sort.Strings(list)
	return strings.Join(list, " AND ")
}

// classifyFile names the licenses in a license file for display. Notice and
// patent files carry no license of their own.
func classifyFile(name, text string) []string {
	base := strings.ToLower(filepath.Base(name))
	if strings.HasPrefix(base, "notice") || strings.HasPrefix(base, "patents") {
		return nil
	}
	return classifyLicense(text)
}

// classifyLicense recognizes the common license texts, including files that
// carry more than one. It only produces the label shown next to a
// component; the full text is always included.
func classifyLicense(text string) []string {
	t := strings.Join(strings.Fields(strings.ToLower(text)), " ")
	has := func(phrases ...string) bool {
		for _, phrase := range phrases {
			if strings.Contains(t, phrase) {
				return true
			}
		}
		return false
	}
	var labels []string
	add := func(label string, ok bool) {
		if ok {
			labels = append(labels, label)
		}
	}
	mpl := has("mozilla public license") && has("version 2.0")
	epl := has("eclipse public license - v 2.0", "eclipse public license v2.0",
		"eclipse public license - version 2.0")
	add("Apache-2.0", has("apache license") && has("version 2.0"))
	add("MPL-2.0", mpl)
	add("EPL-2.0", epl)
	add("EDL-1.0", has("eclipse distribution license - v 1.0"))
	// MPL and EPL name the GNU licenses as secondary licenses, and LGPL
	// builds on the GPL text, so the GNU family is only counted on its own.
	if !mpl && !epl {
		lgpl3 := has("gnu lesser general public license") && has("version 3")
		lgpl21 := has("gnu lesser general public license") && has("version 2.1")
		add("LGPL-3.0", lgpl3)
		add("LGPL-2.1", lgpl21 && !lgpl3)
		add("GPL-3.0", !lgpl3 && !lgpl21 && has("gnu general public license") && has("version 3"))
	}
	add("Unlicense", has("this is free and unencumbered software released into the public domain"))
	add("MIT", has("permission is hereby granted, free of charge") &&
		has("the above copyright notice and this permission notice shall be included"))
	add("ISC", has("permission to use, copy, modify, and/or distribute this software for any purpose",
		"permission to use, copy, modify, and distribute this software for any purpose"))
	if has("redistribution and use in source and binary forms") {
		bsd3 := has("neither the name", "names of its contributors", "name of the copyright holder")
		add("BSD-3-Clause", bsd3)
		add("BSD-2-Clause", !bsd3)
	}
	add("Zlib", has("this software is provided 'as-is', without any express or implied warranty"))
	return labels
}

// contributorPattern matches one person in the README contributors table.
var contributorPattern = regexp.MustCompile(
	`(?s)<a href="https://github\.com/([A-Za-z0-9-]+)">.*?<sub><b>(.*?)</b></sub>`)

// readmeContributors reads the contributors table kept up to date in the
// README by the contributors workflow.
func readmeContributors(path string) ([]credits.Contributor, error) {
	data, err := os.ReadFile(path) //nolint:gosec // fixed repository path
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return parseContributors(string(data))
}

func parseContributors(readme string) ([]credits.Contributor, error) {
	const startMarker, endMarker = "<!-- readme: contributors -start -->", "<!-- readme: contributors -end -->"
	start := strings.Index(readme, startMarker)
	end := strings.Index(readme, endMarker)
	if start < 0 || end < start {
		return nil, errors.New("README contributors block not found")
	}
	var contributors []credits.Contributor
	for _, match := range contributorPattern.FindAllStringSubmatch(readme[start:end], -1) {
		name := strings.TrimSpace(match[2])
		if name == "" {
			name = match[1]
		}
		contributors = append(contributors, credits.Contributor{Name: name, Login: match[1]})
	}
	if len(contributors) == 0 {
		return nil, errors.New("README contributors block lists nobody")
	}
	return contributors, nil
}
