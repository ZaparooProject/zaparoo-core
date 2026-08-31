//go:build linux

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

package mister

import (
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/spf13/afero"
)

const operatorReaderID = "operator"

var (
	operatorEntryPath = filepath.Join(misterconfig.ScriptsDir, "Operator.sh")
	operatorTokenPath = filepath.Join(string(filepath.Separator), "tmp", "zaparoo-operator.token")
)

func newOperatorReader(cfg *config.Instance, fs afero.Fs) *file.Reader {
	if fs == nil {
		fs = afero.NewOsFs()
	}

	return file.NewReaderWithOptions(cfg, &file.ReaderOptions{
		Metadata: readers.DriverMetadata{
			ID:                operatorReaderID,
			DefaultEnabled:    true,
			DefaultAutoDetect: true,
			Description:       "Epilogue Operator cartridge bridge",
		},
		IDs:            []string{operatorReaderID},
		AutoDetectPath: operatorTokenPath,
		IsAvailable: func() bool {
			info, err := fs.Stat(operatorEntryPath)
			return err == nil && info.Mode().IsRegular()
		},
	})
}
