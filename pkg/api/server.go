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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/broker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playtime"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
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
	log.Debug().Str("method", req.Method).Interface("id", req.ID).Msg("received request")
}

// logSafeResponse logs a response but truncates large content to prevent recursive logging issues
func logSafeResponse(result any) {
	switch resp := result.(type) {
	case models.LogDownloadResponse:
		truncated := resp
		if len(truncated.Content) > 100 {
			truncated.Content = truncated.Content[:100] + "... [truncated " +
				strconv.Itoa(len(resp.Content)-100) + " more chars]"
		}
		log.Debug().Interface("result", truncated).Msg("sending response")
	case models.ScreenshotResponse:
		truncated := resp
		if len(truncated.Data) > 100 {
			truncated.Data = truncated.Data[:100] + "... [truncated " +
				strconv.Itoa(len(resp.Data)-100) + " more chars]"
		}
		log.Debug().Interface("result", truncated).Msg("sending response")
	default:
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
		models.MethodLaunch:  methods.HandleRun, // DEPRECATED
		models.MethodRun:     methods.HandleRun,
		models.MethodStop:    methods.HandleStop,
		models.MethodConfirm: methods.HandleConfirm,
		// tokens
		models.MethodTokens:  methods.HandleTokens,
		models.MethodHistory: methods.HandleHistory,
		// media
		models.MethodMedia:               methods.HandleMedia,
		models.MethodMediaGenerate:       methods.HandleGenerateMedia,
		models.MethodMediaGenerateCancel: methods.HandleMediaGenerateCancel,
		models.MethodMediaGenerateResume: methods.HandleMediaGenerateResume,
		models.MethodMediaIndex:          methods.HandleGenerateMedia,
		models.MethodMediaSearch:         methods.HandleMediaSearch,
		models.MethodMediaBrowse:         methods.HandleMediaBrowse,
		models.MethodMediaTags:           methods.HandleMediaTags,
		models.MethodMediaActive:         methods.HandleActiveMedia,
		models.MethodMediaActiveUpdate:   methods.HandleUpdateActiveMedia,
		models.MethodMediaHistory:        methods.HandleMediaHistory,
		models.MethodMediaHistoryTop:     methods.HandleMediaHistoryTop,
		models.MethodMediaLookup:         methods.HandleMediaLookup,
		models.MethodMediaControl:        methods.HandleMediaControl,
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
		// input
		models.MethodInputKeyboard: methods.HandleInputKeyboard,
		models.MethodInputGamepad:  methods.HandleInputGamepad,
		// screenshot
		models.MethodScreenshot: methods.HandleScreenshot,
		// utils
		models.MethodVersion:     methods.HandleVersion,
		models.MethodHealthCheck: methods.HandleHealthCheck,
		// inbox
		models.MethodInbox:       methods.HandleInbox,
		models.MethodInboxDelete: methods.HandleInboxDelete,
		models.MethodInboxClear:  methods.HandleInboxClear,
		// clients (paired API clients)
		models.MethodClients:       methods.HandleClients,
		models.MethodClientsDelete: methods.HandleClientsDelete,
		// auth
		models.MethodSettingsAuthClaim: func(env requests.RequestEnv) (any, error) {
			return methods.HandleSettingsAuthClaim(env, zapscript.FetchWellKnown)
		},
		// update
		models.MethodUpdateCheck: func(env requests.RequestEnv) (any, error) {
			return methods.HandleUpdateCheck(env, updater.Check)
		},
		models.MethodUpdateApply: func(env requests.RequestEnv) (any, error) {
			return methods.HandleUpdateApply(env, updater.Apply, env.State.RestartService)
		},
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
		var clientErr *models.ClientError
		if errors.As(err, &clientErr) {
			log.Warn().Err(err).Str("method", req.Method).Msg("client error")
		} else {
			log.Error().Err(err).Str("method", req.Method).Msg("error handling request")
		}
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

// logWSWriteError logs WebSocket write errors at the appropriate level.
// Session closed errors are expected (client disconnected) and logged as Warn.
func logWSWriteError(err error, msg string) {
	if errors.Is(err, melody.ErrSessionClosed) {
		log.Warn().Err(err).Msg(msg)
	} else {
		log.Error().Err(err).Msg(msg)
	}
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
//
// Iteration is synchronous to preserve strict notification ordering (e.g.
// media.started/stopped sequences): each session's encrypt + write completes
// before the next session is touched.
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

			broadcastToSessions(session, data)
		}
	}
}

// broadcastToSessions sends a notification to every connected session,
// encrypting per-session for sessions that have established encryption.
// Errors on individual sessions are logged but do not stop the broadcast.
func broadcastToSessions(session *melody.Melody, plaintext []byte) {
	sessions, err := session.Sessions()
	if err != nil {
		logWSWriteError(err, "fetching sessions for broadcast")
		return
	}
	for _, s := range sessions {
		if s == nil || s.IsClosed() {
			continue
		}
		writeNotificationToSession(s, plaintext)
	}
}

// writeNotificationToSession writes a single notification to a single
// melody session, encrypting if the session has an established encryption
// session. For encrypted sessions the encrypt + enqueue happens under the
// per-session mutex via SendEncryptedFrame so concurrent writers cannot
// reorder counters on the wire.
//
// On any send-side encryption failure (counter exhaustion, AEAD setup
// error, write failure) the session is closed: a desynced session
// cannot recover and keeping it open hides the bug from the client.
func writeNotificationToSession(s *melody.Session, plaintext []byte) {
	cs := getClientSession(s)
	if cs == nil {
		if err := s.Write(plaintext); err != nil {
			logWSWriteError(err, "broadcasting plaintext notification")
		}
		return
	}
	if err := cs.SendEncryptedFrame(plaintext, s.Write); err != nil {
		logWSWriteError(err, "broadcasting encrypted notification")
		closeMelodySession(s)
	}
}

// handleSSE returns an HTTP handler that streams notifications as Server-Sent
// Events. Each connected client gets its own broker subscription which is
// cleaned up on disconnect.
func handleSSE(notifBroker *broker.Broker, st *state.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		notifs, subID := notifBroker.Subscribe(100)
		defer notifBroker.Unsubscribe(subID)

		w.Header().Set("Content-Type", "text/event-stream")
		flusher.Flush()

		log.Info().Int("subscriber_id", subID).Msg("SSE client connected")

		for {
			select {
			case <-r.Context().Done():
				log.Info().Int("subscriber_id", subID).Msg("SSE client disconnected")
				return
			case <-st.GetContext().Done():
				return
			case notif, ok := <-notifs:
				if !ok {
					return
				}

				obj := models.NotificationObject{
					JSONRPC: "2.0",
					Method:  notif.Method,
					Params:  notif.Params,
				}

				data, err := json.Marshal(obj)
				if err != nil {
					log.Error().Err(err).Msg("marshalling SSE notification")
					continue
				}

				if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
					log.Debug().Err(err).Msg("SSE write failed, client likely disconnected")
					return
				}
				flusher.Flush()
			}
		}
	}
}

// requestResult holds the result of processing a JSON-RPC request.
type requestResult struct {
	Result      any
	Error       *models.ErrorObject
	AfterWrite  func() // called after the response has been written to the client
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
			log.Info().Str("method", req.Method).Msg("received notification, ignoring")
			return requestResult{ShouldReply: false}
		}

		// ID is present (could be null or valid value) - this is a request that needs a response
		resp, rpcError := handleRequest(methodMap, env, req)
		if rpcError != nil {
			return requestResult{ID: req.ID, Error: rpcError, ShouldReply: true}
		}

		// Unwrap ResponseWithCallback to extract the AfterWrite hook
		var afterWrite func()
		if rwc, ok := resp.(models.ResponseWithCallback); ok {
			resp = rwc.Result
			afterWrite = rwc.AfterWrite
		}

		return requestResult{ID: req.ID, Result: resp, AfterWrite: afterWrite, ShouldReply: true}
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
//
// When encryption is enabled the handler also performs transparent
// decryption of encrypted frames and encryption of responses. The first
// frame on a new connection determines whether the session is encrypted
// (has v + e + t + s) or plaintext.
func handleWSMessage(
	methodMap *MethodMap,
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	inTokenQueue chan<- tokens.Token,
	confirmQueue chan<- chan error,
	db *database.Database,
	limitsManager *playtime.LimitsManager,
	player audio.Player,
	indexPauser *syncutil.Pauser,
	encGateway *apimiddleware.EncryptionGateway,
	lastSeenTracker *apimiddleware.LastSeenTracker,
) func(session *melody.Session, msg []byte) {
	return func(session *melody.Session, msg []byte) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("panic in websocket handler")
				err := sendWSError(session, models.NullRPCID, JSONRPCErrorInternalError)
				if err != nil {
					logWSWriteError(err, "error sending panic error response")
				}
			}
		}()

		clientIP := apimiddleware.ParseRemoteIP(session.Request.RemoteAddr)
		isLocal := apimiddleware.IsLoopbackAddr(session.Request.RemoteAddr)
		var sourceIP string
		if clientIP != nil {
			sourceIP = clientIP.String()
		}
		encryptionEnabled := cfg.EncryptionEnabled()

		// An unparseable RemoteAddr would collapse all such clients onto a
		// shared empty-string rate-limit bucket — a false positive could
		// then block every other unparseable client for the same auth
		// token. In practice net/http always populates RemoteAddr from the
		// underlying TCP conn, so this only fires if a future reverse-proxy
		// header parser writes a malformed value. Reject the connection
		// rather than risk a degenerate limiter state. Plaintext local
		// connections do not need a sourceIP and are not rate-limited per
		// (token, IP), so they are exempt.
		if sourceIP == "" && encryptionEnabled && !isLocal {
			log.Warn().
				Str("remote_addr", session.Request.RemoteAddr).
				Msg("ws: rejecting encrypted connection from unparseable remote addr")
			closeMelodySession(session)
			return
		}

		// Decrypt the incoming frame if needed and update the per-session
		// encryption state. Returns the plaintext to dispatch and the
		// resolved client session (or nil for plaintext sessions).
		plaintext, cs, ok := decryptIncomingFrame(
			session, msg, encGateway, encryptionEnabled, isLocal, sourceIP)
		if !ok {
			return
		}

		// Mark the paired client as recently seen. The tracker batches
		// these in memory and the flush goroutine persists them on a
		// 30-second cadence (and once more on graceful shutdown).
		// Plaintext sessions have no associated paired client, so cs is
		// nil and Touch is skipped.
		if cs != nil && lastSeenTracker != nil {
			lastSeenTracker.Touch(cs.AuthToken(), time.Now().Unix())
		}

		// Heartbeat ping/pong runs on the decrypted plaintext so encrypted
		// sessions get an encrypted pong, and remote plaintext probes are
		// rejected by decryptIncomingFrame before reaching this point.
		if bytes.Equal(plaintext, []byte("ping")) {
			if err := writePong(session.Write, cs); err != nil {
				// Encrypted send failed (counter exhausted, write error,
				// etc.) — the encrypted session is desynced and cannot
				// recover. Plaintext sessions are also closed because a
				// write failure means the wire is gone.
				logWSWriteError(err, "sending pong")
				closeMelodySession(session)
			}
			return
		}

		reqCtx, reqCancel := context.WithTimeout(st.GetContext(), config.APIRequestTimeout)
		defer reqCancel()

		env := requests.RequestEnv{
			Context:       reqCtx,
			Platform:      platform,
			Config:        cfg,
			State:         st,
			Database:      db,
			LimitsManager: limitsManager,
			LauncherCache: helpers.GlobalLauncherCache,
			Player:        player,
			TokenQueue:    inTokenQueue,
			ConfirmQueue:  confirmQueue,
			IndexPauser:   indexPauser,
			IsLocal:       isLocal,
			ClientID:      session.Request.RemoteAddr,
		}

		result := processRequestObject(methodMap, env, plaintext)
		if !result.ShouldReply {
			// Notifications and incoming responses don't get replies
			return
		}
		if result.Error != nil {
			err := sendWSEncryptedError(session, cs, result.ID, *result.Error)
			if err != nil {
				logWSWriteError(err, "error sending error response")
				// Encrypted send failed (counter exhausted, write
				// error, etc.) — the encrypted session is desynced
				// and cannot recover. Plaintext sessions are also
				// closed because a write failure means the wire is
				// gone.
				closeMelodySession(session)
			}
		} else {
			err := sendWSEncryptedResponse(session, cs, result.ID, result.Result)
			if err != nil {
				logWSWriteError(err, "error sending response")
				closeMelodySession(session)
			}
		}
		if result.AfterWrite != nil {
			result.AfterWrite()
		}
	}
}

// decryptIncomingFrame is the encryption decision point for WebSocket frames.
// It handles three cases:
//
//   - The session already has an encryption state attached → decrypt with it.
//   - The session has no state and the frame looks like an encrypted first
//     frame → establish a new session.
//   - The frame is plaintext → allowed only when encryption is disabled or
//     the connection is loopback.
//
// Returns (plaintext bytes to dispatch, the active client session if any, ok).
// On policy rejection or decryption failure, the WebSocket is closed and the
// caller should return immediately.
func decryptIncomingFrame(
	session *melody.Session,
	msg []byte,
	encGateway *apimiddleware.EncryptionGateway,
	encryptionEnabled bool,
	isLocal bool,
	sourceIP string,
) (plaintext []byte, cs *apimiddleware.ClientSession, ok bool) {
	// Already-established encrypted session: decrypt with the stored state.
	if cs = getClientSession(session); cs != nil {
		var frame apimiddleware.EncryptedFrame
		if err := json.Unmarshal(msg, &frame); err != nil || frame.Ciphertext == "" {
			log.Warn().Err(err).Msg("ws: malformed encrypted frame on established session")
			closeMelodySession(session)
			return nil, nil, false
		}
		pt, err := cs.DecryptSubsequent(frame)
		if err != nil {
			log.Warn().Err(err).Msg("ws: decryption failed on established session")
			closeMelodySession(session)
			return nil, nil, false
		}
		return pt, cs, true
	}

	// No session yet: detect whether this is an encrypted first frame.
	if apimiddleware.IsEncryptedFirstFrame(msg) {
		var frame apimiddleware.EncryptedFirstFrame
		if err := json.Unmarshal(msg, &frame); err != nil {
			log.Warn().Err(err).Msg("ws: malformed encrypted first frame")
			closeMelodySession(session)
			return nil, nil, false
		}
		if frame.Version != apimiddleware.EncryptionProtoVersion {
			data, marshalErr := unsupportedEncryptionVersionResponse()
			if marshalErr == nil {
				sendWSPlaintext(session, data)
			}
			closeMelodySession(session)
			return nil, nil, false
		}
		newSession, pt, err := encGateway.EstablishSession(frame, sourceIP)
		if err != nil {
			log.Warn().Err(err).Msg("ws: failed to establish encrypted session")
			closeMelodySession(session)
			return nil, nil, false
		}
		setClientSession(session, newSession)
		return pt, newSession, true
	}

	// Plaintext frame: only allowed when encryption is disabled, or from
	// loopback (localhost is always exempt so the TUI / local clients keep
	// working without pairing).
	if encryptionEnabled && !isLocal {
		data, marshalErr := encryptionRequiredErrorResponse()
		if marshalErr == nil {
			sendWSPlaintext(session, data)
		}
		closeMelodySession(session)
		return nil, nil, false
	}
	return msg, nil, true
}

// closeMelodySession best-effort closes a melody WebSocket session, logging
// any error at debug level (the connection may already be closed).
func closeMelodySession(session *melody.Session) {
	if err := session.Close(); err != nil {
		log.Debug().Err(err).Msg("ws: failed to close session")
	}
}

// writePong sends a "pong" heartbeat reply to the client. Plaintext
// sessions get the raw "pong" bytes; encrypted sessions go through
// SendEncryptedFrame so the encrypt + enqueue happens under the
// per-session mutex (preventing wire reorder against concurrent
// broadcasts).
//
// writeFn receives the bytes to emit on the wire. Production callers pass
// session.Write from a melody session; tests pass a capturing function to
// inspect the wire shape without mocking melody.
func writePong(writeFn func([]byte) error, cs *apimiddleware.ClientSession) error {
	if cs == nil {
		if err := writeFn([]byte("pong")); err != nil {
			return fmt.Errorf("write plaintext pong: %w", err)
		}
		return nil
	}
	if err := cs.SendEncryptedFrame([]byte("pong"), writeFn); err != nil {
		return fmt.Errorf("send encrypted pong: %w", err)
	}
	return nil
}

// sendWSEncryptedResponse marshals a JSON-RPC response and sends it over the
// WebSocket, encrypting it if the session has an established encryption
// session, otherwise sending it as plaintext. For encrypted sessions the
// encrypt + enqueue happens under the per-session mutex via
// SendEncryptedFrame so concurrent writers cannot reorder counters on the
// wire.
func sendWSEncryptedResponse(
	session *melody.Session,
	cs *apimiddleware.ClientSession,
	id models.RPCID,
	result any,
) error {
	if cs == nil {
		return sendWSResponse(session, id, result)
	}
	resp := models.ResponseObject{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if err := cs.SendEncryptedFrame(data, session.Write); err != nil {
		return fmt.Errorf("send encrypted response: %w", err)
	}
	return nil
}

// sendWSEncryptedError sends a JSON-RPC error, encrypted if the session is
// encrypted. For encrypted sessions the encrypt + enqueue happens under
// the per-session mutex via SendEncryptedFrame so concurrent writers
// cannot reorder counters on the wire.
func sendWSEncryptedError(
	session *melody.Session,
	cs *apimiddleware.ClientSession,
	id models.RPCID,
	rpcErr models.ErrorObject,
) error {
	if cs == nil {
		return sendWSError(session, id, rpcErr)
	}
	resp := models.ResponseErrorObject{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcErr,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal error response: %w", err)
	}
	if err := cs.SendEncryptedFrame(data, session.Write); err != nil {
		return fmt.Errorf("send encrypted error: %w", err)
	}
	return nil
}

func handlePostRequest(
	methodMap *MethodMap,
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	inTokenQueue chan<- tokens.Token,
	confirmQueue chan<- chan error,
	db *database.Database,
	limitsManager *playtime.LimitsManager,
	player audio.Player,
	indexPauser *syncutil.Pauser,
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
			var maxBytesErr *http.MaxBytesError
			switch {
			case errors.As(err, &maxBytesErr):
				log.Warn().Err(err).Msg("request body too large")
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
				log.Warn().Err(err).Msg("client disconnected during request body read")
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			default:
				log.Error().Err(err).Msg("failed to read request body")
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
			return
		}

		// Derive from r.Context() (already has APIRequestTimeout from middleware)
		// but also cancel on app shutdown via st.GetContext().
		reqCtx, reqCancel := context.WithCancel(r.Context())
		context.AfterFunc(st.GetContext(), reqCancel)
		defer reqCancel()

		env := requests.RequestEnv{
			Context:       reqCtx,
			Platform:      platform,
			Config:        cfg,
			State:         st,
			Database:      db,
			LimitsManager: limitsManager,
			LauncherCache: helpers.GlobalLauncherCache,
			Player:        player,
			TokenQueue:    inTokenQueue,
			ConfirmQueue:  confirmQueue,
			IndexPauser:   indexPauser,
			IsLocal:       apimiddleware.IsLoopbackAddr(r.RemoteAddr),
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
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if result.AfterWrite != nil {
			result.AfterWrite()
		}
	}
}

// Start starts the API web server and blocks until it shuts down.
func Start(
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	inTokenQueue chan<- tokens.Token,
	confirmQueue chan<- chan error,
	db *database.Database,
	limitsManager *playtime.LimitsManager,
	notifBroker *broker.Broker,
	mdnsHostname string,
	player audio.Player,
	indexPauser *syncutil.Pauser,
) error {
	return StartWithReady(
		platform, cfg, st, inTokenQueue, confirmQueue, db, limitsManager,
		notifBroker, mdnsHostname, player, indexPauser, nil,
	)
}

// StartWithReady starts the API web server and reports bind success or failure
// before blocking for shutdown. This lets service startup fail synchronously
// when the configured API port is unavailable.
func StartWithReady(
	platform platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	inTokenQueue chan<- tokens.Token,
	confirmQueue chan<- chan error,
	db *database.Database,
	limitsManager *playtime.LimitsManager,
	notifBroker *broker.Broker,
	mdnsHostname string,
	player audio.Player,
	indexPauser *syncutil.Pauser,
	ready chan<- error,
) error {
	notifyReady := func(err error) {
		if ready != nil {
			ready <- err
		}
	}

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

	// Pairing endpoints have a much more aggressive rate limit (1 req/sec
	// per IP) to throttle online PIN guessing attacks. The PAKE protocol
	// itself prevents offline attacks, but rate limiting is defense in
	// depth against the only feasible online attack: trying many PINs.
	pairingRateLimiter := apimiddleware.NewIPRateLimiterWithLimits(rate.Limit(1), 1)
	pairingRateLimiter.StartCleanup(st.GetContext())

	authConfig := apimiddleware.NewAuthConfig(config.GetAPIKeys)

	// Global middleware applied to all routes. IP filtering is applied
	// per-group: non-WS transports use NonWSIPFilterMiddleware
	// (deny-by-default for remote), while pairing, app, health, and
	// WebSocket routes remain remote-accessible.
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc: originValidator,
		AllowedMethods:  []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:  []string{"Accept", "Content-Type", "Authorization"},
		ExposedHeaders:  []string{},
	}))
	r.Use(privateNetworkAccessMiddleware)

	// Rate limiting only for API routes, not static assets
	apiRateLimitMiddleware := apimiddleware.HTTPRateLimitMiddleware(rateLimiter)
	nonWSIPFilter := apimiddleware.NonWSIPFilterMiddleware(cfg.AllowedIPs)

	if config.IsDevelopmentVersion() {
		r.Mount("/debug", middleware.Profiler())
		log.Info().Msg("pprof endpoints enabled at /debug/pprof/")
	}

	methodMap := NewMethodMap()

	// Construct the pairing manager and the encryption session manager.
	// Both have background cleanup goroutines tied to the service context.
	pairingMgr := NewPairingManager(db.UserDB, st.Notifications)
	pairingMgr.StartCleanup(st.GetContext())

	// Register pairing RPC methods. These close over the pairingMgr so
	// they must be added after it is created, not in NewMethodMap().
	if err := methodMap.AddMethod(models.MethodClientsPairStart,
		methods.HandleClientsPairStart(pairingMgr)); err != nil {
		log.Error().Err(err).Msg("error adding clients.pair.start method")
	}
	if err := methodMap.AddMethod(models.MethodClientsPairCancel,
		methods.HandleClientsPairCancel(pairingMgr)); err != nil {
		log.Error().Err(err).Msg("error adding clients.pair.cancel method")
	}

	encGateway := apimiddleware.NewEncryptionGateway(db.UserDB)
	encGateway.StartCleanup(st.GetContext())

	// LastSeen tracker batches paired-client activity in memory and
	// flushes to Clients.LastSeenAt every 30 seconds. A final flush runs
	// on graceful shutdown via StartFlushLoop's ctx.Done() branch.
	lastSeenTracker := apimiddleware.NewLastSeenTracker(db.UserDB)
	lastSeenDone := lastSeenTracker.StartFlushLoop(st.GetContext(), apimiddleware.DefaultLastSeenFlushInterval)
	defer func() {
		<-lastSeenDone
	}()

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
	// melody's Session.Write is a non-blocking enqueue onto a per-session
	// output channel (default size 256). When that channel fills, the
	// frame is silently dropped and Write still returns nil — for
	// encrypted sessions this would silently desync the send counter
	// against the wire and the client's next decrypt would fail GCM auth.
	// Force-close the underlying TCP connection so the session is torn
	// down promptly and the client reconnects cleanly instead of seeing
	// unexplained decryption errors.
	//
	// We bypass session.Close() here because it would enqueue a
	// CloseMessage envelope onto the same full output channel and re-enter
	// this errorHandler in an infinite recursion. Closing the underlying
	// conn directly causes writePump to fail on its next write, exit, and
	// run the normal session close path.
	session.HandleError(func(s *melody.Session, herr error) {
		if errors.Is(herr, melody.ErrMessageBufferFull) {
			cs := getClientSession(s)
			if cs != nil {
				tok := cs.AuthToken()
				if len(tok) > 8 {
					tok = tok[:8] + "..."
				}
				log.Warn().
					Str("auth_token", tok).
					Msg("ws: encrypted session output buffer full, force-closing to avoid counter desync")
			} else {
				log.Warn().Msg("ws: plaintext session output buffer full, force-closing")
			}
			if conn := s.WebsocketConnection(); conn != nil {
				if cerr := conn.Close(); cerr != nil {
					log.Debug().Err(cerr).Msg("ws: error force-closing overflowed session")
				}
			}
		}
	})
	wsNotifications, wsSubID := notifBroker.Subscribe(100)
	go func() {
		broadcastNotifications(st, session, wsNotifications)
		notifBroker.Unsubscribe(wsSubID)
	}()

	// Pairing endpoints — accessible to remote clients (rate-limited only).
	// Unencrypted by design: PAKE establishes the shared key without ever
	// transmitting it.
	//
	// Both limiters are stacked: the general apiRateLimitMiddleware runs
	// first (cheap check, shared budget) and the stricter pairingRateMiddleware
	// (1 req/sec per IP) runs second. Stacking prevents a client from
	// pairing while simultaneously scraping other endpoints from the same
	// IP, and provides defense in depth if pairingRateLimiter is ever
	// misconfigured.
	pairingRateMiddleware := apimiddleware.HTTPRateLimitMiddleware(pairingRateLimiter)
	r.Group(func(r chi.Router) {
		r.Use(apiRateLimitMiddleware)
		r.Use(pairingRateMiddleware)
		r.Use(middleware.NoCache)
		r.Use(middleware.Timeout(config.APIRequestTimeout))
		r.Post("/api/pair/start", pairingMgr.HandlePairStart())
		r.Post("/api/pair/finish", pairingMgr.HandlePairFinish())
	})

	// WebSocket handler. When encryption is disabled, API key auth is
	// enforced at upgrade time. When encryption is enabled, auth is
	// deferred to the first encrypted frame — successful first-frame
	// decryption proves the client holds a valid pairing key. Localhost is
	// always exempt.
	wsHandler := func(w http.ResponseWriter, r *http.Request, version string) {
		if !cfg.EncryptionEnabled() {
			if !apimiddleware.WebSocketAuthHandler(authConfig, r) {
				http.Error(w, "Unauthorized: API key required", http.StatusUnauthorized)
				return
			}
		}
		err := session.HandleRequest(w, r)
		if err != nil {
			log.Warn().Err(err).Str("version", version).Msg("websocket upgrade failed")
		}
	}

	// WebSocket routes — open to remote clients regardless of AllowedIPs.
	// Encryption (when enabled) or API key auth (when disabled) is the
	// security mechanism here.
	r.Group(func(r chi.Router) {
		r.Use(apiRateLimitMiddleware)
		r.Use(middleware.NoCache)
		r.Use(middleware.Timeout(config.APIRequestTimeout))

		r.Get("/api", func(w http.ResponseWriter, r *http.Request) {
			wsHandler(w, r, "latest")
		})
		r.Get("/api/v0", func(w http.ResponseWriter, r *http.Request) {
			wsHandler(w, r, "v0")
		})
		r.Get("/api/v0.1", func(w http.ResponseWriter, r *http.Request) {
			wsHandler(w, r, "v0.1")
		})
	})

	// Non-WebSocket API routes (HTTP POST + REST GET) — restricted to
	// localhost by default; remote access requires explicit AllowedIPs.
	// These transports do not support encryption; the IP allowlist plus
	// API key auth are the security boundary.
	r.Group(func(r chi.Router) {
		r.Use(nonWSIPFilter)
		r.Use(apimiddleware.HTTPAuthMiddleware(authConfig))
		r.Use(apiRateLimitMiddleware)
		r.Use(middleware.NoCache)
		r.Use(middleware.Timeout(config.APIRequestTimeout))

		postHandler := handlePostRequest(
			methodMap, platform, cfg, st,
			inTokenQueue, confirmQueue,
			db, limitsManager, player,
			indexPauser,
		)
		r.Post("/api", postHandler)
		r.Post("/api/v0", postHandler)
		r.Post("/api/v0.1", postHandler)
	})

	// REST run endpoints — allow_run bypasses IP filter when configured,
	// since the handler validates content against the allow_run patterns.
	runIPFilter := apimiddleware.RunIPFilterMiddleware(cfg.AllowedIPs, cfg.HasAllowRun)
	r.Group(func(r chi.Router) {
		r.Use(runIPFilter)
		r.Use(apimiddleware.HTTPAuthMiddleware(authConfig))
		r.Use(apiRateLimitMiddleware)
		r.Use(middleware.NoCache)
		r.Use(middleware.Timeout(config.APIRequestTimeout))

		runHandler := methods.HandleRunRest(cfg, st, inTokenQueue)
		r.Get("/l/*", runHandler) // DEPRECATED
		r.Get("/r/*", runHandler)
		r.Get("/run/*", runHandler)
	})

	// SSE routes (long-lived connections, no request timeout). Same
	// localhost-by-default policy as the non-WS API routes.
	r.Group(func(r chi.Router) {
		r.Use(nonWSIPFilter)
		r.Use(apimiddleware.HTTPAuthMiddleware(authConfig))
		r.Use(apiRateLimitMiddleware)
		r.Use(middleware.NoCache)

		sseHandler := handleSSE(notifBroker, st)
		r.Get("/api/events", sseHandler)
		r.Get("/api/v0/events", sseHandler)
		r.Get("/api/v0.1/events", sseHandler)
	})

	session.HandleMessage(apimiddleware.WebSocketRateLimitHandler(
		rateLimiter,
		handleWSMessage(
			methodMap, platform, cfg, st, inTokenQueue, confirmQueue,
			db, limitsManager, player, indexPauser, encGateway,
			lastSeenTracker,
		),
	))

	// Static app assets
	r.Get("/app/*", handleApp)
	r.Get("/app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})

	// /health is intentionally remote-accessible with no IP filter, no
	// API key auth, and no rate limiting (only the global Recoverer/CORS
	// middleware applies) so load balancers, uptime checks, and the
	// app's discovery flow can reach it without credentials. The plain
	// "OK" response intentionally leaks no information beyond liveness.
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

	log.Info().Str("listen", cfg.APIListen()).Msg("starting HTTP server")
	log.Debug().Msg("HTTP server attempting to bind")

	// Create the listener before reporting startup success so callers can fail
	// fast when the configured API port is already in use.
	lc := &net.ListenConfig{}
	listener, err := lc.Listen(st.GetContext(), "tcp", server.Addr)
	if err != nil {
		bindErr := fmt.Errorf("failed to bind API listener: %w", err)
		log.Error().Err(bindErr).Msg("failed to bind to port")
		notifyReady(bindErr)
		st.StopService()
		return bindErr
	}

	// If port 0 was requested, update config with the actual bound port
	// so callers can discover which port the server is listening on.
	if port == 0 {
		if addr, ok := listener.Addr().(*net.TCPAddr); ok {
			_ = cfg.SetAPIPort(addr.Port)
		}
	}

	log.Debug().Msg("HTTP server bound to port, ready to accept connections")
	notifyReady(nil)

	go func() {
		// Start serving
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("HTTP server error")
			serverDone <- err
		} else {
			log.Debug().Msg("HTTP server stopped normally")
			serverDone <- nil
		}
	}()

	log.Debug().Msg("HTTP server goroutine launched")

	select {
	case <-st.GetContext().Done():
		log.Info().Msg("initiating HTTP server graceful shutdown")
	case err := <-serverDone:
		if err != nil {
			log.Error().Err(err).Msg("API server failed during operation, stopping service")
			st.StopService()
			return err
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
		return fmt.Errorf("HTTP server shutdown error: %w", err)
	}

	log.Info().Msg("HTTP server shutdown complete")
	return nil
}
