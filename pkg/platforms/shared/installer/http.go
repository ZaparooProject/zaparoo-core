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

package installer

import (
	"context"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/shared/httpclient"
)

func DownloadHTTPFile(opts DownloaderArgs) error {
	// Extended timeout for potentially large game files (700MB+)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Create HTTP client with extended timeout for large files
	client := httpclient.NewClientWithTimeout(10 * time.Minute)

	// Use the shared httpclient's DownloadFile method
	downloadArgs := httpclient.DownloadFileArgs{
		URL:        opts.url,
		OutputPath: opts.finalPath,
		TempPath:   opts.tempPath,
	}

	return client.DownloadFile(ctx, downloadArgs)
}
