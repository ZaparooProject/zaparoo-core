package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/methods"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"net"
	"net/http"
	"strconv"
	"strings"
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

// TODO: can we safely allow launch basic unrestricted for local accounts?
// TODO: should api launches from localhost require allowlist?

const RequestTimeout = 30 * time.Second

var methodMap = map[string]func(requests.RequestEnv) (any, error){
	// running
	models.MethodLaunch: methods.HandleRun, // DEPRECATED
	models.MethodRun:    methods.HandleRun,
	models.MethodStop:   methods.HandleStop,
	// media
	models.MethodMedia:       methods.HandleMedia,
	models.MethodMediaIndex:  methods.HandleIndexMedia,
	models.MethodMediaSearch: methods.HandleGames,
	// settings
	models.MethodSettings:       methods.HandleSettings,
	models.MethodSettingsUpdate: methods.HandleSettingsUpdate,
	// systems
	models.MethodSystems: methods.HandleSystems,
	// history
	models.MethodHistory: methods.HandleHistory,
	// mappings
	models.MethodMappings:       methods.HandleMappings,
	models.MethodMappingsNew:    methods.HandleAddMapping,
	models.MethodMappingsDelete: methods.HandleDeleteMapping,
	models.MethodMappingsUpdate: methods.HandleUpdateMapping,
	models.MethodMappingsReload: methods.HandleReloadMappings,
	// readers
	models.MethodReadersWrite: methods.HandleReaderWrite,
	// utils
	models.MethodStatus:  methods.HandleStatus, // TODO: remove, convert to individual methods
	models.MethodVersion: methods.HandleVersion,
}

func handleRequest(env requests.RequestEnv, req models.RequestObject) (any, error) {
	log.Debug().Interface("request", req).Msg("received request")

	fn, ok := methodMap[req.Method]
	if !ok {
		return nil, errors.New("unknown method")
	}

	if req.Id == nil {
		return nil, errors.New("missing request id")
	}

	var params []byte
	if req.Params != nil {
		var err error
		// double unmarshal to use json decode on params later
		params, err = json.Marshal(req.Params)
		if err != nil {
			return nil, err
		}
	}

	env.Id = *req.Id
	env.Params = params

	return fn(env)
}

func sendResponse(s *melody.Session, id uuid.UUID, result any) error {
	log.Debug().Interface("result", result).Msg("sending response")

	resp := models.ResponseObject{
		JsonRpc: "2.0",
		Id:      id,
		Result:  result,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	return s.Write(data)
}

func sendError(s *melody.Session, id uuid.UUID, code int, message string) error {
	log.Debug().Int("code", code).Str("message", message).Msg("sending error")

	resp := models.ResponseObject{
		JsonRpc: "2.0",
		Id:      id,
		Error: &models.ErrorObject{
			Code:    code,
			Message: message,
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	return s.Write(data)
}

func handleResponse(resp models.ResponseObject) error {
	log.Debug().Interface("response", resp).Msg("received response")
	return nil
}

func Start(
	pl platforms.Platform,
	cfg *config.Instance,
	st *state.State,
	itq chan<- tokens.Token,
	db *database.Database,
	ns <-chan models.Notification,
) {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.NoCache)
	r.Use(middleware.Timeout(RequestTimeout))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"https://*", "http://*", "capacitor://*"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Accept"},
		ExposedHeaders: []string{},
	}))

	m := melody.New()
	m.Upgrader.CheckOrigin = func(r *http.Request) bool { return true }

	// consume and broadcast notifications
	go func(ns <-chan models.Notification) {
		for !st.ShouldStopService() {
			select {
			case n := <-ns:
				ro := models.RequestObject{
					JsonRpc: "2.0",
					Method:  n.Method,
					Params:  n.Params,
				}

				data, err := json.Marshal(ro)
				if err != nil {
					log.Error().Err(err).Msg("marshalling notification request")
					continue
				}

				// TODO: this will not work with encryption
				err = m.Broadcast(data)
				if err != nil {
					log.Error().Err(err).Msg("broadcasting notification")
				}
			case <-time.After(500 * time.Millisecond):
				// TODO: better to wait on a stop channel?
				continue
			}
		}
	}(ns)

	r.Get("/api", func(w http.ResponseWriter, r *http.Request) {
		err := m.HandleRequest(w, r)
		if err != nil {
			log.Error().Err(err).Msg("handling websocket request: latest")
		}
	})

	r.Get("/api/v0", func(w http.ResponseWriter, r *http.Request) {
		err := m.HandleRequest(w, r)
		if err != nil {
			log.Error().Err(err).Msg("handling websocket request: v0")
		}
	})

	r.Get("/api/v0.1", func(w http.ResponseWriter, r *http.Request) {
		err := m.HandleRequest(w, r)
		if err != nil {
			log.Error().Err(err).Msg("handling websocket request: v0.1")
		}
	})

	m.HandleMessage(func(s *melody.Session, msg []byte) {
		// ping command for heartbeat operation
		if bytes.Compare(msg, []byte("ping")) == 0 {
			err := s.Write([]byte("pong"))
			if err != nil {
				log.Error().Err(err).Msg("sending pong")
			}
			return
		}

		if !json.Valid(msg) {
			// TODO: send error response
			log.Error().Msg("data not valid json")
			return
		}

		// try parse a request first, which has a method field
		var req models.RequestObject
		err := json.Unmarshal(msg, &req)

		if err == nil && req.JsonRpc != "2.0" {
			log.Error().Str("jsonrpc", req.JsonRpc).Msg("unsupported payload version")
			// TODO: send error
			return
		}

		if err == nil && req.Method != "" {
			if req.Id == nil {
				// request is notification
				log.Info().Interface("req", req).Msg("received notification, ignoring")
				return
			}

			rawIp := strings.SplitN(s.Request.RemoteAddr, ":", 2)
			clientIp := net.ParseIP(rawIp[0])
			log.Debug().IPAddr("ip", clientIp).Msg("parsed ip")

			resp, err := handleRequest(requests.RequestEnv{
				Platform:   pl,
				Config:     cfg,
				State:      st,
				Database:   db,
				TokenQueue: itq,
				IsLocal:    clientIp.IsLoopback(),
			}, req)
			if err != nil {
				err := sendError(s, *req.Id, 1, err.Error())
				if err != nil {
					log.Error().Err(err).Msg("error sending error response")
				}
				return
			}

			err = sendResponse(s, *req.Id, resp)
			if err != nil {
				log.Error().Err(err).Msg("error sending response")
			}
		}

		// otherwise try parse a response, which has an id field
		var resp models.ResponseObject
		err = json.Unmarshal(msg, &resp)
		if err == nil && resp.Id != uuid.Nil {
			err := handleResponse(resp)
			if err != nil {
				log.Error().Err(err).Msg("error handling response")
			}
			return
		}

		// TODO: send error
		log.Error().Err(err).Msg("message does not match known types")
	})

	r.Get("/l/*", methods.HandleRunRest(cfg, st, itq)) // DEPRECATED
	r.Get("/r/*", methods.HandleRunRest(cfg, st, itq))
	r.Get("/run/*", methods.HandleRunRest(cfg, st, itq))

	err := http.ListenAndServe(":"+strconv.Itoa(cfg.ApiPort()), r)
	if err != nil {
		log.Error().Err(err).Msg("error starting http server")
	}
}
