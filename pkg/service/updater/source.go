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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	selfupdate "github.com/creativeprojects/go-selfupdate"
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

// verifiedSource is the go-selfupdate source for Zaparoo's CDN. It fetches and
// verifies the manifest itself rather than letting go-selfupdate parse an
// unverified document, and hands back only the releases this device may
// install, each reduced to the single archive that belongs to it.
type verifiedSource struct {
	transport *http.Transport
	key       *keyRef
	// verify is otameta.Verify in production; tests replace it so they can sign
	// with a generated key pair.
	verify     func(data, sig []byte) (*otameta.Manifest, error)
	baseURL    string
	stateDir   string
	platformID string
	goarch     string
}

func (s *verifiedSource) ListReleases(
	ctx context.Context,
	repository selfupdate.Repository,
) ([]selfupdate.SourceRelease, error) {
	owner, repo, err := repository.GetSlug()
	if err != nil {
		return nil, fmt.Errorf("reading repository slug: %w", err)
	}

	base, err := url.JoinPath(s.baseURL, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("building metadata URL: %w", err)
	}

	manifest, err := s.fetchManifest(ctx, base)
	if err != nil {
		return nil, err
	}

	// The checksum validator runs later in the same update and has to use the
	// key the publisher actually signed with, not a build-time default.
	s.key.set(manifest.KeyID)

	return s.releasesFor(manifest, base), nil
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

	st := loadState(s.stateDir)
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

	s.persist(&st, manifest.Generation, res)
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
	if err := saveState(s.stateDir, *st); err != nil {
		log.Warn().Err(err).Msg("could not record update manifest generation")
	}
}

// releasesFor reduces the manifest to what go-selfupdate should consider: the
// releases that publish an archive for this device, each carrying that one
// archive plus the metadata assets the validation chain needs.
func (s *verifiedSource) releasesFor(manifest *otameta.Manifest, base string) []selfupdate.SourceRelease {
	releases := make([]selfupdate.SourceRelease, 0, len(manifest.Releases))

	for _, rel := range manifest.Releases {
		if rel == nil {
			continue
		}

		archive, err := otameta.SelectAsset(rel, s.platformID, s.goarch)
		if err != nil {
			// Routine for a release predating this platform, and a deliberate
			// dead end for anything ambiguous: the release is skipped rather
			// than guessed at, and the next best one is offered instead.
			level := log.Debug()
			if errors.Is(err, otameta.ErrAmbiguousAsset) {
				level = log.Error()
			}
			level.Err(err).Str("release", rel.TagName).Msg("skipping release")
			continue
		}

		assets := make([]*selfupdate.HttpAsset, 0, len(rel.Assets))
		for _, a := range rel.Assets {
			if a == nil {
				continue
			}
			// Everything that is not this device's archive is metadata: the
			// checksums file and its signature. Other platforms' archives are
			// dropped so nothing downstream can pick one by accident.
			if a != archive && isReleaseArchive(a.Name) {
				continue
			}
			assets = append(assets, &selfupdate.HttpAsset{
				ID:   a.ID,
				Name: a.Name,
				Size: int(a.Size),
				URL:  resolveAssetURL(a.URL, base),
			})
		}

		releases = append(releases, &selfupdate.HttpRelease{
			ID:      rel.ID,
			Name:    rel.Name,
			TagName: rel.TagName,
			URL:     resolveAssetURL(rel.URL, base),
			Draft:   rel.Draft,
			// The manifest's explicit channel decides this, not GitHub's
			// prerelease flag, so moving a release between channels is a
			// publish decision rather than a property of the git tag.
			Prerelease:   rel.Channel == otameta.ChannelBeta,
			PublishedAt:  rel.PublishedAt,
			ReleaseNotes: rel.ReleaseNotes,
			Assets:       assets,
		})
	}

	log.Debug().
		Int("releases", len(releases)).
		Int64("generation", manifest.Generation).
		Str("platform", s.platformID).
		Str("arch", s.goarch).
		Msg("verified update manifest")

	return releases
}

// DownloadReleaseAsset fetches the archive or any link in its validation chain.
// go-selfupdate only tracks the first validation asset by URL, so the chain
// entries are looked up here.
func (s *verifiedSource) DownloadReleaseAsset(
	ctx context.Context,
	rel *selfupdate.Release,
	assetID int64,
) (io.ReadCloser, error) {
	if rel == nil {
		return nil, selfupdate.ErrInvalidRelease
	}

	downloadURL := ""
	switch {
	case rel.AssetID == assetID:
		downloadURL = rel.AssetURL
	case rel.ValidationAssetID == assetID:
		downloadURL = rel.ValidationAssetURL
	default:
		for _, link := range rel.ValidationChain {
			if link.ValidationAssetID == assetID {
				downloadURL = link.ValidationAssetURL
				break
			}
		}
	}

	if downloadURL == "" {
		return nil, fmt.Errorf("asset ID %d: %w", assetID, selfupdate.ErrAssetNotFound)
	}

	// No client deadline: the archive is the one response whose size is not
	// bounded by a small constant, so total duration is the caller's context to
	// bound. The transport's response-header timeout still catches a dead server.
	client := &http.Client{Transport: s.transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating asset request: %w", err)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading asset: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		if closeErr := res.Body.Close(); closeErr != nil {
			return nil, fmt.Errorf("asset request failed with status %d, and closing the body failed: %w",
				res.StatusCode, closeErr)
		}
		return nil, fmt.Errorf("asset request failed with status %d", res.StatusCode)
	}

	return res.Body, nil
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
// ones, matching how go-selfupdate's own HTTP source resolves them. Release
// archives are already absolute GitHub URLs and pass through untouched.
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

// isReleaseArchive reports whether a name looks like an installable archive
// for any platform, as opposed to a metadata file published alongside them.
func isReleaseArchive(name string) bool {
	if len(name) < len("zaparoo-") || name[:len("zaparoo-")] != "zaparoo-" {
		return false
	}
	for _, ext := range []string{".tar.gz", ".zip"} {
		if len(name) > len(ext) && name[len(name)-len(ext):] == ext {
			return true
		}
	}
	return false
}

var _ selfupdate.Source = (*verifiedSource)(nil)
