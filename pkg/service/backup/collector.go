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

package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

type collectorDefinition struct {
	archive      func(string) string
	trustedRoots []string
	definition   platforms.BackupDefinition
}

type sourceCollector struct {
	ctx                context.Context
	err                error
	excludedSources    map[string]struct{}
	pauser             *syncutil.Pauser
	excludedIdentities []os.FileInfo
	files              []FileRef
	warnings           []models.BackupWarning
	logicalSize        int64
	maxFiles           int
	maxWarnings        int
}

const collectorReadDirBatchSize = 256

var (
	errSensitiveSource       = errors.New("sensitive backup source excluded")
	errSourceIdentityChanged = errors.New("backup source identity changed")
)

type sourceReadCloser struct {
	file *os.File
	root *os.Root
}

type cancelableSource struct {
	source io.ReadCloser
	err    error
	stop   func() bool
	once   sync.Once
}

func (r *sourceReadCloser) Read(p []byte) (int, error) {
	n, err := r.file.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("reading backup source: %w", err)
	}
	return n, nil
}

func (r *sourceReadCloser) Close() error {
	return errors.Join(r.file.Close(), r.root.Close())
}

func (r *cancelableSource) closeSource() {
	r.once.Do(func() { r.err = r.source.Close() })
}

func (r *cancelableSource) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("reading cancelable backup source: %w", err)
	}
	return n, nil
}

func (r *cancelableSource) Close() error {
	r.stop()
	r.closeSource()
	return r.err
}

func newSourceCollector(
	ctx context.Context, excludedSources map[string]struct{},
) *sourceCollector {
	if ctx == nil {
		ctx = context.Background()
	}
	return &sourceCollector{
		ctx: ctx, excludedSources: excludedSources,
		maxFiles: maxArchiveEntries - 1, maxWarnings: maxArchiveEntries,
	}
}

func (c *sourceCollector) continueCollection() bool {
	if c.err != nil {
		return false
	}
	// Checkpoint on the shared media pauser so a filesystem walk yields to a
	// running game; every collect/walk step passes through here.
	if err := c.pauser.Wait(c.ctx); err != nil {
		c.err = fmt.Errorf("collecting backup sources: %w", err)
		return false
	}
	if err := c.ctx.Err(); err != nil {
		c.err = fmt.Errorf("collecting backup sources: %w", err)
		return false
	}
	return true
}

func (m *Manager) collectPlatformDefinitions(collector *sourceCollector, definitions []platforms.BackupDefinition) {
	for i := range definitions {
		def := &definitions[i]
		spec := collectorDefinition{
			definition:   *def,
			trustedRoots: m.definitionTrustedRoots(def),
			archive:      platformArchive,
		}
		collector.collect(&spec)
	}
}

func (m *Manager) definitionTrustedRoots(def *platforms.BackupDefinition) []string {
	if len(def.SourceTrustedRoots) > 0 {
		return canonicalCategoryRoots(def.SourceTrustedRoots, "")
	}
	return definitionCategoryRoots(def, m.pl.RootDirs(m.cfg))
}

func definitionCategoryRoots(def *platforms.BackupDefinition, platformRoots []string) []string {
	storageRoots := make([]string, 0, len(platformRoots)+1)
	if primary, ok := definitionStorageRoot(def); ok {
		storageRoots = append(storageRoots, primary)
	}
	for _, candidate := range platformRoots {
		storageRoot := filepath.Clean(candidate)
		if strings.EqualFold(filepath.Base(storageRoot), "games") {
			storageRoot = filepath.Dir(storageRoot)
		}
		storageRoots = append(storageRoots, storageRoot)
	}
	return canonicalCategoryRoots(storageRoots, def.RestoreRoot)
}

func definitionStorageRoot(def *platforms.BackupDefinition) (string, bool) {
	sourceRoot := filepath.Clean(def.SourceRoot)
	restoreRoot := filepath.Clean(def.RestoreRoot)
	if restoreRoot == "." {
		return sourceRoot, true
	}
	storageRoot := sourceRoot
	for range strings.Split(filepath.ToSlash(restoreRoot), "/") {
		storageRoot = filepath.Dir(storageRoot)
	}
	if filepath.Clean(filepath.Join(storageRoot, restoreRoot)) != sourceRoot {
		return "", false
	}
	return storageRoot, true
}

// canonicalCategoryRoots resolves storage roots but deliberately does not
// resolve the category path appended beneath them. A category-root symlink is
// trusted only when its target is also an independently approved category root.
func canonicalCategoryRoots(storageRoots []string, categoryRoot string) []string {
	seen := make(map[string]struct{}, len(storageRoots))
	canonical := make([]string, 0, len(storageRoots))
	for _, storageRoot := range storageRoots {
		resolved, ok := canonicalPathFromExistingAncestor(storageRoot)
		if !ok {
			continue
		}
		root := filepath.Clean(filepath.Join(resolved, categoryRoot))
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		canonical = append(canonical, root)
	}
	sort.Slice(canonical, func(i, j int) bool { return len(canonical[i]) > len(canonical[j]) })
	return canonical
}

func canonicalPathFromExistingAncestor(candidate string) (string, bool) {
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	current := filepath.Clean(absolute)
	var suffix []string
	for {
		_, statErr := os.Lstat(current)
		if statErr == nil {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", false
			}
			info, targetErr := os.Stat(resolved)
			if targetErr != nil || !info.IsDir() {
				return "", false
			}
			return filepath.Clean(filepath.Join(append([]string{resolved}, suffix...)...)), true
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}

func sourceIdentities(paths map[string]struct{}) ([]os.FileInfo, error) {
	identities := make([]os.FileInfo, 0, len(paths))
	for sourcePath := range paths {
		info, err := os.Stat(sourcePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("stating sensitive backup source: %w", err)
		}
		if info.Mode().IsRegular() {
			identities = append(identities, info)
		}
	}
	return identities, nil
}

func (c *sourceCollector) collect(spec *collectorDefinition) {
	if !c.continueCollection() {
		return
	}
	stack := make(map[string]struct{})
	seen := make(map[string]string)
	c.walk(spec, spec.definition.SourceRoot, "", stack, seen, true)
}

func (c *sourceCollector) walk(
	spec *collectorDefinition,
	physicalPath, logicalRel string,
	stack map[string]struct{},
	seen map[string]string,
	root bool,
) {
	if !c.continueCollection() {
		return
	}
	if len(filepath.ToSlash(logicalRel)) > maxArchivePathLen {
		c.warn(spec.definition.Category, "<path-too-long>", "source path exceeds portable limit")
		return
	}
	info, err := os.Lstat(physicalPath)
	if err != nil {
		if root && errors.Is(err, os.ErrNotExist) {
			return
		}
		c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "source unreadable")
		return
	}

	if info.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := filepath.EvalSymlinks(physicalPath)
		if resolveErr != nil {
			c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "broken symlink")
			return
		}
		resolved, resolveErr = filepath.Abs(resolved)
		if resolveErr != nil {
			c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "invalid symlink target")
			return
		}
		resolved = filepath.Clean(resolved)
		targetInfo, statErr := os.Stat(resolved)
		if statErr != nil {
			c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "broken symlink")
			return
		}
		rootPath, sourceRel, trusted := trustedSource(resolved, spec.trustedRoots)
		if !trusted {
			c.warn(
				spec.definition.Category,
				warningPath(&spec.definition, logicalRel),
				"symlink target outside trusted roots",
			)
			return
		}
		if targetInfo.IsDir() {
			if spec.definition.NonRecursive {
				return
			}
			c.walkDirectory(spec, resolved, logicalRel, stack, seen, rootPath)
			return
		}
		if !targetInfo.Mode().IsRegular() {
			c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "non-regular symlink target")
			return
		}
		if !definitionIncludes(&spec.definition, logicalRel) || !definitionIncludes(&spec.definition, sourceRel) {
			c.warn(
				spec.definition.Category,
				warningPath(&spec.definition, logicalRel),
				"symlink target outside category policy",
			)
			return
		}
		c.add(spec, resolved, rootPath, sourceRel, logicalRel, targetInfo)
		return
	}

	if info.IsDir() {
		resolved, resolveErr := filepath.EvalSymlinks(physicalPath)
		if resolveErr != nil {
			c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "source unreadable")
			return
		}
		resolved, resolveErr = filepath.Abs(resolved)
		if resolveErr != nil {
			c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "source unreadable")
			return
		}
		rootPath, _, trusted := trustedSource(resolved, spec.trustedRoots)
		if !trusted {
			c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "source outside trusted roots")
			return
		}
		c.walkDirectory(spec, filepath.Clean(resolved), logicalRel, stack, seen, rootPath)
		return
	}
	if !info.Mode().IsRegular() {
		c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "non-regular source")
		return
	}
	if !definitionIncludes(&spec.definition, logicalRel) {
		return
	}
	resolved, resolveErr := filepath.Abs(physicalPath)
	if resolveErr != nil {
		c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "source unreadable")
		return
	}
	resolved = filepath.Clean(resolved)
	rootPath, sourceRel, trusted := trustedSource(resolved, spec.trustedRoots)
	if !trusted {
		c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "source outside trusted roots")
		return
	}
	if !definitionIncludes(&spec.definition, sourceRel) {
		c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "source outside category policy")
		return
	}
	c.add(spec, resolved, rootPath, sourceRel, logicalRel, info)
}

func (c *sourceCollector) walkDirectory(
	spec *collectorDefinition,
	physicalPath, logicalRel string,
	stack map[string]struct{},
	seen map[string]string,
	_ string,
) {
	if _, ok := stack[physicalPath]; ok {
		c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "symlink cycle")
		return
	}
	if first, ok := seen[physicalPath]; ok && first != logicalRel {
		c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "duplicate directory target")
		return
	}
	seen[physicalPath] = logicalRel
	stack[physicalPath] = struct{}{}
	defer delete(stack, physicalPath)

	directory, err := os.Open(physicalPath) // #nosec G304 -- path is validated against trusted roots.
	if err != nil {
		c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "source unreadable")
		return
	}
	defer func() { _ = directory.Close() }()
	for c.continueCollection() {
		entries, readErr := directory.ReadDir(collectorReadDirBatchSize)
		for _, entry := range entries {
			childRel := entry.Name()
			if logicalRel != "" {
				childRel = filepath.Join(logicalRel, entry.Name())
			}
			if spec.definition.NonRecursive && logicalRel == "" && entry.IsDir() {
				continue
			}
			c.walk(spec, filepath.Join(physicalPath, entry.Name()), childRel, stack, seen, false)
			if !c.continueCollection() {
				return
			}
		}
		if errors.Is(readErr, io.EOF) {
			return
		}
		if readErr != nil {
			c.warn(spec.definition.Category, warningPath(&spec.definition, logicalRel), "source unreadable")
			return
		}
	}
}

func definitionIncludes(def *platforms.BackupDefinition, rel string) bool {
	if rel == "" || (def.NonRecursive && strings.Contains(filepath.ToSlash(rel), "/")) {
		return false
	}
	return backupPatternsMatch(rel, def.Include) && !backupPatternsMatch(rel, def.Exclude)
}

func trustedSource(target string, roots []string) (root, rel string, ok bool) {
	for _, trustedRoot := range roots {
		candidate, err := filepath.Rel(trustedRoot, target)
		if err != nil || candidate == ".." || strings.HasPrefix(candidate, ".."+string(os.PathSeparator)) {
			continue
		}
		return trustedRoot, candidate, true
	}
	return "", "", false
}

func matchesSourceIdentity(info os.FileInfo, identities []os.FileInfo) bool {
	for _, identity := range identities {
		if os.SameFile(info, identity) {
			return true
		}
	}
	return false
}

func (c *sourceCollector) appendFile(file *FileRef) {
	if !c.continueCollection() {
		return
	}
	if len(file.ArchivePath) > maxArchivePathLen || len(file.RestorePath) > maxArchivePathLen ||
		validateArchivePath(file.ArchivePath) != nil || validateRestorePath(file.RestorePath) != nil {
		c.warn(file.Category, file.RestorePath, "source path is not portable")
		return
	}
	if len(c.files) >= c.maxFiles {
		c.err = fmt.Errorf("backup has too many files: exceeds %d", c.maxFiles)
		return
	}
	if file.Size < 0 {
		c.err = errors.New("backup source has negative size")
		return
	}
	if file.Size > math.MaxInt64-c.logicalSize {
		c.err = errors.New("backup logical size overflow")
		return
	}
	c.logicalSize += file.Size
	c.files = append(c.files, *file)
}

func (c *sourceCollector) add(
	spec *collectorDefinition,
	sourcePath, sourceRoot, sourceRel, logicalRel string,
	info os.FileInfo,
) {
	restorePath := filepath.Join(spec.definition.RestoreRoot, logicalRel)
	if _, excluded := c.excludedSources[filepath.Clean(sourcePath)]; excluded ||
		matchesSourceIdentity(info, c.excludedIdentities) {
		c.warn(spec.definition.Category, restorePath, "sensitive source excluded")
		return
	}
	c.appendFile(&FileRef{
		sourceIdentity: &sourceIdentity{info: info, excludedIdentities: c.excludedIdentities},
		SourceRoot:     sourceRoot,
		SourceRel:      sourceRel,
		ArchivePath:    filepath.ToSlash(spec.archive(restorePath)),
		RestorePath:    filepath.ToSlash(restorePath),
		Category:       spec.definition.Category,
		Size:           info.Size(),
	})
}

func (c *sourceCollector) addTrustedFile(
	sourcePath, category, archivePath, restorePath string,
) error {
	resolved, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return fmt.Errorf("resolving trusted backup source: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("resolving trusted backup source path: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("stating trusted backup source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("trusted backup source is not a regular file: %s", sourcePath)
	}
	root := filepath.Dir(resolved)
	c.appendFile(&FileRef{
		sourceIdentity: &sourceIdentity{info: info},
		SourceRoot:     root,
		SourceRel:      filepath.Base(resolved),
		ArchivePath:    filepath.ToSlash(archivePath),
		RestorePath:    filepath.ToSlash(restorePath),
		Category:       category,
		Size:           info.Size(),
	})
	return c.err
}

func (c *sourceCollector) warn(category, logicalPath, reason string) {
	if !c.continueCollection() {
		return
	}
	if len(c.warnings) >= c.maxWarnings {
		c.err = fmt.Errorf("backup has too many warnings: exceeds %d", c.maxWarnings)
		return
	}
	c.warnings = append(c.warnings, models.BackupWarning{
		Category: category,
		Path:     portableWarningPath(logicalPath),
		Reason:   reason,
	})
}

func portableWarningPath(logicalPath string) string {
	warningPath := strings.ToValidUTF8(filepath.ToSlash(logicalPath), "\uFFFD")
	warningPath = strings.ReplaceAll(warningPath, "\\", "%5C")
	warningPath = strings.ReplaceAll(warningPath, "\x00", "%00")
	if len(warningPath) > maxArchivePathLen {
		return "<path-too-long>"
	}
	if validateRestorePath(warningPath) != nil {
		return "<invalid-path>"
	}
	return warningPath
}

func warningPath(def *platforms.BackupDefinition, logicalRel string) string {
	if logicalRel == "" && def.RestoreRoot == "" {
		return "<root>"
	}
	if logicalRel == "" {
		return def.RestoreRoot
	}
	return filepath.Join(def.RestoreRoot, logicalRel)
}

func (m *Manager) validateManifestPolicy(manifest *Manifest) error {
	if manifest.Platform != m.pl.ID() {
		return fmt.Errorf("backup platform %q does not match %q", manifest.Platform, m.pl.ID())
	}
	userDBFiles := 0
	for _, file := range manifest.Files {
		if file.Category == CategoryZaparoo && file.RestorePath == config.UserDbFile &&
			file.ArchivePath == zaparooArchive(config.UserDbFile) {
			userDBFiles++
		}
	}
	if userDBFiles != 1 {
		return fmt.Errorf("full-device backup must contain exactly one %s payload", zaparooArchive(config.UserDbFile))
	}
	definitions := []platforms.BackupDefinition(nil)
	if provider, ok := m.pl.(platforms.BackupProvider); ok {
		definitions = provider.BackupDefinitions()
	}
	// Zaparoo files are validated against the same definitions collection
	// uses, so the two policies cannot drift. user.db is the one
	// constructed entry, allowed by its exact name.
	zaparooDefinitions := m.zaparooRestoreDefinitions()
	for _, file := range manifest.Files {
		if file.Category == CategoryZaparoo {
			if file.RestorePath != config.UserDbFile && !allowedRestorePath(&file, zaparooDefinitions) {
				return fmt.Errorf("backup path is not collected by Zaparoo policy: %s", file.RestorePath)
			}
			if file.ArchivePath != zaparooArchive(file.RestorePath) {
				return fmt.Errorf("backup archive path does not match restore path: %s", file.ArchivePath)
			}
			continue
		}
		if file.ArchivePath != platformArchive(file.RestorePath) {
			return fmt.Errorf("backup archive path does not match restore path: %s", file.ArchivePath)
		}
		if !allowedRestorePath(&file, definitions) {
			return fmt.Errorf("backup path is not collected by platform policy: %s", file.RestorePath)
		}
	}
	return nil
}

// zaparooRestoreDefinitions returns the collection definitions used to
// validate zaparoo-category restore paths.
func (m *Manager) zaparooRestoreDefinitions() []platforms.BackupDefinition {
	return zaparooBackupDefinitions(helpers.ConfigDir(m.pl), helpers.DataDir(m.pl))
}

func allowedRestorePath(file *FileRef, definitions []platforms.BackupDefinition) bool {
	for i := range definitions {
		def := &definitions[i]
		if def.Category != file.Category {
			continue
		}
		restoreRoot := filepath.ToSlash(def.RestoreRoot)
		rel := file.RestorePath
		if restoreRoot != "" {
			prefix := restoreRoot + "/"
			if !strings.HasPrefix(file.RestorePath, prefix) {
				continue
			}
			rel = strings.TrimPrefix(file.RestorePath, prefix)
		}
		if definitionIncludes(def, filepath.FromSlash(rel)) {
			return true
		}
	}
	return false
}

func openSource(file *FileRef) (io.ReadCloser, error) {
	if file.SourceRoot == "" || file.SourceRel == "" {
		return nil, fmt.Errorf("backup source is not root-scoped: %s", file.RestorePath)
	}
	root, err := os.OpenRoot(file.SourceRoot)
	if err != nil {
		return nil, fmt.Errorf("opening source root %s: %w", file.SourceRoot, err)
	}
	opened, err := root.Open(file.SourceRel)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("opening source %s: %w", file.RestorePath, err)
	}
	if file.sourceIdentity != nil {
		openedInfo, statErr := opened.Stat()
		if statErr != nil {
			_ = opened.Close()
			_ = root.Close()
			return nil, fmt.Errorf("stating opened source %s: %w", file.RestorePath, statErr)
		}
		if !openedInfo.Mode().IsRegular() || !os.SameFile(file.sourceIdentity.info, openedInfo) {
			_ = opened.Close()
			_ = root.Close()
			return nil, fmt.Errorf("%w: %s", errSourceIdentityChanged, file.RestorePath)
		}
		if matchesSourceIdentity(openedInfo, file.sourceIdentity.excludedIdentities) {
			_ = opened.Close()
			_ = root.Close()
			return nil, fmt.Errorf("%w: %s", errSensitiveSource, file.RestorePath)
		}
	}
	return &sourceReadCloser{file: opened, root: root}, nil
}

func newCancelableSource(ctx context.Context, source io.ReadCloser) *cancelableSource {
	cancelable := &cancelableSource{source: source}
	cancelable.stop = context.AfterFunc(ctx, cancelable.closeSource)
	return cancelable
}

func openSourceContext(ctx context.Context, file *FileRef) (io.ReadCloser, error) {
	source, err := openSource(file)
	if err != nil {
		return nil, err
	}
	return newCancelableSource(ctx, source), nil
}
