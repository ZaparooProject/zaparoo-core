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

package state_test

import (
	"context"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type stubSource struct{}

func (*stubSource) Artwork(context.Context, string, string, int) (*readers.MediaArtwork, error) {
	return nil, readers.ErrNoArtwork
}

// displayReader is a mock reader that also accepts an artwork source, standing
// in for a real display driver.
type displayReader struct {
	*mocks.MockReader
	source readers.ArtworkSource
}

func (d *displayReader) SetArtworkSource(source readers.ArtworkSource) {
	d.source = source
}

func newDisplayReader(readerID string) *displayReader {
	m := mocks.NewMockReader()
	m.SetupBasicMock()
	m.ExpectedCalls = nil
	m.On("Close").Return(nil).Maybe()
	m.On("Metadata").Return(readers.DriverMetadata{ID: "zapdisplay"})
	m.On("Path").Return("/dev/ttyACM0")
	m.On("Connected").Return(true)
	m.On("ReaderID").Return(readerID)
	m.On("Capabilities").Return([]readers.Capability{readers.CapabilityDisplay})
	m.On("OnMediaChange", mock.Anything).Return(nil).Maybe()
	return &displayReader{MockReader: m}
}

func TestSetReaderInjectsArtworkSource(t *testing.T) {
	t.Parallel()

	st, _ := state.NewState(nil, "boot")
	source := &stubSource{}
	st.SetArtworkSource(source)

	reader := newDisplayReader("zapdisplay-abc")
	st.SetReader(reader)

	assert.Same(t, source, reader.source, "a display reader should be handed the artwork source on registration")
	assert.Same(t, source, st.ArtworkSource())
}

func TestSetReaderWithoutArtworkSourceIsSafe(t *testing.T) {
	t.Parallel()

	// Readers can connect before the database is open, so no source is set yet.
	st, _ := state.NewState(nil, "boot")
	reader := newDisplayReader("zapdisplay-abc")
	st.SetReader(reader)

	assert.Nil(t, reader.source)
	assert.Nil(t, st.ArtworkSource())
}

func TestSetReaderLeavesNonConsumersAlone(t *testing.T) {
	t.Parallel()

	st, _ := state.NewState(nil, "boot")
	st.SetArtworkSource(&stubSource{})

	// A plain reader does not implement ArtworkConsumer; registering it must
	// not panic or otherwise misbehave.
	m := mocks.NewMockReader()
	m.SetupBasicMock()
	st.SetReader(m)

	require.Len(t, st.ListReaders(), 1)
}

func TestSetReaderPushesCurrentMediaToNewDisplay(t *testing.T) {
	t.Parallel()

	st, ns := state.NewState(nil, "boot")
	// Drain notifications so the channel never fills during the test.
	go func() {
		for range ns { //nolint:revive // draining is the point
		}
	}()

	media := models.NewActiveMedia("snes", "Super Nintendo", "/games/sm.sfc", "Super Metroid", "retroarch")
	st.SetActiveMedia(media)

	reader := newDisplayReader("zapdisplay-abc")
	st.SetReader(reader)

	// Plugging a display in mid-game produces no media change, so registration
	// has to hand it the current state or it would sit on an idle screen.
	reader.AssertCalled(t, "OnMediaChange", media)
}

func TestSetReaderDoesNotPushWhenNothingIsPlaying(t *testing.T) {
	t.Parallel()

	st, _ := state.NewState(nil, "boot")
	reader := newDisplayReader("zapdisplay-abc")
	st.SetReader(reader)

	reader.AssertNotCalled(t, "OnMediaChange", mock.Anything)
}
