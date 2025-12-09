//go:build linux

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package helpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBrowserURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		errMsg  string
		wantErr bool
	}{
		{
			name:    "valid_http_url",
			url:     "http://localhost:7497/app/",
			wantErr: false,
		},
		{
			name:    "valid_https_url",
			url:     "https://example.com/path",
			wantErr: false,
		},
		{
			name:    "valid_http_uppercase",
			url:     "HTTP://localhost:7497/",
			wantErr: false,
		},
		{
			name:    "valid_https_uppercase",
			url:     "HTTPS://example.com/",
			wantErr: false,
		},
		{
			name:    "valid_mixed_case_http",
			url:     "HtTp://localhost/",
			wantErr: false,
		},
		{
			name:    "valid_mixed_case_https",
			url:     "HtTpS://localhost/",
			wantErr: false,
		},
		{
			name:    "invalid_file_scheme",
			url:     "file:///etc/passwd",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
		{
			name:    "invalid_ftp_scheme",
			url:     "ftp://example.com/file",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
		{
			name:    "invalid_javascript_scheme",
			url:     "javascript:alert(1)",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
		{
			name:    "invalid_data_scheme",
			url:     "data:text/html,<script>alert(1)</script>",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
		{
			name:    "invalid_no_scheme",
			url:     "localhost:7497/app/",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
		{
			name:    "invalid_empty_url",
			url:     "",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
		{
			name:    "invalid_just_http",
			url:     "http",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
		{
			name:    "invalid_http_without_colon_slash",
			url:     "http/example.com",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Test ValidateBrowserURL directly - this doesn't execute xdg-open
			err := ValidateBrowserURL(tt.url)

			if tt.wantErr {
				require.Error(t, err, "ValidateBrowserURL should return error for invalid URL")
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err, "ValidateBrowserURL should accept valid URL")
			}
		})
	}
}
