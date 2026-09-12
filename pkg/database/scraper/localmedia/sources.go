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

package localmedia

import (
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esmedia"
)

func mediaArtworkNames(
	media *database.MediaWithFullPath, roots []string,
	containers scraper.ContainerResolver, sources *scraper.SourceIndex, cleanup bool,
) (lookupRoots, names, cleanupNames []string) {
	if sources != nil && sources.HasMedia(media.Path) {
		source, ok := sources.ForMedia(media.Path)
		if !ok || media.IsMissing {
			return nil, nil, nil
		}
		if _, unique := sources.ForDirectory(source.Directory); !unique {
			return nil, nil, nil
		}
		// Same-named directories on separate drives belong to different virtual
		// targets. Do not let cross-root artwork fallback attach one to another.
		root := filepath.Dir(source.Directory)
		names = esmedia.DirectoryArtworkFallbackNames(source.Directory, root)
		return []string{root}, names, names
	}
	isContainer := isContainerLaunchTarget(containers, media)
	names = artworkFallbackNames(media.Path, roots, isContainer)
	if cleanup {
		cleanupNames = names
		if !isContainer {
			cleanupNames = artworkFallbackNames(media.Path, roots, true)
		}
	}
	return roots, names, cleanupNames
}
