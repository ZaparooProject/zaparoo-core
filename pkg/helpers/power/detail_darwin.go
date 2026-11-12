//go:build darwin

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

package power

import (
	"context"
	"fmt"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
)

// ReadDetail reports every battery pmset knows about.
func ReadDetail() (Detail, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pmsetTimeout)
	defer cancel()

	return readDetailDarwin(ctx, &command.RealExecutor{})
}

func readDetailDarwin(ctx context.Context, executor command.Executor) (Detail, error) {
	output, err := executor.Output(ctx, pmsetPath, "-g", "batt")
	if err != nil {
		return Detail{}, fmt.Errorf("reading battery detail from pmset: %w", err)
	}
	return parsePmsetDetail(string(output)), nil
}
