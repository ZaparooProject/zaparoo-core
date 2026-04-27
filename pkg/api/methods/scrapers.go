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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// propertyResponse is the JSON shape for properties read endpoints.
type propertyResponse struct {
	TypeTag     string `json:"typeTag"`
	ContentType string `json:"contentType"`
	Text        string `json:"text"`
}

// HandleGetMediaTitleProperties returns a handler for
// GET /api/v1/titles/{titleDBID}/properties.
func HandleGetMediaTitleProperties(db database.MediaDBI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbid, err := parseDBID(chi.URLParam(r, "titleDBID"))
		if err != nil {
			http.Error(w, "invalid title ID", http.StatusBadRequest)
			return
		}

		// 404 if title does not exist.
		title, err := db.FindMediaTitleByDBID(r.Context(), dbid)
		if err != nil {
			log.Error().Err(err).Int64("titleDBID", dbid).Msg("scrapers: FindMediaTitleByDBID error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if title == nil {
			http.Error(w, "title not found", http.StatusNotFound)
			return
		}

		props, err := db.GetMediaTitleProperties(r.Context(), dbid)
		if err != nil {
			log.Error().Err(err).Int64("titleDBID", dbid).Msg("scrapers: GetMediaTitleProperties error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, toPropertyResponses(props))
	}
}

// HandleGetMediaProperties returns a handler for
// GET /api/v1/media/{mediaDBID}/properties.
func HandleGetMediaProperties(db database.MediaDBI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbid, err := parseDBID(chi.URLParam(r, "mediaDBID"))
		if err != nil {
			http.Error(w, "invalid media ID", http.StatusBadRequest)
			return
		}

		// 404 if media does not exist.
		result, err := db.GetMediaByDBID(r.Context(), dbid)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && result.MediaID == 0) {
			http.Error(w, "media not found", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Error().Err(err).Int64("mediaDBID", dbid).Msg("scrapers: GetMediaByDBID error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		props, err := db.GetMediaProperties(r.Context(), dbid)
		if err != nil {
			log.Error().Err(err).Int64("mediaDBID", dbid).Msg("scrapers: GetMediaProperties error")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, toPropertyResponses(props))
	}
}

func parseDBID(s string) (int64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("invalid ID: %q", s)
	}
	return v, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Error().Err(err).Msg("scrapers: failed to marshal response")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		log.Debug().Err(err).Msg("scrapers: response write error")
	}
}

func toPropertyResponses(props []database.MediaProperty) []propertyResponse {
	out := make([]propertyResponse, 0, len(props))
	for _, p := range props {
		out = append(out, propertyResponse{
			TypeTag:     p.TypeTag,
			ContentType: p.ContentType,
			Text:        p.Text,
		})
	}
	return out
}
