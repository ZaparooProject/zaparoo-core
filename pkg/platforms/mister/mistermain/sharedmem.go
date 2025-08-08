// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package mistermain

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/config"
)

func mapSharedMem(address int64) (*[]byte, *os.File, error) {
	file, err := os.OpenFile(
		"/dev/mem",
		os.O_RDWR|os.O_SYNC,
		0,
	)
	if err != nil {
		return &[]byte{}, nil, fmt.Errorf("error opening /dev/mem: %w", err)
	}

	mem, err := syscall.Mmap(
		int(file.Fd()),
		address,
		0x1000,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		return &[]byte{}, nil, fmt.Errorf("error mapping /dev/mem: %w", err)
	}

	return &mem, file, nil
}

func unmapSharedMem(mem *[]byte, file *os.File) error {
	err := syscall.Munmap(*mem)
	if err != nil {
		return fmt.Errorf("error unmapping /dev/mem: %w", err)
	}

	if file == nil {
		return errors.New("/dev/mem file reference is nil")
	}

	err = file.Close()
	if err != nil {
		return fmt.Errorf("error closing /dev/mem: %w", err)
	}

	return nil
}

func GetActiveIni() (int, error) { // used for reference later
	mem, file, err := mapSharedMem(0x1FFFF000)
	if err != nil {
		return 0, err
	}

	offset := 0xF04
	vs := []byte{(*mem)[offset], (*mem)[offset+1], (*mem)[offset+2], (*mem)[offset+3]}

	err = unmapSharedMem(mem, file)
	if err != nil {
		return 0, err
	}

	if vs[0] == 0x34 && vs[1] == 0x99 && vs[2] == 0xBA {
		return int(vs[3] + 1), nil
	}
	return 0, nil
}

func SetActiveIni(ini int, relaunchCore bool) error {
	if ini < 1 || ini > 4 {
		return fmt.Errorf("ini number out of range: %d", ini)
	}

	mem, file, err := mapSharedMem(0x1FFFF000)
	if err != nil {
		return err
	}

	offset := 0xF04
	(*mem)[offset] = 0x34
	(*mem)[offset+1] = 0x99
	(*mem)[offset+2] = 0xBA
	(*mem)[offset+3] = byte(ini - 1)

	err = unmapSharedMem(mem, file)
	if err != nil {
		return err
	}

	if !relaunchCore {
		return nil
	}

	coreName := GetActiveCoreName()
	if coreName == "" {
		return errors.New("error checking active core")
	}

	if coreName == config.MenuCore {
		err = LaunchMenu()
		if err != nil {
			return err
		}
		return nil
	}

	return LaunchMenu()
}
