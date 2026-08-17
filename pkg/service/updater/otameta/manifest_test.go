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

package otameta

import (
	"crypto/ed25519"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKeyID = "test1"

// signedManifest returns manifest bytes and a detached signature over them,
// signed with a key pair generated for this test, plus a lookup that knows it.
func signedManifest(t *testing.T, body string) (data, sig []byte, lookup func(string) (ed25519.PublicKey, error)) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	data = []byte(body)
	sig = ed25519.Sign(priv, data)
	lookup = func(id string) (ed25519.PublicKey, error) {
		if id != testKeyID {
			return nil, fmt.Errorf("%w: %q", ErrUnknownKeyID, id)
		}
		return pub, nil
	}
	return data, sig, lookup
}

func manifestBody(keyID string, generation int64, version int) string {
	return fmt.Sprintf(`manifest_version: %d
generation: %d
issued_at: 2026-08-17T02:00:00Z
key_id: %s
releases:
  - id: 1
    name: v2.16.1
    tag_name: v2.16.1
    channel: stable
    assets:
      - id: 10
        name: zaparoo-linux_amd64-2.16.1.tar.gz
        url: https://github.com/ZaparooProject/zaparoo-core/releases/download/v2.16.1/zaparoo-linux_amd64-2.16.1.tar.gz
`, version, generation, keyID)
}

func TestVerify_ValidSignature(t *testing.T) {
	t.Parallel()

	data, sig, lookup := signedManifest(t, manifestBody(testKeyID, 412, 1))

	m, err := verifyWith(data, sig, lookup)
	require.NoError(t, err)
	assert.Equal(t, int64(412), m.Generation)
	assert.Equal(t, testKeyID, m.KeyID)
	require.Len(t, m.Releases, 1)
	assert.Equal(t, "v2.16.1", m.Releases[0].TagName)
}

func TestVerify_TamperedBody(t *testing.T) {
	t.Parallel()

	data, sig, lookup := signedManifest(t, manifestBody(testKeyID, 412, 1))

	// Still-valid YAML with one changed character, which is what a useful
	// tamper looks like: a corrupted document would be caught by the parser
	// whether it was signed or not.
	tampered := []byte(strings.Replace(string(data), "generation: 412", "generation: 411", 1))
	require.Len(t, tampered, len(data))

	_, err := verifyWith(tampered, sig, lookup)
	require.ErrorIs(t, err, ErrBadSignature)
}

func TestVerify_WrongKey(t *testing.T) {
	t.Parallel()

	data, sig, _ := signedManifest(t, manifestBody(testKeyID, 412, 1))

	other, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	_, err = verifyWith(data, sig, func(string) (ed25519.PublicKey, error) { return other, nil })
	require.ErrorIs(t, err, ErrBadSignature)
}

func TestVerify_TruncatedSignature(t *testing.T) {
	t.Parallel()

	data, sig, lookup := signedManifest(t, manifestBody(testKeyID, 412, 1))

	_, err := verifyWith(data, sig[:len(sig)-1], lookup)
	require.ErrorIs(t, err, ErrBadSignature)
}

func TestVerify_EmptySignature(t *testing.T) {
	t.Parallel()

	data, _, lookup := signedManifest(t, manifestBody(testKeyID, 412, 1))

	_, err := verifyWith(data, nil, lookup)
	require.ErrorIs(t, err, ErrBadSignature)
}

func TestVerify_UnknownKeyID(t *testing.T) {
	t.Parallel()

	data, sig, lookup := signedManifest(t, manifestBody("k99", 412, 1))

	_, err := verifyWith(data, sig, lookup)
	require.ErrorIs(t, err, ErrUnknownKeyID)
}

func TestVerify_NoKeyID(t *testing.T) {
	t.Parallel()

	data, sig, lookup := signedManifest(t, "manifest_version: 1\ngeneration: 1\n")

	_, err := verifyWith(data, sig, lookup)
	require.ErrorIs(t, err, ErrManifestMalformed)
}

// A future schema is refused rather than parsed on a best-effort basis: the
// fields a newer publisher relies on to keep a device safe may be exactly the
// ones this build would ignore.
func TestVerify_FutureManifestVersion(t *testing.T) {
	t.Parallel()

	data, sig, lookup := signedManifest(t, manifestBody(testKeyID, 412, CurrentManifestVersion+1))

	_, err := verifyWith(data, sig, lookup)
	require.ErrorIs(t, err, ErrManifestTooNew)
}

func TestParse_MissingManifestVersion(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("generation: 1\nkey_id: k1\n"))
	require.ErrorIs(t, err, ErrManifestTooNew)
}

func TestParse_Malformed(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("\tnot: [valid yaml"))
	require.ErrorIs(t, err, ErrManifestMalformed)
}

// buildTargets mirrors the release build matrix. It is duplicated from the
// publish generator on purpose: if the two ever disagree, one of them is wrong
// about what a complete release looks like and the tests should say so.
func buildTargets() []struct{ platform, goarch, ext string } {
	pairs := []struct {
		platform string
		arches   []string
	}{
		{platform: "batocera", arches: []string{"amd64", "arm", "arm64"}},
		{platform: "bazzite", arches: []string{"amd64", "arm64"}},
		{platform: "chimeraos", arches: []string{"amd64", "arm64"}},
		{platform: "libreelec", arches: []string{"amd64", "arm", "arm64"}},
		{platform: "linux", arches: []string{"amd64", "arm64"}},
		{platform: "mac", arches: []string{"amd64", "arm64"}},
		{platform: "mister", arches: []string{"arm"}},
		{platform: "mistex", arches: []string{"arm64"}},
		{platform: "replayos", arches: []string{"arm64"}},
		{platform: "steamos", arches: []string{"amd64"}},
		{platform: "windows", arches: []string{"386", "amd64", "arm64"}},
		{platform: "zapos", arches: []string{"arm64"}},
	}
	zip := map[string]bool{"mac": true, "mister": true, "mistex": true, "windows": true}

	out := make([]struct{ platform, goarch, ext string }, 0, 22)
	for _, p := range pairs {
		ext := ".tar.gz"
		if zip[p.platform] {
			ext = ".zip"
		}
		for _, a := range p.arches {
			out = append(out, struct{ platform, goarch, ext string }{p.platform, a, ext})
		}
	}
	return out
}

// releaseWithFullMatrix builds a release carrying one archive per build target.
func releaseWithFullMatrix(tag string) *Release {
	version := VersionFromTag(tag)
	rel := &Release{TagName: tag}
	for i, t := range buildTargets() {
		rel.Assets = append(rel.Assets, &Asset{
			ID:   int64(i + 1),
			Name: ArchiveBaseName(t.platform, t.goarch, version) + t.ext,
		})
	}
	return rel
}

func TestSelectAsset_EveryBuildTarget(t *testing.T) {
	t.Parallel()

	rel := releaseWithFullMatrix("v2.16.1")

	for _, target := range buildTargets() {
		t.Run(target.platform+"_"+target.goarch, func(t *testing.T) {
			t.Parallel()

			asset, err := SelectAsset(rel, target.platform, target.goarch)
			require.NoError(t, err)
			assert.Equal(t, "zaparoo-"+target.platform+"_"+target.goarch+"-2.16.1"+target.ext, asset.Name)
		})
	}
}

// The arm/arm64 pairs are the ones a prefix match gets wrong.
func TestSelectAsset_ArchConfusables(t *testing.T) {
	t.Parallel()

	for _, platform := range []string{"batocera", "libreelec"} {
		t.Run(platform, func(t *testing.T) {
			t.Parallel()

			// A release that only ships arm64 must not resolve for an arm device.
			rel := &Release{
				TagName: "v2.16.1",
				Assets: []*Asset{
					{ID: 1, Name: ArchiveBaseName(platform, "arm64", "2.16.1") + ".tar.gz"},
				},
			}

			_, err := SelectAsset(rel, platform, "arm")
			require.ErrorIs(t, err, ErrNoAsset)

			asset, err := SelectAsset(rel, platform, "arm64")
			require.NoError(t, err)
			assert.Contains(t, asset.Name, "_arm64-")
		})
	}
}

// The defect this whole mechanism exists for: metadata claiming a new version
// over genuine old archives must select nothing rather than install the old
// binary under the new version's name.
func TestSelectAsset_RelabelledVersionSelectsNothing(t *testing.T) {
	t.Parallel()

	rel := releaseWithFullMatrix("v2.10.1")
	rel.TagName = "v99.0.0"
	rel.Name = "v99.0.0"

	for _, target := range buildTargets() {
		_, err := SelectAsset(rel, target.platform, target.goarch)
		require.ErrorIsf(t, err, ErrNoAsset,
			"%s_%s must not resolve against relabelled assets", target.platform, target.goarch)
	}
}

func TestSelectAsset_PatchVersionMismatch(t *testing.T) {
	t.Parallel()

	rel := releaseWithFullMatrix("v2.16.1")
	rel.TagName = "v2.16.2"

	_, err := SelectAsset(rel, "linux", "amd64")
	require.ErrorIs(t, err, ErrNoAsset)
}

// Names are compared for equality, so a version full of regex metacharacters is
// just a string. This would be a wildcard if selection were pattern-based.
func TestSelectAsset_VersionWithRegexMetacharacters(t *testing.T) {
	t.Parallel()

	rel := &Release{
		TagName: "v2.16.1",
		Assets: []*Asset{
			{ID: 1, Name: "zaparoo-linux_amd64-..........tar.gz"},
			{ID: 2, Name: "zaparoo-linux_amd64-.*.tar.gz"},
		},
	}

	_, err := SelectAsset(rel, "linux", "amd64")
	require.ErrorIs(t, err, ErrNoAsset)
}

func TestSelectAsset_WrongPlatform(t *testing.T) {
	t.Parallel()

	rel := releaseWithFullMatrix("v2.16.1")

	_, err := SelectAsset(rel, "nosuchplatform", "amd64")
	require.ErrorIs(t, err, ErrNoAsset)
}

func TestSelectAsset_Ambiguous(t *testing.T) {
	t.Parallel()

	// Same base name under both archive extensions: nothing legitimate produces
	// this, so it must fail rather than pick one.
	rel := &Release{
		TagName: "v2.16.1",
		Assets: []*Asset{
			{ID: 1, Name: "zaparoo-linux_amd64-2.16.1.tar.gz"},
			{ID: 2, Name: "zaparoo-linux_amd64-2.16.1.zip"},
		},
	}

	_, err := SelectAsset(rel, "linux", "amd64")
	require.ErrorIs(t, err, ErrAmbiguousAsset)
}

func TestSelectAsset_IgnoresNonArchiveAndNilEntries(t *testing.T) {
	t.Parallel()

	rel := &Release{
		TagName: "v2.16.1",
		Assets: []*Asset{
			nil,
			{ID: 1, Name: "checksums.txt"},
			{ID: 2, Name: "zaparoo-linux_amd64-2.16.1.tar.gz.sig"},
			{ID: 3, Name: "zaparoo-linux_amd64-2.16.1.tar.gz"},
		},
	}

	asset, err := SelectAsset(rel, "linux", "amd64")
	require.NoError(t, err)
	assert.Equal(t, int64(3), asset.ID)
}

func TestSelectAsset_NilRelease(t *testing.T) {
	t.Parallel()

	_, err := SelectAsset(nil, "linux", "amd64")
	require.ErrorIs(t, err, ErrNoAsset)
}

func TestVersionFromTag(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2.16.1", VersionFromTag("v2.16.1"))
	assert.Equal(t, "2.16.1", VersionFromTag("2.16.1"))
	assert.Equal(t, "2.17.0-beta1", VersionFromTag("v2.17.0-beta1"))
}

func TestFindRelease(t *testing.T) {
	t.Parallel()

	m := &Manifest{Releases: []*Release{
		nil,
		{TagName: "v2.16.0"},
		{TagName: "v2.16.1"},
	}}

	require.NotNil(t, FindRelease(m, "v2.16.1"))
	assert.Nil(t, FindRelease(m, "v2.99.0"))
	assert.Nil(t, FindRelease(nil, "v2.16.1"))
}

// The channel names and the archive prefix are wire values: the publisher writes
// them and the client matches on them, so a rename here is a fleet-wide break.
// The separate guard that the manifest's yaml field names still decode into
// go-selfupdate's HttpManifest is
// TestRun_ProducedManifestDecodesInGoSelfupdate, which runs against a manifest
// the generator actually produced.
func TestManifest_ChannelNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "stable", ChannelStable)
	assert.Equal(t, "beta", ChannelBeta)
	assert.True(t, strings.HasPrefix(ArchiveBaseName("linux", "amd64", "2.16.1"), "zaparoo-"))
}
