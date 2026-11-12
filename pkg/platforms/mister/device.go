//go:build linux && !android

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

package mister

import (
	"context"
	"fmt"
	"os"
	"time"
	"unsafe"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"golang.org/x/sys/unix"
)

var (
	_ platforms.DevicePowerProvider  = (*Platform)(nil)
	_ platforms.PowerControlProvider = (*Platform)(nil)
)

// The optional battery board is a smart battery on the I2C bus, read the same
// way MiSTer's own menu reads it. The kernel has no driver for it, so it never
// appears as a power supply.
const (
	i2cSlave          = 0x0703
	i2cSMBus          = 0x0720
	smbusRead         = 1
	smbusWrite        = 0
	smbusQuick        = 0
	smbusWordData     = 3
	batteryAddress    = 0x0B
	batteryCapacity   = 0x0D
	batteryReadTries  = 20
	batteryRetryDelay = 500 * time.Microsecond
	batteryBusCount   = 3
	// batteryAbsentRetry is how long to leave the bus alone after finding no
	// battery board. One is not going to appear on a running device.
	batteryAbsentRetry = 10 * time.Minute
)

// smbusDevice is an open I2C bus with the battery selected.
type smbusDevice interface {
	// ReadWord reads a 16-bit register.
	ReadWord(register byte) (uint16, bool)
	Close()
}

// smbusIoctlArgs is struct i2c_smbus_ioctl_data, whose field order the kernel
// fixes.
//
//nolint:govet // fieldalignment: layout is the kernel's
type smbusIoctlArgs struct {
	readWrite byte
	command   byte
	size      uint32
	data      unsafe.Pointer
}

type i2cBus struct {
	file *os.File
}

func (b *i2cBus) access(readWrite, register byte, size uint32, data unsafe.Pointer) bool {
	args := smbusIoctlArgs{readWrite: readWrite, command: register, size: size, data: data}
	//nolint:gosec // G103: the kernel reads the structure during the call
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, b.file.Fd(), i2cSMBus, uintptr(unsafe.Pointer(&args)))
	return errno == 0
}

func (b *i2cBus) ReadWord(register byte) (uint16, bool) {
	// The kernel's data union is 34 bytes; a word read fills the first two.
	var data [34]byte
	//nolint:gosec // G103: the kernel writes into the buffer during the call
	if !b.access(smbusRead, register, smbusWordData, unsafe.Pointer(&data[0])) {
		return 0, false
	}
	return uint16(data[0]) | uint16(data[1])<<8, true
}

func (b *i2cBus) Close() {
	_ = b.file.Close()
}

// openBatteryBus opens an I2C bus and selects the battery on it, or reports
// that no battery answered there.
func openBatteryBus(bus int) (smbusDevice, bool) {
	file, err := os.OpenFile(fmt.Sprintf("/dev/i2c-%d", bus), os.O_RDWR, 0)
	if err != nil {
		return nil, false
	}
	device := &i2cBus{file: file}
	if err := unix.IoctlSetInt(int(file.Fd()), i2cSlave, batteryAddress); err != nil { //nolint:gosec // fd fits
		device.Close()
		return nil, false
	}
	if !device.access(smbusWrite, 0, smbusQuick, nil) {
		device.Close()
		return nil, false
	}
	return device, true
}

// batteryBoard reads the battery board's charge.
type batteryBoard struct {
	open        func(bus int) (smbusDevice, bool)
	sleep       func(time.Duration)
	now         func() time.Time
	absentUntil time.Time
	mu          syncutil.Mutex
}

func newBatteryBoard() *batteryBoard {
	return &batteryBoard{open: openBatteryBus, sleep: time.Sleep, now: time.Now}
}

// validCapacity accepts a charge register reading. A board that is still
// waking up answers with an error word, which read as a signed value falls
// outside 0-100.
func validCapacity(word uint16) (int, bool) {
	signed := int16(word) //nolint:gosec // G115: reinterpreting the register as signed is the point
	if signed < 0 || signed > 100 {
		return 0, false
	}
	return int(signed), true
}

// percent returns the charge, or false when there is no board or it will not
// answer.
func (b *batteryBoard) percent() (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.now().Before(b.absentUntil) {
		return 0, false
	}
	for bus := range batteryBusCount {
		device, ok := b.open(bus)
		if !ok {
			continue
		}
		percent, read := b.readCapacity(device)
		device.Close()
		return percent, read
	}
	b.absentUntil = b.now().Add(batteryAbsentRetry)
	return 0, false
}

func (b *batteryBoard) readCapacity(device smbusDevice) (int, bool) {
	for range batteryReadTries {
		if word, read := device.ReadWord(batteryCapacity); read {
			if percent, valid := validCapacity(word); valid {
				return percent, true
			}
		}
		b.sleep(batteryRetryDelay)
	}
	return 0, false
}

// DevicePower reports the optional battery board. The board only reports its
// charge, so the supply and charge state are left unknown.
func (p *Platform) DevicePower() (power.Detail, error) {
	if p.battery == nil {
		return power.Detail{Source: power.SourceExternal}, nil
	}
	percent, ok := p.battery.percent()
	if !ok {
		return power.Detail{Source: power.SourceExternal}, nil
	}
	return power.Detail{
		Present:   true,
		Percent:   &percent,
		Batteries: []power.Battery{{ID: "battery", Percent: &percent}},
	}, nil
}

// misterPowerControl reboots the device. A MiSTer cannot switch itself off or
// sleep.
func misterPowerControl() hoststatus.PowerController {
	return &hoststatus.ExecPowerControl{
		Executor: &command.RealExecutor{},
		Commands: map[hoststatus.PowerAction][]string{hoststatus.PowerReboot: {"reboot"}},
	}
}

func (*Platform) PowerActions(ctx context.Context) map[hoststatus.PowerAction]hoststatus.Availability {
	return misterPowerControl().PowerActions(ctx)
}

func (*Platform) PreparePowerAction(ctx context.Context, action hoststatus.PowerAction) (func() error, error) {
	//nolint:wrapcheck // sentinel errors from hoststatus are matched by the caller
	return misterPowerControl().PreparePowerAction(ctx, action)
}
