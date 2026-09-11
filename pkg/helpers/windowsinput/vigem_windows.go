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

//go:build windows

package windowsinput

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"golang.org/x/sys/windows"
)

// SetupAPI device-interface enumeration is not wrapped by x/sys/windows.
//
//nolint:gochecknoglobals // lazily resolved system DLL procedures
var (
	modsetupapi = windows.NewLazySystemDLL("setupapi.dll")

	procSetupDiEnumDeviceInterfaces      = modsetupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = modsetupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
)

// guidDevInterfaceViGEmBus is {96E42B22-F5E9-42F8-B043-ED0F932F014F}, the
// device interface the ViGEmBus driver exposes.
const (
	// vigemCommonVersion is the protocol version the bus was built against.
	// It refuses a client that reports any other, so this has to be checked
	// before anything is plugged in.
	vigemCommonVersion = 0x0001

	// targetTypeXbox360Wired is VIGEM_TARGET_TYPE Xbox360Wired.
	targetTypeXbox360Wired = 0

	// serialAttempts bounds the search for a free bus slot. The bus refuses a
	// serial another client already holds, so a client has to probe for one;
	// XInput only ever surfaces four pads, so a machine that needs more
	// attempts than this has a different problem.
	serialAttempts = 32

	// waitTimeout is WAIT_TIMEOUT, which x/sys/windows does not declare.
	waitTimeout = 0x102

	// ioctlTimeoutMillis bounds a request the bus keeps pending, in the
	// milliseconds WaitForSingleObject wants. Plugging a pad in takes
	// milliseconds; ten seconds is long enough that a slow machine is never
	// cut off, and short enough that a driver which has stopped answering does
	// not hold up startup indefinitely.
	ioctlTimeoutMillis = 10_000
)

// ErrDriverUnresponsive reports that the driver accepted a request and never
// completed it.
var ErrDriverUnresponsive = errors.New("the ViGEmBus driver stopped responding")

// maxDeviceDetailSize bounds the SetupAPI detail buffer. A device interface
// path is a few hundred bytes at most.
const maxDeviceDetailSize = 4096

//nolint:gochecknoglobals // constant GUID
var guidDevInterfaceViGEmBus = windows.GUID{
	Data1: 0x96E42B22, Data2: 0xF5E9, Data3: 0x42F8,
	Data4: [8]byte{0xB0, 0x43, 0xED, 0x0F, 0x93, 0x2F, 0x01, 0x4F},
}

type spDeviceInterfaceData struct {
	cbSize             uint32
	interfaceClassGUID windows.GUID
	flags              uint32
	reserved           uintptr
}

// Gamepad is a virtual Xbox 360 pad plugged into the ViGEmBus driver.
//
// The driver's device interface carries no restrictive ACL, so an unelevated
// process in the user's own session can drive it. Administrator rights are
// only needed once, to install the driver.
type Gamepad struct {
	handle windows.Handle
	event  windows.Handle
	serial uint32
	report xusbReport
	mu     syncutil.Mutex
}

// NewGamepad plugs a virtual Xbox 360 pad into ViGEmBus. It returns
// ErrDriverMissing when the driver is not installed.
func NewGamepad() (*Gamepad, error) {
	path, err := vigemDevicePath()
	if err != nil {
		return nil, err
	}

	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("vigem device path %q: %w", path, err)
	}

	handle, err := windows.CreateFile(p,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, fmt.Errorf("open vigem bus: %w", err)
	}

	// The bus refuses a client whose protocol version it was not built
	// against, so this has to happen before anything is plugged in.
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("create vigem event: %w", err)
	}

	g := &Gamepad{handle: handle, event: event}

	version := vigemCheckVersion{
		Size:    uint32(unsafe.Sizeof(vigemCheckVersion{})),
		Version: vigemCommonVersion,
	}
	if err := ioctl(g, ioctlCheckVersion, &version, version.Size); err != nil {
		g.closeHandles()
		return nil, fmt.Errorf("vigem version check: %w", err)
	}

	if err := g.plugIn(); err != nil {
		g.closeHandles()
		return nil, err
	}
	return g, nil
}

// ButtonDown presses a button, identified by its evdev button code.
func (g *Gamepad) ButtonDown(code int) error {
	return g.setButton(code, true)
}

// ButtonUp releases a button, identified by its evdev button code.
func (g *Gamepad) ButtonUp(code int) error {
	return g.setButton(code, false)
}

// Close unplugs the pad and releases the bus handle.
func (g *Gamepad) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.handle == windows.InvalidHandle {
		return nil
	}

	unplug := vigemUnplugTarget{
		Size:     uint32(unsafe.Sizeof(vigemUnplugTarget{})),
		SerialNo: g.serial,
	}
	err := ioctl(g, ioctlUnplugTarget, &unplug, unplug.Size)
	g.closeHandles()
	if err != nil {
		return fmt.Errorf("unplug virtual gamepad: %w", err)
	}
	return nil
}

func (g *Gamepad) setButton(code int, down bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if err := g.report.setButton(code, down); err != nil {
		return err
	}
	return g.submit()
}

// submit sends the current report. The caller must hold g.mu.
func (g *Gamepad) submit() error {
	report := xusbSubmitReport{
		Size:     uint32(unsafe.Sizeof(xusbSubmitReport{})),
		SerialNo: g.serial,
		Report:   g.report,
	}
	// The driver answers ERROR_NO_MORE_ITEMS when nothing has polled the pad
	// yet, because it had no pending USB IN request to complete. The report is
	// still stored and sent on the next poll, so this is not a failure.
	err := ioctl(g, ioctlXusbSubmitReport, &report, report.Size)
	if err != nil && !errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
		return fmt.Errorf("submit gamepad report: %w", err)
	}
	return nil
}

// plugIn claims the first free bus slot. The caller must not hold g.mu.
func (g *Gamepad) plugIn() error {
	var lastErr error
	for serial := uint32(1); serial <= serialAttempts; serial++ {
		plugin := vigemPluginTarget{
			Size:       uint32(unsafe.Sizeof(vigemPluginTarget{})),
			SerialNo:   serial,
			TargetType: targetTypeXbox360Wired,
		}
		if err := ioctl(g, ioctlPluginTarget, &plugin, plugin.Size); err != nil {
			lastErr = err
			continue
		}

		// Plugging in only queues the child device; the bus keeps this second
		// request pending until the pad is powered up and able to take
		// reports. Without it the first report can race the device.
		ready := vigemWaitDeviceReady{
			Size:     uint32(unsafe.Sizeof(vigemWaitDeviceReady{})),
			SerialNo: serial,
		}
		if err := ioctl(g, ioctlWaitDeviceReady, &ready, ready.Size); err != nil {
			lastErr = err
			unplug := vigemUnplugTarget{
				Size:     uint32(unsafe.Sizeof(vigemUnplugTarget{})),
				SerialNo: serial,
			}
			_ = ioctl(g, ioctlUnplugTarget, &unplug, unplug.Size)
			continue
		}

		g.serial = serial
		return nil
	}
	return fmt.Errorf("no free vigem bus slot after %d attempts: %w", serialAttempts, lastErr)
}

func (g *Gamepad) closeHandles() {
	if g.event != 0 {
		_ = windows.CloseHandle(g.event)
		g.event = 0
	}
	if g.handle != windows.InvalidHandle {
		_ = windows.CloseHandle(g.handle)
		g.handle = windows.InvalidHandle
	}
}

// vigemRequest is one of the driver's request structures. Each begins with its
// own size, which is what the caller passes to ioctl.
type vigemRequest interface {
	vigemCheckVersion | vigemPluginTarget | vigemWaitDeviceReady | vigemUnplugTarget | xusbSubmitReport
}

// ioctl issues a buffered IOCTL carrying req, waiting out a request the driver
// pends. It is a free function because Go methods cannot be generic.
func ioctl[T vigemRequest](g *Gamepad, code uint32, req *T, size uint32) error {
	if err := windows.ResetEvent(g.event); err != nil {
		return fmt.Errorf("reset vigem event: %w", err)
	}

	overlapped := windows.Overlapped{HEvent: g.event}
	var returned uint32
	in := (*byte)(unsafe.Pointer(req)) //nolint:gosec // required for Windows API
	err := windows.DeviceIoControl(g.handle, code, in, size, nil, 0, &returned, &overlapped)
	if err == nil {
		return nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		return fmt.Errorf("vigem ioctl 0x%X: %w", code, err)
	}
	return g.awaitPending(code, &overlapped, &returned)
}

// awaitPending waits out a request the bus kept pending, giving up rather than
// blocking for good. WAIT_DEVICE_READY stays pending until the child device
// powers up, and this runs from StartPre, so a driver that never answers would
// otherwise hang the whole service at startup.
//
// A request that is abandoned has to be cancelled and reaped first: the
// overlapped structure is Go memory, and the driver would still be writing
// into it after this call returned.
func (g *Gamepad) awaitPending(code uint32, overlapped *windows.Overlapped, returned *uint32) error {
	event, err := windows.WaitForSingleObject(g.event, ioctlTimeoutMillis)
	if err != nil {
		return fmt.Errorf("vigem ioctl 0x%X: waiting failed: %w", code, err)
	}

	if event == waitTimeout {
		if err := windows.CancelIoEx(g.handle, overlapped); err != nil &&
			!errors.Is(err, windows.ERROR_NOT_FOUND) {
			return fmt.Errorf("vigem ioctl 0x%X timed out and could not be cancelled: %w", code, err)
		}
		// The result is the cancellation, not an answer, so only the reaping
		// matters here.
		_ = windows.GetOverlappedResult(g.handle, overlapped, returned, true)
		return fmt.Errorf("vigem ioctl 0x%X: %w", code, ErrDriverUnresponsive)
	}

	if err := windows.GetOverlappedResult(g.handle, overlapped, returned, true); err != nil {
		return fmt.Errorf("vigem ioctl 0x%X: %w", code, err)
	}
	return nil
}

// driverMissing reports an absent driver. A machine without ViGEmBus is the
// ordinary case, and SetupAPI says so with ERROR_NO_MORE_ITEMS, whose text
// ("No more data is available") only muddies a message a user has to act on.
// Anything else is unexpected and keeps its cause.
func driverMissing(err error) error {
	if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
		return ErrDriverMissing
	}
	return fmt.Errorf("%w: %w", ErrDriverMissing, err)
}

// vigemDevicePath resolves the ViGEmBus device interface path.
func vigemDevicePath() (string, error) {
	set, err := windows.SetupDiGetClassDevsEx(
		&guidDevInterfaceViGEmBus, "", 0,
		windows.DIGCF_PRESENT|windows.DIGCF_DEVICEINTERFACE, 0, "")
	if err != nil {
		return "", driverMissing(err)
	}
	defer func() { _ = windows.SetupDiDestroyDeviceInfoList(set) }()

	iface := spDeviceInterfaceData{}
	iface.cbSize = uint32(unsafe.Sizeof(iface))
	ret, _, callErr := procSetupDiEnumDeviceInterfaces.Call(
		uintptr(set), 0,
		uintptr(unsafe.Pointer(&guidDevInterfaceViGEmBus)), //nolint:gosec // required for Windows API
		0,
		uintptr(unsafe.Pointer(&iface))) //nolint:gosec // required for Windows API
	if ret == 0 {
		return "", driverMissing(callErr)
	}

	// The first call reports the buffer size the path needs.
	var needed uint32
	_, _, _ = procSetupDiGetDeviceInterfaceDetailW.Call(
		uintptr(set),
		uintptr(unsafe.Pointer(&iface)), //nolint:gosec // required for Windows API
		0, 0,
		uintptr(unsafe.Pointer(&needed)), //nolint:gosec // required for Windows API
		0)
	if needed < 8 || needed > maxDeviceDetailSize {
		return "", fmt.Errorf("vigem device path: implausible detail size %d", needed)
	}

	buf := make([]byte, needed)
	// cbSize covers the fixed part of SP_DEVICE_INTERFACE_DETAIL_DATA only,
	// which is 8 bytes on 64-bit and 6 on 32-bit, not the struct's real size.
	detailSize := uint32(8)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		detailSize = 6
	}
	*(*uint32)(unsafe.Pointer(&buf[0])) = detailSize //nolint:gosec // required for Windows API

	ret, _, callErr = procSetupDiGetDeviceInterfaceDetailW.Call(
		uintptr(set),
		uintptr(unsafe.Pointer(&iface)),  //nolint:gosec // required for Windows API
		uintptr(unsafe.Pointer(&buf[0])), //nolint:gosec // required for Windows API
		uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), //nolint:gosec // required for Windows API
		0)
	if ret == 0 {
		return "", fmt.Errorf("vigem device path: %w", callErr)
	}

	// DevicePath is a UTF-16 string starting immediately after cbSize.
	tail := buf[4:]
	chars := unsafe.Slice((*uint16)(unsafe.Pointer(&tail[0])), len(tail)/2) //nolint:gosec // required for Windows API
	return windows.UTF16ToString(chars), nil
}
