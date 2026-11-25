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

//go:build linux

package cdrom

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux ioctls from <linux/cdrom.h> and <scsi/sg.h>.
const (
	ioctlReadTOCHeader   = 0x5305
	ioctlReadTOCEntry    = 0x5306
	ioctlMediaChanged    = 0x5325
	ioctlDriveStatus     = 0x5326
	ioctlDVDReadStruct   = 0x5390
	ioctlSGIO            = 0x2285
	cdslCurrent          = 0x7fffffff
	cdromLBA             = 0x01
	cdromLeadOut         = 0xAA
	cdromDataTrack       = 0x04
	sgDxferFromDev       = -3
	sgInfoOKMask         = 0x1
	scsiReadCD           = 0xBE
	scsiReadTOC          = 0x43
	cdTextFormat         = 0x05
	maxCDTextResponse    = 0xFFFE
	sgTimeoutMillis      = 10000
	maxAudioFramesPerCmd = 27
	dvdStructPhysical    = 0x00
	// dvdStructSize covers the largest member of the kernel's dvd_struct.
	dvdStructSize = 2064
)

// Values of CDROM_DRIVE_STATUS.
const (
	cdsNoInfo        = 0
	cdsNoDisc        = 1
	cdsTrayOpen      = 2
	cdsDriveNotReady = 3
	cdsDiscOK        = 4
)

var (
	readRetryDelay = 10 * time.Millisecond
	unixPread      = unix.Pread
	unixClose      = unix.Close
)

type linuxDrive struct {
	fd int
}

// Open opens a drive read-only and non-blocking, so it succeeds with no disc
// loaded and never closes the tray.
func Open(path string) (Drive, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open optical drive: %w", err)
	}
	return &linuxDrive{fd: fd}, nil
}

func (d *linuxDrive) ioctlInt(req, arg uintptr) (int, error) {
	r, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), req, arg)
	if errno != 0 {
		return 0, errno
	}
	return int(r), nil
}

func (d *linuxDrive) ioctlPtr(req uintptr, arg unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

func (d *linuxDrive) Status() (Status, error) {
	v, err := d.ioctlInt(ioctlDriveStatus, cdslCurrent)
	if err != nil {
		return StatusUnknown, fmt.Errorf("drive status: %w", err)
	}
	switch v {
	case cdsNoDisc:
		return StatusNoDisc, nil
	case cdsTrayOpen:
		return StatusTrayOpen, nil
	case cdsDriveNotReady:
		return StatusNotReady, nil
	case cdsDiscOK:
		return StatusDiscOK, nil
	case cdsNoInfo:
		return StatusUnknown, nil
	default:
		return StatusUnknown, nil
	}
}

func (d *linuxDrive) MediaChanged() (bool, error) {
	v, err := d.ioctlInt(ioctlMediaChanged, cdslCurrent)
	if err != nil {
		return false, fmt.Errorf("media changed: %w", err)
	}
	return v > 0, nil
}

func (d *linuxDrive) IsDVD() bool {
	buf := make([]byte, dvdStructSize)
	buf[0] = dvdStructPhysical
	err := d.ioctlPtr(ioctlDVDReadStruct, unsafe.Pointer(&buf[0])) //nolint:gosec // kernel ABI
	runtime.KeepAlive(buf)
	return err == nil
}

// tocEntry mirrors struct cdrom_tocentry with the LBA address format.
type tocEntry struct {
	Track    uint8
	AdrCtrl  uint8
	Format   uint8
	_        uint8
	LBA      int32
	DataMode uint8
	_        [3]uint8
}

func (d *linuxDrive) readTOCEntry(track uint8) (tocEntry, error) {
	e := tocEntry{Track: track, Format: cdromLBA}
	if err := d.ioctlPtr(ioctlReadTOCEntry, unsafe.Pointer(&e)); err != nil { //nolint:gosec // kernel ABI
		return tocEntry{}, fmt.Errorf("read toc entry %d: %w", track, err)
	}
	return e, nil
}

func (d *linuxDrive) ReadTOC() (TOC, error) {
	var hdr [2]uint8
	if err := d.ioctlPtr(ioctlReadTOCHeader, unsafe.Pointer(&hdr[0])); err != nil { //nolint:gosec // kernel ABI
		return TOC{}, fmt.Errorf("read toc header: %w", err)
	}
	first, last := int(hdr[0]), int(hdr[1])
	if first < 1 || last < first || last > MaxTracks {
		return TOC{}, fmt.Errorf("toc header has tracks %d-%d", first, last)
	}
	tracks := make([]Track, 0, last-first+1)
	for n := first; n <= last; n++ {
		e, err := d.readTOCEntry(uint8(n)) //nolint:gosec // bounded by MaxTracks
		if err != nil {
			return TOC{}, err
		}
		// The control nibble is the high half on little-endian bitfields,
		// which is every platform Core reads drives on.
		ctrl := e.AdrCtrl >> 4
		tracks = append(tracks, Track{Number: n, Start: int(e.LBA), Data: ctrl&cdromDataTrack != 0})
	}
	lead, err := d.readTOCEntry(cdromLeadOut)
	if err != nil {
		return TOC{}, err
	}
	return finishTOC(tracks, int(lead.LBA))
}

// sgIOHdr mirrors struct sg_io_hdr. Pointer fields are unsafe.Pointer so the
// buffers they name stay reachable during the call.
type sgIOHdr struct { //nolint:govet // field order is the kernel ABI
	InterfaceID    int32
	DxferDirection int32
	CmdLen         uint8
	MxSbLen        uint8
	IovecCount     uint16
	DxferLen       uint32
	Dxferp         unsafe.Pointer
	Cmdp           unsafe.Pointer
	Sbp            unsafe.Pointer
	Timeout        uint32
	Flags          uint32
	PackID         int32
	UsrPtr         unsafe.Pointer
	Status         uint8
	MaskedStatus   uint8
	MsgStatus      uint8
	SbLenWr        uint8
	HostStatus     uint16
	DriverStatus   uint16
	Resid          int32
	Duration       uint32
	Info           uint32
}

var errSCSI = errors.New("scsi command failed")

// scsiRead runs a data-in SCSI command and returns how many bytes arrived.
func (d *linuxDrive) scsiRead(cdb, dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, errors.New("empty scsi buffer")
	}
	sense := make([]byte, 32)
	hdr := &sgIOHdr{
		InterfaceID:    'S',
		DxferDirection: sgDxferFromDev,
		CmdLen:         uint8(len(cdb)),           //nolint:gosec // CDBs are 10 or 12 bytes
		MxSbLen:        uint8(len(sense)),         //nolint:gosec // fixed size
		DxferLen:       uint32(len(dst)),          //nolint:gosec // bounded by callers
		Dxferp:         unsafe.Pointer(&dst[0]),   //nolint:gosec // kernel ABI
		Cmdp:           unsafe.Pointer(&cdb[0]),   //nolint:gosec // kernel ABI
		Sbp:            unsafe.Pointer(&sense[0]), //nolint:gosec // kernel ABI
		Timeout:        sgTimeoutMillis,
	}
	err := d.ioctlPtr(ioctlSGIO, unsafe.Pointer(hdr)) //nolint:gosec // kernel ABI
	runtime.KeepAlive(dst)
	runtime.KeepAlive(cdb)
	runtime.KeepAlive(sense)
	if err != nil {
		return 0, fmt.Errorf("sg_io: %w", err)
	}
	if hdr.Status != 0 || hdr.HostStatus != 0 || hdr.DriverStatus != 0 || hdr.Info&sgInfoOKMask != 0 {
		return 0, fmt.Errorf("%w: status %#x host %#x driver %#x", errSCSI,
			hdr.Status, hdr.HostStatus, hdr.DriverStatus)
	}
	n := len(dst) - int(hdr.Resid)
	if n < 0 || n > len(dst) {
		return 0, fmt.Errorf("%w: bad residual %d", errSCSI, hdr.Resid)
	}
	return n, nil
}

func readCDCommand(lba, count int, sectorType, flags byte) []byte {
	cdb := make([]byte, 12)
	cdb[0] = scsiReadCD
	cdb[1] = sectorType << 2
	binary.BigEndian.PutUint32(cdb[2:6], uint32(lba)) //nolint:gosec // LBA checked by callers
	cdb[6] = byte(count >> 16)                        //nolint:gosec // 24-bit transfer length
	cdb[7] = byte(count >> 8)                         //nolint:gosec // 24-bit transfer length
	cdb[8] = byte(count)                              //nolint:gosec // 24-bit transfer length
	cdb[9] = flags
	return cdb
}

func (d *linuxDrive) ReadCDTextPacks() ([]byte, error) {
	return readCDTextPacks(d.scsiRead)
}

func cdTextCommand(allocation int) []byte {
	cdb := make([]byte, 10)
	cdb[0] = scsiReadTOC
	cdb[2] = cdTextFormat
	binary.BigEndian.PutUint16(cdb[7:9], uint16(allocation)) //nolint:gosec // capped at maxCDTextResponse
	return cdb
}

// readCDTextPacks asks for the CD-Text header first and then exactly the
// length it gives. A drive answers a larger request with a short transfer,
// which the MiSTer's USB host controller fails and recovers from by resetting
// the drive.
func readCDTextPacks(scsiRead func(cdb, dst []byte) (int, error)) ([]byte, error) {
	header := make([]byte, 4)
	n, err := scsiRead(cdTextCommand(len(header)), header)
	if err != nil {
		return nil, fmt.Errorf("read cd-text header: %w", err)
	}
	if n < len(header) {
		return nil, nil
	}
	length := min(int(binary.BigEndian.Uint16(header[0:2]))+2, maxCDTextResponse)
	if length <= len(header) {
		return nil, nil
	}
	buf := make([]byte, length)
	n, err = scsiRead(cdTextCommand(length), buf)
	if err != nil {
		return nil, fmt.Errorf("read cd-text: %w", err)
	}
	if n <= len(header) {
		return nil, nil
	}
	return buf[len(header):n], nil
}

func (d *linuxDrive) ReadDataSector(ctx context.Context, lba int, p []byte) error {
	if len(p) != DataSectorSize || lba < 0 {
		return errors.New("bad data sector request")
	}
	if _, err := d.ReadAtContext(ctx, p, int64(lba)*DataSectorSize); err == nil {
		return nil
	} else if ctx.Err() != nil {
		return err
	}
	// Some Mode 2 discs are refused by the block layer. READ CD with only
	// the user data flag returns the 2048 bytes of a Mode 1 or Form 1 sector.
	n, err := d.scsiRead(readCDCommand(lba, 1, 0, 0x10), p)
	if err != nil {
		return fmt.Errorf("read data sector %d: %w", lba, err)
	}
	if n != DataSectorSize {
		return fmt.Errorf("read data sector %d: short read", lba)
	}
	return nil
}

func (d *linuxDrive) ReadAudio(ctx context.Context, lba, count int, p []byte) error {
	if lba < 0 || count <= 0 || len(p) != count*RawSectorSize {
		return errors.New("bad audio read request")
	}
	for done := 0; done < count; {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("read audio: %w", err)
		}
		n := min(count-done, maxAudioFramesPerCmd)
		dst := p[done*RawSectorSize : (done+n)*RawSectorSize]
		// Expected sector type 1 is CD-DA; flag 0x10 returns its 2352 bytes.
		got, err := d.scsiRead(readCDCommand(lba+done, n, 1, 0x10), dst)
		if err != nil {
			return fmt.Errorf("read audio at %d: %w", lba+done, err)
		}
		if got != len(dst) {
			return fmt.Errorf("read audio at %d: short read", lba+done)
		}
		done += n
	}
	return nil
}

func (d *linuxDrive) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	total := 0
	for total < len(p) {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		n, err := unixPread(d.fd, p[total:], off+int64(total))
		if n > 0 {
			total += n
		}
		if err == nil {
			if n == 0 {
				return total, io.EOF
			}
			continue
		}
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
			if waitReadRetry(ctx) != nil {
				return total, fmt.Errorf("wait for optical drive read: %w", ctx.Err())
			}
			continue
		}
		return total, fmt.Errorf("read optical drive: %w", err)
	}
	return total, nil
}

func (d *linuxDrive) Close() error {
	if err := unixClose(d.fd); err != nil {
		return fmt.Errorf("close optical drive: %w", err)
	}
	return nil
}

func waitReadRetry(ctx context.Context) error {
	timer := time.NewTimer(readRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// IsUnsupportedRequest reports whether err means the drive or its driver does
// not support the request, rather than that the request failed.
func IsUnsupportedRequest(err error) bool {
	return errors.Is(err, ErrUnsupported) ||
		errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS)
}
