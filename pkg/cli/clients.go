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

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

func generateQRCode(data string) {
	// Simple ASCII QR code placeholder - in a real implementation, you'd use a QR code library
	if _, err := fmt.Print("\n=== PAIRING QR CODE ===\n"); err != nil {
		log.Error().Err(err).Msg("failed to print header")
	}
	if _, err := fmt.Print("Scan this with your mobile app:\n\n"); err != nil {
		log.Error().Err(err).Msg("failed to print instruction")
	}

	// For now, just display the data - a real implementation would generate ASCII QR code
	if _, err := fmt.Printf("Data: %s\n\n", data); err != nil {
		log.Error().Err(err).Msg("failed to print data")
	}

	// You could use a library like github.com/skip2/go-qrcode for actual QR generation
	note := "Note: QR code display not yet implemented - use manual pairing with the data above\n"
	if _, err := fmt.Print(note); err != nil {
		log.Error().Err(err).Msg("failed to print note")
	}
	if _, err := fmt.Print("======================\n\n"); err != nil {
		log.Error().Err(err).Msg("failed to print footer")
	}
}

func handleShowPairingCode(cfg *config.Instance, pl platforms.Platform) {
	// Open user database to check pairing sessions
	userDB, err := userdb.OpenUserDB(context.Background(), pl)
	if err != nil {
		log.Error().Err(err).Msg("failed to open user database")
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error opening user database: %v\n", err); writeErr != nil {
			log.Error().Err(writeErr).Msg("failed to write error message")
		}
		os.Exit(1)
	}
	defer func() { _ = userDB.Close() }()

	// Create HTTP client to call our own pairing API
	client := &http.Client{Timeout: 10 * time.Second}

	// Call pairing initiate endpoint
	apiURL := fmt.Sprintf("http://localhost:%d/api/pair/initiate", cfg.APIPort())
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, apiURL, strings.NewReader("{}"))
	if err != nil {
		log.Error().Err(err).Msg("failed to create pairing request")
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error creating request: %v\n", err); writeErr != nil {
			log.Error().Err(writeErr).Msg("failed to write error message")
		}
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("failed to initiate pairing")
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error initiating pairing: %v\n", err); writeErr != nil {
			log.Error().Err(writeErr).Msg("failed to write error message")
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var pairingResp api.PairingInitiateResponse
	if err := json.NewDecoder(resp.Body).Decode(&pairingResp); err != nil {
		log.Error().Err(err).Msg("failed to decode pairing response")
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error decoding response: %v\n", err); writeErr != nil {
			log.Error().Err(writeErr).Msg("failed to write error message")
		}
		return
	}

	// Generate QR code data
	serverAddr := fmt.Sprintf("localhost:%d", cfg.APIPort())
	qrData := api.QRCodeData{
		Address: serverAddr,
		Token:   pairingResp.PairingToken,
	}

	jsonData, _ := json.Marshal(qrData)
	generateQRCode(string(jsonData))

	if _, err := fmt.Printf("Pairing token: %s\n", pairingResp.PairingToken); err != nil {
		log.Error().Err(err).Msg("failed to print pairing token")
	}
	if _, err := fmt.Printf("Expires in: %d seconds\n", pairingResp.ExpiresIn); err != nil {
		log.Error().Err(err).Msg("failed to print expiration time")
	}
	if _, err := fmt.Print("\nWaiting for client to pair... (Ctrl+C to cancel)\n"); err != nil {
		log.Error().Err(err).Msg("failed to print waiting message")
	}

	// Wait for user to cancel
	select {}
}

func handleListClients(_ *config.Instance, pl platforms.Platform) {
	// Open user database to list clients
	userDB, err := userdb.OpenUserDB(context.Background(), pl)
	if err != nil {
		log.Error().Err(err).Msg("failed to open user database")
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error opening user database: %v\n", err); writeErr != nil {
			log.Error().Err(writeErr).Msg("failed to write error message")
		}
		os.Exit(1)
	}
	defer func() { _ = userDB.Close() }()

	clients, err := userDB.GetAllClients()
	if err != nil {
		log.Error().Err(err).Msg("failed to get clients")
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error getting clients: %v\n", err); writeErr != nil {
			log.Error().Err(writeErr).Msg("failed to write error message")
		}
		return
	}

	if len(clients) == 0 {
		if _, err := fmt.Println("No paired clients found."); err != nil {
			log.Error().Err(err).Msg("failed to print message")
		}
		return
	}

	if _, err := fmt.Print("Paired clients:\n\n"); err != nil {
		log.Error().Err(err).Msg("failed to print header")
	}
	if _, err := fmt.Printf("%-36s %-20s %-10s %s\n", "Client ID", "Name", "Sequence", "Last Seen"); err != nil {
		log.Error().Err(err).Msg("failed to print column headers")
	}
	if _, err := fmt.Printf("%s\n", strings.Repeat("-", 80)); err != nil {
		log.Error().Err(err).Msg("failed to print separator")
	}

	for i := range clients {
		client := &clients[i]
		if _, err := fmt.Printf("%-36s %-20s %-10d %s\n",
			client.ClientID,
			client.ClientName,
			client.CurrentSeq,
			client.LastSeen.Format("2006-01-02 15:04:05"),
		); err != nil {
			log.Error().Err(err).Msg("failed to print client info")
		}
	}
}

func handleRevokeClient(_ *config.Instance, pl platforms.Platform, clientID string) {
	if clientID == "" {
		if _, err := fmt.Fprint(os.Stderr, "Error: client ID is required\n"); err != nil {
			log.Error().Err(err).Msg("failed to write error message")
		}
		os.Exit(1)
	}

	// Open user database to revoke client
	userDB, err := userdb.OpenUserDB(context.Background(), pl)
	if err != nil {
		log.Error().Err(err).Msg("failed to open user database")
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error opening user database: %v\n", err); writeErr != nil {
			log.Error().Err(writeErr).Msg("failed to write error message")
		}
		os.Exit(1)
	}
	defer func() { _ = userDB.Close() }()

	err = userDB.DeleteClient(clientID)
	if err != nil {
		log.Error().Err(err).Msg("failed to delete client")
		if _, writeErr := fmt.Fprintf(os.Stderr, "Error deleting client: %v\n", err); writeErr != nil {
			log.Error().Err(writeErr).Msg("failed to write error message")
		}
		return
	}

	if _, err := fmt.Printf("Client %s has been revoked successfully.\n", clientID); err != nil {
		log.Error().Err(err).Msg("failed to print success message")
	}
}
