// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

// Crash probe runs separately so fatal failures cannot kill the test runner.
package main

/*
#include <stdio.h>
#include <stdlib.h>

static void native_exit(void) {
	fputs("native diagnostic before immediate exit\n", stderr);
	fflush(stderr);
	_Exit(23);
}
*/
import "C"

import (
	"os"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/crashdump"
)

func main() {
	if _, err := crashdump.Start(os.Args[1], "1.2.3"); err != nil {
		panic(err)
	}
	_, _ = os.Stderr.WriteString("ordinary stderr chatter\n")
	switch os.Args[2] {
	case "panic":
		go func() { panic("probe panic") }()
		select {}
	case "abort":
		C.abort()
	case "native-exit":
		C.native_exit()
	}
}
