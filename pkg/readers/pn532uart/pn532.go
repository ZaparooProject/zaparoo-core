package pn532uart

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/rs/zerolog/log"
	"go.bug.st/serial"
)

const (
	cmdSamConfiguration    = 0x14
	cmdGetFirmwareVersion  = 0x02
	cmdGetGeneralStatus    = 0x04
	cmdInListPassiveTarget = 0x4A
	cmdInDataExchange      = 0x40
	hostToPn532            = 0xD4
	pn532ToHost            = 0xD5
	pn532Ready             = 0x01 //nolint:gosec // keeping as reference
)

var (
	ackFrame        = []byte{0x00, 0x00, 0xFF, 0x00, 0xFF, 0x00}
	nackFrame       = []byte{0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00}
	ErrAckTimeout   = errors.New("timeout waiting for ACK")
	ErrNoFrameFound = errors.New("no frame found")
)

func wakeUp(port serial.Port) error {
	// over uart, pn532 must be (to be safe) "woken up" by sending a 0x55
	// dummy byte and then waiting for some amount of time

	n, err := port.Write([]byte{
		0x55, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	})
	if err != nil {
		return err
	} else if n != 16 {
		return errors.New("wakeup write error, not all bytes written")
	}

	err = port.Drain()
	if err != nil {
		return err
	}

	return nil
}

func sendAck(port serial.Port) error {
	// tells the PN532 the command was received ok! (optional)
	// can also be used to immediately cancel the current processing command

	n, err := port.Write(ackFrame)
	if err != nil {
		return err
	} else if n != len(ackFrame) {
		return errors.New("ack write error, not all bytes written")
	}

	return port.Drain()
}

// Block and wait to receive an ACK frame on the serial port, returning any
// extra data that was received before the ACK frame. Data before the ACK frame
// is not to spec, but is an odd bug happening on Windows.
func waitAck(port serial.Port) ([]byte, error) {
	// pn532 will send this sequence to acknowledge it received
	// the previous command

	tries := 0
	maxTries := 32 // bytes to scan through

	buf := make([]byte, 1)
	ackBuf := make([]byte, 0)
	preAck := make([]byte, 0)

	for {
		if tries >= maxTries {
			return preAck, ErrAckTimeout
		}

		n, err := port.Read(buf)
		if err != nil {
			return preAck, err
		} else if n == 0 {
			tries++
			continue
		}

		ackBuf = append(ackBuf, buf[0])
		if len(ackBuf) < 6 {
			continue
		}

		if bytes.Equal(ackBuf, ackFrame) {
			return preAck, nil
		}
		preAck = append(preAck, ackBuf[0])
		ackBuf = ackBuf[1:]
		tries++
		continue
	}
}

func sendNack(port serial.Port) error {
	// tells the PN532 there was a problem and to resend previous data
	n, err := port.Write(nackFrame)
	if err != nil {
		return err
	} else if n != len(nackFrame) {
		return errors.New("nack write error, not all bytes written")
	}

	return port.Drain()
}

func sendFrame(port serial.Port, cmd byte, args []byte) ([]byte, error) {
	// create frame
	frm := []byte{0x00, 0x00, 0xFF} // preamble and start code

	data := []byte{hostToPn532, cmd}
	data = append(data, args...)

	if len(data) > 255 {
		// TODO: extended frames are not implemented
		return []byte{}, errors.New("data too big for frame")
	}

	dlen := byte(len(data))
	frm = append(frm, dlen, ^dlen+1) // length and length checksum

	checksum := byte(0)

	for _, b := range data {
		frm = append(frm, b)
		checksum += b
	}

	frm = append(frm, ^checksum+1, 0x00) // data checksum and postamble

	// write frame
	err := wakeUp(port)
	if err != nil {
		return []byte{}, err
	}

	n, err := port.Write(frm)
	if err != nil {
		return []byte{}, err
	} else if n != len(frm) {
		return []byte{}, errors.New("write error, not all bytes written")
	}

	err = port.Drain()
	if err != nil {
		return []byte{}, err
	}

	return waitAck(port)
}

// Read a single frame from the serial port, returning the data part of the
// frame. Optionally accepts data to prepend to the read buffer and
// treat as part of the potential frame.
func receiveFrame(port serial.Port, pre []byte) ([]byte, error) {
	tries := 0
	maxTries := 3

retry:
	buf := make([]byte, 255+7)
	if tries == 0 {
		// prepend any leftover response from a skipped ACK
		newBuf := make([]byte, len(pre)+len(buf))
		copy(newBuf, pre)
		copy(newBuf[len(pre):], buf)
		buf = newBuf
	}

	_, err := port.Read(buf)
	if err != nil {
		return []byte{}, err
	}

	// find middle of packet code (0x00 0xff) and skip preamble
	off := 0
	for ; off < len(buf); off++ {
		if buf[off] == 0xFF {
			break
		}
	}
	if off == len(buf) {
		return []byte{}, ErrNoFrameFound
	}

	// check frame length value and checksum (LEN)
	off++
	frameLen := int(buf[off])
	if ((frameLen + int(buf[off+1])) & 0xFF) != 0 {
		if tries < maxTries {
			tries++
			err := sendNack(port)
			if err != nil {
				return []byte{}, err
			}
			log.Debug().Msg("invalid frame length, sending NACK")
			goto retry
		}
		return []byte{}, errors.New("invalid frame length")
	}

	// check frame checksum against data (LCS)
	chk := byte(0)
	for _, b := range buf[off+2 : off+2+frameLen+1] {
		chk += b
	}
	if chk != 0 {
		if tries < maxTries {
			tries++
			err := sendNack(port)
			if err != nil {
				return []byte{}, err
			}
			log.Debug().Msg("invalid frame checksum, sending NACK")
			goto retry
		}
		return []byte{}, errors.New("invalid frame checksum")
	}

	// check tfi
	off += 2
	if buf[off] != pn532ToHost {
		if tries < maxTries {
			tries++
			err := sendNack(port)
			if err != nil {
				return []byte{}, err
			}
			log.Debug().Msg("invalid TFI, sending NACK")
			goto retry
		}
		return []byte{}, errors.New("invalid TFI, expected PN532 to host, got: " + fmt.Sprintf("%x", buf[off]))
	}

	// get frame data
	off++

	// return data part of frame
	data := make([]byte, frameLen-1)
	copy(data, buf[off:off+frameLen-1])

	return data, nil
}

func callCommand(
	port serial.Port,
	cmd byte,
	data []byte,
) ([]byte, error) {
	ackData, err := sendFrame(port, cmd, data)
	if err != nil {
		return []byte{}, err
	}

	if len(ackData) > 0 {
		log.Debug().Msgf("pre ack data: %x", ackData)
	}

	time.Sleep(6 * time.Millisecond)

	res, err := receiveFrame(port, ackData)
	if err != nil {
		return []byte{}, err
	}

	err = sendAck(port)
	if err != nil {
		return []byte{}, err
	}

	return res, nil
}

func SamConfiguration(port serial.Port) error {
	log.Debug().Msg("running sam configuration")
	// sets pn532 to "normal" mode
	res, err := callCommand(port, cmdSamConfiguration, []byte{0x01, 0x14, 0x01})
	if err != nil {
		return err
	} else if len(res) != 1 || res[0] != 0x15 {
		return errors.New("unexpected sam configuration response")
	}

	return nil
}

type FirmwareVersion struct {
	Version          string
	SupportIso14443a bool
	SupportIso14443b bool
	SupportIso18092  bool
}

func GetFirmwareVersion(port serial.Port) (FirmwareVersion, error) {
	log.Debug().Msg("running getfirmwareversion")
	res, err := callCommand(port, cmdGetFirmwareVersion, []byte{})
	if err != nil {
		return FirmwareVersion{}, err
	} else if len(res) != 5 || res[0] != 0x03 {
		return FirmwareVersion{}, errors.New("unexpected firmware version response")
	}

	if res[1] != 0x32 {
		return FirmwareVersion{}, fmt.Errorf("unexpected IC: %x", res[1])
	}

	fv := FirmwareVersion{
		Version:          fmt.Sprintf("%d.%d", res[2], res[3]),
		SupportIso14443a: res[4]&0x01 == 0x01,
		SupportIso14443b: res[4]&0x02 == 0x02,
		SupportIso18092:  res[4]&0x04 == 0x04,
	}

	return fv, nil
}

type GeneralStatus struct {
	LastError    byte
	FieldPresent bool
}

func GetGeneralStatus(port serial.Port) (GeneralStatus, error) {
	log.Debug().Msg("running getgeneralstatus")
	res, err := callCommand(port, cmdGetGeneralStatus, []byte{})
	if err != nil {
		return GeneralStatus{}, err
	} else if len(res) < 4 || res[0] != 0x05 {
		return GeneralStatus{}, errors.New("unexpected general status response")
	}

	gs := GeneralStatus{
		LastError:    res[1],
		FieldPresent: res[2] == 0x01,
	}

	return gs, nil
}

type Target struct {
	Type     string
	UID      string
	UIDBytes []byte
}

func InListPassiveTarget(port serial.Port) (*Target, error) {
	res, err := callCommand(port, cmdInListPassiveTarget, []byte{0x01, 0x00})
	switch {
	case errors.Is(err, ErrNoFrameFound):
		// no tag detected
		return nil, nil
	case err != nil:
		return nil, err
	case len(res) < 2 || res[0] != 0x4B:
		return nil, errors.New("unexpected passive target response")
	case res[1] != 0x01:
		// no tag detected
		return nil, nil
	}

	uidLen := res[6]
	if uidLen == 0 {
		return nil, errors.New("invalid uid length")
	}

	uid := res[7 : 7+uidLen]
	uidStr := fmt.Sprintf("%x", uid)

	tagType := ""
	if bytes.Equal(res[3:6], []byte{0x00, 0x04, 0x08}) {
		tagType = tokens.TypeMifare
	} else if bytes.Equal(res[3:6], []byte{0x00, 0x44, 0x00}) {
		tagType = tokens.TypeNTAG
	}

	return &Target{
		Type:     tagType,
		UID:      uidStr,
		UIDBytes: uid,
	}, nil
}

func InDataExchange(port serial.Port, data []byte) ([]byte, error) {
	log.Debug().Msg("running indataexchange")
	res, err := callCommand(port, cmdInDataExchange, append([]byte{0x01}, data...))
	if err != nil {
		return []byte{}, err
	} else if len(res) < 2 {
		return []byte{}, errors.New("unexpected data exchange response")
	}

	return res, nil
}
