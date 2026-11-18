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
	"encoding/base64"
	"fmt"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/uploads"
	"github.com/spf13/afero"
)

// uploadStagingDirName is the folder in the user images folder that holds
// uploads still being received.
const uploadStagingDirName = "uploads"

// uploadManagers holds the upload sessions of each data directory. A Core
// process has one; tests have one per platform they build.
var (
	uploadManagersMu syncutil.Mutex
	uploadManagers   = make(map[string]*uploads.Manager)
)

// uploadManager returns the upload sessions of the request's platform, with
// the purposes this package's methods take uploads for.
func uploadManager(env *requests.RequestEnv) *uploads.Manager {
	dir := UserImagesDir(env.Platform)
	uploadManagersMu.Lock()
	defer uploadManagersMu.Unlock()
	if manager, ok := uploadManagers[dir]; ok {
		return manager
	}
	manager := uploads.NewManager(afero.NewOsFs())
	manager.Register(uploads.Purpose{
		Name:     UploadPurposeMediaImage,
		MaxBytes: imagefiles.MaxBytes,
		// A folder of its own: pruning the images folder must not remove a
		// staged upload.
		StagingDir: filepath.Join(dir, uploadStagingDirName),
		Validate:   validateUploadedImage,
	})
	uploadManagers[dir] = manager
	return manager
}

// validateUploadedImage accepts a staged upload that is a supported image.
func validateUploadedImage(fs afero.Fs, path string) error {
	data, err := readMediaBinaryFile(fs, path, imagefiles.MaxBytes)
	if err != nil {
		return fmt.Errorf("read uploaded image: %w", err)
	}
	if err := checkMediaUserImage(data); err != nil {
		return fmt.Errorf("uploaded image: %w", err)
	}
	return nil
}

func uploadResponse(info uploads.Info) models.UploadResponse {
	return models.UploadResponse{
		UploadID: info.ID, Purpose: info.Purpose, Size: info.Size, Received: info.Received,
		ChunkSize: info.ChunkSize, Finished: info.Finished,
	}
}

// uploadEnv checks that the request may upload at all: the device itself or
// a paired client, never a stranger on the network.
func uploadEnv(env *requests.RequestEnv) error {
	if env.Platform == nil {
		return models.ClientErrf("uploads are not available")
	}
	return requireAuthenticated(env)
}

// HandleUploadsBegin opens an upload session for a file of a given purpose,
// size and SHA-256.
func HandleUploadsBegin(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	if err := uploadEnv(&env); err != nil {
		return nil, err
	}
	var params models.UploadsBeginParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	manager := uploadManager(&env)
	purpose, ok := manager.Purpose(params.Purpose)
	if !ok {
		return nil, models.ClientErrf("%w: %q", uploads.ErrUnknownPurpose, params.Purpose)
	}
	if purpose.AdminOnly && !isLocalOrAdmin(&env) {
		return nil, models.ClientErrf("%w", ErrForbidden)
	}
	info, err := manager.Begin(params.Purpose, env.ClientID, params.Size, params.SHA256)
	if err != nil {
		return nil, uploadClientError(err)
	}
	return uploadResponse(info), nil
}

// HandleUploadsChunk appends one base64 chunk to an upload session.
func HandleUploadsChunk(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	if err := uploadEnv(&env); err != nil {
		return nil, err
	}
	var params models.UploadsChunkParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if base64.StdEncoding.DecodedLen(len(params.Data)) > uploads.ChunkBytes+2 {
		return nil, models.ClientErr(uploads.ErrChunkTooLarge)
	}
	data, err := base64.StdEncoding.DecodeString(params.Data)
	if err != nil {
		return nil, models.ClientErrf("invalid params: data is not base64: %w", err)
	}
	info, err := uploadManager(&env).Chunk(params.UploadID, env.ClientID, *params.Offset, data)
	if err != nil {
		return nil, uploadClientError(err)
	}
	return uploadResponse(info), nil
}

// HandleUploadsStatus reports an upload session, so a client that lost its
// connection can resume from the received offset.
func HandleUploadsStatus(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	return handleUploadByID(&env, func(manager *uploads.Manager, id string) (uploads.Info, error) {
		return manager.Status(id, env.ClientID)
	})
}

// HandleUploadsFinish verifies a fully received upload, after which the
// method it was made for can take it by its upload ID.
func HandleUploadsFinish(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	return handleUploadByID(&env, func(manager *uploads.Manager, id string) (uploads.Info, error) {
		return manager.Finish(id, env.ClientID)
	})
}

// HandleUploadsCancel discards an upload session.
func HandleUploadsCancel(env requests.RequestEnv) (any, error) { //nolint:gocritic // API handler shape
	if err := uploadEnv(&env); err != nil {
		return nil, err
	}
	var params models.UploadsIDParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	if err := uploadManager(&env).Cancel(params.UploadID, env.ClientID); err != nil {
		return nil, uploadClientError(err)
	}
	return NoContent{}, nil
}

func handleUploadByID(
	env *requests.RequestEnv, call func(manager *uploads.Manager, id string) (uploads.Info, error),
) (any, error) {
	if err := uploadEnv(env); err != nil {
		return nil, err
	}
	var params models.UploadsIDParams
	if err := validation.ValidateAndUnmarshal(env.Params, &params); err != nil {
		return nil, models.ClientErrf("invalid params: %w", err)
	}
	info, err := call(uploadManager(env), params.UploadID)
	if err != nil {
		return nil, uploadClientError(err)
	}
	return uploadResponse(info), nil
}
