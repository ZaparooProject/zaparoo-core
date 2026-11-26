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

package methods_test

import (
	"encoding/json"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// portListingReader is a driver that lists its own candidate paths.
type portListingReader struct {
	*mocks.MockReader
	ports []readers.Port
}

func (r *portListingReader) ListPorts() ([]readers.Port, error) {
	return r.ports, nil
}

func driverReader(metadata *readers.DriverMetadata, caps ...readers.Capability) *mocks.MockReader {
	m := mocks.NewMockReader()
	m.On("Metadata").Return(*metadata)
	m.On("Capabilities").Return(caps).Maybe()
	m.On("Close").Return(nil)
	return m
}

func driversEnv(t *testing.T, supported ...readers.Reader) requests.RequestEnv {
	t.Helper()
	cfg, st := readersEnv(t, "")
	pl := mocks.NewMockPlatform()
	pl.On("SupportedReaders", mock.Anything).Return(supported)
	return requests.RequestEnv{Config: cfg, State: st, Platform: pl}
}

func TestHandleReadersDrivers(t *testing.T) {
	t.Parallel()

	pn532 := driverReader(&readers.DriverMetadata{
		ID: "pn532", Description: "PN532 NFC reader", PathKind: readers.PathKindSerial,
		DefaultEnabled: true, DefaultAutoDetect: true, CanAutoDetect: true,
	})
	// A platform can list one driver twice, such as two PN532 variants.
	pn532Again := driverReader(&readers.DriverMetadata{ID: "pn532"})
	optical := driverReader(&readers.DriverMetadata{
		ID: "opticaldrive", PathKind: readers.PathKindDevice,
		IDSources: []string{"uuid", "label", "merged"},
	})
	oled := driverReader(&readers.DriverMetadata{ID: "tty2oled"}, readers.CapabilityDisplay)

	env := driversEnv(t, pn532, pn532Again, optical, oled)
	result, err := methods.HandleReadersDrivers(env)
	require.NoError(t, err)
	resp, ok := result.(models.ReadersDriversResponse)
	require.True(t, ok)
	require.Len(t, resp.Drivers, 3)

	assert.Equal(t, models.DriverInfo{
		ID: "pn532", Description: "PN532 NFC reader", PathKind: "serial", IDSources: []string{},
		DefaultEnabled: true, DefaultAutoDetect: true, CanAutoDetect: true,
	}, resp.Drivers[0])
	assert.Equal(t, "device", resp.Drivers[1].PathKind)
	assert.Equal(t, []string{"uuid", "label", "merged"}, resp.Drivers[1].IDSources)
	assert.Equal(t, "none", resp.Drivers[2].PathKind, "a driver with no path kind takes no path")
	assert.True(t, resp.Drivers[2].Display)

	for _, r := range []*mocks.MockReader{pn532, pn532Again, optical, oled} {
		r.AssertCalled(t, "Close")
		r.AssertNotCalled(t, "Open", mock.Anything, mock.Anything, mock.Anything)
	}
}

func TestHandleReadersPorts(t *testing.T) {
	t.Parallel()

	optical := &portListingReader{
		MockReader: driverReader(&readers.DriverMetadata{ID: "opticaldrive", PathKind: readers.PathKindDevice}),
		ports:      []readers.Port{{Path: "/dev/sr0", Description: "DVD-RW"}},
	}
	mqtt := driverReader(&readers.DriverMetadata{ID: "mqtt", PathKind: readers.PathKindText})
	env := driversEnv(t, optical, mqtt)

	t.Run("a driver that lists its paths", func(t *testing.T) {
		t.Parallel()
		env := env
		env.Params = json.RawMessage(`{"driver":"opticaldrive"}`)
		result, err := methods.HandleReadersPorts(env)
		require.NoError(t, err)
		assert.Equal(t, models.ReadersPortsResponse{
			Ports: []models.PortInfo{{Path: "/dev/sr0", Description: "DVD-RW"}},
		}, result)
	})

	t.Run("a free text path has no list", func(t *testing.T) {
		t.Parallel()
		env := env
		env.Params = json.RawMessage(`{"driver":"MQTT"}`)
		result, err := methods.HandleReadersPorts(env)
		require.NoError(t, err)
		assert.Equal(t, models.ReadersPortsResponse{Ports: []models.PortInfo{}}, result)
	})

	t.Run("an unknown driver", func(t *testing.T) {
		t.Parallel()
		env := env
		env.Params = json.RawMessage(`{"driver":"nope"}`)
		_, err := methods.HandleReadersPorts(env)
		var clientErr *models.ClientError
		require.ErrorAs(t, err, &clientErr)
	})

	t.Run("the driver is required", func(t *testing.T) {
		t.Parallel()
		env := env
		env.Params = json.RawMessage(`{}`)
		_, err := methods.HandleReadersPorts(env)
		require.Error(t, err)
	})
}

func TestHandleReaders_ConnectEntries(t *testing.T) {
	t.Parallel()

	cfg, st := readersEnv(t, `
[[readers.connect]]
driver = "pn532"
path = "/dev/ttyUSB1"

[[readers.connect]]
driver = "simpleserial"
path = "/dev/ttyACM0"

[[readers.connect]]
enabled = false
driver = "pn532"
path = "/dev/ttyUSB2"
`)
	st.SetReaderConnectError("simpleserial:/dev/ttyACM0", "no such device")

	reader := func(path, readerID string) *mocks.MockReader {
		m := mocks.NewMockReader()
		m.On("Path").Return(path).Maybe()
		m.On("ReaderID").Return(readerID).Maybe()
		m.On("Metadata").Return(readers.DriverMetadata{ID: "pn532"}).Maybe()
		m.On("IDs").Return([]string{"pn532"}).Maybe()
		m.On("Info").Return("PN532").Maybe()
		m.On("Connected").Return(true).Maybe()
		m.On("Capabilities").Return([]readers.Capability{}).Maybe()
		return m
	}
	rs := []readers.Reader{
		reader("/dev/ttyUSB0", "pn532-aaaaaaaa"),
		reader("/dev/ttyUSB1", "pn532-bbbbbbbb"),
	}

	result, err := methods.HandleReaders(cfg, st, rs)
	require.NoError(t, err)
	resp, ok := result.(models.ReadersResponse)
	require.True(t, ok)
	require.Len(t, resp.Readers, 3, "a disabled connection is not listed")

	assert.Equal(t, models.ReaderSourceAuto, resp.Readers[0].Source)
	assert.Equal(t, "/dev/ttyUSB0", resp.Readers[0].Path)
	assert.Equal(t, models.ReaderSourceConnect, resp.Readers[1].Source)
	assert.Empty(t, resp.Readers[1].Error)

	failed := resp.Readers[2]
	assert.Equal(t, "simpleserial", failed.Driver)
	assert.Equal(t, "/dev/ttyACM0", failed.Path)
	assert.Equal(t, models.ReaderSourceConnect, failed.Source)
	assert.False(t, failed.Connected)
	assert.Empty(t, failed.ReaderID)
	assert.Equal(t, "no such device", failed.Error)
	assert.NotNil(t, failed.Capabilities)
}

func TestHandleReaders_ConnectEntrySharingPath(t *testing.T) {
	t.Parallel()

	cfg, st := readersEnv(t, `
[[readers.connect]]
driver = "PN532"
path = "/dev/ttyUSB0"

[[readers.connect]]
driver = "simpleserial"
path = "/dev/ttyUSB0"
`)
	st.SetReaderConnectError("simpleserial:/dev/ttyUSB0", "path already used by pn532:/dev/ttyUSB0")

	m := mocks.NewMockReader()
	m.On("Path").Return("/dev/ttyUSB0").Maybe()
	m.On("ReaderID").Return("pn532-aaaaaaaa").Maybe()
	m.On("Metadata").Return(readers.DriverMetadata{ID: "pn532"}).Maybe()
	m.On("IDs").Return([]string{"pn532"}).Maybe()
	m.On("Info").Return("PN532").Maybe()
	m.On("Connected").Return(true).Maybe()
	m.On("Capabilities").Return([]readers.Capability{}).Maybe()

	result, err := methods.HandleReaders(cfg, st, []readers.Reader{m})
	require.NoError(t, err)
	resp, ok := result.(models.ReadersResponse)
	require.True(t, ok)
	require.Len(t, resp.Readers, 2, "the open entry is listed once and the rejected one keeps its row")

	assert.Equal(t, "pn532-aaaaaaaa", resp.Readers[0].ReaderID)
	assert.Equal(t, models.ReaderSourceConnect, resp.Readers[0].Source)

	rejected := resp.Readers[1]
	assert.Equal(t, "simpleserial", rejected.Driver)
	assert.Equal(t, "/dev/ttyUSB0", rejected.Path)
	assert.False(t, rejected.Connected)
	assert.Equal(t, "path already used by pn532:/dev/ttyUSB0", rejected.Error)
}
