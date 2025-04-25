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
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"

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
var JSONRPCErrorServerError = models.ErrorObject{
	Code:    -32000,
	Message: "Server error",
}

func maybeUUID(req models.RequestObject) uuid.UUID {
	if req.Id == nil {
		return uuid.Nil
	} else {
		return *req.Id
	}
}

var methodMap = map[string]func(requests.RequestEnv) (any, error){
	// run
	models.MethodLaunch:    methods.HandleRun, // DEPRECATED
	models.MethodRun:       methods.HandleRun,
	models.MethodRunScript: methods.HandleRunScript,
	models.MethodStop:      methods.HandleStop,
	// tokens
	models.MethodTokens:  methods.HandleTokens,
	models.MethodHistory: methods.HandleHistory,
	// media
	models.MethodMedia:       methods.HandleMedia,
	models.MethodMediaIndex:  methods.HandleIndexMedia,
	models.MethodMediaSearch: methods.HandleGames,
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
	models.MethodReadersWrite: methods.HandleReaderWrite,
	// utils
	models.MethodVersion: methods.HandleVersion,
}

func handleRequest(env requests.RequestEnv, req models.RequestObject) (any, error) {
	log.Debug().Interface("request", req).Msg("received request")

	fn, ok := methodMap[strings.ToLower(req.Method)]
	if !ok {
		return nil, fmt.Errorf("unknown method: %s", req.Method)
	}

	if req.Id == nil {
		return nil, fmt.Errorf("missing ID for request: %s", req.Method)
	}

	var params []byte
	if req.Params != nil {
		var err error
		// double unmarshal to use json decode on params later
		params, err = json.Marshal(req.Params)
		if err != nil {
			return nil, fmt.Errorf("error parsing params: %w", err)
		}
	}

	env.Id = *req.Id
	env.Params = params

	return fn(env)
}

func sendResponse(session *melody.Session, id uuid.UUID, result any) error {
	log.Debug().Interface("result", result).Msg("sending response")

	resp := models.ResponseObject{
		JsonRpc: "2.0",
		ID:      id,
		Result:  result,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("error marshalling response: %w", err)
	}

	return session.Write(data)
}

func sendError(session *melody.Session, id uuid.UUID, error models.ErrorObject) error {
	log.Debug().Int("code", error.Code).Str("message", error.Message).Msg("sending error")

	resp := models.ResponseObject{
		JsonRpc: "2.0",
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

func handleApp(w http.ResponseWriter, r *http.Request) {
	appFs, err := fs.Sub(assets.App, "_app/dist")
	if err != nil {
		log.Error().Err(err).Msg("error opening app dist")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.StripPrefix("/app", http.FileServer(http.FS(appFs))).ServeHTTP(w, r)
}

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
				JsonRpc: "2.0",
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

func handleWSMessage(
	platform platforms.Platform,
	cfg *config.Instance,
	state *state.State,
	inTokenQueue chan<- tokens.Token,
	db *database.Database,
) func(
	session *melody.Session,
	msg []byte,
) {
	return func(
		session *melody.Session,
		msg []byte,
	) {
		// ping command for heartbeat operation
		if bytes.Compare(msg, []byte("ping")) == 0 {
			err := session.Write([]byte("pong"))
			if err != nil {
				log.Error().Err(err).Msg("sending pong")
			}
			return
		}

		if !json.Valid(msg) {
			log.Error().Msg("data not valid json")
			err := sendError(session, uuid.Nil, JSONRPCErrorParseError)
			if err != nil {
				log.Error().Err(err).Msg("error sending error response")
				return
			}
			return
		}

		// try parse a request first, which has a method field
		var req models.RequestObject
		err := json.Unmarshal(msg, &req)

		if err == nil && req.JsonRpc != "2.0" {
			log.Error().Str("jsonrpc", req.JsonRpc).Msg("unsupported payload version")
			err := sendError(session, maybeUUID(req), JSONRPCErrorInvalidRequest)
			if err != nil {
				log.Error().Err(err).Msg("error sending error response")
			}
			return
		}

		if err == nil && req.Method != "" {
			if req.Id == nil {
				// request is notification
				log.Info().Interface("req", req).Msg("received notification, ignoring")
				return
			}

			rawIp := strings.SplitN(session.Request.RemoteAddr, ":", 2)
			clientIp := net.ParseIP(rawIp[0])

			resp, err := handleRequest(requests.RequestEnv{
				Platform:   platform,
				Config:     cfg,
				State:      state,
				Database:   db,
				TokenQueue: inTokenQueue,
				IsLocal:    clientIp.IsLoopback(),
			}, req)
			if err != nil {
				// TODO: handlers should return their own error object
				err := sendError(session, *req.Id, JSONRPCErrorServerError)
				if err != nil {
					log.Error().Err(err).Msg("error sending error response")
				}
				return
			}

			err = sendResponse(session, *req.Id, resp)
			if err != nil {
				log.Error().Err(err).Msg("error sending response")
			}
		}

		// otherwise try parse a response, which has an id field
		var resp models.ResponseObject
		err = json.Unmarshal(msg, &resp)
		if err == nil && resp.ID != uuid.Nil {
			err := handleResponse(resp)
			if err != nil {
				log.Error().Err(err).Msg("error handling response")
			}
			return
		}

		log.Error().Err(err).Msg("message does not match known types")
		err = sendError(session, uuid.Nil, JSONRPCErrorInvalidRequest)
		if err != nil {
			log.Error().Err(err).Msg("error sending error response")
		}
		return
	}
}

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

	session := melody.New()
	session.Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
	go broadcastNotifications(state, session, notifications)

	r.Get("/api", func(w http.ResponseWriter, r *http.Request) {
		err := session.HandleRequest(w, r)
		if err != nil {
			log.Error().Err(err).Msg("handling websocket request: latest")
		}
	})

	r.Get("/api/v0", func(w http.ResponseWriter, r *http.Request) {
		err := session.HandleRequest(w, r)
		if err != nil {
			log.Error().Err(err).Msg("handling websocket request: v0")
		}
	})

	r.Get("/api/v0.1", func(w http.ResponseWriter, r *http.Request) {
		err := session.HandleRequest(w, r)
		if err != nil {
			log.Error().Err(err).Msg("handling websocket request: v0.1")
		}
	})

	session.HandleMessage(handleWSMessage(platform, cfg, state, inTokenQueue, db))

	r.Get("/l/*", methods.HandleRunRest(cfg, state, inTokenQueue)) // DEPRECATED
	r.Get("/r/*", methods.HandleRunRest(cfg, state, inTokenQueue))
	r.Get("/run/*", methods.HandleRunRest(cfg, state, inTokenQueue))
	r.Get("/select-item/*", methods.HandleItemSelect(cfg, state, inTokenQueue))
	r.Get("/selected-item", methods.HandleSelectedItem(cfg, state, inTokenQueue))
	r.Get("/app/*", handleApp)
	r.Get("/app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})

	err := http.ListenAndServe(":"+strconv.Itoa(cfg.ApiPort()), r)
	if err != nil {
		log.Error().Err(err).Msg("error starting http server")
	}
}
