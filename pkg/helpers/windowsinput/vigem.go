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

package windowsinput

import "errors"

// The ViGEmBus protocol: the IOCTL codes and request structures the driver
// accepts. The driver validates each request's Size field, so the layouts here
// have to match the ones in the driver's BusShared.h exactly; a mismatch is
// rejected with an error that says nothing about the real cause, which is why
// they are pinned by tests.
const (
	fileDeviceBusExtender = 0x2a
	methodBuffered        = 0
	fileWriteData         = 2

	vigemBase = 0x801
)

//nolint:gochecknoglobals // derived from ctlCode, which is not a constant expression
var (
	ioctlPluginTarget     = ctlCode(fileDeviceBusExtender, vigemBase+0x000, methodBuffered, fileWriteData)
	ioctlUnplugTarget     = ctlCode(fileDeviceBusExtender, vigemBase+0x001, methodBuffered, fileWriteData)
	ioctlCheckVersion     = ctlCode(fileDeviceBusExtender, vigemBase+0x002, methodBuffered, fileWriteData)
	ioctlWaitDeviceReady  = ctlCode(fileDeviceBusExtender, vigemBase+0x003, methodBuffered, fileWriteData)
	ioctlXusbSubmitReport = ctlCode(fileDeviceBusExtender, vigemBase+0x201, methodBuffered, fileWriteData)
)

// ErrDriverMissing reports that the ViGEmBus driver is not installed. That is
// the expected state on a machine that has never asked for gamepad support, so
// it has to reach the user as a clear message rather than a silent success.
var ErrDriverMissing = errors.New("the ViGEmBus driver is not installed")

// ctlCode builds a Windows IOCTL code, mirroring the CTL_CODE macro.
func ctlCode(devType, function, method, access uint32) uint32 {
	return devType<<16 | access<<14 | function<<2 | method
}

type vigemCheckVersion struct {
	Size    uint32
	Version uint32
}

type vigemPluginTarget struct {
	Size       uint32
	SerialNo   uint32
	TargetType uint32
	VendorID   uint16
	ProductID  uint16
}

type vigemWaitDeviceReady struct {
	Size     uint32
	SerialNo uint32
}

type vigemUnplugTarget struct {
	Size     uint32
	SerialNo uint32
}

type xusbSubmitReport struct {
	Size     uint32
	SerialNo uint32
	Report   xusbReport
}
