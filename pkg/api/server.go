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

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/methods"
	apimiddleware "github.com/ZaparooProject/zaparoo-core/v2/pkg/api/middleware"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
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

// logSafeRequest logs a request but avoids logging sensitive or large content
func logSafeRequest(req *models.RequestObject) {
	if req.Method == models.MethodSettingsLogsDownload {
		log.Debug().Str("method", req.Method).Interface("id", req.ID).Msg("received logs download request")
	} else {
		log.Debug().Interface("request", req).Msg("received request")
	}
}

// logSafeResponse logs a response but truncates large content to prevent recursive logging issues
func logSafeResponse(result any) {
	if logResp, ok := result.(models.LogDownloadResponse); ok {
		truncated := logResp
		if len(truncated.Content) > 100 {
			truncated.Content = truncated.Content[:100] + "... [truncated " +
				strconv.Itoa(len(logResp.Content)-100) + " more chars]"
		}
		log.Debug().Interface("result", truncated).Msg("sending response")
	} else {
		log.Debug().Interface("result", result).Msg("sending response")
	}
}

type MethodMap struct {
	sync.Map
}

func (m *MethodMap) Store(key, value any) {
	m.Map.Store(key, value)
}

func (m *MethodMap) Load(key any) (any, bool) {
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
		models.MethodMedia:               methods.HandleMedia,
		models.MethodMediaGenerate:       methods.HandleGenerateMedia,
		models.MethodMediaGenerateCancel: methods.HandleMediaGenerateCancel,
		models.MethodMediaIndex:          methods.HandleGenerateMedia,
		models.MethodMediaSearch:         methods.HandleMediaSearch,
		models.MethodMediaTags:           methods.HandleMediaTags,
		models.MethodMediaActive:         methods.HandleActiveMedia,
		models.MethodMediaActiveUpdate:   methods.HandleUpdateActiveMedia,
		// settings
		models.MethodSettings:             methods.HandleSettings,
		models.MethodSettingsUpdate:       methods.HandleSettingsUpdate,
		models.MethodSettingsReload:       methods.HandleSettingsReload,
		models.MethodSettingsLogsDownload: methods.HandleLogsDownload,
		models.MethodPlaytimeLimits:       methods.HandlePlaytimeLimits,
		models.MethodPlaytimeLimitsUpdate: methods.HandlePlaytimeLimitsUpdate,
		models.MethodPlaytime:             methods.HandlePlaytime,
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
		models.MethodReaders: func(env requests.RequestEnv) (any, error) {
			return methods.HandleReaders(env.State.ListReaders())
		},
		models.MethodReadersWrite: func(env requests.RequestEnv) (any, error) {
			ls := env.State.GetLastScanned()
			return methods.HandleReaderWrite(
				env.Params,
				env.State.ListReaders(),
				&ls,
				env.State.SetWroteToken,
			)
		},
		models.MethodReadersWriteCancel: func(env requests.RequestEnv) (any, error) {
			return methods.HandleReaderWriteCancel(
				env.Params,
				env.State.ListReaders(),
			)
		},
		// utils
		models.MethodVersion:     methods.HandleVersion,
		models.MethodHealthCheck: methods.HandleHealthCheck,
		// inbox
		models.MethodInbox:       methods.HandleInbox,
		models.MethodInboxDelete: methods.HandleInboxDelete,
		models.MethodInboxClear:  methods.HandleInboxClear,
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
	logSafeRequest(&req)

	fn, ok := methodMap.GetMethod(req.Method)
	if !ok {
		log.Warn().Str("method", req.Method).Msg("unknown method")
		return nil, &JSONRPCErrorMethodNotFound
	}

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
func sendWSResponse(session *melody.Session, id models.RPCID, result any) error {
	logSafeResponse(result)

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
func sendWSError(session *melody.Session, id models.RPCID, errObj models.ErrorObject) error {
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

// mimeFallbacks covers types not in Go's built-in mime package.
var mimeFallbacks = map[string]string{
	".woff":  "font/woff",
	".woff2": "font/woff2",
}

// fsCustom404 creates a file server handler with SPA fallback support.
// Unknown paths fall back to index.html for client-side routing.
func fsCustom404(root http.FileSystem) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := r.URL.Path

		f, err := root.Open(upath)
		if err != nil {
			if os.IsNotExist(err) {
				serveIndex(w, r, root)
				return
			}
			log.Error().Err(err).Str("path", upath).Msg("error opening file")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer func() { _ = f.Close() }()

		stat, err := f.Stat()
		if err != nil {
			log.Error().Err(err).Str("path", upath).Msg("error stating file")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		if stat.IsDir() {
			serveIndex(w, r, root)
			return
		}

		ext := filepath.Ext(upath)
		if ct := mime.TypeByExtension(ext); ct != "" {
			w.Header().Set("Content-Type", ct)
		} else if ct, ok := mimeFallbacks[ext]; ok {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")

		http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
	})
}

// serveIndex serves the SPA index.html for client-side routing.
func serveIndex(w http.ResponseWriter, r *http.Request, root http.FileSystem) {
	index, err := root.Open("index.html")
	if err != nil {
		log.Error().Err(err).Msg("error opening index.html")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer func() { _ = index.Close() }()

	stat, err := index.Stat()
	if err != nil {
		log.Error().Err(err).Msg("error stating index.html")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", stat.ModTime(), index)
}

const errMsgAppNotFound = "Zaparoo App files not found. " +
	"Copy the built zaparoo-app files to pkg/assets/_app/dist/"

// handleApp serves the embedded Zaparoo App web build to the client.
func handleApp(w http.ResponseWriter, r *http.Request) {
	appFs, err := fs.Sub(assets.App, "_app/dist")
	if err != nil {
		log.Error().Err(err).Msg("error opening app dist")
		http.Error(w, errMsgAppNotFound, http.StatusInternalServerError)
		return
	}

	if _, err := appFs.Open("index.html"); err != nil {
		log.Error().Msg("zaparoo-app files not found in embedded filesystem")
		http.Error(w, errMsgAppNotFound, http.StatusInternalServerError)
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

// checkWebSocketOrigin validates WebSocket origin requests based on security policy.
// It checks static origins and dynamically fetches custom origins from the provider.
func checkWebSocketOrigin(
	origin string,
	staticOrigins []string,
	customOriginsProvider OriginsProvider,
	apiPort int,
) bool {
	if origin == "" {
		log.Debug().Msg("websocket origin: empty origin allowed (same-origin)")
		return true
	}

	// Check static origins (case-insensitive)
	for _, allowed := range staticOrigins {
		if strings.EqualFold(origin, allowed) {
			log.Debug().Msgf("websocket origin: %s allowed (static match)", origin)
			return true
		}
	}

	// Check custom origins (fetched dynamically)
	customOrigins := expandCustomOrigins(customOriginsProvider(), apiPort)
	for _, allowed := range customOrigins {
		if strings.EqualFold(origin, allowed) {
			log.Debug().Msgf("websocket origin: %s allowed (custom match)", origin)
			return true
		}
	}

	// Parse origin URL
	u, err := url.Parse(origin)
	if err != nil {
		log.Debug().Msgf("websocket origin: %s rejected (invalid URL: %v)", origin, err)
		return false
	}

	// Allow localhost and 127.0.0.1 on any port
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
			return false
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

// expandCustomOrigins expands custom origin entries into full origin URLs.
// Custom origins can be:
// - Full URLs with port: http://example.com:7497 (used as-is)
// - Full URLs without port: http://example.com (adds version with API port too)
// - Other schemes: capacitor://localhost, ionic://localhost (used as-is)
// - Hostnames only: example.com (adds http/https with and without port)
func expandCustomOrigins(customOrigins []string, port int) []string {
	var result []string
	for _, origin := range customOrigins {
		origin = strings.TrimSpace(origin)
		origin = strings.TrimSuffix(origin, "/")

		if strings.Contains(origin, "://") {
			result = append(result, origin)

			if strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://") {
				u, err := url.Parse(origin)
				if err == nil && u.Port() == "" {
					result = append(result, fmt.Sprintf("%s:%d", origin, port))
				}
			}
		} else {
			result = append(result,
				"http://"+origin,
				"https://"+origin,
				fmt.Sprintf("http://%s:%d", origin, port),
				fmt.Sprintf("https://%s:%d", origin, port),
			)
		}
	}
	return result
}

// buildStaticAllowedOrigins creates the static part of allowed origins (base + local IPs).
func buildStaticAllowedOrigins(baseOrigins, localIPs []string, port int) []string {
	result := make([]string, 0, len(baseOrigins)+len(localIPs)*2)
	result = append(result, baseOrigins...)

	for _, localIP := range localIPs {
		result = append(result,
			fmt.Sprintf("http://%s:%d", localIP, port),
			fmt.Sprintf("https://%s:%d", localIP, port),
		)
	}

	return result
}

// buildDynamicAllowedOrigins creates the allowed origins list for CORS/WebSocket
func buildDynamicAllowedOrigins(baseOrigins, localIPs []string, port int, customOrigins []string) []string {
	result := buildStaticAllowedOrigins(baseOrigins, localIPs, port)
	result = append(result, expandCustomOrigins(customOrigins, port)...)
	return result
}

// OriginsProvider is a function that returns custom origins from config.
type OriginsProvider func() []string

// makeOriginValidator creates an origin validation function for CORS middleware.
// It checks against static origins and dynamically fetches custom origins on each request.
func makeOriginValidator(
	staticOrigins []string,
	customOriginsProvider OriginsProvider,
	port int,
) func(*http.Request, string) bool {
	staticSet := make(map[string]struct{}, len(staticOrigins))
	for _, o := range staticOrigins {
		staticSet[strings.ToLower(o)] = struct{}{}
	}

	return func(_ *http.Request, origin string) bool {
		lowerOrigin := strings.ToLower(origin)

		// Check static origins first
		if _, ok := staticSet[lowerOrigin]; ok {
			return true
		}

		// Check custom origins (fetched dynamically)
		customOrigins := expandCustomOrigins(customOriginsProvider(), port)
		for _, allowed := range customOrigins {
			if strings.EqualFold(origin, allowed) {
				return true
			}
		}

		return false
	}
}

// privateNetworkAccessMiddleware adds the Access-Control-Allow-Private-Network
// header for preflight requests. This allows HTTPS websites (like zaparoo.app)
// to connect to Zaparoo Core running on local network IPs.
// See: https://wicg.github.io/private-network-access/
func privateNetworkAccessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a preflight request asking for private network access
		if r.Method == http.MethodOptions &&
			r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}
		next.ServeHTTP(w, r)
	})
}

// broadcastNotifications consumes and broadcasts all incoming API
// notifications to all connected clients.
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
			req := models.NotificationObject{
				JSONRPC: "2.0",
				Method:  notif.Method,
				Params:  notif.Params,
			}

			data, err := json.Marshal(req)
			if err != nil {
				log.Error().Err(err).Msg("marshalling notification request")
				continue
			}

			// TODO: this will not work with encryption
			// Broadcast synchronously to maintain strict notification ordering.
			// This is critical for media.started/stopped sequences where order matters.
			// The broadcastNotifications goroutine already runs async, so we don't need
			// another level of async that would cause out-of-order delivery.
			err = session.Broadcast(data)
			if err != nil {
				log.Error().Err(err).Msg("broadcasting notification")
			}
		}
	}
}

// requestResult holds the result of processing a JSON-RPC request.
type requestResult struct {
	Result      any
	Error       *models.ErrorObject
	ID          models.RPCID
	ShouldReply bool
}

// processRequestObject parses and handles incoming JSON-RPC messages.
// Per JSON-RPC 2.0 spec:
// - Notifications (missing ID): server MUST NOT reply
// - Incoming responses: server MUST NOT reply
// - Requests: server MUST reply
func processRequestObject(
	methodMap *MethodMap,
	env requests.RequestEnv, //nolint:gocritic // single-use parameter in API handler
	msg []byte,
) requestResult {
	if !json.Valid(msg) {
		log.Warn().Msg("request payload is not valid JSON")
		return requestResult{ID: models.NullRPCID, Error: &JSONRPCErrorParseError, ShouldReply: true}
	}

	// try parse a request first, which has a method field
	var req models.RequestObject
	err := json.Unmarshal(msg, &req)

	if err == nil && req.JSONRPC != "2.0" {
		log.Warn().Str("version", req.JSONRPC).Msg("unsupported JSON-RPC version")
		return requestResult{ID: req.ID, Error: &JSONRPCErrorInvalidRequest, ShouldReply: true}
	}

	if err == nil && req.Method != "" {
		if req.ID.IsAbsent() {
			// Missing ID = notification per JSON-RPC 2.0 spec
			// Server MUST NOT reply to notifications
			log.Info().Interface("req", req).Msg("received notification, ignoring")
			return requestResult{ShouldReply: false}
		}

		// ID is present (could be null or valid value) - this is a request that needs a response
		resp, rpcError := handleRequest(methodMap, env, req)
		if rpcError != nil {
			return requestResult{ID: req.ID, Error: rpcError, ShouldReply: true}
		}
		return requestResult{ID: req.ID, Result: resp, ShouldReply: true}
	}

	// otherwise try parse a response, which has an id field
	var resp models.ResponseObject
	err = json.Unmarshal(msg, &resp)
	if err == nil && !resp.ID.IsAbsent() {
		// This is an incoming response - handle it but don't reply
		err := handleResponse(resp)
		if err != nil {
			log.Error().Err(err).Msg("error handling response")
		}
		return requestResult{ShouldReply: false}
	}

	// can't identify the message
	return requestResult{ID: models.NullRPCID, Error: &JSONRPCErrorInvalidRequest, ShouldReply: true}
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
	limitsManager *playtime.LimitsManager,
	player audio.Player,
) func(session *melody.Session, msg []byte) {
	return func(session *melody.Session, msg []byte) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("panic in websocket handler")
				err := sendWSError(session, models.NullRPCID, JSONRPCErrorInternalError)
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
		env := requests.RequestEnv{
			Platform:      platform,
			Config:        cfg,
			State:         st,
			Database:      db,
			LimitsManager: limitsManager,
			LauncherCache: helpers.GlobalLauncherCache,
			Player:        player,
			TokenQueue:    inTokenQueue,
			IsLocal:       clientIP.IsLoopback(),
			ClientID:      session.Request.RemoteAddr,
		}

		result := processRequestObject(methodMap, env, msg)
		if !result.ShouldReply {
			// Notifications and incoming responses don't get replies
			return
		}
		if result.Error != nil {
			err := sendWSError(session, result.ID, *result.Error)
			if err != nil {
				log.Error().Err(err).Msg("error sending error response")
			}
		} else {
			err := sendWSResponse(session, result.ID, result.Result)
			if err != nil {
				log.Error().Err(err).Msg("error sending response")
			}
		}
	}
}

func handlePostRequest(
	methodMap *MethodMap,
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	inTokenQueue chan<- tokens.Token,
	db *database.Database,
	limitsManager *playtime.LimitsManager,
	player audio.Player,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaType != "application/json" {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}

		const maxPostBodySize = 1 << 20 // 1MB
		r.Body = http.MaxBytesReader(w, r.Body, maxPostBodySize)

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Error().Err(err).Msg("failed to read request body")
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		rawIP := strings.SplitN(r.RemoteAddr, ":", 2)
		clientIP := net.ParseIP(rawIP[0])
		env := requests.RequestEnv{
			Platform:      platform,
			Config:        cfg,
			State:         st,
			Database:      db,
			LimitsManager: limitsManager,
			LauncherCache: helpers.GlobalLauncherCache,
			Player:        player,
			TokenQueue:    inTokenQueue,
			IsLocal:       clientIP.IsLoopback(),
			ClientID:      r.RemoteAddr,
		}

		result := processRequestObject(methodMap, env, body)
		if !result.ShouldReply {
			// Notifications and incoming responses don't get replies
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var respBody []byte
		if result.Error != nil {
			errorResp := models.ResponseErrorObject{
				JSONRPC: "2.0",
				ID:      result.ID,
				Error:   result.Error,
			}
			respBody, err = json.Marshal(errorResp)
			if err != nil {
				log.Error().Err(err).Msg("error marshalling error response")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		} else {
			resp := models.ResponseObject{
				JSONRPC: "2.0",
				ID:      result.ID,
				Result:  result.Result,
			}
			respBody, err = json.Marshal(resp)
			if err != nil {
				log.Error().Err(err).Msg("error marshalling response")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err = w.Write(respBody)
		if err != nil {
			log.Error().Err(err).Msg("failed to write response")
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
	limitsManager *playtime.LimitsManager,
	notifications <-chan models.Notification,
	mdnsHostname string,
	player audio.Player,
) {
	// Extract port from listen address or use default
	port := cfg.APIPort()
	listenAddr := cfg.APIListen()
	if _, portStr, err := net.SplitHostPort(listenAddr); err == nil && portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

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

	// Build static origins (base + local IPs + mDNS + OS hostname)
	staticOrigins := buildStaticAllowedOrigins(baseOrigins, localIPs, port)

	if mdnsHostname != "" {
		mdnsLocal := mdnsHostname + ".local"
		staticOrigins = append(staticOrigins,
			"http://"+mdnsLocal,
			"https://"+mdnsLocal,
			fmt.Sprintf("http://%s:%d", mdnsLocal, port),
			fmt.Sprintf("https://%s:%d", mdnsLocal, port),
		)
		log.Debug().Str("hostname", mdnsLocal).Msg("added mDNS hostname to allowed origins")
	}

	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		staticOrigins = append(staticOrigins,
			"http://"+hostname,
			"https://"+hostname,
			fmt.Sprintf("http://%s:%d", hostname, port),
			fmt.Sprintf("https://%s:%d", hostname, port),
		)
		log.Debug().Str("hostname", hostname).Msg("added OS hostname to allowed origins")
	}

	log.Debug().Msgf("staticOrigins: %v", staticOrigins)

	// Create origin validator that checks static origins + dynamic custom origins
	originValidator := makeOriginValidator(staticOrigins, cfg.AllowedOrigins, port)

	r := chi.NewRouter()

	rateLimiter := apimiddleware.NewIPRateLimiter()
	rateLimiter.StartCleanup(st.GetContext())

	ipFilter := apimiddleware.NewIPFilter(cfg.AllowedIPs)
	authConfig := apimiddleware.NewAuthConfig(config.GetAPIKeys)

	// Global middleware for all routes
	r.Use(apimiddleware.HTTPIPFilterMiddleware(ipFilter))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(config.APIRequestTimeout))
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc: originValidator,
		AllowedMethods:  []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:  []string{"Accept", "Content-Type", "Authorization"},
		ExposedHeaders:  []string{},
	}))
	r.Use(privateNetworkAccessMiddleware)

	// Rate limiting only for API routes, not static assets
	apiRateLimitMiddleware := apimiddleware.HTTPRateLimitMiddleware(rateLimiter)

	if strings.HasSuffix(config.AppVersion, "-dev") {
		r.Mount("/debug", middleware.Profiler())
		log.Info().Msg("pprof endpoints enabled at /debug/pprof/")
	}

	methodMap := NewMethodMap()

	session := melody.New()
	defer func() {
		if err := session.Close(); err != nil {
			log.Error().Err(err).Msg("WebSocket session close error")
		}
	}()
	session.Upgrader.CheckOrigin = func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		log.Debug().Msgf("websocket origin: %s", origin)
		return checkWebSocketOrigin(origin, staticOrigins, cfg.AllowedOrigins, port)
	}
	go broadcastNotifications(st, session, notifications)

	// API routes
	r.Group(func(r chi.Router) {
		r.Use(apimiddleware.HTTPAuthMiddleware(authConfig))
		r.Use(apiRateLimitMiddleware)
		r.Use(middleware.NoCache)

		// WebSocket handler that checks auth before upgrade
		wsHandler := func(w http.ResponseWriter, r *http.Request, version string) {
			if !apimiddleware.WebSocketAuthHandler(authConfig, r) {
				http.Error(w, "Unauthorized: API key required", http.StatusUnauthorized)
				return
			}
			err := session.HandleRequest(w, r)
			if err != nil {
				log.Error().Err(err).Msgf("handling websocket request: %s", version)
			}
		}

		r.Get("/api", func(w http.ResponseWriter, r *http.Request) {
			wsHandler(w, r, "latest")
		})
		r.Post("/api", handlePostRequest(methodMap, platform, cfg, st, inTokenQueue, db, limitsManager, player))

		r.Get("/api/v0", func(w http.ResponseWriter, r *http.Request) {
			wsHandler(w, r, "v0")
		})
		r.Post("/api/v0", handlePostRequest(methodMap, platform, cfg, st, inTokenQueue, db, limitsManager, player))

		r.Get("/api/v0.1", func(w http.ResponseWriter, r *http.Request) {
			wsHandler(w, r, "v0.1")
		})
		r.Post("/api/v0.1", handlePostRequest(methodMap, platform, cfg, st, inTokenQueue, db, limitsManager, player))

		// REST action endpoints
		r.Get("/l/*", methods.HandleRunRest(cfg, st, inTokenQueue)) // DEPRECATED
		r.Get("/r/*", methods.HandleRunRest(cfg, st, inTokenQueue))
		r.Get("/run/*", methods.HandleRunRest(cfg, st, inTokenQueue))
	})

	session.HandleMessage(apimiddleware.WebSocketRateLimitHandler(
		rateLimiter,
		handleWSMessage(methodMap, platform, cfg, st, inTokenQueue, db, limitsManager, player),
	))

	// Static app assets
	r.Get("/app/*", handleApp)
	r.Get("/app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})

	// the health endpoint is behind every standard middleware we added
	// the response is a simple string on purpose, we want just to be able
	// to see if the server is up and answering
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Redirect root to app for users who forget /app/
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})

	server := &http.Server{
		Addr:              cfg.APIListen(),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverDone := make(chan error, 1)
	serverReady := make(chan struct{})

	go func() {
		log.Info().Str("listen", cfg.APIListen()).Msg("starting HTTP server")
		log.Debug().Msg("HTTP server goroutine started, attempting to bind")

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
			log.Error().Err(err).Msg("API server failed to start, stopping service")
			st.StopService()
			return
		}
	}

	select {
	case <-st.GetContext().Done():
		log.Info().Msg("initiating HTTP server graceful shutdown")
	case err := <-serverDone:
		if err != nil {
			log.Error().Err(err).Msg("API server failed during operation, stopping service")
			st.StopService()
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
