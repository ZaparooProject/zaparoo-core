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

package zapscript

import (
	"testing"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Builtins expose methods even when Core's expression environment has none.
// Dependency/build optimizations must retain these supported expressions.
func TestExpressionMethodCompatibility(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		expression string
		want       string
	}{
		{`duration("1h").Seconds()`, "3600"},
		{`date("2023-08-14").Year()`, "2023"},
		{`date("2023-08-14").Add(duration("24h")).Format("2006-01-02")`, "2023-08-15"},
		{`get([date("2023-08-14")], 0).Year()`, "2023"},
		{`device.hostname + "-" + string(duration("1h").Seconds())`, "test-device-3600"},
	} {
		t.Run(tt.expression, func(t *testing.T) {
			t.Parallel()
			parser := gozapscript.NewParser(gozapscript.TokExpStart + tt.expression + gozapscript.TokExprEnd)
			got, err := parser.EvalExpressions(gozapscript.ArgExprEnv{
				Device: gozapscript.ExprEnvDevice{Hostname: "test-device"},
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
