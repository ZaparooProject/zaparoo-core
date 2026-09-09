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
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/apidiag"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
)

// BenchmarkAPIDiagnosticLifecycle isolates instrumentation cost. No library I/O,
// network, or application latency is included; pool handles are unavailable.
func BenchmarkAPIDiagnosticLifecycle(b *testing.B) {
	response := make([]string, 25)
	for i := range response {
		response[i] = "Synthetic benchmark result"
	}
	env := &requests.RequestEnv{Database: &database.Database{MediaDB: &mediadb.MediaDB{}, UserDB: &userdb.UserDB{}}}
	for _, mode := range []string{"control", "disabled", "enabled", "operation_timeout"} {
		b.Run(mode, func(b *testing.B) {
			methodMap := NewMethodMap()
			reports := 0
			if mode == "enabled" || mode == "operation_timeout" {
				methodMap.timeoutReporter = func(apidiag.Report) { reports++ }
			}
			b.ReportAllocs()
			for b.Loop() {
				ctx, cancel := context.WithTimeout(b.Context(), 30*time.Second)
				var recorder *apidiag.Recorder
				if mode != "control" {
					ctx, recorder = beginAPIDiagnostics(ctx, methodMap, env, "media.search", apidiag.HTTP, 0)
					endHandler := apidiag.Begin(ctx, apidiag.Handler)
					for _, stage := range []apidiag.Stage{
						apidiag.ConcurrencySlot, apidiag.DatabasePool, apidiag.Database,
					} {
						end := apidiag.Begin(ctx, stage)
						end()
					}
					if mode == "operation_timeout" {
						apidiag.RecordError(ctx, context.DeadlineExceeded)
					}
					apidiag.RecordError(ctx, nil)
					endHandler()
				}
				var data []byte
				var err error
				if mode == "control" {
					data, err = json.Marshal(response)
				} else {
					data, err = marshalDiagnosticResponse(ctx, response)
					end := apidiag.Begin(ctx, apidiag.ResponseWrite)
					end()
				}
				if err != nil || len(data) == 0 {
					b.Fatal("response serialization failed", err)
				}
				recorder.Finish()
				cancel()
			}
			if mode == "operation_timeout" {
				if reports != b.N {
					b.Fatalf("got %d reports for %d requests", reports, b.N)
				}
			} else if reports != 0 {
				b.Fatal("unexpected timeout report")
			}
		})
	}
}
