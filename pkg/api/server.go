package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/state"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/olahol/melody"
	"github.com/rs/zerolog/log"
)

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

func isValidMethodName(name string) bool {
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r == '.') {
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
		return fmt.Errorf("method name cannot be empty")
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
	return fn.(func(requests.RequestEnv) (any, error)), true
}

func (m *MethodMap) ListMethods() []string {
	var ms []string
	m.Range(func(key, value interface{}) bool {
		ms = append(ms, key.(string))
		return true
	})
	return ms
}

func NewMethodMap() *MethodMap {
	var m MethodMap

	defaultMethods := map[string]func(requests.RequestEnv) (any, error){
		// run
		models.MethodLaunch:    methods.HandleRun,
		models.MethodRun:       methods.HandleRun,
		models.MethodRunScript: methods.HandleRunScript,
		models.MethodStop:      methods.HandleStop,
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
		models.MethodSettings:       methods.HandleSettings,
		models.MethodSettingsUpdate: methods.HandleSettingsUpdate,
		models.MethodSettingsReload: methods.HandleSettingsReload,
		// systems
		models.MethodSystems: methods.HandleSystems,
		// mappings
		models.MethodMappings:       methods.HandleMappings,
		models.MethodMappingsNew:    methods.HandleAddMapping,
		models.MethodMappingsDelete: methods.HandleDeleteMapping,
		models.MethodMappingsUpdate: methods.HandleUpdateMapping,
		models.MethodMappingsReload: methods.HandleReloadMappings,
		// readers
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
func handleRequest(methodMap *MethodMap, env requests.RequestEnv, req models.RequestObject) (any, *models.ErrorObject) {
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

	return session.Write(data)
}

// sendWSError sends a JSON-RPC error object response to the client.
func sendWSError(session *melody.Session, id uuid.UUID, error models.ErrorObject) error {
	log.Debug().Int("code", error.Code).Str("message", error.Message).Msg("sending error")

	resp := models.ResponseErrorObject{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &error,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("error marshalling error response: %w", err)
	}

	return session.Write(data)
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
				index, err := root.Open("index.html")
				if err != nil {
					log.Error().Err(err).Msg("error opening index.html")
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				http.ServeContent(w, r, "index.html", time.Now(), index)
				return
			} else {
				log.Error().Err(err).Str("path", r.URL.Path).Msg("error opening file")
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
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

// broadcastNotifications consumes and broadcasts all incoming API
// notifications to all connected clients.
func broadcastNotifications(
	state *state.State,
	session *melody.Melody,
	notifications <-chan models.Notification,
) {
	for {
		select {
		case <-state.GetContext().Done():
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

			// TODO: this will not work with encryption
			err = session.Broadcast(data)
			if err != nil {
				log.Error().Err(err).Msg("broadcasting notification")
			}
		}
	}
}

func processRequestObject(
	methodMap *MethodMap,
	env requests.RequestEnv,
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
		} else {
			return *req.ID, resp, nil
		}
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
	state *state.State,
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
		if bytes.Compare(msg, []byte("ping")) == 0 {
			err := session.Write([]byte("pong"))
			if err != nil {
				log.Error().Err(err).Msg("sending pong")
			}
			return
		}

		rawIp := strings.SplitN(session.Request.RemoteAddr, ":", 2)
		clientIp := net.ParseIP(rawIp[0])
		env := requests.RequestEnv{
			Platform:   platform,
			Config:     cfg,
			State:      state,
			Database:   db,
			TokenQueue: inTokenQueue,
			IsLocal:    clientIp.IsLoopback(),
		}

		id, resp, rpcError := processRequestObject(methodMap, env, msg)
		if rpcError != nil {
			err := sendWSError(session, id, *rpcError)
			if err != nil {
				log.Error().Err(err).Msg("error sending error response")
			}
		} else {
			err := sendWSResponse(session, id, resp)
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
	state *state.State,
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

		rawIp := strings.SplitN(r.RemoteAddr, ":", 2)
		clientIp := net.ParseIP(rawIp[0])
		env := requests.RequestEnv{
			Platform:   platform,
			Config:     cfg,
			State:      state,
			Database:   db,
			TokenQueue: inTokenQueue,
			IsLocal:    clientIp.IsLoopback(),
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
	state *state.State,
	inTokenQueue chan<- tokens.Token,
	db *database.Database,
	notifications <-chan models.Notification,
) {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.NoCache)
	r.Use(middleware.Timeout(config.ApiRequestTimeout))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"https://*", "http://*", "capacitor://*"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Accept"},
		ExposedHeaders: []string{},
	}))

	if strings.HasSuffix(config.AppVersion, "-dev") {
		r.Mount("/debug", middleware.Profiler())
	}

	methodMap := NewMethodMap()

	session := melody.New()
	session.Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	go broadcastNotifications(state, session, notifications)

	r.Get("/api", func(w http.ResponseWriter, r *http.Request) {
		err := session.HandleRequest(w, r)
		if err != nil {
			log.Error().Err(err).Msg("handling websocket request: latest")
		}
	})
	r.Post("/api", handlePostRequest(methodMap, platform, cfg, state, inTokenQueue, db))

	r.Get("/api/v0", func(w http.ResponseWriter, r *http.Request) {
		err := session.HandleRequest(w, r)
		if err != nil {
			log.Error().Err(err).Msg("handling websocket request: v0")
		}
	})
	r.Post("/api/v0", handlePostRequest(methodMap, platform, cfg, state, inTokenQueue, db))

	r.Get("/api/v0.1", func(w http.ResponseWriter, r *http.Request) {
		err := session.HandleRequest(w, r)
		if err != nil {
			log.Error().Err(err).Msg("handling websocket request: v0.1")
		}
	})
	r.Post("/api/v0.1", handlePostRequest(methodMap, platform, cfg, state, inTokenQueue, db))

	session.HandleMessage(handleWSMessage(methodMap, platform, cfg, state, inTokenQueue, db))

	r.Get("/l/*", methods.HandleRunRest(cfg, state, inTokenQueue)) // DEPRECATED
	r.Get("/r/*", methods.HandleRunRest(cfg, state, inTokenQueue))
	r.Get("/run/*", methods.HandleRunRest(cfg, state, inTokenQueue))

	r.Get("/app/*", handleApp)
	r.Get("/app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})

	err := http.ListenAndServe(":"+strconv.Itoa(cfg.ApiPort()), r)
	if err != nil {
		log.Error().Err(err).Msg("error starting http server")
	}
}
