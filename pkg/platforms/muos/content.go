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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/afero"
)

const (
	runDir           = "/run/muos"
	contentFile      = runDir + "/content"
	overlayFile      = runDir + "/overlay"
	safeQuitFile     = runDir + "/safe_quit"
	infoContentDir   = "/opt/muos/share/info/content"
	assignFile       = runDir + "/storage/info/manifest/assign.json"
	foregroundFile   = "/opt/muos/config/system/foreground_process"
	lastPlayFile     = "/opt/muos/config/boot/last_play"
	boardNameFile    = "/opt/muos/device/config/board/name"
	extraCoresFile   = "/opt/muos/config/settings/advanced/extracores"
	romMountFile     = "/opt/muos/device/config/storage/rom/mount"
	sdcardMountFile  = "/opt/muos/device/config/storage/sdcard/mount"
	usbMountFile     = "/opt/muos/device/config/storage/usb/mount"
	defaultROMMount  = "/mnt/mmc"
	defaultSDMount   = "/mnt/sdcard"
	defaultUSBMount  = "/mnt/usb"
	romsDirName      = "ROMS"
	maxManifestSize  = 4 << 20
	maxSmallFileSize = 64 << 10
)

//nolint:gochecknoglobals // Manifest sources in the frontend's load order.
var manifestRoots = []string{
	runDir + "/storage/info/manifest/core",
	runDir + "/storage/info/manifest/coresync",
	"/opt/muos/share/info/manifest/core",
}

// coreJSONFiles are the single-file core manifests that muOS 2606 and
// earlier ship instead of the per-system tree. The user copy replaces the
// bundled one when it holds a JSON object.
//
//nolint:gochecknoglobals // Manifest sources in the frontend's load order.
var coreJSONFiles = []string{
	runDir + "/storage/info/manifest/core.json",
	"/opt/muos/share/info/manifest/core.json",
}

// contentAsset is a per-launch setting the frontend hands to launch.sh in
// its own file under /run/muos.
type contentAsset struct {
	ext      string
	runFile  string
	fallback string
}

//nolint:gochecknoglobals // Static table mirroring the frontend's launch assets.
var contentAssets = []contentAsset{
	{ext: "gov", runFile: runDir + "/governor", fallback: "default"},
	{ext: "con", runFile: runDir + "/controller", fallback: "system"},
	{ext: "rac", runFile: runDir + "/retroarch", fallback: "false"},
	{ext: "flt", runFile: runDir + "/filter", fallback: "none"},
	{ext: "shd", runFile: runDir + "/shader", fallback: "none"},
	{ext: "ovl", runFile: runDir + "/overlay_options", fallback: "none"},
}

var errNoCore = errors.New("no muOS core is assigned to this content")

// coreAssignment is the core a piece of content runs with.
type coreAssignment struct {
	core      string
	system    string
	catalogue string
	lookup    string
	tag       string
}

// contentRequest is everything launch.sh needs to run one piece of content.
type contentRequest struct {
	assets  map[string]string
	path    string
	name    string
	romsDir string
	relDir  string
	file    string
	coreAssignment
}

// contentLines renders the nine line request launch.sh reads.
func (r *contentRequest) contentLines() string {
	lines := []string{
		r.name, r.core, r.system, r.catalogue, r.lookup, r.tag,
		r.romsDir, r.relDir, r.file,
	}
	return strings.Join(lines, "\n") + "\n"
}

// contentName is the display name muOS uses for a content file: the file
// name without its last extension.
func contentName(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" && ext != base {
		return strings.TrimSuffix(base, ext)
	}
	return base
}

func readLimited(fs afero.Fs, path string, limit int64) ([]byte, error) {
	f, err := fs.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func readLines(fs afero.Fs, path string) ([]string, error) {
	data, err := readLimited(fs, path, maxSmallFileSize)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return lines, nil
}

func readFirstLine(fs afero.Fs, path string) string {
	lines, err := readLines(fs, path)
	if err != nil || len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func fileExists(fs afero.Fs, path string) bool {
	info, err := fs.Stat(path)
	return err == nil && !info.IsDir()
}

func lineAt(lines []string, n int) string {
	if n < len(lines) {
		return lines[n]
	}
	return ""
}

// mountPoints returns the storage mounts in the frontend's priority order.
func mountPoints(fs afero.Fs) []string {
	mounts := make([]string, 0, 3)
	for _, m := range []struct{ file, fallback string }{
		{usbMountFile, defaultUSBMount},
		{sdcardMountFile, defaultSDMount},
		{romMountFile, defaultROMMount},
	} {
		mount := readFirstLine(fs, m.file)
		if mount == "" || !filepath.IsAbs(mount) {
			mount = m.fallback
		}
		mount = filepath.Clean(mount)
		dup := false
		for _, existing := range mounts {
			if existing == mount {
				dup = true
				break
			}
		}
		if !dup {
			mounts = append(mounts, mount)
		}
	}
	return mounts
}

func romMount(fs afero.Fs) string {
	mount := readFirstLine(fs, romMountFile)
	if mount == "" || !filepath.IsAbs(mount) {
		return defaultROMMount
	}
	return filepath.Clean(mount)
}

// splitContentPath finds the ROMS directory a path is under and returns the
// directory relative to it.
func splitContentPath(fs afero.Fs, path string) (romsDir, relDir string, err error) {
	for _, mount := range mountPoints(fs) {
		roms := filepath.Join(mount, romsDirName)
		if !strings.HasPrefix(path, roms+"/") {
			continue
		}
		rel, relErr := filepath.Rel(roms, filepath.Dir(path))
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, "../") {
			continue
		}
		if rel == "." {
			rel = ""
		}
		return roms, rel, nil
	}
	return "", "", fmt.Errorf("path is not inside a muOS ROMS folder: %s", path)
}

func hasLineBreak(values ...string) bool {
	for _, v := range values {
		if strings.ContainsAny(v, "\n\r") {
			return true
		}
	}
	return false
}

// infoDir is the content settings directory for a directory under ROMS.
func infoDir(relDir string) string {
	if relDir == "" {
		return infoContentDir
	}
	return filepath.Join(infoContentDir, relDir)
}

func parentRel(relDir string) string {
	parent := filepath.Dir(relDir)
	if parent == "." || parent == "/" {
		return ""
	}
	return parent
}

// coreFromCfg reads core, system, catalogue, lookup and tag starting at a
// line offset: 1 for a per-content file, 0 for core.cfg.
func coreFromCfg(fs afero.Fs, path string, offset int) (coreAssignment, bool) {
	lines, err := readLines(fs, path)
	if err != nil {
		return coreAssignment{}, false
	}
	a := coreAssignment{
		core:      lineAt(lines, offset),
		system:    lineAt(lines, offset+1),
		catalogue: lineAt(lines, offset+2),
		lookup:    lineAt(lines, offset+3),
		tag:       lineAt(lines, offset+4),
	}
	if a.core == "" || a.system == "" || a.tag == "" {
		return coreAssignment{}, false
	}
	return a, true
}

// resolveContent builds a launch request for a ROM using the same sources
// the frontend uses: a per-content file, the folder's core.cfg, automatic
// assignment by folder name, then the nearest core_recursive.cfg.
func resolveContent(fs afero.Fs, db *coreDB, path string) (*contentRequest, error) {
	if hasLineBreak(path) {
		return nil, errors.New("content path contains a line break")
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("content path is not absolute: %s", path)
	}
	info, err := fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("content not found: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("content is not a file: %s", path)
	}

	romsDir, relDir, err := splitContentPath(fs, path)
	if err != nil {
		return nil, err
	}

	req := &contentRequest{
		path:    path,
		name:    contentName(path),
		romsDir: romsDir + "/",
		relDir:  relDir,
		file:    filepath.Base(path),
	}
	dir := infoDir(relDir)

	auto := map[string]string{}
	assigned, ok := coreFromCfg(fs, filepath.Join(dir, req.name+".cfg"), 1)
	if !ok {
		assigned, ok = coreFromCfg(fs, filepath.Join(dir, "core.cfg"), 0)
	}
	if !ok && relDir != "" {
		assigned, ok = autoAssign(db, filepath.Base(relDir), auto)
	}
	if !ok {
		for d := parentRel(relDir); ; d = parentRel(d) {
			assigned, ok = coreFromCfg(
				fs, filepath.Join(infoDir(d), "core_recursive.cfg"), 0)
			if ok || d == "" {
				break
			}
		}
	}
	if !ok && strings.Contains(relDir, "/") {
		top, _, _ := strings.Cut(relDir, "/")
		assigned, ok = autoAssign(db, top, auto)
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNoCore, path)
	}

	if assigned.lookup == "" {
		assigned.lookup = "0"
	}
	if assigned.catalogue == "" {
		assigned.catalogue = assigned.system
	}
	req.coreAssignment = assigned
	if hasLineBreak(req.core, req.system, req.catalogue, req.lookup, req.tag, req.relDir, req.file) {
		return nil, errors.New("content settings contain a line break")
	}

	req.assets = make(map[string]string, len(contentAssets))
	for _, a := range contentAssets {
		value := readAsset(fs, relDir, req.name, a.ext)
		if value == "" {
			value = auto[a.ext]
		}
		if value == "" {
			value = a.fallback
		}
		req.assets[a.ext] = value
	}
	return req, nil
}

// autoAssign resolves a folder through the assign index and the system's
// default core. Governor and control scheme go into auto, matching what the
// frontend saves alongside an automatic assignment.
func autoAssign(db *coreDB, folder string, auto map[string]string) (coreAssignment, bool) {
	if db == nil {
		return coreAssignment{}, false
	}
	system, found := db.systemForFolder(folder)
	if !found {
		return coreAssignment{}, false
	}
	choice, found := db.defaultCore(system)
	if !found || choice.Core == "" {
		return coreAssignment{}, false
	}

	governor := choice.Governor
	if governor == "" {
		governor = db.systemGovernor(system)
	}
	auto["gov"] = cpuProfileValue(governor)
	control := choice.Control
	if control == "" {
		control = db.systemControl(system)
	}
	if control != "" {
		auto["con"] = control
	}

	return coreAssignment{
		core:      choice.Core,
		system:    system,
		catalogue: db.catalogue(system),
		lookup:    strconv.Itoa(db.lookup(system)),
		tag:       choice.tag(),
	}, true
}

// readAsset returns a content setting from its per-content file, then the
// folder default. Content at the top of ROMS only has the global default.
func readAsset(fs afero.Fs, relDir, name, ext string) string {
	var candidates []string
	if relDir == "" {
		candidates = []string{filepath.Join(infoContentDir, "core."+ext)}
	} else {
		dir := infoDir(relDir)
		candidates = []string{
			filepath.Join(dir, name+"."+ext),
			filepath.Join(dir, "core."+ext),
		}
	}
	for _, c := range candidates {
		if !fileExists(fs, c) {
			continue
		}
		data, err := readLimited(fs, c, maxSmallFileSize)
		if err != nil {
			continue
		}
		if value := string(data); value != "" && value != "(null)" {
			return value
		}
	}
	return ""
}

// writeAssets writes the per-launch settings launch.sh applies.
func writeAssets(fs afero.Fs, req *contentRequest) error {
	if err := fs.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", runDir, err)
	}
	for _, a := range contentAssets {
		if err := afero.WriteFile(fs, a.runFile, []byte(req.assets[a.ext]), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", a.runFile, err)
		}
	}
	return nil
}

// writeContentFile hands the request to the frontend's content loader. It
// is written to a temporary file and renamed so the loader never sees a
// partial request.
func writeContentFile(fs afero.Fs, req *contentRequest) error {
	tmp := contentFile + ".tmp"
	f, err := fs.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create content request: %w", err)
	}
	if _, err := f.WriteString(req.contentLines()); err != nil {
		_ = f.Close()
		_ = fs.Remove(tmp)
		return fmt.Errorf("write content request: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = fs.Remove(tmp)
		return fmt.Errorf("sync content request: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = fs.Remove(tmp)
		return fmt.Errorf("close content request: %w", err)
	}
	if err := fs.Rename(tmp, contentFile); err != nil {
		_ = fs.Remove(tmp)
		return fmt.Errorf("publish content request: %w", err)
	}
	return nil
}

func removeIfExists(fs afero.Fs, path string) error {
	if err := fs.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// overlayState is what launch.sh records about the content it is running.
type overlayState struct {
	name   string
	system string
	core   string
}

func readOverlay(fs afero.Fs) (overlayState, bool) {
	data, err := readLimited(fs, overlayFile, maxSmallFileSize)
	if err != nil {
		return overlayState{}, false
	}
	lines := strings.Split(string(data), "\n")
	state := overlayState{
		name:   strings.TrimSpace(lineAt(lines, 0)),
		system: strings.TrimSpace(lineAt(lines, 1)),
		core:   strings.TrimSpace(lineAt(lines, 2)),
	}
	if state.name == "" {
		return overlayState{}, false
	}
	return state, true
}

// lastPlayedPath returns the ROM recorded by the frontend's last launch,
// when its name matches the given content name.
func lastPlayedPath(fs afero.Fs, name string) (string, bool) {
	cfg := readFirstLine(fs, lastPlayFile)
	if cfg == "" || !filepath.IsAbs(cfg) {
		return "", false
	}
	lines, err := readLines(fs, cfg)
	if err != nil || lineAt(lines, 0) != name {
		return "", false
	}
	romsDir, relDir, file := lineAt(lines, 6), lineAt(lines, 7), lineAt(lines, 8)
	if romsDir == "" || file == "" {
		return "", false
	}
	return filepath.Clean(romsDir + relDir + "/" + file), true
}
