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
		verify: func(data, sig []byte) (*otameta.Manifest, error) {
			if !ed25519.Verify(ms.pub, data, sig) {
				return nil, otameta.ErrBadSignature
			}
			return otameta.Parse(data)
		},
	}
}

// loaded returns a source that has already fetched and verified the manifest,
// which is the state every selection question is asked in.
func (ms *manifestServer) loaded(t *testing.T, stateDir, platformID, goarch string) *verifiedSource {
	t.Helper()

	src := ms.source(stateDir, platformID, goarch)
	require.NoError(t, src.load(t.Context(), updateOwner, updateRepo))
	return src
}

// assertOffersNewest checks a loaded manifest still selects normally, which is
// what makes a cached copy usable rather than merely present.
func assertOffersNewest(t *testing.T, src *verifiedSource) {
	t.Helper()

	rel := offered(t, src, otameta.ChannelStable)
	require.NotNil(t, rel)
	assert.Equal(t, "v2.16.1", rel.TagName)
}

// offered is the release a device following channel is given, or nil when the
// manifest holds nothing for it.
func offered(t *testing.T, src *verifiedSource, channel string) *otameta.Release {
	t.Helper()

	rel, err := src.selectRelease(channel)
	require.NoError(t, err)
	return rel
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

// Selection is by version rather than by publish order, so the manifest listing
// the newest release first is not what makes it the one offered.
func TestVerifiedSource_SelectsTheNewestRelease(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	rel := offered(t, ms.loaded(t, t.TempDir(), "linux", "amd64"), otameta.ChannelStable)

	require.NotNil(t, rel)
	assert.Equal(t, "v2.16.1", rel.TagName)
	// The release is offered whole. Picking this device's archive out of it is
	// the install's job, and it does that against the same signed manifest.
	assert.Equal(t, []string{
		"zaparoo-linux_amd64-2.16.1.tar.gz",
		"zaparoo-zapos_arm64-2.16.1.tar.gz",
		"checksums.txt",
		"checksums.txt.sig",
	}, assetNames(rel))
}

// A release predating a platform is skipped rather than failing the whole
// manifest — one unavailable release must not stop every other device updating.
func TestVerifiedSource_SkipsReleasesWithoutAnAsset(t *testing.T) {
	t.Parallel()

	// Only the newer release carries a zapos archive.
	ms := newManifestServer(t, twoReleaseManifest(412))
	rel := offered(t, ms.loaded(t, t.TempDir(), "zapos", "arm64"), otameta.ChannelStable)

	require.NotNil(t, rel)
	assert.Equal(t, "v2.16.1", rel.TagName)
}

// A device no release was built for is offered nothing, which is an answer
// rather than a failure.
func TestVerifiedSource_OffersNothingWithoutAMatchingRelease(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	rel := offered(t, ms.loaded(t, t.TempDir(), "mister", "arm"), otameta.ChannelStable)

	assert.Nil(t, rel)
}

// The manifest's channel decides who is offered a release, so moving one
// between channels is a publish decision rather than a client one.
func TestVerifiedSource_ChannelDrivesSelection(t *testing.T) {
	t.Parallel()

	// Only the newest release moves to beta; the manifest lists it first.
	body := strings.Replace(twoReleaseManifest(412), "channel: stable", "channel: beta", 1)
	ms := newManifestServer(t, body)
	src := ms.loaded(t, t.TempDir(), "linux", "amd64")

	stable := offered(t, src, otameta.ChannelStable)
	require.NotNil(t, stable)
	assert.Equal(t, "v2.15.1", stable.TagName, "a stable device must not be offered a beta")

	// Beta is what a device accepts rather than all it accepts, so a beta
	// device is offered the newest release on either channel.
	beta := offered(t, src, otameta.ChannelBeta)
	require.NotNil(t, beta)
	assert.Equal(t, "v2.16.1", beta.TagName)
}

// Relative metadata URLs resolve against the repository base; absolute archive
// URLs pass through.
func TestVerifiedSource_ResolvesRelativeAssetURLs(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	rel := offered(t, ms.loaded(t, t.TempDir(), "linux", "amd64"), otameta.ChannelStable)

	require.NotNil(t, rel)
	require.Len(t, rel.Assets, 4)
	assert.Equal(t,
		"https://github.com/ZaparooProject/zaparoo-core/releases/download/v2.16.1/"+
			"zaparoo-linux_amd64-2.16.1.tar.gz",
		rel.Assets[0].URL)
	assert.Equal(t, ms.URL+testRepoPath+"/checksums.txt", rel.Assets[2].URL)
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

	err = src.load(t.Context(), updateOwner, updateRepo)
	require.ErrorIs(t, err, otameta.ErrBadSignature)
}

func TestVerifiedSource_AdvancesWatermark(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))

	err := ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
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

	err := ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.NoError(t, err)

	ms.setBody(twoReleaseManifest(411), `"v0"`)

	err = ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.ErrorIs(t, err, ErrGenerationRollback)

	// A rejected fetch must not move the watermark backwards.
	assert.Equal(t, int64(412), loadState(dir).ManifestGeneration)
}

// A byte-identical republish is not an attack.
func TestVerifiedSource_AcceptsSameGeneration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))

	err := ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.NoError(t, err)

	ms.setBody(twoReleaseManifest(412), `"v1-again"`)

	assertOffersNewest(t, ms.loaded(t, dir, "linux", "amd64"))
}

// The second check should send If-None-Match and fall back to the cached bytes
// on a 304, while still fetching the signature fresh and re-verifying.
func TestVerifiedSource_ConditionalGETUsesCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ms := newManifestServer(t, twoReleaseManifest(412))

	err := ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.NoError(t, err)

	assertOffersNewest(t, ms.loaded(t, dir, "linux", "amd64"))

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

	err := ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.NoError(t, err)

	st := loadState(dir)
	require.Empty(t, st.ManifestETag)
	require.Equal(t, testLastModified, st.ManifestLastModified)

	assertOffersNewest(t, ms.loaded(t, dir, "linux", "amd64"))
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

	err := ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.NoError(t, err)

	// The body moves on, but Last-Modified does not, so the manifest is still
	// answered 304 while the signature is re-fetched over the new bytes.
	ms.setBody(twoReleaseManifest(413), "")

	err = ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
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

	err := ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.NoError(t, err)

	ms.setBody(twoReleaseManifest(413), "")
	ms.setLastModified("Tue, 18 Aug 2026 09:00:00 GMT")

	assertOffersNewest(t, ms.loaded(t, dir, "linux", "amd64"))

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

	err := ms.source(dir, "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, manifestCacheName)))

	assertOffersNewest(t, ms.loaded(t, dir, "linux", "amd64"))
	assert.Equal(t, []byte(twoReleaseManifest(412)), loadCachedManifest(dir),
		"the cache should be rewritten from the served bytes")
}

func TestVerifiedSource_RejectsOversizedManifest(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	ms.setBody(strings.Repeat("#", maxManifestBytes+1), `"big"`)

	err := ms.source(t.TempDir(), "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger than")
}

func TestVerifiedSource_SignatureFetchFailure(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	ms.sigStatus.Store(http.StatusNotFound)

	err := ms.source(t.TempDir(), "linux", "amd64").load(t.Context(), updateOwner, updateRepo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching manifest signature")
	assert.Equal(t, int64(0), ms.manifestGets.Load(), "no manifest is fetched without a signature")
}

// With no state directory there is no watermark and no cache, but checks still
// work and are still verified.
func TestVerifiedSource_NoStateDir(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))

	assertOffersNewest(t, ms.loaded(t, "", "linux", "amd64"))
}

// An ambiguous release is skipped rather than guessed at, and the next best
// release is still offered.
func TestVerifiedSource_SkipsAmbiguousRelease(t *testing.T) {
	t.Parallel()

	body := strings.Replace(twoReleaseManifest(412),
		"name: zaparoo-zapos_arm64-2.16.1.tar.gz",
		"name: zaparoo-linux_amd64-2.16.1.zip", 1)
	ms := newManifestServer(t, body)

	rel := offered(t, ms.loaded(t, t.TempDir(), "linux", "amd64"), otameta.ChannelStable)
	require.NotNil(t, rel)
	assert.Equal(t, "v2.15.1", rel.TagName)
}

func TestResolvedAssetCopies(t *testing.T) {
	t.Parallel()

	archive := &otameta.Asset{ID: 1, Name: "zaparoo-linux_amd64-2.2.0.tar.gz", URL: "assets/core.tar.gz"}
	metadata := &otameta.Asset{ID: 2, Name: "checksums.txt", URL: "assets/checksums.txt"}
	resolved := resolvedAssetCopies(
		[]*otameta.Asset{nil, archive, metadata},
		"https://updates.example/releases",
	)

	require.Len(t, resolved, 2)
	assert.NotSame(t, archive, resolved[0])
	assert.Equal(t, "https://updates.example/releases/assets/core.tar.gz", resolved[0].URL)
	assert.Equal(t, "https://updates.example/releases/assets/checksums.txt", resolved[1].URL)
	assert.Equal(t, "assets/core.tar.gz", archive.URL, "resolving must not mutate signed manifest assets")
}

func TestResolveAssetURL(t *testing.T) {
	t.Parallel()

	base := "https://updates.zaparoo.org/ZaparooProject/zaparoo-core"
	assert.Equal(t, "https://example.com/a.zip", resolveAssetURL("https://example.com/a.zip", base))
	assert.Equal(t, base+"/checksums.txt", resolveAssetURL("checksums.txt", base))
	assert.Empty(t, resolveAssetURL("", base))
}

func assetNames(rel *otameta.Release) []string {
	names := make([]string, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		names = append(names, a.Name)
	}
	return names
}

func TestVerifiedSource_ReleaseForVersionNeedsALoadedManifest(t *testing.T) {
	t.Parallel()

	src := newManifestServer(t, twoReleaseManifest(412)).source(t.TempDir(), "linux", "amd64")

	_, err := src.releaseForVersion("2.16.1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has not been loaded")
	assert.Equal(t, int64(0), src.manifestGeneration())
}

// TestVerifiedSource_ReleaseForVersion covers the lookup selection and the
// rollout check both go through: a version names a release, and the release has
// to come back out of the manifest this process verified.
func TestVerifiedSource_ReleaseForVersion(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	src := ms.source(t.TempDir(), "linux", "amd64")
	err := src.load(t.Context(), updateOwner, updateRepo)
	require.NoError(t, err)
	assert.Equal(t, int64(412), src.manifestGeneration())

	rel, err := src.releaseForVersion("2.16.1")
	require.NoError(t, err)
	assert.Equal(t, "v2.16.1", rel.TagName)

	// Every asset comes back: the caller picks its own archive by name and still
	// needs the checksum files alongside it.
	names := make([]string, 0, len(rel.Assets))
	for _, asset := range rel.Assets {
		names = append(names, asset.Name)
	}
	assert.Equal(t, []string{
		"zaparoo-linux_amd64-2.16.1.tar.gz",
		"zaparoo-zapos_arm64-2.16.1.tar.gz",
		"checksums.txt",
		"checksums.txt.sig",
	}, names)

	// Manifest-relative URLs resolve against the manifest, and the cached
	// manifest keeps its original values so a second lookup resolves the same.
	assert.Equal(t, ms.URL+testRepoPath+"/checksums.txt", rel.Assets[2].URL)
	assert.Equal(t, "checksums.txt", src.manifest.Releases[0].Assets[2].URL,
		"resolving asset URLs must not rewrite the verified manifest")

	again, err := src.releaseForVersion("2.16.1")
	require.NoError(t, err)
	assert.Equal(t, ms.URL+testRepoPath+"/checksums.txt", again.Assets[2].URL)
}

// A version tag that is not in the manifest must fail the install rather than
// fall through to some other release.
func TestVerifiedSource_ReleaseForVersionRejectsUnknownVersions(t *testing.T) {
	t.Parallel()

	ms := newManifestServer(t, twoReleaseManifest(412))
	src := ms.source(t.TempDir(), "linux", "amd64")
	err := src.load(t.Context(), updateOwner, updateRepo)
	require.NoError(t, err)

	for name, version := range map[string]string{
		"absent from the manifest": "2.99.0",
		"not a version at all":     "not-a-version",
		"empty":                    "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := src.releaseForVersion(version)
			require.Error(t, err)
		})
	}
}

// A data directory that cannot be written is ordinary on read-only install
// media. Bookkeeping the check could not persist must not fail the check.
func TestVerifiedSource_ReadOnlyStateDirStillChecks(t *testing.T) {
	t.Parallel()
	skipUnlessDirPermsEnforced(t)

	ms := newManifestServer(t, twoReleaseManifest(412))
	dir := t.TempDir()
	makeDirUnwritable(t, dir)
	src := ms.source(dir, "linux", "amd64")

	require.NoError(t, src.load(t.Context(), updateOwner, updateRepo))
	assertOffersNewest(t, src)

	// Nothing was stored, so the next check has to fetch the manifest in full
	// rather than send validators it cannot pair with a cached body.
	require.NoError(t, src.load(t.Context(), updateOwner, updateRepo))
	assertOffersNewest(t, src)
	assert.Equal(t, int64(2), ms.manifestGets.Load())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
