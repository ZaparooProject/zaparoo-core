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

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	chi "github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
)

var allowedOrigins = []string{
	"capacitor://localhost", // iOS Capacitor v3+
	"ionic://localhost",     // iOS Capacitor v2
	"https://localhost",     // Android
	"http://localhost",      // Fallback/development
}

var JSONRPCErrorParseError = models.ErrorObject{
	Code:    -32700,
	Message: "Parse error",
}

var JSONRPCErrorInvalidRequest = models.ErrorObject{
	Code:    -32600,
	Message: "Invalid Request",
}

var JSONRPCErrorMethodNotFound = models.ErrorObject{
	Code:    -32601,
	Message: "Method not found",
}

var JSONRPCErrorInvalidParams = models.ErrorObject{
	Code:    -32602,
	Message: "Invalid params",
}

var JSONRPCErrorInternalError = models.ErrorObject{
	Code:    -32603,
	Message: "Internal error",
}

func makeJSONRPCError(code int, message string) models.ErrorObject {
	return models.ErrorObject{
		Code:    code,
		Message: message,
	}
}

type MethodMap struct {
	sync.Map
}

func (m *MethodMap) Store(key, value any) {
	m.Map.Store(key, value)
}

func (m *MethodMap) Load(key any) (value any, ok bool) {
	return m.Map.Load(key)
}

func (m *MethodMap) Range(f func(key, value any) bool) {
	m.Map.Range(f)
}

func isValidMethodName(name string) bool {
	for _, r := range name {
		if (r < 'a' || r > 'z') && r != '.' {
			return false
		}
	}
	return name != ""
}

func (m *MethodMap) AddMethod(
	name string,
	handler func(requests.RequestEnv) (any, error),
) error {
	if name == "" {
		return errors.New("method name cannot be empty")
	} else if !isValidMethodName(name) {
		return fmt.Errorf("method name contains invalid characters: %s", name)
	} else if _, exists := m.GetMethod(name); exists {
		return fmt.Errorf("method already exists: %s", name)
	}
	m.Store(strings.ToLower(name), handler)
	return nil
}

func (m *MethodMap) GetMethod(name string) (func(requests.RequestEnv) (any, error), bool) {
	fn, ok := m.Load(strings.ToLower(name))
	if !ok {
		return nil, false
	}
	method, ok := fn.(func(requests.RequestEnv) (any, error))
	if !ok {
		return nil, false
	}
	return method, true
}

func (m *MethodMap) ListMethods() []string {
	var ms []string
	m.Range(func(key, _ any) bool {
		ms = append(ms, key.(string))
		return true
	})
	return ms
}

func NewMethodMap() *MethodMap {
	var m MethodMap

	defaultMethods := map[string]func(requests.RequestEnv) (any, error){
		// run
		models.MethodLaunch: methods.HandleRun, // DEPRECATED
		models.MethodRun:    methods.HandleRun,
		models.MethodStop:   methods.HandleStop,
		// tokens
		models.MethodTokens:  methods.HandleTokens,
		models.MethodHistory: methods.HandleHistory,
		// media
		models.MethodMedia:             methods.HandleMedia,
		models.MethodMediaGenerate:     methods.HandleGenerateMedia,
		models.MethodMediaIndex:        methods.HandleGenerateMedia,
		models.MethodMediaSearch:       methods.HandleMediaSearch,
		models.MethodMediaActive:       methods.HandleActiveMedia,
		models.MethodMediaActiveUpdate: methods.HandleUpdateActiveMedia,
		// settings
		models.MethodSettings:             methods.HandleSettings,
		models.MethodSettingsUpdate:       methods.HandleSettingsUpdate,
		models.MethodSettingsReload:       methods.HandleSettingsReload,
		models.MethodSettingsLogsDownload: methods.HandleLogsDownload,
		// systems
		models.MethodSystems: methods.HandleSystems,
		// launchers
		models.MethodLaunchersRefresh: methods.HandleLaunchersRefresh,
		// mappings
		models.MethodMappings:       methods.HandleMappings,
		models.MethodMappingsNew:    methods.HandleAddMapping,
		models.MethodMappingsDelete: methods.HandleDeleteMapping,
		models.MethodMappingsUpdate: methods.HandleUpdateMapping,
		models.MethodMappingsReload: methods.HandleReloadMappings,
		// readers
		models.MethodReaders:            methods.HandleReaders,
		models.MethodReadersWrite:       methods.HandleReaderWrite,
		models.MethodReadersWriteCancel: methods.HandleReaderWriteCancel,
		// utils
		models.MethodVersion: methods.HandleVersion,
	}

	for name, fn := range defaultMethods {
		err := m.AddMethod(name, fn)
		if err != nil {
			log.Error().Err(err).Msgf("error adding default method: %s", name)
		}
	}

	return &m
}

// handleRequest validates a client request and forwards it to the
// appropriate method handler. Returns the method's result object.
//
//nolint:gocritic // single-use parameter in API handler
func handleRequest(
	methodMap *MethodMap,
	env requests.RequestEnv,
	req models.RequestObject,
) (any, *models.ErrorObject) {
	log.Debug().Interface("request", req).Msg("received request")

	fn, ok := methodMap.GetMethod(req.Method)
	if !ok {
		log.Error().Str("method", req.Method).Msg("unknown method")
		return nil, &JSONRPCErrorMethodNotFound
	}

	if req.ID == nil {
		log.Error().Str("method", req.Method).Msg("missing ID for request")
		return nil, &JSONRPCErrorInvalidRequest
	}

	env.ID = *req.ID
	env.Params = req.Params

	resp, err := fn(env)
	if err != nil {
		log.Error().Err(err).Msg("error handling request")
		// TODO: return error object from methods
		rpcError := makeJSONRPCError(1, err.Error())
		return nil, &rpcError
	}
	return resp, nil
}

// sendWSResponse marshals a method result and sends it to the client.
func sendWSResponse(session *melody.Session, id uuid.UUID, result any) error {
	log.Debug().Interface("result", result).Msg("sending response")

	resp := models.ResponseObject{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("error marshalling response: %w", err)
	}

	if err := session.Write(data); err != nil {
		return fmt.Errorf("failed to write websocket response: %w", err)
	}
	return nil
}

// sendWSError sends a JSON-RPC error object response to the client.
func sendWSError(session *melody.Session, id uuid.UUID, errObj models.ErrorObject) error {
	log.Debug().Int("code", errObj.Code).Str("message", errObj.Message).Msg("sending error")

	resp := models.ResponseErrorObject{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &errObj,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("error marshalling error response: %w", err)
	}

	err = session.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to session: %w", err)
	}
	return nil
}

func handleResponse(resp models.ResponseObject) error {
	log.Debug().Interface("response", resp).Msg("received response")
	return nil
}

func fsCustom404(root http.FileSystem) http.Handler {
	appFS := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := root.Open(r.URL.Path)
		if err != nil {
			if os.IsNotExist(err) {
				index, indexErr := root.Open("index.html")
				if indexErr != nil {
					log.Error().Err(indexErr).Msg("error opening index.html")
					http.Error(w, indexErr.Error(), http.StatusInternalServerError)
					return
				}
				http.ServeContent(w, r, "index.html", time.Now(), index)
				return
			}
			log.Error().Err(err).Str("path", r.URL.Path).Msg("error opening file")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = f.Close()
		if err != nil {
			log.Error().Err(err).Msg("error closing file")
		}
		appFS.ServeHTTP(w, r)
	})
}

// handleApp serves the embedded Zaparoo App web build to the client.
func handleApp(w http.ResponseWriter, r *http.Request) {
	appFs, err := fs.Sub(assets.App, "_app/dist")
	if err != nil {
		log.Error().Err(err).Msg("error opening app dist")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.StripPrefix("/app", fsCustom404(http.FS(appFs))).ServeHTTP(w, r)
}

// isPrivateIP checks if an IP address is in private ranges
func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	// Check RFC1918 private ranges
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16", // Link-local
	}

	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil && network.Contains(ip) {
			return true
		}
	}

	return false
}

// checkWebSocketOrigin validates WebSocket origin requests based on security policy
func checkWebSocketOrigin(origin string, allowedOrigins []string, apiPort int) bool {
	// Allow empty origin (same-origin requests)
	if origin == "" {
		log.Debug().Msg("websocket origin: empty origin allowed (same-origin)")
		return true
	}

	// Check explicit allowed origins first
	for _, allowed := range allowedOrigins {
		if origin == allowed {
			log.Debug().Msgf("websocket origin: %s allowed (explicit match)", origin)
			return true
		}
	}

	// Parse origin URL
	u, err := url.Parse(origin)
	if err != nil {
		log.Debug().Msgf("websocket origin: %s rejected (invalid URL: %v)", origin, err)
		return false
	}

	// Allow localhost and 127.0.0.1 on any port (http or https)
	hostname := u.Hostname()
	if hostname == "localhost" || hostname == "127.0.0.1" {
		log.Debug().Msgf("websocket origin: %s allowed (localhost any port)", origin)
		return true
	}

	// Allow private IP addresses only on the correct API port
	if isPrivateIP(hostname) {
		port := u.Port()
		if port == "" && (u.Scheme == "http" || u.Scheme == "https") {
			log.Debug().Msgf("websocket origin: %s rejected (private IP needs explicit port)", origin)
			return false // explicit port required for private IPs
		}
		if port == strconv.Itoa(apiPort) {
			log.Debug().Msgf("websocket origin: %s allowed (private IP correct port)", origin)
			return true
		}
		log.Debug().Msgf("websocket origin: %s rejected (private IP wrong port: %s, expected: %d)",
			origin, port, apiPort)
		return false
	}

	log.Debug().Msgf("websocket origin: %s rejected (not allowed)", origin)
	return false
}

// buildDynamicAllowedOrigins creates the allowed origins list for CORS/WebSocket
func buildDynamicAllowedOrigins(baseOrigins, localIPs []string, port int, customOrigins []string) []string {
	result := make([]string, 0, len(baseOrigins)+len(localIPs)*2)
	result = append(result, baseOrigins...)

	// Add all provided local IPs
	for _, localIP := range localIPs {
		result = append(result,
			fmt.Sprintf("http://%s:%d", localIP, port),
			fmt.Sprintf("https://%s:%d", localIP, port),
		)
	}

	// Add custom origins
	for _, origin := range customOrigins {
		result = append(result,
			fmt.Sprintf("http://%s", origin),
			fmt.Sprintf("https://%s", origin),
		)
	}

	return result
}

// broadcastNotifications consumes and broadcasts all incoming API
// notifications to all connected clients with appropriate encryption.
func broadcastNotifications(
	st *state.State,
	session *melody.Melody,
	notifications <-chan models.Notification,
) {
	for {
		select {
		case <-st.GetContext().Done():
			log.Debug().Msg("closing HTTP server via context cancellation")
			return
		case notif := <-notifications:
			req := models.RequestObject{
				JSONRPC: "2.0",
				Method:  notif.Method,
				Params:  notif.Params,
			}

			data, err := json.Marshal(req)
			if err != nil {
				log.Error().Err(err).Msg("marshalling notification request")
				continue
			}

			// Broadcast to localhost sessions (unencrypted)
			_ = session.BroadcastFilter(data, func(s *melody.Session) bool {
				rawIP := strings.SplitN(s.Request.RemoteAddr, ":", 2)
				clientIP := net.ParseIP(rawIP[0])
				return clientIP.IsLoopback()
			})

			// Broadcast to authenticated remote sessions (encrypted)
			_ = session.BroadcastFilter(nil, func(s *melody.Session) bool {
				// Check if this is a remote connection
				rawIP := strings.SplitN(s.Request.RemoteAddr, ":", 2)
				clientIP := net.ParseIP(rawIP[0])
				if clientIP.IsLoopback() {
					return false // Skip localhost
				}

				// Check if session is authenticated
				device, authenticated := s.Get("device")
				if !authenticated {
					return false // Skip unauthenticated
				}

				// Encrypt notification for this session
				deviceObj, ok := device.(*database.Device)
				if !ok {
					log.Error().Msg("invalid device type in session")
					return false
				}
				encrypted, iv, err := apimiddleware.EncryptPayload(data, deviceObj.SharedSecret)
				if err != nil {
					log.Error().Err(err).Str("device_id", deviceObj.DeviceID).Msg("failed to encrypt notification")
					return false
				}

				encResponse := apimiddleware.EncryptedRequest{
					Encrypted: encrypted,
					IV:        iv,
					AuthToken: "", // Not needed for notifications
				}

				encData, err := json.Marshal(encResponse)
				if err != nil {
					log.Error().Err(err).Str("device_id", deviceObj.DeviceID).
						Msg("failed to marshal encrypted notification")
					return false
				}

				// Send encrypted data to this session
				if err := s.Write(encData); err != nil {
					log.Error().Err(err).Str("device_id", deviceObj.DeviceID).
						Msg("failed to send encrypted notification")
				}

				return false // Don't include in broadcast since we already sent manually
			})
		}
	}
}

func processRequestObject(
	methodMap *MethodMap,
	env requests.RequestEnv, //nolint:gocritic // single-use parameter in API handler
	msg []byte,
) (uuid.UUID, any, *models.ErrorObject) {
	if !json.Valid(msg) {
		log.Error().Msg("request payload is not valid JSON")
		return uuid.Nil, nil, &JSONRPCErrorParseError
	}

	// try parse a request first, which has a method field
	var req models.RequestObject
	err := json.Unmarshal(msg, &req)

	if err == nil && req.JSONRPC != "2.0" {
		id := uuid.Nil
		if req.ID != nil {
			id = *req.ID
		}
		log.Error().Str("version", req.JSONRPC).Msg("unsupported JSON-RPC version")
		return id, nil, &JSONRPCErrorInvalidRequest
	}

	if err == nil && req.Method != "" {
		if req.ID == nil {
			// request is notification, we don't do anything with these yet
			log.Info().Interface("req", req).Msg("received notification, ignoring")
			return uuid.Nil, nil, nil
		}

		// request is a request
		resp, rpcError := handleRequest(methodMap, env, req)
		if rpcError != nil {
			return *req.ID, nil, rpcError
		}
		return *req.ID, resp, nil
	}

	// otherwise try parse a response, which has an id field
	var resp models.ResponseObject
	err = json.Unmarshal(msg, &resp)
	if err == nil && resp.ID != uuid.Nil {
		err := handleResponse(resp)
		if err != nil {
			log.Error().Err(err).Msg("error handling response")
			return resp.ID, nil, &JSONRPCErrorInternalError
		}
		return resp.ID, nil, nil
	}

	// can't identify the message
	return uuid.Nil, nil, &JSONRPCErrorInvalidRequest
}

// handleWSMessage parses all incoming WS requests, identifies what type of
// JSON-RPC object they may be and forwards them to the appropriate function
// to handle that type of message.
func handleWSMessage(
	methodMap *MethodMap,
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	inTokenQueue chan<- tokens.Token,
	db *database.Database,
) func(session *melody.Session, msg []byte) {
	return func(session *melody.Session, msg []byte) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("panic in websocket handler")
				err := sendWSError(session, uuid.Nil, JSONRPCErrorInternalError)
				if err != nil {
					log.Error().Err(err).Msg("error sending panic error response")
				}
			}
		}()

		// ping command for heartbeat operation
		if bytes.Equal(msg, []byte("ping")) {
			err := session.Write([]byte("pong"))
			if err != nil {
				log.Error().Err(err).Msg("sending pong")
			}
			return
		}

		rawIP := strings.SplitN(session.Request.RemoteAddr, ":", 2)
		clientIP := net.ParseIP(rawIP[0])
		isLocal := clientIP.IsLoopback()

		// Handle authentication for remote connections
		if !isLocal {
			device, authenticated := session.Get("device")
			if !authenticated {
				// First message must be authentication
				err := handleWSAuthentication(session, msg, db)
				if err != nil {
					log.Error().Err(err).Msg("WebSocket authentication failed")
					_ = session.Close()
				}
				return
			}

			// Decrypt message for authenticated remote connection
			deviceObj, ok := device.(*database.Device)
			if !ok {
				log.Error().Msg("invalid device type in session")
				err := sendWSError(session, uuid.Nil, JSONRPCErrorInternalError)
				if err != nil {
					log.Error().Err(err).Msg("failed to send WebSocket error")
				}
				return
			}
			decryptedMsg, err := handleWSDecryption(session, msg, deviceObj, db)
			if err != nil {
				log.Error().Err(err).Msg("WebSocket decryption failed")
				err := sendWSError(session, uuid.Nil, JSONRPCErrorInvalidRequest)
				if err != nil {
					log.Error().Err(err).Msg("error sending decryption error response")
				}
				return
			}
			msg = decryptedMsg
		}

		env := requests.RequestEnv{
			Platform:   platform,
			Config:     cfg,
			State:      st,
			Database:   db,
			TokenQueue: inTokenQueue,
			IsLocal:    isLocal,
		}

		id, resp, rpcError := processRequestObject(methodMap, env, msg)
		if rpcError != nil {
			err := sendWSError(session, id, *rpcError)
			if err != nil {
				log.Error().Err(err).Msg("error sending error response")
			}
		} else {
			// Encrypt response for remote authenticated connections
			if !isLocal {
				if device, authenticated := session.Get("device"); authenticated {
					deviceObj, ok := device.(*database.Device)
					if !ok {
						log.Error().Msg("invalid device type in session")
						return
					}
					err := sendWSResponseEncrypted(session, id, resp, deviceObj)
					if err != nil {
						log.Error().Err(err).Msg("error sending encrypted response")
					}
					return
				}
			}

			// Send unencrypted response for localhost
			err := sendWSResponse(session, id, resp)
			if err != nil {
				log.Error().Err(err).Msg("error sending response")
			}
		}
	}
}

type WSAuthMessage struct {
	AuthToken string `json:"authToken"`
}

func handleWSAuthentication(session *melody.Session, msg []byte, db *database.Database) error {
	var authMsg WSAuthMessage
	if err := json.Unmarshal(msg, &authMsg); err != nil {
		return fmt.Errorf("invalid auth message format: %w", err)
	}

	if authMsg.AuthToken == "" {
		return errors.New("missing auth token")
	}

	// Validate auth token and get device
	device, err := db.UserDB.GetDeviceByAuthToken(authMsg.AuthToken)
	if err != nil {
		return fmt.Errorf("invalid auth token: %w", err)
	}

	// Store device in session
	session.Set("device", device)

	// Send authentication success response
	authResponse := map[string]any{
		"authenticated": true,
		"device_id":     device.DeviceID,
	}

	responseData, _ := json.Marshal(authResponse)
	err = session.Write(responseData)
	if err != nil {
		return fmt.Errorf("failed to send auth response: %w", err)
	}

	log.Debug().Str("device_id", device.DeviceID).Msg("WebSocket authenticated")
	return nil
}

func handleWSDecryption(_ *melody.Session, msg []byte, device *database.Device, db *database.Database) ([]byte, error) {
	// Parse encrypted message
	var encMsg apimiddleware.EncryptedRequest
	if err := json.Unmarshal(msg, &encMsg); err != nil {
		return nil, fmt.Errorf("invalid encrypted message format: %w", err)
	}

	// Decrypt payload (we can reuse the middleware function by creating a temporary import)
	decryptedPayload, err := apimiddleware.DecryptPayload(encMsg.Encrypted, encMsg.IV, device.SharedSecret)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	// Parse and validate sequence/nonce
	var payload apimiddleware.DecryptedPayload
	if unmarshalErr := json.Unmarshal(decryptedPayload, &payload); unmarshalErr != nil {
		return nil, fmt.Errorf("invalid decrypted payload: %w", unmarshalErr)
	}

	// CRITICAL SECTION: Acquire device lock to prevent race conditions
	// between validation and database update
	unlockDevice := apimiddleware.LockDevice(device.DeviceID)
	defer unlockDevice()

	// Re-fetch device state under lock to get latest sequence/nonce state
	freshDevice, err := db.UserDB.GetDeviceByID(device.DeviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to re-fetch device under lock: %w", err)
	}
	
	// Validate sequence and nonce with fresh device state
	if !apimiddleware.ValidateSequenceAndNonce(freshDevice, payload.Seq, payload.Nonce) {
		return nil, errors.New("invalid sequence or replay detected")
	}

	// Update device state under lock
	userDB, ok := db.UserDB.(*userdb.UserDB)
	if !ok {
		return nil, errors.New("failed to cast UserDB to concrete type")
	}
	if updateErr := apimiddleware.UpdateDeviceState(userDB, freshDevice, payload.Seq, payload.Nonce); updateErr != nil {
		return nil, fmt.Errorf("failed to update device state: %w", updateErr)
	}

	// Return JSON-RPC payload without sequence/nonce
	originalPayload := map[string]any{
		"jsonrpc": payload.JSONRPC,
		"method":  payload.Method,
		"id":      payload.ID,
	}
	if payload.Params != nil {
		originalPayload["params"] = payload.Params
	}

	result, err := json.Marshal(originalPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	return result, nil
}

func sendWSResponseEncrypted(session *melody.Session, id uuid.UUID, result any, device *database.Device) error {
	// Create response object
	resp := models.ResponseObject{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}

	// Marshal to JSON
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("error marshalling response: %w", err)
	}

	// Encrypt the response
	encrypted, iv, err := apimiddleware.EncryptPayload(data, device.SharedSecret)
	if err != nil {
		return fmt.Errorf("failed to encrypt response: %w", err)
	}

	// Send encrypted response
	encResponse := apimiddleware.EncryptedRequest{
		Encrypted: encrypted,
		IV:        iv,
		AuthToken: "", // Not needed for responses
	}

	encData, err := json.Marshal(encResponse)
	if err != nil {
		return fmt.Errorf("failed to marshal encrypted response: %w", err)
	}

	if err := session.Write(encData); err != nil {
		return fmt.Errorf("failed to send encrypted response: %w", err)
	}
	return nil
}

func handlePostRequest(
	methodMap *MethodMap,
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	inTokenQueue chan<- tokens.Token,
	db *database.Database,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "Content-Type is not application/json", http.StatusUnsupportedMediaType)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Error().Err(err).Msg("failed to read request body")
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
			return
		}

		rawIP := strings.SplitN(r.RemoteAddr, ":", 2)
		clientIP := net.ParseIP(rawIP[0])
		env := requests.RequestEnv{
			Platform:   platform,
			Config:     cfg,
			State:      st,
			Database:   db,
			TokenQueue: inTokenQueue,
			IsLocal:    clientIP.IsLoopback(),
		}

		var respBody []byte
		id, resp, rpcError := processRequestObject(methodMap, env, body)
		if rpcError != nil {
			errorResp := models.ResponseErrorObject{
				JSONRPC: "2.0",
				ID:      id,
				Error:   rpcError,
			}
			respBody, err = json.Marshal(errorResp)
			if err != nil {
				log.Error().Err(err).Msg("error marshalling error response")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		} else {
			resp := models.ResponseObject{
				JSONRPC: "2.0",
				ID:      id,
				Result:  resp,
			}
			respBody, err = json.Marshal(resp)
			if err != nil {
				log.Error().Err(err).Msg("error marshalling response")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, err = w.Write(respBody)
		if err != nil {
			log.Error().Err(err).Msg("failed to write error response")
		}
	}
}

// Start starts the API web server and blocks until it shuts down.
func Start(
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	inTokenQueue chan<- tokens.Token,
	db *database.Database,
	notifications <-chan models.Notification,
) {
	port := cfg.APIPort()
	baseOrigins := make([]string, 0, len(allowedOrigins)+4)
	baseOrigins = append(baseOrigins, allowedOrigins...)
	baseOrigins = append(baseOrigins,
		fmt.Sprintf("http://localhost:%d", port),
		fmt.Sprintf("https://localhost:%d", port),
		fmt.Sprintf("http://127.0.0.1:%d", port),
		fmt.Sprintf("https://127.0.0.1:%d", port),
	)

	localIPs := helpers.GetAllLocalIPs()
	for _, localIP := range localIPs {
		log.Debug().Msgf("adding local IP to allowed origins: %s", localIP)
	}

	customOrigins := cfg.AllowedOrigins()
	dynamicAllowedOrigins := buildDynamicAllowedOrigins(baseOrigins, localIPs, port, customOrigins)

	log.Debug().Msgf("dynamicAllowedOrigins: %v", dynamicAllowedOrigins)

	r := chi.NewRouter()

	rateLimiter := apimiddleware.NewIPRateLimiter()
	rateLimiter.StartCleanup(st.GetContext())

	r.Use(apimiddleware.HTTPRateLimitMiddleware(rateLimiter))
	r.Use(middleware.Recoverer)
	r.Use(middleware.NoCache)
	r.Use(middleware.Timeout(config.APIRequestTimeout))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: dynamicAllowedOrigins,
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Content-Type"},
		ExposedHeaders: []string{},
	}))

	if strings.HasSuffix(config.AppVersion, "-dev") {
		r.Mount("/debug", middleware.Profiler())
		log.Info().Msg("pprof endpoints enabled at /debug/pprof/")
	}

	methodMap := NewMethodMap()

	session := melody.New()
	session.Upgrader.CheckOrigin = func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		log.Debug().Msgf("websocket origin: %s", origin)
		return checkWebSocketOrigin(origin, dynamicAllowedOrigins, port)
	}
	go broadcastNotifications(st, session, notifications)

	// Pairing endpoints (no authentication required)
	r.Post("/api/pair/initiate", handlePairingInitiate(db))
	r.Post("/api/pair/complete", handlePairingComplete(db))

	// Protected API routes with authentication middleware
	r.Route("/api", func(r chi.Router) {
		r.Use(apimiddleware.AuthMiddleware(db))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			err := session.HandleRequest(w, r)
			if err != nil {
				log.Error().Err(err).Msg("handling websocket request: latest")
			}
		})
		r.Post("/", handlePostRequest(methodMap, platform, cfg, st, inTokenQueue, db))
	})

	r.Route("/api/v0", func(r chi.Router) {
		r.Use(apimiddleware.AuthMiddleware(db))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			err := session.HandleRequest(w, r)
			if err != nil {
				log.Error().Err(err).Msg("handling websocket request: v0")
			}
		})
		r.Post("/", handlePostRequest(methodMap, platform, cfg, st, inTokenQueue, db))
	})

	r.Route("/api/v0.1", func(r chi.Router) {
		r.Use(apimiddleware.AuthMiddleware(db))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			err := session.HandleRequest(w, r)
			if err != nil {
				log.Error().Err(err).Msg("handling websocket request: v0.1")
			}
		})
		r.Post("/", handlePostRequest(methodMap, platform, cfg, st, inTokenQueue, db))
	})

	session.HandleMessage(apimiddleware.WebSocketRateLimitHandler(
		rateLimiter,
		handleWSMessage(methodMap, platform, cfg, st, inTokenQueue, db),
	))

	r.Get("/l/*", methods.HandleRunRest(cfg, st, inTokenQueue)) // DEPRECATED
	r.Get("/r/*", methods.HandleRunRest(cfg, st, inTokenQueue))
	r.Get("/run/*", methods.HandleRunRest(cfg, st, inTokenQueue))

	r.Get("/app/*", handleApp)
	r.Get("/app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})

	server := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.APIPort()),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverDone := make(chan error, 1)
	serverReady := make(chan struct{})

	go func() {
		log.Info().Msgf("starting HTTP server on port %d", cfg.APIPort())
		log.Debug().Msg("HTTP server goroutine started, attempting to bind to port")

		// Create a listener to ensure we can bind to the port before continuing
		lc := &net.ListenConfig{}
		listener, err := lc.Listen(st.GetContext(), "tcp", server.Addr)
		if err != nil {
			log.Error().Err(err).Msg("failed to bind to port")
			serverDone <- err
			return
		}

		// Signal that server is ready to accept connections
		log.Debug().Msg("HTTP server bound to port, ready to accept connections")
		close(serverReady)

		// Start serving
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("HTTP server error")
			serverDone <- err
		} else {
			log.Debug().Msg("HTTP server stopped normally")
			serverDone <- nil
		}
	}()

	log.Debug().Msg("HTTP server goroutine launched, waiting for server to be ready")

	// Wait for server to be ready or fail to start
	select {
	case <-serverReady:
		log.Debug().Msg("HTTP server is ready to accept connections")
	case err := <-serverDone:
		if err != nil {
			log.Error().Err(err).Msg("server failed to start")
			return
		}
	}

	select {
	case <-st.GetContext().Done():
		log.Info().Msg("initiating HTTP server graceful shutdown")
	case err := <-serverDone:
		if err != nil {
			log.Error().Err(err).Msg("HTTP server failed to start")
			return
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	} else {
		log.Info().Msg("HTTP server shutdown complete")
	}
}
