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

// Logging hooks for the vendored chip cores. Core diagnostics are dropped:
// nothing in a VGM file is worth a log line per register write.

#ifndef ZVGM_LOGGING_H
#define ZVGM_LOGGING_H

#include "../stdtype.h"

#define DEVLOG_OFF 0x00
#define DEVLOG_ERROR 0x01
#define DEVLOG_WARN 0x02
#define DEVLOG_INFO 0x03
#define DEVLOG_DEBUG 0x04
#define DEVLOG_TRACE 0x05

typedef void (*DEVCB_LOG)(void *userParam, void *source, UINT8 level, const char *message);

typedef struct zvgm_dev_logger {
	DEVCB_LOG func;
	void *source;
	void *param;
} DEV_LOGGER;

static inline void dev_logger_set(DEV_LOGGER *logger, void *source, DEVCB_LOG func, void *param)
{
	logger->func = func;
	logger->source = source;
	logger->param = param;
}

static inline void emu_logf(const DEV_LOGGER *logger, UINT8 level, const char *format, ...)
{
	(void)logger;
	(void)level;
	(void)format;
}

#endif
