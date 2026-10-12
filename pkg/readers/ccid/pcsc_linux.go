//go:build linux && !android

/*
Zaparoo Core
Copyright (c) 2026 The Zaparoo Project Contributors.
SPDX-License-Identifier: GPL-3.0-or-later

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

//nolint:gocritic // the checks misread the code cgo generates for this file
package ccid

/*
#cgo LDFLAGS: -ldl

#include <dlfcn.h>
#include <stdlib.h>

// The pcsc-lite types, declared here because the library is loaded at run
// time and its headers are not available where release builds are made.
typedef long zap_long;
typedef unsigned long zap_dword;

typedef struct {
	const char *reader;
	void *user_data;
	zap_dword current_state;
	zap_dword event_state;
	zap_dword atr_len;
	unsigned char atr[33];
} zap_reader_state;

typedef struct {
	unsigned long protocol;
	unsigned long pci_length;
} zap_io_request;

static void *zap_pcsc_lib;
static zap_long (*zap_establish)(zap_dword, const void *, const void *, zap_long *);
static zap_long (*zap_release)(zap_long);
static zap_long (*zap_list)(zap_long, const char *, char *, zap_dword *);
static zap_long (*zap_status)(zap_long, zap_dword, zap_reader_state *, zap_dword);
static zap_long (*zap_connect)(zap_long, const char *, zap_dword, zap_dword, zap_long *, zap_dword *);
static zap_long (*zap_disconnect)(zap_long, zap_dword);
static zap_long (*zap_transmit)(zap_long, const zap_io_request *, const unsigned char *, zap_dword,
	zap_io_request *, unsigned char *, zap_dword *);

// zap_pcsc_load returns 1 when the library and every function were found.
static int zap_pcsc_load(void) {
	zap_pcsc_lib = dlopen("libpcsclite.so.1", RTLD_NOW | RTLD_LOCAL);
	if (zap_pcsc_lib == NULL) {
		return 0;
	}
	zap_establish = dlsym(zap_pcsc_lib, "SCardEstablishContext");
	zap_release = dlsym(zap_pcsc_lib, "SCardReleaseContext");
	zap_list = dlsym(zap_pcsc_lib, "SCardListReaders");
	zap_status = dlsym(zap_pcsc_lib, "SCardGetStatusChange");
	zap_connect = dlsym(zap_pcsc_lib, "SCardConnect");
	zap_disconnect = dlsym(zap_pcsc_lib, "SCardDisconnect");
	zap_transmit = dlsym(zap_pcsc_lib, "SCardTransmit");
	return zap_establish && zap_release && zap_list && zap_status && zap_connect && zap_disconnect &&
		zap_transmit;
}

static zap_long zap_call_establish(zap_long *ctx) {
	return zap_establish(2, NULL, NULL, ctx);
}

static zap_long zap_call_release(zap_long ctx) {
	return zap_release(ctx);
}

static zap_long zap_call_list(zap_long ctx, char *buf, zap_dword *len) {
	return zap_list(ctx, NULL, buf, len);
}

static zap_long zap_call_status(zap_long ctx, zap_reader_state *state) {
	return zap_status(ctx, 0, state, 1);
}

static zap_long zap_call_connect(zap_long ctx, const char *reader, zap_long *card, zap_dword *protocol) {
	return zap_connect(ctx, reader, 2, 3, card, protocol);
}

static zap_long zap_call_disconnect(zap_long card, zap_dword disposition) {
	return zap_disconnect(card, disposition);
}

static zap_long zap_call_transmit(zap_long card, zap_dword protocol, const unsigned char *send,
	zap_dword send_len, unsigned char *recv, zap_dword *recv_len) {
	zap_io_request pci = {protocol, sizeof(zap_io_request)};
	return zap_transmit(card, &pci, send, send_len, NULL, recv, recv_len);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

// PC/SC result codes. They are compared as 32-bit values because the library
// returns them in a long, which sign-extends differently by word size.
const (
	pcscSuccess            = 0x00000000
	pcscUnknownReader      = 0x80100009
	pcscReaderUnavailable  = 0x80100017
	pcscNoService          = 0x8010001D
	pcscServiceStopped     = 0x8010001E
	pcscNoReadersAvailable = 0x8010002E

	pcscStatePresent     = 0x0020
	pcscStateUnknown     = 0x0004
	pcscStateUnavailable = 0x0008

	pcscLeaveCard = 0
	pcscResetCard = 1

	pcscMaxResponse = 512
)

var errNoPCSCLibrary = errors.New("libpcsclite.so.1 is not installed")

// loadPCSC loads the library once. It is left loaded for the life of the
// process.
var loadPCSC = sync.OnceValue(func() bool {
	return C.zap_pcsc_load() == 1
})

type pcscError uint32

func (c pcscError) Error() string {
	switch c {
	case pcscNoService:
		return "PC/SC service is not running"
	case pcscServiceStopped:
		return "PC/SC service has stopped"
	default:
		return fmt.Sprintf("PC/SC error 0x%08X", uint32(c))
	}
}

func pcscResult(rv C.zap_long, op string) error {
	code := pcscError(uint32(rv)) //nolint:gosec // truncation to the 32-bit code is the point
	if code == pcscSuccess {
		return nil
	}
	switch code {
	case pcscUnknownReader, pcscReaderUnavailable, pcscNoService, pcscServiceStopped:
		return fmt.Errorf("%s: %w: %w", op, errReaderGone, code)
	default:
		return fmt.Errorf("%s: %w", op, code)
	}
}

func establishPCSC() (pcscContext, error) {
	if !loadPCSC() {
		return nil, errNoPCSCLibrary
	}
	var handle C.zap_long
	rv := C.zap_call_establish(&handle)
	if code := pcscError(uint32(rv)); code != pcscSuccess { //nolint:gosec // 32-bit result code
		// Not a missing reader: there is no service to ask about readers.
		return nil, fmt.Errorf("establish PC/SC context: %w", code)
	}
	return &dlContext{handle: handle}, nil
}

type dlContext struct {
	handle C.zap_long
}

func (c *dlContext) ListReaders() ([]string, error) {
	var size C.zap_dword
	rv := C.zap_call_list(c.handle, nil, &size)
	if pcscError(uint32(rv)) == pcscNoReadersAvailable || size == 0 { //nolint:gosec // 32-bit result code
		return nil, nil
	}
	if err := pcscResult(rv, "list readers"); err != nil {
		return nil, err
	}

	buf := make([]byte, size)
	rv = C.zap_call_list(c.handle, (*C.char)(unsafe.Pointer(&buf[0])), &size)
	if pcscError(uint32(rv)) == pcscNoReadersAvailable { //nolint:gosec // 32-bit result code
		return nil, nil
	}
	if err := pcscResult(rv, "list readers"); err != nil {
		return nil, err
	}

	var names []string
	for _, name := range strings.Split(string(buf[:size]), "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func (c *dlContext) Status(name string) (pcscStatus, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	state := C.zap_reader_state{reader: cname}
	if err := pcscResult(C.zap_call_status(c.handle, &state), "reader status"); err != nil {
		return pcscStatus{}, err
	}
	if state.event_state&(pcscStateUnknown|pcscStateUnavailable) != 0 {
		return pcscStatus{}, errReaderGone
	}

	status := pcscStatus{Present: state.event_state&pcscStatePresent != 0}
	if n := int(state.atr_len); n > 0 && n <= len(state.atr) {
		status.ATR = C.GoBytes(unsafe.Pointer(&state.atr[0]), C.int(n))
	}
	return status, nil
}

func (c *dlContext) Connect(name string) (pcscCard, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	var handle C.zap_long
	var protocol C.zap_dword
	if err := pcscResult(C.zap_call_connect(c.handle, cname, &handle, &protocol), "connect"); err != nil {
		return nil, err
	}
	return &dlCard{handle: handle, protocol: protocol}, nil
}

func (c *dlContext) Release() error {
	return pcscResult(C.zap_call_release(c.handle), "release PC/SC context")
}

type dlCard struct {
	handle   C.zap_long
	protocol C.zap_dword
}

func (c *dlCard) Transmit(apdu []byte) ([]byte, error) {
	if len(apdu) == 0 {
		return nil, errors.New("empty APDU")
	}
	recv := make([]byte, pcscMaxResponse)
	recvLen := C.zap_dword(len(recv))
	rv := C.zap_call_transmit(
		c.handle, c.protocol,
		(*C.uchar)(unsafe.Pointer(&apdu[0])), C.zap_dword(len(apdu)),
		(*C.uchar)(unsafe.Pointer(&recv[0])), &recvLen,
	)
	if err := pcscResult(rv, "transmit"); err != nil {
		return nil, err
	}
	if int(recvLen) > len(recv) {
		return nil, errors.New("transmit: response longer than the buffer")
	}
	return recv[:recvLen], nil
}

func (c *dlCard) Disconnect(reset bool) error {
	disposition := C.zap_dword(pcscLeaveCard)
	if reset {
		disposition = pcscResetCard
	}
	return pcscResult(C.zap_call_disconnect(c.handle, disposition), "disconnect")
}
