// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

#include <limits.h>

// CI must reject this intentional overflow. A successful exit means the
// sanitizer configuration is missing or permits recovery.
int main(void) {
    volatile int maximum = INT_MAX;
    volatile int result = maximum + 1;
    return result == 0;
}
