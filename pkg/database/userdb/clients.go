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
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/google/uuid"
)

func (db *UserDB) CreateClient(clientName, authToken string, sharedSecret []byte) (*database.Client, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	clientID := uuid.New().String()
	authTokenHash := hashAuthToken(authToken)
	now := time.Now().Unix()

	client := &database.Client{
		ClientID:      clientID,
		ClientName:    clientName,
		AuthTokenHash: authTokenHash,
		SharedSecret:  sharedSecret,
		CurrentSeq:    0,
		SeqWindow:     make([]byte, 8), // 64-bit window
		NonceCache:    make([]string, 0),
		CreatedAt:     time.Unix(now, 0),
		LastSeen:      time.Unix(now, 0),
	}

	nonceCacheJSON, err := json.Marshal(client.NonceCache)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal nonce cache: %w", err)
	}

	query := `
		INSERT INTO clients (client_id, client_name, auth_token_hash, shared_secret, 
							 current_seq, seq_window, nonce_cache, created_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = db.sql.ExecContext(context.Background(), query,
		client.ClientID,
		client.ClientName,
		client.AuthTokenHash,
		client.SharedSecret,
		client.CurrentSeq,
		client.SeqWindow,
		string(nonceCacheJSON),
		now,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	return client, nil
}

func (db *UserDB) GetClientByAuthToken(authToken string) (*database.Client, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	return db.getClientByAuthTokenConstantTime(authToken)
}

func (db *UserDB) getClientByAuthTokenConstantTime(authToken string) (*database.Client, error) {
	targetHash := hashAuthToken(authToken)
	targetHashBytes := []byte(targetHash)

	// Get all clients to prevent timing attacks through database query optimization
	clients, err := db.GetAllClients()
	if err != nil {
		return nil, fmt.Errorf("failed to get clients: %w", err)
	}

	var foundClient *database.Client
	// Use constant-time comparison for all clients
	for i := range clients {
		client := &clients[i]
		clientHashBytes := []byte(client.AuthTokenHash)

		// Ensure both hashes are same length to prevent timing attacks
		if len(targetHashBytes) == len(clientHashBytes) {
			if subtle.ConstantTimeCompare(targetHashBytes, clientHashBytes) == 1 {
				foundClient = client
				// Don't break - continue checking all clients for constant time
			}
		}
	}

	if foundClient == nil {
		return nil, errors.New("client not found")
	}

	return foundClient, nil
}

func (db *UserDB) GetClientByID(clientID string) (*database.Client, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `
		SELECT client_id, client_name, auth_token_hash, shared_secret, current_seq, 
			   seq_window, nonce_cache, created_at, last_seen
		FROM clients 
		WHERE client_id = ?
	`

	var client database.Client
	var nonceCacheJSON string
	var createdAt, lastSeen int64

	err := db.sql.QueryRowContext(context.Background(), query, clientID).Scan(
		&client.ClientID,
		&client.ClientName,
		&client.AuthTokenHash,
		&client.SharedSecret,
		&client.CurrentSeq,
		&client.SeqWindow,
		&nonceCacheJSON,
		&createdAt,
		&lastSeen,
	)
	if err != nil {
		return nil, fmt.Errorf("client not found: %w", err)
	}

	client.CreatedAt = time.Unix(createdAt, 0)
	client.LastSeen = time.Unix(lastSeen, 0)

	err = json.Unmarshal([]byte(nonceCacheJSON), &client.NonceCache)
	if err != nil {
		client.NonceCache = make([]string, 0) // Fallback to empty cache
	}

	return &client, nil
}

func (db *UserDB) UpdateClientSequence(clientID string, newSeq uint64, seqWindow []byte, nonceCache []string) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	nonceCacheJSON, err := json.Marshal(nonceCache)
	if err != nil {
		return fmt.Errorf("failed to marshal nonce cache: %w", err)
	}

	query := `
		UPDATE clients 
		SET current_seq = ?, seq_window = ?, nonce_cache = ?, last_seen = ?
		WHERE client_id = ?
	`

	_, err = db.sql.ExecContext(
		context.Background(), query, newSeq, seqWindow, string(nonceCacheJSON), time.Now().Unix(), clientID,
	)
	if err != nil {
		return fmt.Errorf("failed to update client sequence: %w", err)
	}

	return nil
}

func (db *UserDB) GetAllClients() ([]database.Client, error) {
	if db.sql == nil {
		return nil, ErrNullSQL
	}

	query := `
		SELECT client_id, client_name, auth_token_hash, shared_secret, current_seq, 
			   seq_window, nonce_cache, created_at, last_seen
		FROM clients 
		ORDER BY last_seen DESC
	`

	rows, err := db.sql.QueryContext(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("failed to query clients: %w", err)
	}
	defer func() { _ = rows.Close() }()

	clients := make([]database.Client, 0)
	for rows.Next() {
		var client database.Client
		var nonceCacheJSON string
		var createdAt, lastSeen int64

		scanErr := rows.Scan(
			&client.ClientID,
			&client.ClientName,
			&client.AuthTokenHash,
			&client.SharedSecret,
			&client.CurrentSeq,
			&client.SeqWindow,
			&nonceCacheJSON,
			&createdAt,
			&lastSeen,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("failed to scan client row: %w", scanErr)
		}

		client.CreatedAt = time.Unix(createdAt, 0)
		client.LastSeen = time.Unix(lastSeen, 0)

		err = json.Unmarshal([]byte(nonceCacheJSON), &client.NonceCache)
		if err != nil {
			client.NonceCache = make([]string, 0) // Fallback to empty cache
		}

		clients = append(clients, client)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading client rows: %w", err)
	}

	return clients, nil
}

func (db *UserDB) DeleteClient(clientID string) error {
	if db.sql == nil {
		return ErrNullSQL
	}

	query := `DELETE FROM clients WHERE client_id = ?`
	result, err := db.sql.ExecContext(context.Background(), query, clientID)
	if err != nil {
		return fmt.Errorf("failed to delete client: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return errors.New("client not found")
	}

	return nil
}

func hashAuthToken(authToken string) string {
	hash := sha256.Sum256([]byte(authToken))
	return hex.EncodeToString(hash[:])
}
