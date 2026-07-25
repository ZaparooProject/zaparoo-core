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
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/rs/zerolog/log"
)

type apiRequestPriority int

const (
	apiPriorityHigh apiRequestPriority = iota
	apiPriorityNormal
	apiPriorityLow
)

func requestTimeoutForAPIMethod(method string) time.Duration {
	if models.MethodHasUnboundedRuntime(method) {
		return 0
	}
	return config.APIRequestTimeout
}

func requestContextForAPIMethod(
	parent context.Context, method string,
) (context.Context, context.CancelFunc) {
	timeout := requestTimeoutForAPIMethod(method)
	if timeout == 0 {
		//nolint:gosec // Caller receives and owns the cancellation function.
		return context.WithCancel(parent)
	}
	//nolint:gosec // Caller receives and owns the cancellation function.
	return context.WithTimeout(parent, timeout)
}

func classifyAPIMethod(method string) apiRequestPriority {
	method = strings.ToLower(method)

	switch method {
	case models.MethodMediaHistoryLatest,
		models.MethodRun,
		models.MethodRunScript,
		models.MethodLaunch,
		models.MethodStop,
		models.MethodConfirm,
		models.MethodSettingsUpdate,
		models.MethodPlaytimeLimitsUpdate,
		models.MethodClientsDelete,
		models.MethodInboxDelete,
		models.MethodInboxClear,
		models.MethodReadersWrite,
		models.MethodReadersWriteCancel,
		models.MethodMediaActiveUpdate,
		models.MethodMediaControl,
		models.MethodMappingsNew,
		models.MethodMappingsDelete,
		models.MethodMappingsUpdate,
		models.MethodMappingsReload:
		return apiPriorityHigh
	case models.MethodMediaImage,
		models.MethodMediaGenerate,
		models.MethodMediaGenerateCancel,
		models.MethodMediaGenerateResume,
		models.MethodMediaIndex,
		models.MethodMediaScrape,
		models.MethodMediaScrapeStatus,
		models.MethodMediaScrapeCancel,
		models.MethodMediaScrapeResume,
		models.MethodMediaCleanOrphans,
		models.MethodSettingsLogsDownload:
		return apiPriorityLow
	default:
		if strings.HasPrefix(method, "media.scrape") || strings.HasPrefix(method, "media.generate") {
			return apiPriorityLow
		}
		return apiPriorityNormal
	}
}

func methodFromAPIRequestPayload(msg []byte) string {
	var req models.RequestObject
	if err := json.Unmarshal(msg, &req); err != nil {
		log.Debug().Err(err).Msg("failed to unmarshal API request payload")
		return ""
	}
	return strings.ToLower(req.Method)
}

func isImageAPIMethod(method string) bool {
	return strings.EqualFold(method, models.MethodMediaImage)
}

func isMediaDBTransactionAPIMethod(method string) bool {
	return strings.EqualFold(method, models.MethodMediaTagsUpdate) ||
		strings.EqualFold(method, models.MethodMediaMetaUpdate)
}

// isMediaDBFreeInstantMethod reports whether method is an instant control
// method that never touches MediaDB, so it must never wait on wsMediaDBMu
// behind a slow tag/meta write or a long-running indexing commit. run/launch
// only enqueue a token, stop only signals the platform launcher, and
// media.control only drives launcher controls (its script path is validated
// to disallow media-launching commands, so it never queries MediaDB either).
// TestIsControlAllowed_BlocksMediaDBReadingCommands in pkg/zapscript guards
// this invariant — it fails if a future MediaDB-reading command is ever added
// without being rejected by isControlAllowed.
func isMediaDBFreeInstantMethod(method string) bool {
	return strings.EqualFold(method, models.MethodRun) ||
		strings.EqualFold(method, models.MethodLaunch) ||
		strings.EqualFold(method, models.MethodStop) ||
		strings.EqualFold(method, models.MethodMediaControl)
}
