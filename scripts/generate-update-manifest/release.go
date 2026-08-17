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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// githubRelease is the subset of `gh release view --json ...` output the
// generator consumes.
type githubRelease struct {
	PublishedAt  time.Time     `json:"publishedAt"`
	TagName      string        `json:"tagName"`
	URL          string        `json:"url"`
	Assets       []githubAsset `json:"assets"`
	IsDraft      bool          `json:"isDraft"`
	IsPrerelease bool          `json:"isPrerelease"`
}

type githubAsset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// archiveFile is a release archive that has been downloaded and hashed
// locally. The digest here is the one that ends up in the signed manifest, so
// it is always computed from bytes on disk rather than taken from an API
// response.
type archiveFile struct {
	Name   string
	SHA256 string
	Size   int64
}

var errDigestMismatch = errors.New("locally computed digest does not match GitHub")

func loadGithubRelease(fs afero.Fs, path string) (*githubRelease, error) {
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		return nil, fmt.Errorf("reading GitHub release metadata: %w", err)
	}

	var rel githubRelease
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, fmt.Errorf("parsing GitHub release metadata: %w", err)
	}

	return &rel, nil
}

func validateGithubReleaseMetadata(rel *githubRelease, tag, channel string) error {
	if rel.TagName == "" {
		return errors.New("GitHub release metadata is missing tagName")
	}
	if rel.TagName != tag {
		return fmt.Errorf("GitHub release metadata tag %q does not match version %q", rel.TagName, tag)
	}
	if rel.URL == "" {
		return fmt.Errorf("GitHub release metadata for %s is missing url", rel.TagName)
	}
	if rel.PublishedAt.IsZero() {
		return fmt.Errorf("GitHub release metadata for %s is missing publishedAt", rel.TagName)
	}
	if rel.IsDraft {
		return fmt.Errorf("GitHub release %s is a draft", rel.TagName)
	}
	if want := channelForPrerelease(rel.IsPrerelease); want != channel {
		return fmt.Errorf(
			"GitHub release %s is marked prerelease=%t which belongs in the %s channel, not %s",
			rel.TagName, rel.IsPrerelease, want, channel,
		)
	}

	return nil
}

// scanArchives hashes every release archive in dir. This is the only place
// digests enter the pipeline: nothing downstream trusts a digest supplied by
// an API.
func scanArchives(fs afero.Fs, dir string) ([]archiveFile, error) {
	entries, err := afero.ReadDir(fs, dir)
	if err != nil {
		return nil, fmt.Errorf("reading archives directory: %w", err)
	}

	files := make([]archiveFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isUpdateArchive(entry.Name()) {
			continue
		}
		digest, err := hashFile(fs, filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		files = append(files, archiveFile{
			Name:   entry.Name(),
			SHA256: digest,
			Size:   entry.Size(),
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w in %s", errNoAssets, dir)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func hashFile(fs afero.Fs, path string) (string, error) {
	f, err := fs.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() {
		_ = f.Close()
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// crossCheckGithubDigests asserts that what we downloaded and hashed matches
// what GitHub says it published, in both directions. A one-sided check would
// let a truncated download through, and trusting GitHub's digest outright
// would make the release API part of the trust base.
func crossCheckGithubDigests(rel *githubRelease, files []archiveFile) error {
	local := make(map[string]archiveFile, len(files))
	for _, f := range files {
		local[f.Name] = f
	}

	remote := make(map[string]githubAsset, len(rel.Assets))
	for _, a := range rel.Assets {
		if isUpdateArchive(a.Name) {
			remote[a.Name] = a
		}
	}

	var problems []string
	for name, f := range local {
		a, ok := remote[name]
		if !ok {
			problems = append(problems, name+": downloaded but not present in the GitHub release")
			continue
		}
		want := strings.TrimPrefix(a.Digest, "sha256:")
		if want == "" {
			problems = append(problems, name+": GitHub release asset has no sha256 digest")
			continue
		}
		if !strings.EqualFold(want, f.SHA256) {
			problems = append(problems, fmt.Sprintf(
				"%s: GitHub reports %s, local file hashes to %s", name, want, f.SHA256,
			))
			continue
		}
		if a.Size != 0 && a.Size != f.Size {
			problems = append(problems, fmt.Sprintf(
				"%s: GitHub reports %d bytes, local file is %d", name, a.Size, f.Size,
			))
		}
	}
	for name := range remote {
		if _, ok := local[name]; !ok {
			problems = append(problems, name+": present in the GitHub release but was not downloaded")
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("%w: %s", errDigestMismatch, strings.Join(problems, "; "))
	}

	return nil
}

// buildReleaseAssets turns hashed archives into manifest asset entries,
// followed by the shared metadata files every release points at. IDs continue
// from startAssetID and the new high-water mark is returned.
func buildReleaseAssets(
	tag string, files []archiveFile, startAssetID int64,
) (assets []*asset, lastAssetID int64) {
	assetID := startAssetID
	assets = make([]*asset, 0, len(files)+2)

	for _, f := range files {
		assetID++
		assets = append(assets, &asset{
			ID:     assetID,
			Name:   f.Name,
			Size:   f.Size,
			SHA256: f.SHA256,
			URL:    githubReleaseDownloadBase + "/" + tag + "/" + f.Name,
		})
	}

	for _, name := range []string{checksumsName, checksumsSigName} {
		assetID++
		assets = append(assets, &asset{
			ID:   assetID,
			Name: name,
			URL:  name,
		})
	}

	return assets, assetID
}
