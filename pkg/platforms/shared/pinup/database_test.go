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
package pinup

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureSQL mirrors the parts of Popper's schema Core reads, populated with
// the cases the reader must handle: hidden games and emulators, a non-pinball
// emulator, NULL columns, control characters and oversized text.
const fixtureSQL = `
CREATE TABLE Emulators (
	EMUID INTEGER PRIMARY KEY, EmuName VARCHAR(100), Description VARCHAR(200),
	DirGames VARCHAR(250), DirMedia VARCHAR(255), EmuDisplay VARCHAR(200),
	Visible INTEGER DEFAULT 1, DirRoms VARCHAR(250), EmuLaunchDir VARCHAR(250),
	GamesExt VARCHAR(200), LaunchScript TEXT, PostScript TEXT,
	ProcessName VARCHAR(50), WinTitle VARCHAR(50)
);
CREATE TABLE Games (
	GameID INTEGER PRIMARY KEY, EMUID INTEGER, GameName VARCHAR(200),
	GameFileName VARCHAR(250), GameDisplay VARCHAR(200), Visible INTEGER DEFAULT 1,
	Notes TEXT, GameYear INTEGER, ROM VARCHAR(100), Manufact VARCHAR(200),
	NumPlayers INTEGER, GameType VARCHAR(50), TAGS VARCHAR(200), Category VARCHAR(200),
	Author VARCHAR(200), GameTheme VARCHAR(100), GameRating INTEGER, IPDBNum VARCHAR(100), ALTEXE VARCHAR(250)
);
INSERT INTO Emulators
	(EMUID, EmuName, EmuDisplay, Visible, DirMedia, GamesExt, LaunchScript, ProcessName, WinTitle) VALUES
	(1, 'Visual Pinball X', 'VPX', 1, 'C:\vPinball\PinUPSystem\POPMedia\Visual Pinball X', 'vpx',
	 'START "" VPinballX.exe -play "[GAMEFULLNAME]"', 'VPinballX', 'Visual Pinball Player'),
	(2, 'Future Pinball', 'FP', 1, 'C:\vPinball\PinUPSystem\POPMedia\Future Pinball', 'fpt',
	 'START "" "[DIREMU]\BAM\FPLoader.exe" /open "[GAMEFULLNAME]"', 'Future Pinball', ''),
	(3, 'MAME', 'Arcade', 1, 'C:\vPinball\PinUPSystem\POPMedia\MAME', 'zip', 'mame64.exe [GAMENAME]', 'mame64', ''),
	(4, 'Pinball FX3', 'FX3', 0, 'C:\vPinball\PinUPSystem\POPMedia\Pinball FX3', 'pxp',
	 'steam.exe -applaunch 442120', '', ''),
	(5, 'Zaccaria Pinball', NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(6, 'Pinball Arcade', 'TPA', 1, NULL, 'lnk', NULL, NULL, NULL);
INSERT INTO Games
	(GameID, EMUID, GameName, GameFileName, GameDisplay, Visible, Notes, GameYear, Manufact, NumPlayers,
	 GameType, Category, Author, GameTheme, GameRating) VALUES
	(10, 1, 'Attack from Mars (Bally 1995)', 'Attack from Mars (Bally 1995).vpx', 'Attack from Mars', 1,
	 ' Great table ', 1995, 'Bally', 4, 'SS', 'Recreation', 'VPW', 'Aliens', 9),
	(11, 1, 'Hidden Table', 'Hidden Table.vpx', 'Hidden', 0, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(12, 2, 'fp_table', 'fp_table.fpt', NULL, 1, NULL, 2004, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(13, 3, 'pacman', 'pacman.zip', 'Pac-Man', 1, NULL, 1980, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(14, 4, 'fx3 table', 'fx3.pxp', 'Hidden Emulator Table', 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(15, 1, 'Bad' || char(1) || 'Name', 'bad.vpx', '', 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(16, 5, 'Zaccaria Table', 'zac.exe', 'Zaccaria Table', 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(17, 1, 'Long Notes', 'long.vpx', 'Long Notes', 1, substr(replace(hex(zeroblob(3000)), '0', 'n'), 1, 5000),
	 NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(18, 1, NULL, 'only.vpx', 'Only Display', 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(19, 1, 'Null Visible', 'nullvis.vpx', 'Null Visible', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
	(20, 6, 'tpa_table', 'tpa.lnk', 'Untrackable Table', 1, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL);
UPDATE Games SET ALTEXE = 'VPinballX107_64bit.exe' WHERE GameID = 10;
`

// openFixture creates a Popper-shaped database and returns the open handle so
// a WAL test can keep the sidecar files alive while the reader runs.
func openFixture(t *testing.T, dbPath string, wal bool) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	ctx := context.Background()
	if wal {
		var mode string
		require.NoError(t, db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode))
		require.Equal(t, "wal", strings.ToLower(mode))
	}
	_, err = db.ExecContext(ctx, fixtureSQL)
	require.NoError(t, err)
	return db
}

func TestReadLibrary(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), databaseName)
	require.NoError(t, openFixture(t, dbPath, false).Close())

	lib, err := ReadLibrary(context.Background(), dbPath)
	require.NoError(t, err)

	emuIDs := make([]int, 0, len(lib.Emulators))
	for id := range lib.Emulators {
		emuIDs = append(emuIDs, id)
	}
	assert.ElementsMatch(t, []int{1, 2, 5, 6}, emuIDs, "hidden and non-pinball emulators are dropped")

	vpx := lib.Emulators[1]
	assert.Equal(t, "Visual Pinball X", vpx.Name)
	assert.Equal(t, "VPX", vpx.Display)
	assert.Equal(t, "VPinballX", vpx.ProcessName)
	assert.Equal(t, "Visual Pinball Player", vpx.WindowTitle)
	assert.Equal(t, `C:\vPinball\PinUPSystem\POPMedia\Visual Pinball X`, vpx.MediaDir)
	assert.Equal(t, ClassVisualPinball, ClassifyEmulator(&vpx))
	assert.True(t, vpx.Visible)
	fp2 := lib.Emulators[2]
	assert.Equal(t, ClassFuturePinball, ClassifyEmulator(&fp2))
	zac := lib.Emulators[5]
	assert.Equal(t, ClassZaccaria, ClassifyEmulator(&zac), "NULL Visible defaults to visible")

	ids := make([]int, 0, len(lib.Tables))
	for _, table := range lib.Tables {
		ids = append(ids, table.ID)
	}
	assert.Equal(t, []int{10, 12, 16, 17, 18, 19, 20}, ids)

	afm, emu, ok := lib.Table(10)
	require.True(t, ok)
	assert.Equal(t, 1, emu.ID)
	assert.Equal(t, Table{
		ID: 10, EmulatorID: 1,
		Name: "Attack from Mars (Bally 1995)", Display: "Attack from Mars",
		FileName: "Attack from Mars (Bally 1995).vpx", Manufacturer: "Bally",
		GameType: "SS", Category: "Recreation", Theme: "Aliens", Notes: "Great table", Author: "VPW",
		AltExe: "VPinballX107_64bit.exe", Year: 1995, Players: 4, Rating: 9,
	}, afm)

	fp, _, ok := lib.Table(12)
	require.True(t, ok)
	assert.Empty(t, fp.Display)
	assert.Equal(t, "fp_table", fp.DisplayName())
	assert.Equal(t, 2004, fp.Year)

	long, _, ok := lib.Table(17)
	require.True(t, ok)
	assert.Len(t, long.Notes, maxFieldLength, "oversized text is truncated, not dropped")

	onlyDisplay, _, ok := lib.Table(18)
	require.True(t, ok)
	assert.Empty(t, onlyDisplay.Name)
	assert.Equal(t, "Only Display", onlyDisplay.DisplayName())

	_, _, ok = lib.Table(11)
	assert.False(t, ok, "hidden game")
	_, _, ok = lib.Table(13)
	assert.False(t, ok, "non-pinball emulator")
	_, _, ok = lib.Table(14)
	assert.False(t, ok, "hidden emulator")
	_, _, ok = lib.Table(15)
	assert.False(t, ok, "control characters in the only name")
}

func TestReadLibraryWAL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, databaseName)
	db := openFixture(t, dbPath, true)
	defer func() { require.NoError(t, db.Close()) }()
	require.FileExists(t, dbPath+"-wal")
	require.FileExists(t, dbPath+"-shm")

	lib, err := ReadLibrary(context.Background(), dbPath)
	require.NoError(t, err)
	assert.Len(t, lib.Tables, 7)
}

func TestReadLibraryErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		_, err := ReadLibrary(context.Background(), filepath.Join(t.TempDir(), databaseName))
		require.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("not a regular file", func(t *testing.T) {
		t.Parallel()
		_, err := ReadLibrary(context.Background(), t.TempDir())
		require.ErrorContains(t, err, "not a regular file")
	})

	t.Run("not a database", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), databaseName)
		require.NoError(t, os.WriteFile(dbPath, []byte("this is not sqlite"), 0o600))
		_, err := ReadLibrary(context.Background(), dbPath)
		require.Error(t, err)
	})

	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), databaseName)
		require.NoError(t, openFixture(t, dbPath, false).Close())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := ReadLibrary(ctx, dbPath)
		require.Error(t, err)
	})
}

func TestReadOnlyDSN(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "file:///tmp/PUPDatabase.db?mode=ro&_busy_timeout=1000", readOnlyDSN("/tmp/PUPDatabase.db"))
	assert.Equal(t,
		"file:///C:/vPinball/PinUP%20System/PUPDatabase.db?mode=ro&_busy_timeout=1000",
		readOnlyDSN(`C:\vPinball\PinUP System\PUPDatabase.db`),
		"a drive path must be rooted so SQLite does not read the drive as a URI authority",
	)
}
