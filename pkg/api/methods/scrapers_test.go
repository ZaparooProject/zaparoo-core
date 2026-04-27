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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// routeRequest wires r into a chi router at path using the given method and
// handler, then serves it. This lets URL params resolve properly.
func routeRequest(
	method, _, pattern string, handler http.HandlerFunc, r *http.Request,
) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Method(method, pattern, handler)
	router.ServeHTTP(rr, r)
	return rr
}

// --- properties endpoint tests ---

func TestHandleGetMediaTitleProperties_TitleNotFound(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockMediaDBI()
	db.On("FindMediaTitleByDBID", mock.Anything, int64(999)).Return((*database.MediaTitle)(nil), nil)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/titles/999/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/999/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
	db.AssertExpectations(t)
}

func TestHandleGetMediaTitleProperties_EmptyProperties(t *testing.T) {
	t.Parallel()

	title := &database.MediaTitle{DBID: 1, Name: "Test Game"}
	db := helpers.NewMockMediaDBI()
	db.On("FindMediaTitleByDBID", mock.Anything, int64(1)).Return(title, nil)
	db.On("GetMediaTitleProperties", mock.Anything, int64(1)).Return([]database.MediaProperty{}, nil)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/titles/1/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/1/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	require.Equal(t, http.StatusOK, rr.Code)

	var out []propertyResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	assert.Empty(t, out)
	db.AssertExpectations(t)
}

func TestHandleGetMediaTitleProperties_WithProperties(t *testing.T) {
	t.Parallel()

	title := &database.MediaTitle{DBID: 1, Name: "Test Game"}
	props := []database.MediaProperty{
		{TypeTag: "property:description", Text: "A great game", ContentType: "text/plain"},
		{TypeTag: "property:image.boxart", Text: "/roms/snes/images/mario.png", ContentType: "image/png"},
	}

	db := helpers.NewMockMediaDBI()
	db.On("FindMediaTitleByDBID", mock.Anything, int64(1)).Return(title, nil)
	db.On("GetMediaTitleProperties", mock.Anything, int64(1)).Return(props, nil)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/titles/1/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/1/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	require.Equal(t, http.StatusOK, rr.Code)

	var out []propertyResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	require.Len(t, out, 2)
	assert.Equal(t, "property:description", out[0].TypeTag)
	assert.Equal(t, "A great game", out[0].Text)
	assert.Equal(t, "text/plain", out[0].ContentType)
	assert.Equal(t, "property:image.boxart", out[1].TypeTag)
	db.AssertExpectations(t)
}

func TestHandleGetMediaTitleProperties_DBError(t *testing.T) {
	t.Parallel()

	title := &database.MediaTitle{DBID: 1}
	db := helpers.NewMockMediaDBI()
	db.On("FindMediaTitleByDBID", mock.Anything, int64(1)).Return(title, nil)
	db.On("GetMediaTitleProperties", mock.Anything, int64(1)).Return(
		[]database.MediaProperty(nil), errors.New("db error"))

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/titles/1/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/1/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	db.AssertExpectations(t)
}

func TestHandleGetMediaTitleProperties_InvalidID(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockMediaDBI()

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/titles/abc/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/titles/abc/properties",
		"/api/v1/titles/{titleDBID}/properties",
		HandleGetMediaTitleProperties(db),
		req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleGetMediaProperties_MediaNotFound(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockMediaDBI()
	// GetMediaByDBID returns zero-value result (MediaID == 0) when not found.
	db.On("GetMediaByDBID", mock.Anything, int64(999)).Return(
		database.SearchResultWithCursor{}, nil)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/media/999/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/media/999/properties",
		"/api/v1/media/{mediaDBID}/properties",
		HandleGetMediaProperties(db),
		req)
	assert.Equal(t, http.StatusNotFound, rr.Code)
	db.AssertExpectations(t)
}

func TestHandleGetMediaProperties_EmptyProperties(t *testing.T) {
	t.Parallel()

	db := helpers.NewMockMediaDBI()
	db.On("GetMediaByDBID", mock.Anything, int64(1)).Return(
		database.SearchResultWithCursor{MediaID: 1, Name: "Test ROM"}, nil)
	db.On("GetMediaProperties", mock.Anything, int64(1)).Return([]database.MediaProperty{}, nil)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/media/1/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/media/1/properties",
		"/api/v1/media/{mediaDBID}/properties",
		HandleGetMediaProperties(db),
		req)
	require.Equal(t, http.StatusOK, rr.Code)

	var out []propertyResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	assert.Empty(t, out)
	db.AssertExpectations(t)
}

func TestHandleGetMediaProperties_WithProperties(t *testing.T) {
	t.Parallel()

	props := []database.MediaProperty{
		{TypeTag: "property:video", Text: "/roms/snes/videos/mario.mp4", ContentType: "video/mp4"},
	}

	db := helpers.NewMockMediaDBI()
	db.On("GetMediaByDBID", mock.Anything, int64(5)).Return(
		database.SearchResultWithCursor{MediaID: 5, Name: "Test ROM"}, nil)
	db.On("GetMediaProperties", mock.Anything, int64(5)).Return(props, nil)

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodGet, "/api/v1/media/5/properties", http.NoBody)
	rr := routeRequest(http.MethodGet, "/api/v1/media/5/properties",
		"/api/v1/media/{mediaDBID}/properties",
		HandleGetMediaProperties(db),
		req)
	require.Equal(t, http.StatusOK, rr.Code)

	var out []propertyResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	require.Len(t, out, 1)
	assert.Equal(t, "property:video", out[0].TypeTag)
	assert.Equal(t, "/roms/snes/videos/mario.mp4", out[0].Text)
	db.AssertExpectations(t)
}
