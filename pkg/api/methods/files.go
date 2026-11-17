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

package methods

import (
	"errors"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/hostfiles"
)

const (
	filesBrowseDefaultLimit = 100
	// filesImageTypeTag labels a files.image response, which is not a
	// property of any media.
	filesImageTypeTag = "file:image"
)

// hostBrowser returns the browser over the request's index roots and image
// folder. Browsing is for the device itself and paired clients, never a
// stranger on the network.
func hostBrowser(env *requests.RequestEnv) (*hostfiles.Browser, error) {
	if env.Platform == nil || env.Config == nil {
		return nil, models.ClientErrf("file browsing is not available")
	}
	if err := requireAuthenticated(env); err != nil {
		return nil, err
	}
	return hostfiles.NewBrowser(
		env.Platform.RootDirs(env.Config), env.Config.ImageDir(helpers.DataDir(env.Platform)),
	), nil
}

// hostFilesClientError turns a browse error a client caused into a client
// error.
func hostFilesClientError(err error) error {
	for _, clientErr := range []error{
		hostfiles.ErrUnknownRoot, hostfiles.ErrInvalidPath, hostfiles.ErrNotFound, hostfiles.ErrNotDir,
		hostfiles.ErrNotFile, hostfiles.ErrBusy, hostfiles.ErrTimeout,
	} {
		if errors.Is(err, clientErr) {
			return models.ClientErr(err)
		}
	}
	return err
}

// HandleFilesBrowse lists the folders of the Core host a client may browse,
// or the folders and images of one folder under them.
func HandleFilesBrowse(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	var params models.FilesBrowseParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	browser, err := hostBrowser(&env)
	if err != nil {
		return nil, err
	}
	if params.Root == "" {
		if params.Path != "" {
			return nil, models.ClientErrf("invalid params: path needs a root")
		}
		roots := browser.Roots()
		response := models.FilesBrowseResponse{
			Roots: make([]models.FilesRoot, 0, len(roots)), Entries: []models.FilesEntry{},
		}
		for _, root := range roots {
			response.Roots = append(response.Roots, models.FilesRoot{ID: root.ID, Name: root.Name})
		}
		return response, nil
	}

	listing, err := browser.ListImages(env.Context, params.Root, params.Path)
	if err != nil {
		return nil, hostFilesClientError(err)
	}
	offset, limit := 0, filesBrowseDefaultLimit
	if params.Offset != nil {
		offset = *params.Offset
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	total := len(listing.Entries)
	start := min(offset, total)
	page := listing.Entries[start:min(start+limit, total)]
	response := models.FilesBrowseResponse{
		Root: params.Root, Path: params.Path, Total: total, Truncated: listing.Truncated,
		Entries: make([]models.FilesEntry, 0, len(page)),
	}
	for i := range page {
		response.Entries = append(response.Entries, models.FilesEntry{
			Name: page[i].Name, Path: page[i].Path, IsDir: page[i].IsDir, Size: page[i].Size,
			Modified: page[i].Modified.UTC().Format(time.RFC3339),
		})
	}
	return response, nil
}

// HandleFilesImage returns one image found with files.browse, at full size
// or resized through the thumbnail cache, the way media.image does.
func HandleFilesImage(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	var params models.FilesImageParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	localPath := params.Delivery == mediaImageDeliveryPath
	if localPath && (params.MaxSize == nil || *params.MaxSize <= 0) {
		return nil, models.ClientErrf("files.image: localPath delivery requires a positive maxSize")
	}
	browser, err := hostBrowser(&env)
	if err != nil {
		return nil, err
	}
	path, err := browser.ResolveImage(params.Root, params.Path)
	if err != nil {
		return nil, hostFilesClientError(err)
	}
	maxSize := 0
	if params.MaxSize != nil {
		maxSize = int(snapThumbMaxSize(*params.MaxSize))
	}
	return serveDeckImage(env.Context, deckImageSource{localPath: path}, filesImageTypeTag, maxSize, localPath)
}
