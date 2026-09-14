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
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
)

const mediaAssetMaxChunkBytes = int64(512 * 1024)

const mediaAssetPDFProbeBytes = int64(1024)

type mediaAssetDefinition struct {
	typeTag     string
	contentType string
	extension   string
}

// mediaAssetDefinitions is deliberately an allowlist rather than a projection
// of every file-backed metadata property. New asset types need their own format
// validation before they can expose bytes to clients.
//
//nolint:gochecknoglobals // Immutable media asset policy.
var mediaAssetDefinitions = map[string]mediaAssetDefinition{
	"manual": {
		typeTag:     tags.PropertyTypeTag(tags.TagPropertyManual),
		contentType: "application/pdf",
		extension:   "pdf",
	},
}

type parsedMediaAssetRequest struct {
	definition mediaAssetDefinition
	ref        *mediaRefParam
	assetType  string
	etag       string
	offset     int64
	length     int64
}

// HandleMediaAsset returns one bounded base64 chunk from an allowlisted file
// property associated with indexed media.
//
//nolint:gocritic // RequestEnv is copied once at the API handler boundary.
func HandleMediaAsset(env requests.RequestEnv) (any, error) {
	params, err := parseMediaAssetRequest(env.Params)
	if err != nil {
		return nil, err
	}

	resolved, err := resolveMediaRefs(&env, []mediaRefParam{*params.ref})
	if err != nil {
		return nil, err
	}
	if resolved[0].Err != nil {
		return nil, resolved[0].Err
	}
	row := resolved[0].Row

	property, err := resolveMediaAssetProperty(&env, row, params.definition.typeTag)
	if err != nil {
		return nil, err
	}
	if property == nil || property.Text == "" || property.BlobDBID != nil {
		return nil, mediaAssetNotFoundError(params.assetType)
	}
	if !strings.EqualFold(filepath.Ext(property.Text), "."+params.definition.extension) {
		return nil, mediaAssetNotFoundError(params.assetType)
	}

	file, err := openTrustedMediaAsset(&env, row.System.SystemID, property.Text)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("media.asset: stat asset: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, mediaAssetNotFoundError(params.assetType)
	}
	if validationErr := validatePDFAsset(file, info.Size()); validationErr != nil {
		return nil, mediaAssetNotFoundError(params.assetType)
	}

	etag := mediaAssetETag(row, params.definition.typeTag, property.Text, info.Size(), info.ModTime().UnixNano())
	if params.etag != "" && params.etag != etag {
		return nil, models.ClientErrf("media.asset: asset changed; restart from offset 0")
	}
	if params.offset > info.Size() {
		return nil, models.ClientErrf("media.asset: offset exceeds asset size")
	}
	if contextErr := env.Context.Err(); contextErr != nil {
		return nil, contextErr
	}

	readLength := min(params.length, info.Size()-params.offset)
	data := make([]byte, int(readLength))
	if readLength > 0 {
		n, readErr := file.ReadAt(data, params.offset)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, fmt.Errorf("media.asset: read asset chunk: %w", readErr)
		}
		data = data[:n]
		readLength = int64(n)
	}
	if contextErr := env.Context.Err(); contextErr != nil {
		return nil, contextErr
	}

	afterInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("media.asset: restat asset: %w", err)
	}
	if afterInfo.Size() != info.Size() || afterInfo.ModTime().UnixNano() != info.ModTime().UnixNano() {
		return nil, models.ClientErrf("media.asset: asset changed; restart from offset 0")
	}

	next := params.offset + readLength
	complete := next >= info.Size()
	var nextOffset *int64
	if !complete {
		nextOffset = &next
	}

	return models.MediaAssetResponse{
		NextOffset:  nextOffset,
		AssetType:   params.assetType,
		TypeTag:     params.definition.typeTag,
		ContentType: params.definition.contentType,
		Extension:   params.definition.extension,
		ETag:        etag,
		Data:        base64.StdEncoding.EncodeToString(data),
		Size:        info.Size(),
		Offset:      params.offset,
		Length:      readLength,
		Complete:    complete,
	}, nil
}

func parseMediaAssetRequest(raw json.RawMessage) (parsedMediaAssetRequest, error) {
	var params models.MediaAssetParams
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		return parsedMediaAssetRequest{}, models.ClientErrf("invalid params: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return parsedMediaAssetRequest{}, models.ClientErrf("invalid params: %w", err)
	}

	definition, ok := mediaAssetDefinitions[params.AssetType]
	if !ok {
		return parsedMediaAssetRequest{}, models.ClientErrf("media.asset: unsupported assetType %q", params.AssetType)
	}
	ref := mediaRefParam{MediaID: params.MediaID, System: params.System, Path: params.Path}
	if err := validateMediaRef(ref); err != nil {
		return parsedMediaAssetRequest{}, models.ClientErrf("invalid params: %w", err)
	}

	offset := int64(0)
	if params.Offset != nil {
		offset = *params.Offset
	}
	if offset < 0 {
		return parsedMediaAssetRequest{}, models.ClientErrf("invalid params: offset must be non-negative")
	}
	length := mediaAssetMaxChunkBytes
	if params.Length != nil {
		length = *params.Length
	}
	if length <= 0 || length > mediaAssetMaxChunkBytes {
		return parsedMediaAssetRequest{}, models.ClientErrf(
			"invalid params: length must be between 1 and %d", mediaAssetMaxChunkBytes,
		)
	}
	if offset > 0 && params.ETag == "" {
		return parsedMediaAssetRequest{}, models.ClientErrf("invalid params: etag is required when offset is non-zero")
	}

	return parsedMediaAssetRequest{
		definition: definition,
		ref:        &ref,
		assetType:  params.AssetType,
		etag:       params.ETag,
		offset:     offset,
		length:     length,
	}, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return errors.New("multiple JSON values")
}

func resolveMediaAssetProperty(
	env *requests.RequestEnv,
	row *database.MediaFullRow,
	typeTag string,
) (*database.MediaProperty, error) {
	mediaIDs, err := equivalentMediaIDs(env, row)
	if err != nil {
		return nil, err
	}

	var mediaPropsByID map[int64][]database.MediaProperty
	if len(mediaIDs) == 1 {
		props, propErr := env.Database.MediaDB.GetMediaPropertyMetadata(env.Context, row.DBID)
		if propErr != nil {
			return nil, fmt.Errorf("media.asset: get media properties: %w", propErr)
		}
		mediaPropsByID = map[int64][]database.MediaProperty{row.DBID: props}
	} else {
		mediaPropsByID, err = env.Database.MediaDB.GetMediaPropertyMetadataByMediaDBIDs(env.Context, mediaIDs)
		if err != nil {
			return nil, fmt.Errorf("media.asset: get alias properties: %w", err)
		}
	}
	for _, mediaID := range mediaIDs {
		if prop := findMediaAssetProperty(mediaPropsByID[mediaID], typeTag); prop != nil {
			return prop, nil
		}
	}

	titleProps, err := env.Database.MediaDB.GetMediaTitlePropertyMetadata(env.Context, row.Title.DBID)
	if err != nil {
		return nil, fmt.Errorf("media.asset: get title properties: %w", err)
	}
	return findMediaAssetProperty(titleProps, typeTag), nil
}

func findMediaAssetProperty(props []database.MediaProperty, typeTag string) *database.MediaProperty {
	for i := range props {
		if props[i].TypeTag == typeTag {
			return &props[i]
		}
	}
	return nil
}

func mediaAssetNotFoundError(assetType string) error {
	//nolint:wrapcheck // QuietClientError is the intentional API boundary error.
	return models.QuietClientErrf("media.asset: no %s asset found", assetType)
}

func validatePDFAsset(file *os.File, size int64) error {
	if size <= 0 {
		return errors.New("empty PDF")
	}
	probeLength := min(size, mediaAssetPDFProbeBytes)
	probe := make([]byte, int(probeLength))
	n, err := file.ReadAt(probe, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read PDF header: %w", err)
	}
	if !bytes.Contains(probe[:n], []byte("%PDF-")) {
		return errors.New("missing PDF header")
	}
	return nil
}

func mediaAssetETag(
	row *database.MediaFullRow,
	typeTag, sourcePath string,
	size, modTimeNanos int64,
) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(
		h, "%s\x00%s\x00%s\x00%s\x00%d\x00%d",
		row.System.SystemID, filepath.ToSlash(row.Path), typeTag,
		filepath.ToSlash(sourcePath), size, modTimeNanos,
	)
	return hex.EncodeToString(h.Sum(nil))
}

func openTrustedMediaAsset(
	env *requests.RequestEnv,
	systemID string,
	assetPath string,
) (*os.File, error) {
	if !filepath.IsAbs(assetPath) {
		return nil, mediaAssetNotFoundError("requested")
	}
	assetPath = filepath.Clean(assetPath)

	roots, err := trustedMediaAssetRoots(env, systemID)
	if err != nil {
		return nil, err
	}
	for _, rootPath := range roots {
		rel, relErr := filepath.Rel(rootPath, assetPath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}

		root, openRootErr := os.OpenRoot(rootPath)
		if openRootErr != nil {
			continue
		}
		file, openErr := root.Open(rel)
		_ = root.Close()
		if openErr == nil {
			return file, nil
		}
	}
	return nil, mediaAssetNotFoundError("requested")
}

func trustedMediaAssetRoots(env *requests.RequestEnv, systemID string) ([]string, error) {
	roots := make([]string, 0, 16)
	if env.Platform != nil {
		roots = append(roots, env.Platform.RootDirs(env.Config)...)
	}
	if env.Database != nil && env.Database.MediaDB != nil {
		sourceRoots, err := env.Database.MediaDB.GetMediaSourceRoots(env.Context, systemID)
		if err != nil {
			return nil, fmt.Errorf("media.asset: get trusted media roots: %w", err)
		}
		roots = append(roots, sourceRoots...)
	}
	if env.Config != nil {
		if customRoot := env.Config.ScraperGamelistXMLCustomPath(); customRoot != "" {
			roots = append(roots, customRoot)
		}
	}
	if env.Platform != nil && (env.Platform.ID() == ids.Mister || env.Platform.ID() == ids.Mistex) {
		roots = append(roots, "/media/usb6/docs", "/media/usb7/docs")
	}

	seen := make(map[string]struct{}, len(roots))
	cleaned := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		root = filepath.Clean(root)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		cleaned = append(cleaned, root)
	}
	sort.Slice(cleaned, func(i, j int) bool { return len(cleaned[i]) > len(cleaned[j]) })
	return cleaned, nil
}
