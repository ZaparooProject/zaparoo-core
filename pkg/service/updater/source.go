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
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/rs/zerolog/log"
)

const (
	// maxManifestBytes caps the manifest read. Retention keeps the published
	// document to a few hundred kilobytes; the cap is what stops a compromised
	// or misbehaving CDN from making a device read until it runs out of memory.
	maxManifestBytes = 4 << 20

	manifestFileName    = "manifest.yaml"
	manifestSigFileName = "manifest.yaml.sig"

	manifestFetchTimeout = 60 * time.Second

	// responseHeaderTimeout bounds how long a request may sit before the server
	// starts answering. It is applied at the transport rather than as a client
	// deadline because an archive download on a MiSTer over a slow SD-card-backed
	// link can legitimately run for minutes: a stalled connection has to fail
	// fast without capping a slow-but-progressing transfer.
	responseHeaderTimeout = 30 * time.Second
)

// ErrGenerationRollback means the CDN served metadata older than something this
// device already accepted. Signed metadata cannot be forged, but a stale or
// deliberately replayed copy can still hide a release that fixes a problem, so
// it is refused rather than used.
var ErrGenerationRollback = errors.New("update manifest is older than one already seen")

// errManifestNotLoaded means a release was asked for before the manifest behind
// it was fetched. It is a programming error rather than a device condition.
var errManifestNotLoaded = errors.New("update manifest has not been loaded")

// verifiedSource is Zaparoo's CDN as an update source. It fetches the manifest
// and its detached signature, refuses anything that does not verify or that
// replays an older generation, and answers which release this device should be
// offered.
type verifiedSource struct {
	transport    *http.Transport
	verify       func(data, sig []byte) (*otameta.Manifest, error)
	manifest     *otameta.Manifest
	baseURL      string
	stateDir     string
	platformID   string
	goarch       string
	manifestBase string
}

// cacheValidators are the conditional-request tokens for a copy already on
// disk. Both are sent when known: servers that offer an ETag prefer it, and the
// ones that do not still answer If-Modified-Since.
type cacheValidators struct {
	etag         string
	lastModified string
}

// httpResult is a size-capped response body, or the fact that the cached copy
// is still current.
type httpResult struct {
	etag         string
	lastModified string
	body         []byte
	notModified  bool
}

// load fetches and verifies the manifest this device selects from. It is
// separate from selection because a check and the install that follows it both
// read the same fetched copy.
func (s *verifiedSource) load(ctx context.Context, owner, repo string) error {
	base, err := url.JoinPath(s.baseURL, owner, repo)
	if err != nil {
		return fmt.Errorf("building metadata URL: %w", err)
	}

	manifest, err := s.fetchManifest(ctx, base)
	if err != nil {
		return err
	}

	s.manifest = manifest
	s.manifestBase = base
	return nil
}

// releaseForVersion returns one release from the verified manifest with its
// asset URLs resolved to absolute ones.
func (s *verifiedSource) releaseForVersion(version string) (*otameta.Release, error) {
	if s.manifest == nil {
		return nil, errManifestNotLoaded
	}
	want, err := semver.NewVersion(version)
	if err != nil {
		return nil, fmt.Errorf("reading selected release version %q: %w", version, err)
	}

	for _, rel := range s.manifest.Releases {
		if rel == nil {
			continue
		}
		candidate, parseErr := semver.NewVersion(otameta.VersionFromTag(rel.TagName))
		if parseErr != nil || !candidate.Equal(want) {
			continue
		}
		return s.resolvedRelease(rel), nil
	}
	return nil, fmt.Errorf("selected release %s is missing from the verified manifest", version)
}

// resolvedRelease copies a release out of the verified manifest with its asset
// URLs made absolute, leaving the manifest's own copy untouched.
func (s *verifiedSource) resolvedRelease(rel *otameta.Release) *otameta.Release {
	resolved := *rel
	resolved.Assets = resolvedAssetCopies(rel.Assets, s.manifestBase)
	return &resolved
}

func (s *verifiedSource) manifestGeneration() int64 {
	if s.manifest == nil {
		return 0
	}
	return s.manifest.Generation
}

// fetchManifest retrieves the manifest and its detached signature, verifies
// them, and enforces the generation watermark. The signature is always fetched
// fresh; the manifest body is only re-downloaded when the CDN says it changed.
func (s *verifiedSource) fetchManifest(ctx context.Context, base string) (*otameta.Manifest, error) {
	sig, err := s.get(ctx, base+"/"+manifestSigFileName, cacheValidators{}, ed25519.SignatureSize)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest signature: %w", err)
	}
	if sig.notModified {
		return nil, errors.New("manifest signature request was answered with an unexpected 304")
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	st, stateErr := loadStateWithError(s.stateDir)
	if stateErr != nil {
		log.Warn().Err(stateErr).Msg("could not load updater state, treating as unseen without overwriting it")
		st = updaterState{}
	}
	cached := loadCachedManifest(s.stateDir)

	// Only conditional when there are bytes on disk for a 304 to refer back to.
	var validators cacheValidators
	if len(cached) > 0 {
		validators = cacheValidators{
			etag:         st.ManifestETag,
			lastModified: st.ManifestLastModified,
		}
	}

	res, err := s.get(ctx, base+"/"+manifestFileName, validators, maxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}

	data := res.body
	if res.notModified {
		log.Debug().Msg("update manifest unchanged since last check, using cached copy")
		data = cached
	}

	// Verified whether it came off the network or off disk. The cache is a
	// bandwidth optimisation, never a trust boundary.
	manifest, err := s.verify(data, sig.body)
	if err != nil {
		return nil, fmt.Errorf("verifying manifest: %w", err)
	}

	if manifest.Generation < st.ManifestGeneration {
		return nil, fmt.Errorf("%w: served generation %d, already accepted %d",
			ErrGenerationRollback, manifest.Generation, st.ManifestGeneration)
	}

	if stateErr == nil {
		s.persist(&st, manifest.Generation, res)
	}
	return manifest, nil
}

// persist advances the watermark and refreshes the cache. Nothing here is
// fatal: a device that cannot write its state still updates, it just re-reads
// the manifest next time and loses replay protection until the write succeeds.
func (s *verifiedSource) persist(st *updaterState, generation int64, res *httpResult) {
	if res.notModified && generation == st.ManifestGeneration {
		return
	}

	if !res.notModified {
		if err := saveCachedManifest(s.stateDir, res.body); err != nil {
			log.Warn().Err(err).Msg("could not cache verified update manifest")
			// Validators pointing at a manifest we failed to store would make
			// the next check take a 304 with nothing to pair it with.
			res.etag = ""
			res.lastModified = ""
		}
		st.ManifestETag = res.etag
		st.ManifestLastModified = res.lastModified
	}

	st.ManifestGeneration = generation
	st.ManifestSeenAt = time.Now().UTC()
	if err := saveState(s.stateDir, st); err != nil {
		log.Warn().Err(err).Msg("could not record update manifest generation")
	}
}

func resolvedAssetCopies(assets []*otameta.Asset, base string) []*otameta.Asset {
	resolved := make([]*otameta.Asset, 0, len(assets))
	for _, asset := range assets {
		if asset == nil {
			continue
		}
		copyAsset := *asset
		copyAsset.URL = resolveAssetURL(asset.URL, base)
		resolved = append(resolved, &copyAsset)
	}
	return resolved
}

// selectRelease returns the release this device should be offered: the highest
// version that is published, installable here, and on a channel this device
// follows. Nothing is offered when no release qualifies.
//
// Selection is by version and not by publish date, so a hotfix cut on an older
// line after a newer release cannot displace the newer one. A release with no
// archive for this device is skipped rather than treated as newest, which is
// what lets a platform added later coexist with releases predating it.
func (s *verifiedSource) selectRelease(channel string) (*otameta.Release, error) {
	if s.manifest == nil {
		return nil, errManifestNotLoaded
	}

	var (
		best    *otameta.Release
		bestVer *semver.Version
	)
	for _, rel := range s.manifest.Releases {
		version, ok := s.installableVersion(rel, channel)
		if !ok {
			continue
		}
		if best == nil || version.GreaterThan(bestVer) {
			best, bestVer = rel, version
		}
	}

	log.Debug().
		Int("releases", len(s.manifest.Releases)).
		Int64("generation", s.manifest.Generation).
		Str("platform", s.platformID).
		Str("arch", s.goarch).
		Str("channel", channel).
		Msg("verified update manifest")

	if best == nil {
		return nil, nil //nolint:nilnil // no release for this device is an answer, not a failure
	}
	// The release itself, not another lookup by its version: two entries can
	// carry versions that compare equal while sitting on different channels,
	// and a second pass by version would hand back whichever came first rather
	// than the one this device is allowed to install.
	return s.resolvedRelease(best), nil
}

// installableVersion reports the version of a release this device could install,
// or false when the release is not a candidate at all.
func (s *verifiedSource) installableVersion(rel *otameta.Release, channel string) (*semver.Version, bool) {
	if rel == nil || rel.Draft {
		return nil, false
	}
	// A beta device is offered stable releases too, because the beta channel is
	// what a device accepts rather than all it accepts. Unknown manifest values
	// are never treated as stable by default.
	switch rel.Channel {
	case otameta.ChannelStable:
		// Stable releases are accepted by both channels.
	case otameta.ChannelBeta:
		if channel != otameta.ChannelBeta {
			return nil, false
		}
	default:
		return nil, false
	}
	version, err := semver.NewVersion(otameta.VersionFromTag(rel.TagName))
	if err != nil {
		log.Debug().Str("release", rel.TagName).Msg("skipping release with an unreadable version")
		return nil, false
	}
	if _, err := otameta.SelectAsset(rel, s.platformID, s.goarch); err != nil {
		// Routine for a release predating this platform, and a deliberate dead
		// end for anything ambiguous: the release is skipped rather than guessed
		// at, and the next best one is offered instead.
		level := log.Debug()
		if errors.Is(err, otameta.ErrAmbiguousAsset) {
			level = log.Error()
		}
		level.Err(err).Str("release", rel.TagName).Msg("skipping release")
		return nil, false
	}
	return version, true
}

// get reads at most limit bytes from a URL. Accept-Encoding is deliberately
// left to the transport so responses are transparently gzipped on the wire and
// handed back as the original bytes the signature covers.
func (s *verifiedSource) get(
	ctx context.Context, target string, validators cacheValidators, limit int64,
) (*httpResult, error) {
	client := &http.Client{Timeout: manifestFetchTimeout, Transport: s.transport}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if validators.etag != "" {
		req.Header.Set("If-None-Match", validators.etag)
	}
	if validators.lastModified != "" {
		req.Header.Set("If-Modified-Since", validators.lastModified)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			log.Debug().Err(closeErr).Msg("closing update metadata response")
		}
	}()

	if res.StatusCode == http.StatusNotModified {
		return &httpResult{
			notModified:  true,
			etag:         validators.etag,
			lastModified: validators.lastModified,
		}, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d", res.StatusCode)
	}

	// One byte past the limit, so a body of exactly limit bytes is accepted and
	// anything longer is detected rather than silently truncated.
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response is larger than the %d byte limit", limit)
	}

	return &httpResult{
		body:         body,
		etag:         res.Header.Get("ETag"),
		lastModified: res.Header.Get("Last-Modified"),
	}, nil
}

// resolveAssetURL turns the manifest's relative metadata URLs into absolute
// ones. Release archives are already absolute GitHub URLs and pass through
// untouched.
func resolveAssetURL(raw, base string) string {
	if raw == "" {
		return ""
	}
	if _, err := url.ParseRequestURI(raw); err == nil {
		return raw
	}
	joined, err := url.JoinPath(base, raw)
	if err != nil {
		return raw
	}
	return joined
}
