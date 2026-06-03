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

package api

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/stretchr/testify/assert"
)

func TestClassifyAPIMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		want   apiRequestPriority
	}{
		{"high media tags update", models.MethodMediaTagsUpdate, apiPriorityHigh},
		{"high run case insensitive", "RUN", apiPriorityHigh},
		{"low media generate", models.MethodMediaGenerate, apiPriorityLow},
		{"low media image", models.MethodMediaImage, apiPriorityLow},
		{"low scrape prefix", "media.scrape.queue", apiPriorityLow},
		{"low generate prefix", "media.generate.extra", apiPriorityLow},
		{"unknown normal", "custom.method", apiPriorityNormal},
		{"empty normal", "", apiPriorityNormal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, classifyAPIMethod(tt.method))
		})
	}
}

func TestMethodFromAPIRequestPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
		msg  []byte
	}{
		{
			name: "valid lower",
			msg:  []byte(`{"jsonrpc":"2.0","method":"media.image","id":1}`),
			want: "media.image",
		},
		{
			name: "valid mixed case",
			msg:  []byte(`{"jsonrpc":"2.0","method":"Media.Tags.Update","id":1}`),
			want: "media.tags.update",
		},
		{name: "missing method", msg: []byte(`{"jsonrpc":"2.0","id":1}`), want: ""},
		{name: "empty method", msg: []byte(`{"jsonrpc":"2.0","method":"","id":1}`), want: ""},
		{name: "malformed json", msg: []byte(`{"jsonrpc":"2.0","method":`), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, methodFromAPIRequestPayload(tt.msg))
		})
	}
}

func TestIsImageAPIMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		want   bool
	}{
		{"image exact", models.MethodMediaImage, true},
		{"image case insensitive", "MEDIA.IMAGE", true},
		{"other method", models.MethodMediaMeta, false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isImageAPIMethod(tt.method))
		})
	}
}

func TestIsMediaDBTransactionAPIMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		want   bool
	}{
		{"tags update exact", models.MethodMediaTagsUpdate, true},
		{"tags update case insensitive", "MEDIA.TAGS.UPDATE", true},
		{"image false", models.MethodMediaImage, false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isMediaDBTransactionAPIMethod(tt.method))
		})
	}
}
