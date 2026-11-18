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
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/uploads"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type userFieldsTestEnv struct {
	paths []string
	ids   []int64
	env   requests.RequestEnv
}

func newUserFieldsTestEnv(t *testing.T) *userFieldsTestEnv {
	t.Helper()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir(), HostManagedPaths: true}).Maybe()
	pl.On("Scrapers", mock.Anything).Return(map[string]platforms.Scraper{}).Maybe()

	paths := []string{
		filepath.Join("roms", "NES", "Metroid (USA).nes"),
		filepath.Join("roms", "NES", "Metroid (Europe).nes"),
	}
	ids := addTestMediaPaths(t, mediaDB, paths...)
	return &userFieldsTestEnv{
		env: requests.RequestEnv{
			Context:  context.Background(),
			Platform: pl,
			Database: &database.Database{MediaDB: mediaDB, UserDB: userDB, UserImages: NewUserImages(pl)},
			IsLocal:  true,
		},
		paths: paths,
		ids:   ids,
	}
}

func (e *userFieldsTestEnv) update(t *testing.T, mediaIndex int, patch string) models.MediaMetaMediaResponse {
	t.Helper()
	params := fmt.Sprintf(`{"mediaId":%d,"media":%s}`, e.ids[mediaIndex], patch)
	result, err := HandleMediaMetaUpdate(withParams(&e.env, params))
	require.NoError(t, err)
	response, ok := result.(models.MediaMetaResponse)
	require.True(t, ok, "unexpected response type %T", result)
	return response.Media
}

func (e *userFieldsTestEnv) updateErr(t *testing.T, patch string) error {
	t.Helper()
	_, err := HandleMediaMetaUpdate(withParams(&e.env, fmt.Sprintf(`{"mediaId":%d,"media":%s}`, e.ids[0], patch)))
	require.Error(t, err)
	return err
}

func isClientErr(err error) bool {
	var clientErr *models.ClientError
	return errors.As(err, &clientErr)
}

func TestHandleMediaMetaUpdate_NameDescriptionAndTags(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)

	media := e.update(t, 0, `{
		"name": "  Metroid: Zero  ",
		"description": "A custom description.",
		"tags": {"genre": ["RPG", "puzzle"], "developer": ["T&E Soft"], "year": ["1986"]}
	}`)

	assert.Equal(t, "Metroid: Zero", media.Title.Name)
	assert.Equal(t, "A custom description.",
		media.Title.Properties[tags.PropertyTypeTag(tags.TagPropertyDescription)].Text)
	assert.Equal(t,
		[]string{"description", "name", "tags.developer", "tags.genre", "tags.year"}, media.Title.UserEdited)
	assert.NotContains(t, media.Title.Properties, tags.PropertyTypeTag(tags.TagPropertyUserFields),
		"the marker is internal")
	got := make(map[string][]string)
	labels := make(map[string]string)
	for _, tag := range media.Title.Tags {
		got[tag.Type] = append(got[tag.Type], tags.UnpadTagValue(tag.Tag))
		labels[tag.Type+":"+tag.Tag] = tag.Label
	}
	assert.ElementsMatch(t, []string{"rpg", "puzzle"}, got["genre"])
	assert.Equal(t, []string{"t-and-e-soft"}, got["developer"])
	assert.Equal(t, "T&E Soft", labels["developer:t-and-e-soft"])
	assert.Equal(t, []string{"1986"}, got["year"])

	// The other version of the title shows the same text.
	other := e.update(t, 1, `{"description": "Edited from the other version."}`)
	assert.Equal(t, "Metroid: Zero", other.Title.Name)

	reset := e.update(t, 0, `{"name": null, "description": null, "tags": {"genre": null}}`)
	assert.Equal(t, "Metroid", reset.Title.Name)
	assert.NotContains(t, reset.Title.Properties, tags.PropertyTypeTag(tags.TagPropertyDescription))
	assert.Equal(t, []string{"tags.developer", "tags.year"}, reset.Title.UserEdited)
}

func TestHandleMediaMetaUpdate_RejectsInvalidFields(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)

	tooManyGenres, err := json.Marshal(slices.Repeat([]string{"action"}, mediaUserTagsMaxValues+1))
	require.NoError(t, err)
	longDescription := strings.Repeat("a", mediaUserDescriptionMaxLength+1)
	for name, patch := range map[string]string{
		"name too long":                 fmt.Sprintf(`{"name": %q}`, strings.Repeat("a", mediaUserNameMaxLength+1)),
		"description not a string":      `{"description": 5}`,
		"description too long":          fmt.Sprintf(`{"description": %q}`, longDescription),
		"description control character": `{"description": "a\u0007b"}`,
		"tags not an object":            `{"tags": ["genre"]}`,
		"tag values not a list":         `{"tags": {"genre": "action"}}`,
		"too many tag values":           `{"tags": {"genre": ` + string(tooManyGenres) + `}}`,
		"images not an object":          `{"images": "boxart"}`,
		"image not a source":            `{"images": {"boxart": "cover.png"}}`,
		"empty name":                    `{"name": "   "}`,
		"control character":             `{"name": "a\u0007b"}`,
		"name not a string":             `{"name": 5}`,
		"unknown genre":                 `{"tags": {"genre": ["not-a-real-genre"]}}`,
		"two years":                     `{"tags": {"year": ["1990", "1991"]}}`,
		"uneditable tag type":           `{"tags": {"region": ["us"]}}`,
		"unknown image kind":            `{"images": {"poster": null}}`,
		"relative image path":           `{"images": {"boxart": {"kind": "file", "path": "cover.png"}}}`,
		"upload without an id":          `{"images": {"boxart": {"kind": "upload"}}}`,
		"unknown field":                 `{"rating": "5"}`,
		"unknown image source":          `{"images": {"boxart": {"kind": "url", "path": "/x"}}}`,
	} {
		updateErr := e.updateErr(t, patch)
		assert.Truef(t, isClientErr(updateErr), "%s: want a client error, got %v", name, updateErr)
	}
	fields, err := e.env.Database.UserDB.ListMediaUserFields()
	require.NoError(t, err)
	assert.Empty(t, fields, "a rejected update stores nothing")
}

func TestHandleMediaMetaUpdate_ImageFromHostFile(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	source := filepath.Join(t.TempDir(), "cover.png")
	require.NoError(t, os.WriteFile(source, testPNG(t, 10), 0o600))

	media := e.update(t, 0, fmt.Sprintf(`{"images": {"boxart": {"kind": "file", "path": %q}}}`, source))

	assert.Equal(t, []string{"images.boxart"}, media.UserEdited)
	stored := media.Properties[tags.PropertyTypeTag(tags.TagPropertyImageBoxart)].Text
	assert.Equal(t, UserImagesDir(e.env.Platform), filepath.Dir(stored), "the image is copied into Core's folder")
	data, err := os.ReadFile(stored) //nolint:gosec // a path under the test's own temp dir
	require.NoError(t, err)
	assert.Equal(t, testPNG(t, 10), data)

	fields, err := e.env.Database.UserDB.ListMediaUserFields()
	require.NoError(t, err)
	require.Len(t, fields, 1)
	assert.Equal(t, filepath.Base(stored), fields[0].Value, "UserDB keeps the file's name, not its bytes")

	// The other version of the game keeps its own art.
	result, err := HandleMediaMeta(withParams(&e.env, fmt.Sprintf(`{"mediaId":%d}`, e.ids[1])))
	require.NoError(t, err)
	other, ok := result.(models.MediaMetaResponse)
	require.True(t, ok)
	assert.NotContains(t, other.Media.Properties, tags.PropertyTypeTag(tags.TagPropertyImageBoxart))

	reset := e.update(t, 0, `{"images": {"boxart": null}}`)
	assert.Empty(t, reset.UserEdited)
	assert.NotContains(t, reset.Properties, tags.PropertyTypeTag(tags.TagPropertyImageBoxart))
}

func TestHandleMediaMetaUpdate_ImageFileNeedsLocalOrAdmin(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	e.env.IsLocal = false
	e.env.ClientRole = "member"
	source := filepath.Join(t.TempDir(), "cover.png")
	require.NoError(t, os.WriteFile(source, testPNG(t, 10), 0o600))

	err := e.updateErr(t, fmt.Sprintf(`{"images": {"boxart": {"kind": "file", "path": %q}}}`, source))
	require.ErrorIs(t, err, ErrForbidden)
}

func TestHandleMediaMetaUpdate_ImageFileMustBeAnImage(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	source := filepath.Join(t.TempDir(), "cover.png")
	require.NoError(t, os.WriteFile(source, []byte("not an image at all"), 0o600))

	err := e.updateErr(t, fmt.Sprintf(`{"images": {"boxart": {"kind": "file", "path": %q}}}`, source))
	assert.True(t, isClientErr(err))
}

// uploadImage sends data through the upload methods in chunks of chunkSize
// and returns the finished upload's ID.
func (e *userFieldsTestEnv) uploadImage(t *testing.T, data []byte, chunkSize int) string {
	t.Helper()
	sum := sha256.Sum256(data)
	result, err := HandleUploadsBegin(withParams(&e.env, fmt.Sprintf(
		`{"purpose":"media-image","size":%d,"sha256":%q}`, len(data), hex.EncodeToString(sum[:]),
	)))
	require.NoError(t, err)
	begun, ok := result.(models.UploadResponse)
	require.True(t, ok)
	assert.Equal(t, uploads.ChunkBytes, begun.ChunkSize)

	for offset := 0; offset < len(data); offset += chunkSize {
		end := min(offset+chunkSize, len(data))
		params, marshalErr := json.Marshal(map[string]any{
			"uploadId": begun.UploadID, "offset": offset,
			"data": base64.StdEncoding.EncodeToString(data[offset:end]),
		})
		require.NoError(t, marshalErr)
		result, err = HandleUploadsChunk(withParams(&e.env, string(params)))
		require.NoError(t, err)
		chunk, isUpload := result.(models.UploadResponse)
		require.True(t, isUpload)
		assert.Equal(t, int64(end), chunk.Received)
	}
	result, err = HandleUploadsFinish(withParams(&e.env, fmt.Sprintf(`{"uploadId":%q}`, begun.UploadID)))
	require.NoError(t, err)
	finished, ok := result.(models.UploadResponse)
	require.True(t, ok)
	assert.True(t, finished.Finished)
	return begun.UploadID
}

func TestHandleMediaMetaUpdate_ImageFromUpload(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	// A paired member on the network: no host path, so it uploads.
	e.env.IsLocal = false
	e.env.ClientRole = "member"
	e.env.ClientID = "phone"
	data := testPNG(t, 64)

	uploadID := e.uploadImage(t, data, 7)
	media := e.update(t, 0, fmt.Sprintf(`{"images": {"screenshot": {"kind": "upload", "uploadId": %q}}}`, uploadID))

	stored := media.Properties[tags.PropertyTypeTag(tags.TagPropertyImageScreenshot)].Text
	got, err := os.ReadFile(stored) //nolint:gosec // a path under the test's own temp dir
	require.NoError(t, err)
	assert.Equal(t, data, got)

	// The upload is used up, and its staged file is gone.
	_, err = HandleUploadsStatus(withParams(&e.env, fmt.Sprintf(`{"uploadId":%q}`, uploadID)))
	require.ErrorIs(t, err, uploads.ErrNotFound)
	entries, err := os.ReadDir(UserImagesDir(e.env.Platform))
	require.NoError(t, err)
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}
	assert.Equal(t, []string{filepath.Base(stored)}, files)
	staged, err := os.ReadDir(filepath.Join(UserImagesDir(e.env.Platform), uploadStagingDirName))
	require.NoError(t, err)
	assert.Empty(t, staged)
}

func TestHandleUploads_RejectsWhatIsNotAnImage(t *testing.T) {
	t.Parallel()
	cases := map[string][]byte{
		"plain text": []byte("plain text pretending to be a cover"),
		// Starts like a PNG, so a type sniff alone accepts it.
		"png signature only": append([]byte("\x89PNG\r\n\x1a\n"), []byte("not the rest of a png")...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newUserFieldsTestEnv(t)
			sum := sha256.Sum256(data)

			result, err := HandleUploadsBegin(withParams(&e.env, fmt.Sprintf(
				`{"purpose":"media-image","size":%d,"sha256":%q}`, len(data), hex.EncodeToString(sum[:]),
			)))
			require.NoError(t, err)
			begun, ok := result.(models.UploadResponse)
			require.True(t, ok)
			params, err := json.Marshal(map[string]any{
				"uploadId": begun.UploadID, "offset": 0, "data": base64.StdEncoding.EncodeToString(data),
			})
			require.NoError(t, err)
			_, err = HandleUploadsChunk(withParams(&e.env, string(params)))
			require.NoError(t, err)

			_, err = HandleUploadsFinish(withParams(&e.env, fmt.Sprintf(`{"uploadId":%q}`, begun.UploadID)))
			require.Error(t, err)
			assert.True(t, isClientErr(err))
		})
	}
}

func TestHandleUploads_RequireAPairedOrLocalClient(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	e.env.IsLocal = false

	_, err := HandleUploadsBegin(withParams(&e.env,
		`{"purpose":"media-image","size":10,"sha256":"`+hex.EncodeToString(make([]byte, 32))+`"}`))
	require.ErrorIs(t, err, ErrForbidden)
}

func TestHandleUploads_UnknownPurposeAndOversize(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	hash := hex.EncodeToString(make([]byte, 32))

	_, err := HandleUploadsBegin(withParams(&e.env, `{"purpose":"game","size":10,"sha256":"`+hash+`"}`))
	require.ErrorIs(t, err, uploads.ErrUnknownPurpose)
	_, err = HandleUploadsBegin(withParams(&e.env,
		fmt.Sprintf(`{"purpose":"media-image","size":%d,"sha256":%q}`, 11<<20, hash)))
	require.ErrorIs(t, err, uploads.ErrTooLarge)
}

// A chunk is refused before it reaches the session when it is larger than a
// chunk can be or is not base64.
func TestHandleUploads_ChunkValidation(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	data := []byte("0123456789")
	sum := sha256.Sum256(data)
	result, err := HandleUploadsBegin(withParams(&e.env, fmt.Sprintf(
		`{"purpose":"media-image","size":%d,"sha256":%q}`, len(data), hex.EncodeToString(sum[:]),
	)))
	require.NoError(t, err)
	begun, ok := result.(models.UploadResponse)
	require.True(t, ok)
	chunk := func(data string) error {
		params, marshalErr := json.Marshal(map[string]any{"uploadId": begun.UploadID, "offset": 0, "data": data})
		require.NoError(t, marshalErr)
		_, chunkErr := HandleUploadsChunk(withParams(&e.env, string(params)))
		return chunkErr
	}

	require.ErrorIs(t, chunk(base64.StdEncoding.EncodeToString(make([]byte, uploads.ChunkBytes+3))),
		uploads.ErrChunkTooLarge)
	err = chunk("not base64!")
	require.Error(t, err)
	assert.True(t, isClientErr(err))
	_, err = HandleUploadsChunk(withParams(&e.env, `{"uploadId":"`+begun.UploadID+`"}`))
	require.Error(t, err, "a chunk needs an offset and data")
	assert.True(t, isClientErr(err))

	status, err := HandleUploadsStatus(withParams(&e.env, fmt.Sprintf(`{"uploadId":%q}`, begun.UploadID)))
	require.NoError(t, err)
	assert.Equal(t, int64(0), status.(models.UploadResponse).Received, "a refused chunk writes nothing")
}

func TestHandleUploads_CancelDiscardsTheUpload(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	hash := hex.EncodeToString(make([]byte, 32))
	result, err := HandleUploadsBegin(withParams(&e.env, `{"purpose":"media-image","size":10,"sha256":"`+hash+`"}`))
	require.NoError(t, err)
	begun, ok := result.(models.UploadResponse)
	require.True(t, ok)
	idParams := fmt.Sprintf(`{"uploadId":%q}`, begun.UploadID)

	_, err = HandleUploadsCancel(withParams(&e.env, idParams))
	require.NoError(t, err)
	_, err = HandleUploadsStatus(withParams(&e.env, idParams))
	require.ErrorIs(t, err, uploads.ErrNotFound)
	_, err = HandleUploadsCancel(withParams(&e.env, idParams))
	require.ErrorIs(t, err, uploads.ErrNotFound)
	_, err = HandleUploadsCancel(withParams(&e.env, `{}`))
	require.Error(t, err)
	assert.True(t, isClientErr(err))
}

func TestHandleUploads_UnavailableWithoutAPlatform(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	e.env.Platform = nil

	_, err := HandleUploadsCancel(withParams(&e.env, `{"uploadId":"x"}`))
	require.Error(t, err)
	assert.True(t, isClientErr(err))
	_, err = HandleUploadsStatus(withParams(&e.env, `{"uploadId":"x"}`))
	require.Error(t, err)
	_, err = HandleUploadsChunk(withParams(&e.env, `{"uploadId":"x","offset":0,"data":"eA=="}`))
	require.Error(t, err)
}

// A reset leaves the field empty; the scrapers that fill missing fields are
// asked to fill it again, for that one file.
func TestHandleMediaMetaUpdate_ResetScrapesTheFileAgain(t *testing.T) {
	// Not parallel — manipulates shared scrapingStatusInstance.
	ClearScrapingStatus()
	statusInstance.clear()
	t.Cleanup(ClearScrapingStatus)

	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "Metroid (USA).nes"))
	ids := addTestMediaPaths(t, mediaDB, path)

	gotOptions := make(chan scraper.ScrapeOptions, 2)
	fake := func(id string, fillMissing bool) platforms.Scraper {
		return platforms.Scraper{
			ID: id, Name: id, SupportsFillMissing: fillMissing,
			Scrape: func(
				_ context.Context, _ *config.Instance, _ platforms.Platform,
				_ afero.Fs, _ *database.Database, opts scraper.ScrapeOptions,
				_ platforms.ScraperCustomOptions, ch chan<- scraper.ScrapeUpdate,
			) error {
				gotOptions <- opts
				go func() {
					ch <- scraper.ScrapeUpdate{Done: true}
					close(ch)
				}()
				return nil
			},
		}
	}
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{DataDir: t.TempDir(), HostManagedPaths: true}).Maybe()
	pl.On("Scrapers", mock.Anything).Return(map[string]platforms.Scraper{
		"fills": fake("fills", true), "manual-only": fake("manual-only", false),
	})
	pl.SetupBasicMock()
	st, ns := state.NewState(pl, "test")
	t.Cleanup(st.StopService)
	drainNotifications(t, ns)
	e := &userFieldsTestEnv{
		env: requests.RequestEnv{
			Context: context.Background(), Platform: pl, State: st, IsLocal: true,
			Database: &database.Database{MediaDB: mediaDB, UserDB: userDB, UserImages: NewUserImages(pl)},
		},
		paths: []string{path}, ids: ids,
	}

	e.update(t, 0, `{"description": "mine"}`)
	select {
	case <-gotOptions:
		require.FailNow(t, "setting a field must not start a scrape")
	default:
	}

	e.update(t, 0, `{"description": null}`)
	opts := receiveScrapeOptions(t, gotOptions)
	assert.True(t, opts.FillMissing)
	require.NotNil(t, opts.Scope)
	assert.Equal(t, ids[0], opts.Scope.MediaID)
	assert.Equal(t, []string{"NES"}, opts.Systems)
	require.Eventually(t, func() bool { return !IsScrapingRunning() }, 2*time.Second, 10*time.Millisecond)
	select {
	case extra := <-gotOptions:
		require.FailNowf(t, "a scraper that cannot fill missing fields ran", "%+v", extra)
	default:
	}

	// A reset stays written when a later edit in the same request fails, so
	// the file is still scraped again.
	e.update(t, 0, `{"description": "mine"}`)
	missing := filepath.Join(t.TempDir(), "missing.png")
	err := e.updateErr(t, fmt.Sprintf(
		`{"description": null, "images": {"boxart": {"kind": "file", "path": %q}}}`, missing,
	))
	assert.True(t, isClientErr(err))
	opts = receiveScrapeOptions(t, gotOptions)
	assert.Equal(t, ids[0], opts.Scope.MediaID)
	require.Eventually(t, func() bool { return !IsScrapingRunning() }, 2*time.Second, 10*time.Millisecond)
}

// failingFieldsUserDB fails to record user fields while fail is set.
type failingFieldsUserDB struct {
	database.UserDBI
	fail bool
}

func (f *failingFieldsUserDB) SetMediaUserField(field *database.MediaUserField, titlePaths []string) error {
	if f.fail {
		return errors.New("disk full")
	}
	if err := f.UserDBI.SetMediaUserField(field, titlePaths); err != nil {
		return fmt.Errorf("set media user field: %w", err)
	}
	return nil
}

// An upload whose write fails stays available, so the client can retry it.
func TestHandleMediaMetaUpdate_UploadSurvivesAFailedWrite(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	userDB := &failingFieldsUserDB{UserDBI: e.env.Database.UserDB, fail: true}
	e.env.Database.UserDB = userDB
	data := testPNG(t, 32)
	uploadID := e.uploadImage(t, data, 1024)
	patch := fmt.Sprintf(`{"images": {"boxart": {"kind": "upload", "uploadId": %q}}}`, uploadID)

	writeErr := e.updateErr(t, patch)
	assert.False(t, isClientErr(writeErr), "a failed write is the device's fault: %v", writeErr)
	_, err := HandleUploadsStatus(withParams(&e.env, fmt.Sprintf(`{"uploadId":%q}`, uploadID)))
	require.NoError(t, err, "the upload must outlive the failed write")

	userDB.fail = false
	media := e.update(t, 0, patch)
	stored := media.Properties[tags.PropertyTypeTag(tags.TagPropertyImageBoxart)].Text
	got, err := os.ReadFile(stored) //nolint:gosec // a path under the test's own temp dir
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

// Pruning the images folder leaves an upload that is still waiting to be
// used, however long ago its last chunk arrived.
func TestHandleMediaMetaUpdate_PruneLeavesStagedUploads(t *testing.T) {
	t.Parallel()
	e := newUserFieldsTestEnv(t)
	data := testPNG(t, 32)
	uploadID := e.uploadImage(t, data, 1024)
	old := time.Now().Add(-time.Hour)
	err := filepath.WalkDir(UserImagesDir(e.env.Platform), func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Chtimes(path, old, old) //nolint:gosec // the test's own temp dir
	})
	require.NoError(t, err)

	pruneMediaUserImages(&e.env)

	media := e.update(t, 0, fmt.Sprintf(`{"images": {"boxart": {"kind": "upload", "uploadId": %q}}}`, uploadID))
	stored := media.Properties[tags.PropertyTypeTag(tags.TagPropertyImageBoxart)].Text
	got, err := os.ReadFile(stored) //nolint:gosec // a path under the test's own temp dir
	require.NoError(t, err)
	assert.Equal(t, data, got)
}
