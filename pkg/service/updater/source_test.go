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

package updater

import (
	"crypto/ed25519"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testRepoPath = "/ZaparooProject/zaparoo-core"
	// testLastModified is a fixed HTTP-date; nothing under test reads a clock.
	testLastModified = "Mon, 17 Aug 2026 01:26:54 GMT"
)

// manifestServer serves a signed manifest, counting how often each document was
// actually fetched so conditional-GET behaviour is observable.
type manifestServer struct {
	*httptest.Server
	body         atomic.Pointer[[]byte]
	etag         atomic.Pointer[string]
	lastModified atomic.Pointer[string]
	priv         ed25519.PrivateKey
	pub          ed25519.PublicKey
	manifestGets atomic.Int64
	manifest304s atomic.Int64
	sigGets      atomic.Int64
	sigStatus    atomic.Int64
}

func newManifestServer(t *testing.T, body string) *manifestServer {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	ms := &manifestServer{pub: pub, priv: priv}
	ms.setBody(body, `"v1"`)
	ms.setLastModified(testLastModified)

	ms.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := *ms.body.Load()
		switch r.URL.Path {
		case testRepoPath + "/" + manifestSigFileName:
			ms.sigGets.Add(1)
			if status := ms.sigStatus.Load(); status != 0 {
				w.WriteHeader(int(status))
				return
			}
			_, _ = w.Write(ed25519.Sign(ms.priv, data))
		case testRepoPath + "/" + manifestFileName:
			ms.manifestGets.Add(1)
			etag := *ms.etag.Load()
			lastMod := *ms.lastModified.Load()
			if etag != "" {
				w.Header().Set("ETag", etag)
			}
			if lastMod != "" {
				w.Header().Set("Last-Modified", lastMod)
			}
			// RFC 7232 has a server that offers both prefer If-None-Match, so
			// only one validator is consulted per request.
			switch {
			case etag != "":
				if r.Header.Get("If-None-Match") == etag {
					ms.manifest304s.Add(1)
					w.WriteHeader(http.StatusNotModified)
					return
				}
			case lastMod != "":
				if r.Header.Get("If-Modified-Since") == lastMod {
					ms.manifest304s.Add(1)
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
			_, _ = w.Write(data)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ms.Close)

	return ms
}

func (ms *manifestServer) setBody(body, etag string) {
	data := []byte(body)
	ms.body.Store(&data)
	ms.etag.Store(&etag)
}

func (ms *manifestServer) setLastModified(lastModified string) {
	ms.lastModified.Store(&lastModified)
}

// source returns a source pointed at this server, verifying against the key
// pair the server signs with rather than an embedded production one.
func (ms *manifestServer) source(stateDir, platformID, goarch string) *verifiedSource {
	return &verifiedSource{
		baseURL:    ms.URL,
		transport:  http.DefaultTransport.(*http.Transport).Clone(),
		stateDir:   stateDir,
		platformID: platformID,
		goarch:     goarch,
		key:        &keyRef{},
		verify: func(data, sig []byte) (*otameta.Manifest, error) {
			if !ed25519.Verify(ms.pub, data, sig) {
				return nil, otameta.ErrBadSignature
			}
			return otameta.Parse(data)
		},
	}
}

func testRepo() selfupdate.Repository {
	return selfupdate.NewRepositorySlug("ZaparooProject", "zaparoo-core")
}

// twoReleaseManifest carries a full-matrix release and an older one that
// predates a platform, which is the shape the live manifest actually has.
func twoReleaseManifest(generation int64) string {
	const dl = "https://github.com/ZaparooProject/zaparoo-core/releases/download"
	return fmt.Sprintf(`manifest_version: 1
generation: %d
issued_at: 2026-08-17T02:00:00Z
key_id: test1
last_release_id: 2
last_asset_id: 40
releases:
  - id: 2
    name: v2.16.1
    tag_name: v2.16.1
    channel: stable
    published_at: 2026-08-10T00:00:00Z
    release_notes: newest
    url: https://github.com/ZaparooProject/zaparoo-core/releases/tag/v2.16.1
    assets:
      - id: 20
        name: zaparoo-linux_amd64-2.16.1.tar.gz
        size: 8123456
        sha256: aaaa
        url: %[2]s/v2.16.1/zaparoo-linux_amd64-2.16.1.tar.gz
      - id: 21
        name: zaparoo-zapos_arm64-2.16.1.tar.gz
        size: 8123456
        url: %[2]s/v2.16.1/zaparoo-zapos_arm64-2.16.1.tar.gz
      - id: 22
        name: checksums.txt
        url: checksums.txt
      - id: 23
        name: checksums.txt.sig
        url: checksums.txt.sig
  - id: 1
    name: v2.15.1
    tag_name: v2.15.1
    channel: stable
    published_at: 2026-06-01T00:00:00Z
    release_notes: older
    assets:
      - id: 10
        name: zaparoo-linux_amd64-2.15.1.tar.gz
        url: %[2]s/v2.15.1/zaparoo-linux_amd64-2.15.1.tar.gz
      - id: 11
        name: checksums.txt
        url: checksums.txt
`, generation, dl)
}

func TestVerifiedSource_ListReleases(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	src := ms.source(t.TempDir(), "linux", "amd64")

	releases, err := src.ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	require.Len(t, releases, 2)

	// Each release keeps only this device's archive plus the metadata assets.
	newest := releases[0]
	assert.Equal(t, "v2.16.1", newest.GetTagName())
	assert.Equal(t, []string{
		"zaparoo-linux_amd64-2.16.1.tar.gz",
		"checksums.txt",
		"checksums.txt.sig",
	}, assetNames(newest))

	// The key the manifest named is what the checksums validator must use.
	assert.Equal(t, "test1", src.key.get())
}

// Another platform's archive must never reach go-selfupdate, so nothing
// downstream can pick one by accident.
func TestVerifiedSource_DropsOtherPlatformArchives(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))

	releases, err := ms.source(t.TempDir(), "zapos", "arm64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	require.NotEmpty(t, releases)

	assert.Equal(t, []string{
		"zaparoo-zapos_arm64-2.16.1.tar.gz",
		"checksums.txt",
		"checksums.txt.sig",
	}, assetNames(releases[0]))
}

// A release predating a platform is skipped rather than failing the whole
// manifest — one unavailable release must not stop every other device updating.
func TestVerifiedSource_SkipsReleasesWithoutAnAsset(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))

	releases, err := ms.source(t.TempDir(), "zapos", "arm64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	require.Len(t, releases, 1)
	assert.Equal(t, "v2.16.1", releases[0].GetTagName())
}

// The manifest's explicit channel decides prerelease, not GitHub's flag, so
// moving a release between channels is a publish decision.
func TestVerifiedSource_ChannelDrivesPrerelease(t *testing.T) {
	t.Parallel()

	body := strings.Replace(twoReleaseManifest(412), "channel: stable", "channel: beta", 1)
	ms := newManifestServer(t, body)

	releases, err := ms.source(t.TempDir(), "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	require.Len(t, releases, 2)
	assert.True(t, releases[0].GetPrerelease())
	assert.False(t, releases[1].GetPrerelease())
}

// Relative metadata URLs resolve against the repository base, matching how
// go-selfupdate's own source handles them; absolute archive URLs pass through.
func TestVerifiedSource_ResolvesRelativeAssetURLs(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))

	releases, err := ms.source(t.TempDir(), "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	require.NotEmpty(t, releases)

	assets := releases[0].GetAssets()
	require.Len(t, assets, 3)
	assert.Equal(t,
		"https://github.com/ZaparooProject/zaparoo-core/releases/download/v2.16.1/"+
			"zaparoo-linux_amd64-2.16.1.tar.gz",
		assets[0].GetBrowserDownloadURL())
	assert.Equal(t, ms.URL+testRepoPath+"/checksums.txt", assets[1].GetBrowserDownloadURL())
}

func TestVerifiedSource_RejectsBadSignature(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	src := ms.source(t.TempDir(), "linux", "amd64")

	// A key that did not sign anything the server serves.
	other, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	src.verify = func(data, sig []byte) (*otameta.Manifest, error) {
		if !ed25519.Verify(other, data, sig) {
			return nil, otameta.ErrBadSignature
		}
		return otameta.Parse(data)
	}

	_, err = src.ListReleases(t.Context(), testRepo())
	require.ErrorIs(t, err, otameta.ErrBadSignature)
}

func TestVerifiedSource_AdvancesWatermark(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))

	_, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)

	st := loadState(dir)
	assert.Equal(t, int64(412), st.ManifestGeneration)
	assert.Equal(t, `"v1"`, st.ManifestETag)
	assert.Equal(t, testLastModified, st.ManifestLastModified)
	assert.False(t, st.ManifestSeenAt.IsZero())
	assert.Equal(t, []byte(twoReleaseManifest(412)), loadCachedManifest(dir))
}

// Signed metadata cannot be forged, but a replayed older copy can still hide a
// release that fixes a problem.
func TestVerifiedSource_RejectsGenerationRollback(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))

	_, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)

	ms.setBody(twoReleaseManifest(411), `"v0"`)

	_, err = ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.ErrorIs(t, err, ErrGenerationRollback)

	// A rejected fetch must not move the watermark backwards.
	assert.Equal(t, int64(412), loadState(dir).ManifestGeneration)
}

// A byte-identical republish is not an attack.
func TestVerifiedSource_AcceptsSameGeneration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))

	_, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)

	ms.setBody(twoReleaseManifest(412), `"v1-again"`)

	releases, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	assert.Len(t, releases, 2)
}

// The second check should send If-None-Match and fall back to the cached bytes
// on a 304, while still fetching the signature fresh and re-verifying.
func TestVerifiedSource_ConditionalGETUsesCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))

	_, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)

	releases, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	require.Len(t, releases, 2)

	assert.Equal(t, int64(2), ms.manifestGets.Load())
	assert.Equal(t, int64(1), ms.manifest304s.Load(),
		"the second check should have been answered from cache via If-None-Match")
	assert.Equal(t, int64(2), ms.sigGets.Load(), "the signature must be fetched fresh every check")
}

// The production shape: Bunny never sends an ETag, because it only forwards one
// the origin sends and Bunny Storage sends none. Last-Modified alone has to be
// enough to reuse the cache, or every check re-downloads the whole manifest.
func TestVerifiedSource_ConditionalGETWithoutETag(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))
	ms.setBody(twoReleaseManifest(412), "")

	_, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)

	st := loadState(dir)
	require.Empty(t, st.ManifestETag)
	require.Equal(t, testLastModified, st.ManifestLastModified)

	releases, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	assert.Len(t, releases, 2)
	assert.Equal(t, int64(1), ms.manifest304s.Load(),
		"the second check should have been answered from cache via If-Modified-Since")
	assert.Equal(t, int64(2), ms.sigGets.Load(), "the signature must be fetched fresh every check")
}

// A CDN caches the manifest and its signature as independent objects, so there
// is a window after a republish where the two can disagree. The cached bytes
// must lose to the fresh signature rather than being accepted: a check that
// fails is recoverable on the next one, a mismatched pair being trusted is not.
func TestVerifiedSource_RejectsCachedManifestAgainstNewerSignature(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))
	ms.setBody(twoReleaseManifest(412), "")

	_, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)

	// The body moves on, but Last-Modified does not, so the manifest is still
	// answered 304 while the signature is re-fetched over the new bytes.
	ms.setBody(twoReleaseManifest(413), "")

	_, err = ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.ErrorIs(t, err, otameta.ErrBadSignature)
	require.Equal(t, int64(1), ms.manifest304s.Load())

	// The rejected fetch must leave the watermark and the cache as they were.
	assert.Equal(t, int64(412), loadState(dir).ManifestGeneration)
	assert.Equal(t, []byte(twoReleaseManifest(412)), loadCachedManifest(dir))
}

// A changed Last-Modified means the manifest moved, so the cache must not be
// reused even though the request was conditional.
func TestVerifiedSource_NewLastModifiedRefetches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))
	ms.setBody(twoReleaseManifest(412), "")

	_, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)

	ms.setBody(twoReleaseManifest(413), "")
	ms.setLastModified("Tue, 18 Aug 2026 09:00:00 GMT")

	releases, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	assert.Len(t, releases, 2)

	st := loadState(dir)
	assert.Equal(t, int64(413), st.ManifestGeneration, "the newer manifest must have been read")
	assert.Equal(t, "Tue, 18 Aug 2026 09:00:00 GMT", st.ManifestLastModified)
	assert.Equal(t, []byte(twoReleaseManifest(413)), loadCachedManifest(dir))
}

// An ETag with no cached bytes behind it must produce a full refetch, not a 304
// with nothing to pair it with.
func TestVerifiedSource_MissingCacheRefetches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))

	_, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, manifestCacheName)))

	releases, err := ms.source(dir, "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	assert.Len(t, releases, 2)
	assert.Equal(t, []byte(twoReleaseManifest(412)), loadCachedManifest(dir),
		"the cache should be rewritten from the served bytes")
}

func TestVerifiedSource_RejectsOversizedManifest(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	ms.setBody(strings.Repeat("#", maxManifestBytes+1), `"big"`)

	_, err := ms.source(t.TempDir(), "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger than")
}

func TestVerifiedSource_SignatureFetchFailure(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	ms.sigStatus.Store(http.StatusNotFound)

	_, err := ms.source(t.TempDir(), "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching manifest signature")
	assert.Equal(t, int64(0), ms.manifestGets.Load(), "no manifest is fetched without a signature")
}

// With no state directory there is no watermark and no cache, but checks still
// work and are still verified.
func TestVerifiedSource_NoStateDir(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))

	releases, err := ms.source("", "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	assert.Len(t, releases, 2)
}

// An ambiguous release is skipped rather than guessed at, and the next best
// release is still offered.
func TestVerifiedSource_SkipsAmbiguousRelease(t *testing.T) {
	t.Parallel()

	body := strings.Replace(twoReleaseManifest(412),
		"name: zaparoo-zapos_arm64-2.16.1.tar.gz",
		"name: zaparoo-linux_amd64-2.16.1.zip", 1)
	ms := newManifestServer(t, body)

	releases, err := ms.source(t.TempDir(), "linux", "amd64").ListReleases(t.Context(), testRepo())
	require.NoError(t, err)
	require.Len(t, releases, 1)
	assert.Equal(t, "v2.15.1", releases[0].GetTagName())
}

func TestIsReleaseArchive(t *testing.T) {
	t.Parallel()

	assert.True(t, isReleaseArchive("zaparoo-linux_amd64-2.16.1.tar.gz"))
	assert.True(t, isReleaseArchive("zaparoo-mister_arm-2.16.1.zip"))
	assert.False(t, isReleaseArchive("checksums.txt"))
	assert.False(t, isReleaseArchive("checksums.txt.sig"))
	assert.False(t, isReleaseArchive("zaparoo-linux_amd64-2.16.1.tar.gz.sig"))
	assert.False(t, isReleaseArchive("zaparoo-"))
	assert.False(t, isReleaseArchive(""))
}

func TestResolveAssetURL(t *testing.T) {
	t.Parallel()

	base := "https://updates.zaparoo.org/ZaparooProject/zaparoo-core"
	assert.Equal(t, "https://example.com/a.zip", resolveAssetURL("https://example.com/a.zip", base))
	assert.Equal(t, base+"/checksums.txt", resolveAssetURL("checksums.txt", base))
	assert.Empty(t, resolveAssetURL("", base))
}

func assetNames(rel selfupdate.SourceRelease) []string {
	assets := rel.GetAssets()
	names := make([]string, 0, len(assets))
	for _, a := range assets {
		names = append(names, a.GetName())
	}
	return names
}
