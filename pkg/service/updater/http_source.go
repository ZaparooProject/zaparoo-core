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
	"fmt"
	"io"
	"net/http"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

type validationChainHTTPSource struct {
	source    selfupdate.Source
	transport *http.Transport
}

func (s *validationChainHTTPSource) ListReleases(
	ctx context.Context,
	repository selfupdate.Repository,
) ([]selfupdate.SourceRelease, error) {
	return s.source.ListReleases(ctx, repository)
}

func (s *validationChainHTTPSource) DownloadReleaseAsset(
	ctx context.Context,
	rel *selfupdate.Release,
	assetID int64,
) (io.ReadCloser, error) {
	if rel == nil {
		return nil, selfupdate.ErrInvalidRelease
	}
	if rel.AssetID == assetID || rel.ValidationAssetID == assetID {
		return s.source.DownloadReleaseAsset(ctx, rel, assetID)
	}

	for _, validationAsset := range rel.ValidationChain {
		if validationAsset.ValidationAssetID == assetID {
			return s.downloadURL(ctx, validationAsset.ValidationAssetURL)
		}
	}

	return nil, fmt.Errorf("asset ID %d: %w", assetID, selfupdate.ErrAssetNotFound)
}

func (s *validationChainHTTPSource) downloadURL(ctx context.Context, url string) (io.ReadCloser, error) {
	if url == "" {
		return nil, selfupdate.ErrAssetNotFound
	}

	client := &http.Client{Transport: s.transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("HTTP request failed with status code %d", res.StatusCode)
	}

	return res.Body, nil
}

var _ selfupdate.Source = (*validationChainHTTPSource)(nil)
