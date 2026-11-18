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

//nolint:revive,tagliatelle // custom validation tags (letter) are unknown to revive; wire shape is snake_case
package remote

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/validation"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
)

// This file is the Zaparoo Online remote-operations wire contract. Online
// speaks snake_case on both directions of an operation; Core's local API
// models are camelCase. Every field that crosses that boundary is listed
// here by hand: a params translator per verb (wire in -> method params
// out) and a result encoder per response type (method response in -> wire
// out). A field missing from these lists does not exist on the wire, so
// adding one to a local model never silently widens what a remote caller
// can send or see. Unknown, misspelled, or camelCase fields in incoming
// params are rejected, not ignored.

// decodeWireParams strictly decodes raw into out, then validates it.
func decodeWireParams(raw json.RawMessage, out any) error {
	if err := decodeParams(raw, out); err != nil {
		return err
	}
	if err := validation.DefaultValidator.Validate(out); err != nil {
		return fmt.Errorf("validate remote operation params: %w", err)
	}
	return nil
}

// encodeMethodParams marshals a Core params struct into the JSON the API
// method registry decodes.
func encodeMethodParams(params any) (json.RawMessage, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encode method params: %w", err)
	}
	return encoded, nil
}

// Params: wire -> Core method.

type wireSearchParams struct {
	Query       *string   `json:"query"`
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzy_system"`
	MaxResults  *int      `json:"max_results" validate:"omitempty,gt=0,max=100"`
	Cursor      *string   `json:"cursor"`
	Tags        *[]string `json:"tags" validate:"omitempty,dive,min=1"`
	Letter      *string   `json:"letter" validate:"omitempty,letter"`
	GroupBy     *string   `json:"group_by" validate:"omitempty,oneof=title"`
	TitleID     *int64    `json:"title_id" validate:"omitempty,gt=0"`
}

func translateSearchParams(raw json.RawMessage) (json.RawMessage, error) {
	var params wireSearchParams
	if err := decodeWireParams(raw, &params); err != nil {
		return nil, err
	}
	return encodeMethodParams(models.SearchParams{
		Query:       params.Query,
		Systems:     params.Systems,
		FuzzySystem: params.FuzzySystem,
		MaxResults:  params.MaxResults,
		Cursor:      params.Cursor,
		Tags:        params.Tags,
		Letter:      params.Letter,
		GroupBy:     params.GroupBy,
		TitleID:     params.TitleID,
	})
}

type wireVersionsParams struct {
	MediaID    *int64  `json:"media_id" validate:"omitempty,gt=0"`
	MaxResults *int    `json:"max_results" validate:"omitempty,gt=0,max=100"`
	Cursor     *string `json:"cursor"`
	System     string  `json:"system" validate:"omitempty,min=1"`
	Path       string  `json:"path" validate:"omitempty,min=1"`
}

func translateVersionsParams(raw json.RawMessage) (json.RawMessage, error) {
	var params wireVersionsParams
	if err := decodeWireParams(raw, &params); err != nil {
		return nil, err
	}
	return encodeMethodParams(models.MediaVersionsParams{
		MediaID:    params.MediaID,
		MaxResults: params.MaxResults,
		Cursor:     params.Cursor,
		System:     params.System,
		Path:       params.Path,
	})
}

// remoteSimilarMaxResults and remoteRecommendedMaxResults are sent when the
// operation names no size, so an oversized result has a real page size to
// halve.
const (
	remoteSimilarMaxResults     = 20
	remoteRecommendedMaxResults = 12
)

type wireSimilarParams struct {
	MediaID     *int64    `json:"media_id" validate:"omitempty,gt=0"`
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzy_system"`
	MaxResults  *int      `json:"max_results" validate:"omitempty,gt=0,max=50"`
	System      string    `json:"system" validate:"omitempty,min=1"`
	Path        string    `json:"path" validate:"omitempty,min=1"`
}

func translateSimilarParams(raw json.RawMessage) (json.RawMessage, error) {
	var params wireSimilarParams
	if err := decodeWireParams(raw, &params); err != nil {
		return nil, err
	}
	maxResults := remoteSimilarMaxResults
	if params.MaxResults != nil {
		maxResults = *params.MaxResults
	}
	return encodeMethodParams(models.MediaSimilarParams{
		MediaID:     params.MediaID,
		Systems:     params.Systems,
		FuzzySystem: params.FuzzySystem,
		MaxResults:  &maxResults,
		System:      params.System,
		Path:        params.Path,
	})
}

type wireRecommendedParams struct {
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzy_system"`
	MaxResults  *int      `json:"max_results" validate:"omitempty,gt=0,max=30"`
	MaxSections *int      `json:"max_sections" validate:"omitempty,gt=0,max=12"`
}

func translateRecommendedParams(raw json.RawMessage) (json.RawMessage, error) {
	var params wireRecommendedParams
	if err := decodeWireParams(raw, &params); err != nil {
		return nil, err
	}
	maxResults := remoteRecommendedMaxResults
	if params.MaxResults != nil {
		maxResults = *params.MaxResults
	}
	return encodeMethodParams(models.MediaRecommendedParams{
		Systems:     params.Systems,
		FuzzySystem: params.FuzzySystem,
		MaxResults:  &maxResults,
		MaxSections: params.MaxSections,
	})
}

type wireBrowseParams struct {
	Path        *string   `json:"path"`
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzy_system"`
	MaxResults  *int      `json:"max_results" validate:"omitempty,gt=0,max=100"`
	Cursor      *string   `json:"cursor"`
	Letter      *string   `json:"letter" validate:"omitempty,letter"`
	Sort        *string   `json:"sort" validate:"omitempty,oneof=name-asc name-desc filename-asc filename-desc"`
}

func translateBrowseParams(raw json.RawMessage) (json.RawMessage, error) {
	var params wireBrowseParams
	if err := decodeWireParams(raw, &params); err != nil {
		return nil, err
	}
	return encodeMethodParams(models.BrowseParams{
		Path:        params.Path,
		Systems:     params.Systems,
		FuzzySystem: params.FuzzySystem,
		MaxResults:  params.MaxResults,
		Cursor:      params.Cursor,
		Letter:      params.Letter,
		Sort:        params.Sort,
	})
}

type wireSystemsParams struct {
	All bool `json:"all"`
}

func translateSystemsParams(raw json.RawMessage) (json.RawMessage, error) {
	var params wireSystemsParams
	if err := decodeWireParams(raw, &params); err != nil {
		return nil, err
	}
	return encodeMethodParams(models.SystemsParams{All: params.All})
}

type wireLaunchersParams struct {
	Systems     *[]string `json:"systems" validate:"omitempty,dive,min=1"`
	FuzzySystem *bool     `json:"fuzzy_system"`
}

// translateLaunchersParams additionally requires a systems filter: the
// unfiltered launchers list has no shrink path and can exceed queryLimit on
// platforms with many launchers (250+ on MiSTer).
func translateLaunchersParams(raw json.RawMessage) (json.RawMessage, error) {
	var params wireLaunchersParams
	if err := decodeWireParams(raw, &params); err != nil {
		return nil, err
	}
	if params.Systems == nil || len(*params.Systems) == 0 {
		return nil, errors.New("systems filter is required")
	}
	return encodeMethodParams(models.LaunchersParams{
		Systems:     params.Systems,
		FuzzySystem: params.FuzzySystem,
	})
}

type wireEchoParams struct {
	Message string `json:"message" validate:"max=256"`
}

// translateEchoParams validates echo's params and returns them unchanged:
// echo runs locally, so there is no method params shape to translate to.
func translateEchoParams(raw json.RawMessage) (json.RawMessage, error) {
	var params wireEchoParams
	if err := decodeWireParams(raw, &params); err != nil {
		return nil, err
	}
	return raw, nil
}

type wireCommandParams struct {
	Value string `json:"value"`
}

// translateCommandParams validates a structural command verb's params and
// returns them unchanged for executeCommand, which does the value checks.
func translateCommandParams(raw json.RawMessage) (json.RawMessage, error) {
	var params wireCommandParams
	if err := decodeWireParams(raw, &params); err != nil {
		return nil, err
	}
	return raw, nil
}

// translateNoParams accepts absent params or an empty object only.
func translateNoParams(raw json.RawMessage) (json.RawMessage, error) {
	if err := requireEmptyParams(raw); err != nil {
		return nil, err
	}
	return json.RawMessage(`{}`), nil
}

// Results: Core method response -> wire.

// asResponse unwraps a method's response as T, accepting either the value
// or a pointer to it, so an encoder is indifferent to how a handler returns.
func asResponse[T any](response any) (T, bool) {
	switch typed := response.(type) {
	case *T:
		if typed != nil {
			return *typed, true
		}
	case T:
		return typed, true
	}
	var zero T
	return zero, false
}

var errUnexpectedResponse = errors.New("unexpected method response type")

type wireTag struct {
	Tag   string `json:"tag"`
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
	Count int64  `json:"count,omitempty"`
}

// encodeTags keeps a nil input nil so the wire carries the same null/empty
// distinction as the local API response it mirrors.
func encodeTags(tags []database.TagInfo) []wireTag {
	if tags == nil {
		return nil
	}
	out := make([]wireTag, 0, len(tags))
	for _, tag := range tags {
		out = append(out, wireTag{Tag: tag.Tag, Type: tag.Type, Label: tag.Label, Count: tag.Count})
	}
	return out
}

type wireSystem struct {
	ReleaseDate  *string  `json:"release_date,omitempty"`
	Manufacturer *string  `json:"manufacturer,omitempty"`
	MediaCount   *int     `json:"media_count,omitempty"`
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name,omitempty"`
	Category     string   `json:"category,omitempty"`
	ZapScript    string   `json:"zap_script,omitempty"`
	Categories   []string `json:"categories,omitempty"`
}

func encodeSystem(system *models.System) wireSystem {
	return wireSystem{
		ReleaseDate:  system.ReleaseDate,
		Manufacturer: system.Manufacturer,
		MediaCount:   system.MediaCount,
		ID:           system.ID,
		Name:         system.Name,
		Category:     system.Category,
		Categories:   system.Categories,
		ZapScript:    system.ZapScript,
	}
}

type wireSystemsResponse struct {
	Systems []wireSystem `json:"systems"`
}

func encodeSystemsResponse(response any) (any, error) {
	resp, ok := asResponse[models.SystemsResponse](response)
	if !ok {
		return nil, errUnexpectedResponse
	}
	out := wireSystemsResponse{}
	if resp.Systems != nil {
		out.Systems = make([]wireSystem, 0, len(resp.Systems))
		for i := range resp.Systems {
			out.Systems = append(out.Systems, encodeSystem(&resp.Systems[i]))
		}
	}
	return out, nil
}

type wireMisterCore struct {
	Name    string `json:"name"`
	File    string `json:"file"`
	MGLPath string `json:"mgl_path"`
}

type wireLauncher struct {
	MisterCore         *wireMisterCore `json:"mister_core,omitempty"`
	ID                 string          `json:"id"`
	SystemID           string          `json:"system_id,omitempty"`
	SystemName         string          `json:"system_name,omitempty"`
	AvailabilityReason string          `json:"availability_reason,omitempty"`
	Backend            string          `json:"backend,omitempty"`
	Groups             []string        `json:"groups,omitempty"`
	Available          bool            `json:"available"`
	Default            bool            `json:"default,omitempty"`
}

type wireLaunchersResponse struct {
	Launchers []wireLauncher `json:"launchers"`
}

func encodeLaunchersResponse(response any) (any, error) {
	resp, ok := asResponse[models.LaunchersResponse](response)
	if !ok {
		return nil, errUnexpectedResponse
	}
	out := wireLaunchersResponse{}
	if resp.Launchers != nil {
		out.Launchers = make([]wireLauncher, 0, len(resp.Launchers))
		for i := range resp.Launchers {
			launcher := &resp.Launchers[i]
			encoded := wireLauncher{
				ID:                 launcher.ID,
				SystemID:           launcher.SystemID,
				SystemName:         launcher.SystemName,
				AvailabilityReason: launcher.AvailabilityReason,
				Backend:            launcher.Backend,
				Groups:             launcher.Groups,
				Available:          launcher.Available,
				Default:            launcher.Default,
			}
			if launcher.MisterCore != nil {
				encoded.MisterCore = &wireMisterCore{
					Name:    launcher.MisterCore.Name,
					File:    launcher.MisterCore.File,
					MGLPath: launcher.MisterCore.MGLPath,
				}
			}
			out.Launchers = append(out.Launchers, encoded)
		}
	}
	return out, nil
}

type wirePagination struct {
	NextCursor  *string `json:"next_cursor,omitempty"`
	HasNextPage bool    `json:"has_next_page"`
	PageSize    int     `json:"page_size"`
}

func encodePagination(pagination *models.PaginationInfo) *wirePagination {
	if pagination == nil {
		return nil
	}
	return &wirePagination{
		NextCursor:  pagination.NextCursor,
		HasNextPage: pagination.HasNextPage,
		PageSize:    pagination.PageSize,
	}
}

type wireSearchResult struct {
	RelPath            *string    `json:"relative_path,omitempty"`
	VersionCount       *int       `json:"version_count,omitempty"`
	System             wireSystem `json:"system"`
	Name               string     `json:"name"`
	Path               string     `json:"path"`
	ZapScript          string     `json:"zap_script"`
	Tags               []wireTag  `json:"tags"`
	DisambiguatingTags []wireTag  `json:"disambiguating_tags,omitempty"`
	MediaID            int64      `json:"media_id,omitempty"`
	TitleID            int64      `json:"title_id,omitempty"`
	HasCover           bool       `json:"has_cover"`
}

type wireSearchResults struct {
	Pagination *wirePagination    `json:"pagination,omitempty"`
	Results    []wireSearchResult `json:"results"`
	Total      int                `json:"total"`
}

func encodeSearchResult(result *models.SearchResultMedia) wireSearchResult {
	return wireSearchResult{
		RelPath:            result.RelPath,
		System:             encodeSystem(&result.System),
		Name:               result.Name,
		Path:               result.Path,
		ZapScript:          result.ZapScript,
		Tags:               encodeTags(result.Tags),
		DisambiguatingTags: encodeTags(result.DisambiguatingTags),
		VersionCount:       result.VersionCount,
		MediaID:            result.MediaID,
		TitleID:            result.TitleID,
		HasCover:           result.HasCover,
	}
}

func encodeSearchResults(response any) (any, error) {
	resp, ok := asResponse[models.SearchResults](response)
	if !ok {
		return nil, errUnexpectedResponse
	}
	out := wireSearchResults{Pagination: encodePagination(resp.Pagination), Total: resp.Total}
	if resp.Results != nil {
		out.Results = make([]wireSearchResult, 0, len(resp.Results))
		for i := range resp.Results {
			out.Results = append(out.Results, encodeSearchResult(&resp.Results[i]))
		}
	}
	return out, nil
}

type wireVersion struct {
	Reason  string   `json:"reason"`
	Sources []string `json:"sources,omitempty"`
	wireSearchResult
	Current bool `json:"current,omitempty"`
}

type wireVersionsResults struct {
	Pagination *wirePagination `json:"pagination,omitempty"`
	Versions   []wireVersion   `json:"versions"`
	Total      int             `json:"total"`
}

func encodeVersionsResults(response any) (any, error) {
	resp, ok := asResponse[models.MediaVersionsResponse](response)
	if !ok {
		return nil, errUnexpectedResponse
	}
	out := wireVersionsResults{Pagination: encodePagination(resp.Pagination), Total: resp.Total}
	if resp.Versions != nil {
		out.Versions = make([]wireVersion, 0, len(resp.Versions))
		for i := range resp.Versions {
			version := &resp.Versions[i]
			out.Versions = append(out.Versions, wireVersion{
				wireSearchResult: encodeSearchResult(&version.SearchResultMedia),
				Reason:           version.Reason,
				Sources:          version.Sources,
				Current:          version.Current,
			})
		}
	}
	return out, nil
}

type wireReason struct {
	Code string    `json:"code"`
	Tags []wireTag `json:"tags,omitempty"`
}

type wireSuggestedMedia struct {
	Reasons []wireReason `json:"reasons"`
	wireSearchResult
}

func encodeSuggestedMedia(items []models.SuggestedMedia) []wireSuggestedMedia {
	out := make([]wireSuggestedMedia, 0, len(items))
	for i := range items {
		reasons := make([]wireReason, 0, len(items[i].Reasons))
		for _, reason := range items[i].Reasons {
			reasons = append(reasons, wireReason{Code: reason.Code, Tags: encodeTags(reason.Tags)})
		}
		out = append(out, wireSuggestedMedia{
			wireSearchResult: encodeSearchResult(&items[i].SearchResultMedia),
			Reasons:          reasons,
		})
	}
	return out
}

type wireSimilarResults struct {
	Results []wireSuggestedMedia `json:"results"`
}

func encodeSimilarResults(response any) (any, error) {
	resp, ok := asResponse[models.MediaSimilarResponse](response)
	if !ok {
		return nil, errUnexpectedResponse
	}
	return wireSimilarResults{Results: encodeSuggestedMedia(resp.Results)}, nil
}

type wireRecommendedSection struct {
	Seed   *wireSearchResult    `json:"seed,omitempty"`
	Tag    *wireTag             `json:"tag,omitempty"`
	Kind   string               `json:"kind"`
	Signal string               `json:"signal,omitempty"`
	Items  []wireSuggestedMedia `json:"items"`
}

type wireRecommendedResults struct {
	Sections []wireRecommendedSection `json:"sections"`
}

func encodeRecommendedResults(response any) (any, error) {
	resp, ok := asResponse[models.MediaRecommendedResponse](response)
	if !ok {
		return nil, errUnexpectedResponse
	}
	out := wireRecommendedResults{Sections: make([]wireRecommendedSection, 0, len(resp.Sections))}
	for i := range resp.Sections {
		section := &resp.Sections[i]
		wire := wireRecommendedSection{
			Kind:   section.Kind,
			Signal: section.Signal,
			Items:  encodeSuggestedMedia(section.Items),
		}
		if section.Seed != nil {
			seed := encodeSearchResult(section.Seed)
			wire.Seed = &seed
		}
		if section.Tag != nil {
			wire.Tag = &wireTag{Tag: section.Tag.Tag, Type: section.Tag.Type, Label: section.Tag.Label}
		}
		out.Sections = append(out.Sections, wire)
	}
	return out, nil
}

type wireBrowseEntry struct {
	SystemID           *string   `json:"system_id,omitempty"`
	RelPath            *string   `json:"relative_path,omitempty"`
	ZapScript          *string   `json:"zap_script,omitempty"`
	FileCount          *int      `json:"file_count,omitempty"`
	Group              *string   `json:"group,omitempty"`
	VersionCount       *int      `json:"version_count,omitempty"`
	Path               string    `json:"path"`
	Type               string    `json:"type"`
	Name               string    `json:"name"`
	SystemIDs          []string  `json:"system_ids,omitempty"`
	Tags               []wireTag `json:"tags,omitempty"`
	DisambiguatingTags []wireTag `json:"disambiguating_tags,omitempty"`
	MediaID            int64     `json:"media_id,omitempty"`
	HasCover           bool      `json:"has_cover"`
}

type wireBrowseResults struct {
	Pagination *wirePagination   `json:"pagination,omitempty"`
	Path       string            `json:"path"`
	Entries    []wireBrowseEntry `json:"entries"`
	TotalFiles int               `json:"total_files"`
	TotalDirs  int               `json:"total_dirs"`
}

func encodeBrowseResults(response any) (any, error) {
	resp, ok := asResponse[models.BrowseResults](response)
	if !ok {
		return nil, errUnexpectedResponse
	}
	out := wireBrowseResults{
		Pagination: encodePagination(resp.Pagination),
		Path:       resp.Path,
		TotalFiles: resp.TotalFiles,
		TotalDirs:  resp.TotalDirs,
	}
	if resp.Entries != nil {
		out.Entries = make([]wireBrowseEntry, 0, len(resp.Entries))
		for i := range resp.Entries {
			entry := &resp.Entries[i]
			out.Entries = append(out.Entries, wireBrowseEntry{
				SystemID:           entry.SystemID,
				RelPath:            entry.RelPath,
				ZapScript:          entry.ZapScript,
				FileCount:          entry.FileCount,
				Group:              entry.Group,
				Path:               entry.Path,
				Type:               entry.Type,
				Name:               entry.Name,
				SystemIDs:          entry.SystemIDs,
				Tags:               encodeTags(entry.Tags),
				DisambiguatingTags: encodeTags(entry.DisambiguatingTags),
				VersionCount:       entry.VersionCount,
				MediaID:            entry.MediaID,
				HasCover:           entry.HasCover,
			})
		}
	}
	return out, nil
}

type wireVersionResponse struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

func encodeVersionResponse(response any) (any, error) {
	resp, ok := asResponse[models.VersionResponse](response)
	if !ok {
		return nil, errUnexpectedResponse
	}
	return wireVersionResponse{Version: resp.Version, Platform: resp.Platform}, nil
}

// encodeEmptyResult reports a side-effect verb's success as an empty object
// regardless of what the method returned: stop's response carries nothing a
// remote caller is entitled to see.
func encodeEmptyResult(any) (any, error) {
	return map[string]any{}, nil
}
