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

package mediascanner

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/sourcepath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
)

// indexResults adds backpressured host records without growing the ordinary
// scanner's in-memory file list. Stopping the range stops the host walk too.
func indexResults(
	ctx context.Context, files []platforms.ScanResult, host platforms.HostMediaScan,
	system string, checkpoint func() error,
) iter.Seq2[platforms.ScanResult, error] {
	return func(yield func(platforms.ScanResult, error) bool) {
		for _, file := range files {
			if !yield(file, nil) {
				return
			}
		}
		if host == nil {
			return
		}
		stopped := errors.New("index consumer stopped")
		err := host.Walk(ctx, system, checkpoint, func(file platforms.ScanResult) error {
			if _, _, err := sourcepath.Parse(file.Path); err != nil {
				return fmt.Errorf("validate host identity: %w", err)
			}
			if !yield(file, nil) {
				return stopped
			}
			return nil
		})
		if err != nil && !errors.Is(err, stopped) {
			yield(platforms.ScanResult{}, err)
		}
	}
}
