// Package vdfbinary parses Valve's binary VDF format.
//
// This is a vendored and modified version of github.com/TimDeve/valve-vdf-binary
// Licensed under MIT. See LICENSE file in this directory.
package vdfbinary

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	ErrEmptyVDF     = errors.New("the vdf you are trying to parse appears empty")
	ErrNotBinaryVDF = errors.New("the vdf appears not to be binary, are you sure it is not a text vdf?")
	ErrCorruptedVDF = errors.New("reached the end of the file earlier than expected, your file might be corrupted")
)

func Parse(r io.Reader) (VdfValue, error) {
	buf := bufio.NewReader(r)

	byteArr, err := buf.Peek(1)
	if errors.Is(err, io.EOF) {
		return vdfValue{}, ErrEmptyVDF
	}
	if err != nil {
		return vdfValue{}, fmt.Errorf("peek error: %w", err)
	}

	b := byteArr[0]
	if b != vdfMarkerMap && b != vdfMarkerString && b != vdfMarkerNumber && b != vdfMarkerEndOfMap {
		return vdfValue{}, ErrNotBinaryVDF
	}

	p, err := parseMap(buf)
	if errors.Is(err, io.EOF) {
		return vdfValue{}, ErrCorruptedVDF
	}
	return p, err
}

func parseMap(buf *bufio.Reader) (vdfValue, error) {
	m := make(VdfMap)

	for {
		b, err := buf.ReadByte()
		if err != nil {
			return vdfValue{}, fmt.Errorf("read byte error: %w", err)
		}

		if b == vdfMarkerEndOfMap {
			break
		}

		key, err := parseString(buf)
		if err != nil {
			return vdfValue{}, err
		}

		var value vdfValue
		switch b {
		case vdfMarkerMap:
			value, err = parseMap(buf)
		case vdfMarkerNumber:
			value, err = parseNumber(buf)
		case vdfMarkerString:
			value, err = parseStringValue(buf)
		default:
			err = fmt.Errorf("unexpected byte: 0x%02x, your file might be corrupted", b)
		}

		if err != nil {
			return vdfValue{}, err
		}

		m[strings.ToLower(key)] = value
	}

	return vdfValue{m}, nil
}

func parseNumber(buf *bufio.Reader) (vdfValue, error) {
	bf := make([]byte, 4)

	l, err := buf.Read(bf)
	if err != nil {
		return vdfValue{}, fmt.Errorf("read number error: %w", err)
	}

	if l != len(bf) {
		return vdfValue{}, errors.New("number did not have the required amount of bytes")
	}

	number := binary.LittleEndian.Uint32(bf)

	return vdfValue{number}, nil
}

func parseString(buf *bufio.Reader) (string, error) {
	s, err := buf.ReadString(vdfMarkerEndOfString)
	if err == nil {
		return s[:len(s)-1], nil
	}
	return "", fmt.Errorf("read string error: %w", err)
}

func parseStringValue(buf *bufio.Reader) (vdfValue, error) {
	s, err := parseString(buf)
	return vdfValue{s}, err
}
