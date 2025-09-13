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

package userdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/google/uuid"
)

func (db *UserDB) CreateDevice(deviceName, authToken string, sharedSecret []byte) (*database.Device, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	deviceID := uuid.New().String()
	authTokenHash := hashAuthToken(authToken)
	now := time.Now().Unix()

	device := &database.Device{
		DeviceID:      deviceID,
		DeviceName:    deviceName,
		AuthTokenHash: authTokenHash,
		SharedSecret:  sharedSecret,
		CurrentSeq:    0,
		SeqWindow:     make([]byte, 8), // 64-bit window
		NonceCache:    make([]string, 0),
		CreatedAt:     time.Unix(now, 0),
		LastSeen:      time.Unix(now, 0),
	}

	nonceCacheJSON, err := json.Marshal(device.NonceCache)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal nonce cache: %w", err)
	}

	query := `
		INSERT INTO devices (device_id, device_name, auth_token_hash, shared_secret, 
							 current_seq, seq_window, nonce_cache, created_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = db.sql.ExecContext(context.Background(), query,
		device.DeviceID,
		device.DeviceName,
		device.AuthTokenHash,
		device.SharedSecret,
		device.CurrentSeq,
		device.SeqWindow,
		string(nonceCacheJSON),
		now,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create device: %w", err)
	}

	return device, nil
}

func (db *UserDB) GetDeviceByAuthToken(authToken string) (*database.Device, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	authTokenHash := hashAuthToken(authToken)

	query := `
		SELECT device_id, device_name, auth_token_hash, shared_secret, current_seq, 
			   seq_window, nonce_cache, created_at, last_seen
		FROM devices 
		WHERE auth_token_hash = ?
	`

	var device database.Device
	var nonceCacheJSON string
	var createdAt, lastSeen int64

	err := db.sql.QueryRowContext(context.Background(), query, authTokenHash).Scan(
		&device.DeviceID,
		&device.DeviceName,
		&device.AuthTokenHash,
		&device.SharedSecret,
		&device.CurrentSeq,
		&device.SeqWindow,
		&nonceCacheJSON,
		&createdAt,
		&lastSeen,
	)
	if err != nil {
		return nil, fmt.Errorf("device not found: %w", err)
	}

	device.CreatedAt = time.Unix(createdAt, 0)
	device.LastSeen = time.Unix(lastSeen, 0)

	err = json.Unmarshal([]byte(nonceCacheJSON), &device.NonceCache)
	if err != nil {
		device.NonceCache = make([]string, 0) // Fallback to empty cache
	}

	return &device, nil
}

func (db *UserDB) GetDeviceByID(deviceID string) (*database.Device, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `
		SELECT device_id, device_name, auth_token_hash, shared_secret, current_seq, 
			   seq_window, nonce_cache, created_at, last_seen
		FROM devices 
		WHERE device_id = ?
	`

	var device database.Device
	var nonceCacheJSON string
	var createdAt, lastSeen int64

	err := db.sql.QueryRowContext(context.Background(), query, deviceID).Scan(
		&device.DeviceID,
		&device.DeviceName,
		&device.AuthTokenHash,
		&device.SharedSecret,
		&device.CurrentSeq,
		&device.SeqWindow,
		&nonceCacheJSON,
		&createdAt,
		&lastSeen,
	)
	if err != nil {
		return nil, fmt.Errorf("device not found: %w", err)
	}

	device.CreatedAt = time.Unix(createdAt, 0)
	device.LastSeen = time.Unix(lastSeen, 0)

	err = json.Unmarshal([]byte(nonceCacheJSON), &device.NonceCache)
	if err != nil {
		device.NonceCache = make([]string, 0) // Fallback to empty cache
	}

	return &device, nil
}

func (db *UserDB) UpdateDeviceSequence(deviceID string, newSeq uint64, seqWindow []byte, nonceCache []string) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	nonceCacheJSON, err := json.Marshal(nonceCache)
	if err != nil {
		return fmt.Errorf("failed to marshal nonce cache: %w", err)
	}

	query := `
		UPDATE devices 
		SET current_seq = ?, seq_window = ?, nonce_cache = ?, last_seen = ?
		WHERE device_id = ?
	`

	_, err = db.sql.ExecContext(
		context.Background(), query, newSeq, seqWindow, string(nonceCacheJSON), time.Now().Unix(), deviceID,
	)
	if err != nil {
		return fmt.Errorf("failed to update device sequence: %w", err)
	}

	return nil
}

func (db *UserDB) GetAllDevices() ([]database.Device, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `
		SELECT device_id, device_name, auth_token_hash, shared_secret, current_seq, 
			   seq_window, nonce_cache, created_at, last_seen
		FROM devices 
		ORDER BY last_seen DESC
	`

	rows, err := db.sql.QueryContext(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("failed to query devices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	devices := make([]database.Device, 0)
	for rows.Next() {
		var device database.Device
		var nonceCacheJSON string
		var createdAt, lastSeen int64

		scanErr := rows.Scan(
			&device.DeviceID,
			&device.DeviceName,
			&device.AuthTokenHash,
			&device.SharedSecret,
			&device.CurrentSeq,
			&device.SeqWindow,
			&nonceCacheJSON,
			&createdAt,
			&lastSeen,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("failed to scan device row: %w", scanErr)
		}

		device.CreatedAt = time.Unix(createdAt, 0)
		device.LastSeen = time.Unix(lastSeen, 0)

		err = json.Unmarshal([]byte(nonceCacheJSON), &device.NonceCache)
		if err != nil {
			device.NonceCache = make([]string, 0) // Fallback to empty cache
		}

		devices = append(devices, device)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading device rows: %w", err)
	}

	return devices, nil
}

func (db *UserDB) DeleteDevice(deviceID string) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	query := `DELETE FROM devices WHERE device_id = ?`
	result, err := db.sql.ExecContext(context.Background(), query, deviceID)
	if err != nil {
		return fmt.Errorf("failed to delete device: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return errors.New("device not found")
	}

	return nil
}

func hashAuthToken(authToken string) string {
	hash := sha256.Sum256([]byte(authToken))
	return hex.EncodeToString(hash[:])
}
