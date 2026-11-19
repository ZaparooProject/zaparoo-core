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
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/imagefiles"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/uploads"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"golang.org/x/image/webp"
)

const (
	mediaUserNameMaxLength        = 255
	mediaUserDescriptionMaxLength = 10000
	mediaUserTagsMaxValues        = 32
	// mediaUserImagePrefix names every stored user image; the rest of a
	// file's name is its content hash, so two games given the same image
	// share one file.
	mediaUserImagePrefix = "img"
	// UploadPurposeMediaImage is the upload purpose of an image for
	// media.meta.update.
	UploadPurposeMediaImage = "media-image"
)

// mediaUserTagTypes are the title tag types a user may edit.
var mediaUserTagTypes = []tags.TagType{
	tags.TagTypeDeveloper, tags.TagTypePublisher, tags.TagTypeYear, tags.TagTypeGenre, tags.TagTypePlayers,
}

// mediaUserImageKinds are the image kinds a user may set, each the
// "image-<kind>" property of a file.
var mediaUserImageKinds = []string{
	"image", "boxart", "boxart3d", "boxartside", "boxartback", "screenshot", "thumbnail",
	"marquee", "wheel", "fanart", "titleshot", "map",
}

// mediaFieldEdit is one user-edited field a media.meta.update sets or resets.
type mediaFieldEdit struct {
	// value is the field's new stored value; nil resets the field.
	value *string
	// image is the source of an image field being set.
	image *models.MediaImageInput
	field string
	// imageData is a file or browse image's bytes, read and checked by
	// prepareMediaImageEdits before anything is written.
	imageData []byte
}

// UserImagesDir is the folder that holds the images users set on games. It is
// user data, kept out of backups: UserDB records which image a game uses.
func UserImagesDir(pl platforms.Platform) string {
	return filepath.Join(helpers.DataDir(pl), "media", "images-user")
}

// NewUserImages returns the store of the images users set on games.
func NewUserImages(pl platforms.Platform) *imagefiles.Store {
	return &imagefiles.Store{FS: afero.NewOsFs(), Dir: UserImagesDir(pl)}
}

func userImages(env *requests.RequestEnv) (*imagefiles.Store, error) {
	if env.Platform == nil {
		return nil, errors.New("platform is not available")
	}
	return NewUserImages(env.Platform), nil
}

// parseMediaUserName accepts a display name or null.
func parseMediaUserName(raw json.RawMessage) (mediaFieldEdit, error) {
	edit := mediaFieldEdit{field: database.MediaUserFieldName}
	if isJSONNull(raw) {
		return edit, nil
	}
	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		return edit, fmt.Errorf("media.name must be a string or null: %w", err)
	}
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return edit, errors.New("media.name cannot be empty; use null to reset")
	case utf8.RuneCountInString(name) > mediaUserNameMaxLength:
		return edit, fmt.Errorf("media.name is longer than %d characters", mediaUserNameMaxLength)
	case strings.ContainsFunc(name, unicode.IsControl):
		return edit, errors.New("media.name cannot contain control characters")
	}
	edit.value = &name
	return edit, nil
}

// parseMediaUserDescription accepts a description or null.
func parseMediaUserDescription(raw json.RawMessage) (mediaFieldEdit, error) {
	edit := mediaFieldEdit{field: database.MediaUserFieldDescription}
	if isJSONNull(raw) {
		return edit, nil
	}
	var description string
	if err := json.Unmarshal(raw, &description); err != nil {
		return edit, fmt.Errorf("media.description must be a string or null: %w", err)
	}
	description = strings.TrimSpace(description)
	if utf8.RuneCountInString(description) > mediaUserDescriptionMaxLength {
		return edit, fmt.Errorf("media.description is longer than %d characters", mediaUserDescriptionMaxLength)
	}
	if strings.ContainsFunc(description, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
	}) {
		return edit, errors.New("media.description cannot contain control characters")
	}
	edit.value = &description
	return edit, nil
}

// parseMediaUserTags accepts an object of tag type to a list of values or
// null. A list is the full set the title carries for that type; an empty
// list is a title with none.
func parseMediaUserTags(raw json.RawMessage) ([]mediaFieldEdit, error) {
	var byType map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byType); err != nil || byType == nil {
		return nil, errors.New("media.tags must be an object of tag type to a list of values or null")
	}
	tagTypes := make([]string, 0, len(byType))
	for tagType := range byType {
		tagTypes = append(tagTypes, tagType)
	}
	sort.Strings(tagTypes)

	edits := make([]mediaFieldEdit, 0, len(tagTypes))
	for _, tagType := range tagTypes {
		if !slices.Contains(mediaUserTagTypes, tags.TagType(tagType)) {
			return nil, fmt.Errorf("media.tags.%s cannot be edited", tagType)
		}
		edit := mediaFieldEdit{field: database.MediaUserFieldTagPrefix + tagType}
		if isJSONNull(byType[tagType]) {
			edits = append(edits, edit)
			continue
		}
		var rawValues []string
		if err := json.Unmarshal(byType[tagType], &rawValues); err != nil {
			return nil, fmt.Errorf("media.tags.%s must be a list of strings or null: %w", tagType, err)
		}
		values, err := normalizeMediaUserTags(tags.TagType(tagType), rawValues)
		if err != nil {
			return nil, err
		}
		encoded := database.EncodeMediaUserTags(values)
		edit.value = &encoded
		edits = append(edits, edit)
	}
	return edits, nil
}

// normalizeMediaUserTags turns what a user typed into canonical tag values,
// the way the scrapers do: a company keeps its name as the tag's label.
func normalizeMediaUserTags(tagType tags.TagType, rawValues []string) ([]database.MediaUserTagValue, error) {
	if len(rawValues) > mediaUserTagsMaxValues {
		return nil, fmt.Errorf("media.tags.%s has more than %d values", tagType, mediaUserTagsMaxValues)
	}
	if tags.IsExclusiveType(tagType) && len(rawValues) > 1 {
		return nil, fmt.Errorf("media.tags.%s takes one value", tagType)
	}
	values := make([]database.MediaUserTagValue, 0, len(rawValues))
	for _, rawValue := range rawValues {
		rawValue = strings.TrimSpace(rawValue)
		value := tags.NormalizeTagValue(string(tagType), rawValue)
		if err := tags.ValidateTagValue(tagType, value); err != nil {
			return nil, fmt.Errorf("media.tags.%s: %q is not a valid value", tagType, rawValue)
		}
		if slices.ContainsFunc(values, func(v database.MediaUserTagValue) bool { return v.Tag == value }) {
			continue
		}
		tagValue := database.MediaUserTagValue{Tag: value}
		if (tagType == tags.TagTypeDeveloper || tagType == tags.TagTypePublisher) && rawValue != value {
			tagValue.Label = rawValue
		}
		values = append(values, tagValue)
	}
	return values, nil
}

// parseMediaUserImages accepts an object of image kind to an image source or
// null.
func parseMediaUserImages(raw json.RawMessage) ([]mediaFieldEdit, error) {
	var byKind map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byKind); err != nil || byKind == nil {
		return nil, errors.New("media.images must be an object of image kind to an image source or null")
	}
	kinds := make([]string, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	edits := make([]mediaFieldEdit, 0, len(kinds))
	for _, kind := range kinds {
		if !slices.Contains(mediaUserImageKinds, kind) {
			return nil, fmt.Errorf("media.images.%s is not an image kind", kind)
		}
		edit := mediaFieldEdit{field: database.MediaUserFieldImagePrefix + kind}
		if isJSONNull(byKind[kind]) {
			edits = append(edits, edit)
			continue
		}
		var input models.MediaImageInput
		decoder := json.NewDecoder(bytes.NewReader(byKind[kind]))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return nil, fmt.Errorf("media.images.%s must be an image source or null: %w", kind, err)
		}
		switch input.Kind {
		case models.MediaImageKindFile:
			if input.UploadID != "" || input.Root != "" || !filepath.IsAbs(input.Path) {
				return nil, fmt.Errorf("media.images.%s: a file image needs an absolute path and nothing else", kind)
			}
		case models.MediaImageKindUpload:
			if input.Path != "" || input.Root != "" || input.UploadID == "" {
				return nil, fmt.Errorf("media.images.%s: an upload image needs an uploadId and nothing else", kind)
			}
		case models.MediaImageKindBrowse:
			if input.UploadID != "" || input.Root == "" || input.Path == "" {
				return nil, fmt.Errorf("media.images.%s: a browse image needs a root and a path", kind)
			}
		default:
			return nil, fmt.Errorf("media.images.%s: unknown image kind %q", kind, input.Kind)
		}
		edit.image = &input
		edits = append(edits, edit)
	}
	return edits, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// prepareMediaImageEdits checks every image an update sets before any part
// of the update is written: a file or browse image is read and checked as an
// image, and an upload must be a finished one of this caller's. An update
// whose image is not usable then changes nothing.
func prepareMediaImageEdits(env *requests.RequestEnv, edits []mediaFieldEdit) error {
	for i := range edits {
		edit := &edits[i]
		if edit.image == nil {
			continue
		}
		if edit.image.Kind == models.MediaImageKindUpload {
			if err := uploadManager(env).Ready(
				edit.image.UploadID, uploadOwner(env), UploadPurposeMediaImage,
			); err != nil {
				return uploadClientError(err)
			}
			continue
		}
		data, err := readMediaImageSource(env, edit.image)
		if err != nil {
			return err
		}
		edit.imageData = data
	}
	return nil
}

// readMediaImageSource reads and checks the image a file or browse edit
// names.
func readMediaImageSource(env *requests.RequestEnv, input *models.MediaImageInput) ([]byte, error) {
	var data []byte
	var err error
	switch {
	case input.Kind == models.MediaImageKindBrowse:
		data, err = readBrowsedMediaImage(env, input)
	case !isLocalOrAdmin(env):
		// A file image reads any image on the Core host.
		return nil, models.ClientErrf("%w", ErrForbidden)
	default:
		data, err = readHostMediaImage(input.Path)
	}
	if err != nil {
		return nil, err
	}
	if err = checkMediaUserImage(data); err != nil {
		return nil, models.ClientErr(err)
	}
	return data, nil
}

// readBrowsedMediaImage reads a browse image: one files.browse listed, inside
// a browse root, read through the file the root opened.
func readBrowsedMediaImage(env *requests.RequestEnv, input *models.MediaImageInput) ([]byte, error) {
	browser, err := hostBrowser(env)
	if err != nil {
		return nil, err
	}
	opened, err := browser.OpenImage(env.Context, input.Root, input.Path)
	if err != nil {
		return nil, hostFilesClientError(err)
	}
	defer func() { _ = opened.File.Close() }()
	data, err := readOpenedMediaFile(opened.File, opened.Info, imagefiles.MaxBytes)
	return data, mediaImageSourceReadError(err)
}

// readHostMediaImage reads a file image, named by its path on the Core host.
func readHostMediaImage(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, models.ClientErrf("image file not found: %s", path)
	}
	data, err := readMediaBinaryFile(afero.NewOsFs(), path, imagefiles.MaxBytes)
	return data, mediaImageSourceReadError(err)
}

// mediaImageSourceReadError is the client error for a failed image read.
func mediaImageSourceReadError(err error) error {
	if err == nil {
		return nil
	}
	var tooLarge *mediaBinaryTooLargeError
	if errors.As(err, &tooLarge) {
		return models.ClientErr(imagefiles.ErrTooLarge)
	}
	return models.ClientErrf("image file cannot be read: %w", err)
}

// storeMediaUserImage stores the image an edit names in the user images
// folder and hands the file name UserDB keeps to commit. An upload is
// consumed only once commit succeeds, so a failed write can be retried with
// the same upload.
func storeMediaUserImage(env *requests.RequestEnv, edit *mediaFieldEdit, commit func(name string) error) error {
	store, err := userImages(env)
	if err != nil {
		return err
	}
	if edit.image.Kind == models.MediaImageKindUpload {
		var commitErr error
		takeErr := uploadManager(env).Take(edit.image.UploadID, uploadOwner(env), UploadPurposeMediaImage,
			func(path string) error {
				data, readErr := readMediaBinaryFile(afero.NewOsFs(), path, imagefiles.MaxBytes)
				if readErr != nil {
					return fmt.Errorf("read uploaded image: %w", readErr)
				}
				name, saveErr := store.Save(mediaUserImagePrefix, data)
				if saveErr != nil {
					return fmt.Errorf("store uploaded image: %w", saveErr)
				}
				commitErr = commit(name)
				return commitErr
			})
		switch {
		case commitErr != nil:
			return commitErr
		case takeErr != nil:
			return uploadClientError(takeErr)
		}
		return nil
	}

	if edit.imageData == nil {
		return errors.New("image edit was not prepared")
	}
	name, err := store.Save(mediaUserImagePrefix, edit.imageData)
	if err != nil {
		if errors.Is(err, imagefiles.ErrNotImage) || errors.Is(err, imagefiles.ErrTooLarge) {
			return models.ClientErr(err)
		}
		return fmt.Errorf("failed to store image: %w", err)
	}
	return commit(name)
}

// checkMediaUserImage accepts data that is a supported image which is whole,
// not only bytes that start like one or a whole header: a cut-off file would
// be stored and then never display. A still image must decode in full. An
// animated GIF or WebP decodes its first frame where the decoder can, and its
// block or chunk structure must run complete to the end, so every frame is
// present without holding them all in memory. The frame its header declares is
// checked against mediaImageMaxPixels before the decoder allocates it.
func checkMediaUserImage(data []byte) error {
	contentType, err := imagefiles.ContentType(data)
	if err != nil {
		return fmt.Errorf("check image type: %w", err)
	}
	type codec struct {
		config func(io.Reader) (image.Config, error)
		decode func(io.Reader) (image.Image, error)
	}
	c, ok := map[string]codec{
		"image/jpeg": {jpeg.DecodeConfig, jpeg.Decode},
		"image/png":  {png.DecodeConfig, png.Decode},
		"image/gif":  {gif.DecodeConfig, gif.Decode},
		"image/webp": {webp.DecodeConfig, webp.Decode},
	}[contentType]
	if !ok {
		return imagefiles.ErrNotImage
	}
	cfg, err := c.config(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("%w: %w", imagefiles.ErrNotImage, err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > mediaImageMaxPixels {
		return fmt.Errorf("%w: %dx%d is past the %d pixel limit",
			imagefiles.ErrNotImage, cfg.Width, cfg.Height, mediaImageMaxPixels)
	}
	switch contentType {
	case "image/gif":
		if err := checkGIFBlocks(data); err != nil {
			return fmt.Errorf("%w: %w", imagefiles.ErrNotImage, err)
		}
	case "image/webp":
		animated, err := checkWebPChunks(data)
		if err != nil {
			return fmt.Errorf("%w: %w", imagefiles.ErrNotImage, err)
		}
		// The WebP decoder reads no animation frames and refuses the file.
		if animated {
			return nil
		}
	}
	if _, err := c.decode(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("%w: %w", imagefiles.ErrNotImage, err)
	}
	return nil
}

var errImageCutOff = errors.New("image ends before its last block")

// checkGIFBlocks walks a GIF's blocks from the screen descriptor to the
// trailer, so a frame cut off after the first one is caught without decoding
// it. Bytes after the trailer are ignored, as decoders do.
func checkGIFBlocks(data []byte) error {
	// "GIF89a" and the logical screen descriptor.
	p := 13
	if len(data) < p {
		return errImageCutOff
	}
	if flags := data[10]; flags&0x80 != 0 {
		p += 3 << (flags&0x07 + 1)
	}
	for {
		if p >= len(data) {
			return errImageCutOff
		}
		switch data[p] {
		case 0x3B:
			return nil
		case 0x21:
			// Introducer and label, then data sub-blocks.
			p += 2
		case 0x2C:
			if p+10 > len(data) {
				return errImageCutOff
			}
			flags := data[p+9]
			p += 10
			if flags&0x80 != 0 {
				p += 3 << (flags&0x07 + 1)
			}
			// LZW minimum code size, then data sub-blocks.
			p++
		default:
			return fmt.Errorf("unknown gif block 0x%02x", data[p])
		}
		for {
			if p >= len(data) {
				return errImageCutOff
			}
			n := int(data[p])
			p += 1 + n
			if n == 0 {
				break
			}
		}
	}
}

// checkWebPChunks walks a WebP's top-level chunks to the end its RIFF header
// declares, so a file cut off in a later animation frame is caught. It reports
// whether the VP8X header marks the file as animated.
func checkWebPChunks(data []byte) (bool, error) {
	if len(data) < 12 {
		return false, errImageCutOff
	}
	end := 8 + int64(binary.LittleEndian.Uint32(data[4:8]))
	if end > int64(len(data)) {
		return false, errImageCutOff
	}
	animated := false
	for p := int64(12); p < end; {
		if p+8 > end {
			return false, errImageCutOff
		}
		size := int64(binary.LittleEndian.Uint32(data[p+4 : p+8]))
		if string(data[p:p+4]) == "VP8X" && size > 0 && p+8 < end {
			animated = data[p+8]&0x02 != 0
		}
		next := p + 8 + size + size&1
		// A final odd-sized chunk may leave its padding byte out of the total.
		if next > end && (size&1 == 0 || next != end+1) {
			return false, errImageCutOff
		}
		p = next
	}
	return animated, nil
}

// mediaUserImagesMu serializes storing user images, writing the rows that
// name them and pruning the folder, so a prune never removes a file another
// edit is about to name. Every transport's requests share it.
var mediaUserImagesMu syncutil.Mutex

type mediaFieldChanges struct {
	tags    bool
	images  bool
	cleared bool
}

// applyMediaFieldEdits writes each edit to UserDB and the media database,
// then brings the caches that read them up to date.
func applyMediaFieldEdits(env *requests.RequestEnv, row *database.MediaFullRow, edits []mediaFieldEdit) error {
	if len(edits) == 0 {
		return nil
	}
	changes, err := writeMediaFieldEdits(env, row, edits)
	// Edits written before a failed one stay written, so the caches and the
	// refill follow them either way.
	if changes.tags {
		refreshScrapedTags(env.Context, env.Database.MediaDB)
	}
	if changes.images {
		mediaImageNoImages.clear()
		invalidateChangedScrapeThumbnails(env.Database.MediaDB)
	}
	if changes.cleared {
		refillMediaAfterReset(env, row)
	}
	return err
}

// writeMediaFieldEdits stores each edit's image and writes the edits in order,
// stopping at the first that fails, and reports what the written ones changed.
func writeMediaFieldEdits(
	env *requests.RequestEnv, row *database.MediaFullRow, edits []mediaFieldEdit,
) (changes mediaFieldChanges, err error) {
	mediaUserImagesMu.Lock()
	defer mediaUserImagesMu.Unlock()
	defer func() {
		if changes.images {
			pruneMediaUserImages(env)
		}
	}()

	apply := func(field string, value *string) error {
		result, applyErr := database.ApplyMediaUserField(env.Context, env.Database, row, field, value)
		if applyErr != nil {
			return mediaWriteClientError(
				fmt.Errorf("failed to apply media %s: %w", field, applyErr), database.MediaWriteOperationNone,
			)
		}
		changes.tags = changes.tags || result.TagsChanged
		changes.images = changes.images || result.ImagesChanged
		changes.cleared = changes.cleared || len(result.ClearedMediaDBIDs) > 0
		return nil
	}
	for i := range edits {
		edit := &edits[i]
		if edit.image == nil {
			err = apply(edit.field, edit.value)
		} else {
			err = storeMediaUserImage(env, edit, func(name string) error {
				return apply(edit.field, &name)
			})
		}
		if err != nil {
			return changes, err
		}
	}
	return changes, nil
}

// pruneMediaUserImages removes stored user images no game uses any more.
func pruneMediaUserImages(env *requests.RequestEnv) {
	store, err := userImages(env)
	if err != nil {
		return
	}
	fields, err := env.Database.UserDB.ListMediaUserFields()
	if err != nil {
		log.Warn().Err(err).Msg("failed to list media user fields; user images not pruned")
		return
	}
	keep := make(map[string]struct{})
	for i := range fields {
		if strings.HasPrefix(fields[i].Field, database.MediaUserFieldImagePrefix) {
			keep[fields[i].Value] = struct{}{}
		}
	}
	store.Prune(keep, time.Now())
}

// refillMediaAfterReset asks the scrapers to fill what a reset field left
// empty on one file, and on its title. It runs each scraper that fills
// missing fields, scoped to that file; when another media job is running the
// jobs wait in the queue instead.
func refillMediaAfterReset(env *requests.RequestEnv, row *database.MediaFullRow) {
	if env.Platform == nil || env.State == nil {
		return
	}
	path, err := database.CanonicalScrapePath(row.Path, false)
	if err != nil {
		return
	}
	scope := &database.ScrapeScope{SystemID: row.System.SystemID, Path: path, MediaID: row.DBID}
	if scope.Validate() != nil {
		return
	}
	scrapers := env.Platform.Scrapers(env.Config)
	var jobs []database.ScrapeJob
	for id, s := range scrapers {
		if !s.SupportsFillMissing || s.Scrape == nil {
			continue
		}
		if len(s.SupportedSystemIDs) > 0 && !slices.Contains(s.SupportedSystemIDs, row.System.SystemID) {
			continue
		}
		jobs = append(jobs, database.ScrapeJob{
			ScraperID: id, Systems: []string{row.System.SystemID}, FillMissing: true, Scope: scope,
			RunID: uuid.NewString(),
		})
	}
	if len(jobs) == 0 {
		return
	}
	sortScrapeQueue(jobs, scrapers)
	first := jobs[0]
	operation := &database.ScrapingOperation{
		Version: 1, ScraperID: first.ScraperID, Systems: first.Systems, RunID: first.RunID,
		FillMissing: true, Scope: scope, Pending: jobs[1:],
	}
	if _, startErr := startMediaScrapeOperation(env, operation, nil, false); startErr != nil {
		for i := range jobs {
			jobs[i].RunID = ""
		}
		if queueErr := enqueueScrapeJobs(env.Database.MediaDB, jobs, scrapers); queueErr != nil {
			log.Warn().Err(errors.Join(startErr, queueErr)).Str("path", row.Path).
				Msg("failed to scrape a file again after a metadata reset")
		}
	}
}

// takeUserEditedFields removes the internal user-fields marker from a
// response's properties and returns the fields it lists, in the names
// media.meta.update takes.
func takeUserEditedFields(props map[string]models.MediaMetaPropertyItem) []string {
	typeTag := tags.PropertyTypeTag(tags.TagPropertyUserFields)
	item, ok := props[typeTag]
	if !ok {
		return nil
	}
	delete(props, typeTag)
	marker := database.DecodeMediaUserFieldsMarker(item.Text)
	edited := make([]string, 0, len(marker.Fields))
	for _, field := range marker.Fields {
		switch {
		case strings.HasPrefix(field, database.MediaUserFieldTagPrefix):
			edited = append(edited, "tags."+strings.TrimPrefix(field, database.MediaUserFieldTagPrefix))
		case strings.HasPrefix(field, database.MediaUserFieldImagePrefix):
			edited = append(edited, "images."+strings.TrimPrefix(field, database.MediaUserFieldImagePrefix))
		default:
			edited = append(edited, field)
		}
	}
	sort.Strings(edited)
	return edited
}

// uploadClientError turns an upload service error a client caused into a
// client error.
func uploadClientError(err error) error {
	for _, clientErr := range []error{
		uploads.ErrUnknownPurpose, uploads.ErrTooLarge, uploads.ErrInvalidSize, uploads.ErrInvalidHash,
		uploads.ErrTooManySessions, uploads.ErrNotFound, uploads.ErrWrongOffset, uploads.ErrChunkTooLarge,
		uploads.ErrEmptyChunk, uploads.ErrOverrun, uploads.ErrIncomplete, uploads.ErrHashMismatch,
		uploads.ErrFinished, uploads.ErrNotFinished, uploads.ErrWrongPurpose,
		imagefiles.ErrNotImage, imagefiles.ErrTooLarge,
	} {
		if errors.Is(err, clientErr) {
			return models.ClientErr(err)
		}
	}
	return err
}
