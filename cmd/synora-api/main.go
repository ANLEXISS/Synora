package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	webapi "synora/internal/api"
	"synora/internal/bus"
	"synora/internal/coreclient"
	"synora/internal/security"
	"synora/pkg/contract"
)

type healthResponse struct {
	Service   string    `json:"service"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

type snapshotProvider interface {
	Snapshot() (*contract.PublicSnapshot, error)
}

type stateProvider interface {
	State() (*contract.PublicSnapshot, error)
}

type systemHealthProvider interface {
	SystemHealth() (*contract.RuntimeHealth, error)
}

type validationProvider interface {
	Validations() ([]contract.ValidationRequest, error)
	ResolveValidation(string, json.RawMessage) (*contract.ValidationRequest, error)
}

type pairingProvider interface {
	StartPairing() (*security.PairingStartResponse, error)
	CompletePairing(json.RawMessage) (*security.PairingCompleteResponse, error)
}

func main() {

	addr := ":8080"
	securityPath := getenv("SYNORA_SECURITY", security.DefaultPath)
	securityConfig, err := security.Load(securityPath)
	if err != nil {
		log.Fatal(err)
	}
	if securityConfig.APITokenHash == "" {
		log.Fatal("security config requires api_token_hash or api_token")
	}

	busClient, err := bus.NewClient(
		getenv("SYNORA_BUS", "/run/synora/bus.sock"),
		"api",
	)

	if err != nil {
		log.Fatal(err)
	}

	core := coreclient.New(busClient)
	wsHub := newWebSocketHub(core)
	go wsHub.observeBus(busClient)
	simulationRunner := newSimulationRunner(busClient, wsHub)
	webEnabled := getenvBool("SYNORA_WEB_ENABLED", true)
	webRoot := getenv("SYNORA_WEB_ROOT", "/var/lib/synora/web")
	webServer := &webapi.Server{
		WebEnabled: webEnabled,
		WebRoot:    webRoot,
	}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/state", handleState(core))
	apiMux.HandleFunc("/api/simulation/scenarios", handleSimulationScenarios())
	apiMux.HandleFunc("/api/simulation/run", handleSimulationRun(simulationRunner))
	apiMux.HandleFunc("/api/simulation/runs/", handleSimulationRunStatus(simulationRunner))
	apiMux.HandleFunc("/api/cge/summary", handleCGESummary(core))
	apiMux.HandleFunc("/api/cge/sequences", handleCGESequences(core))
	apiMux.HandleFunc("/api/cge/transitions", handleCGETransitions(core))
	apiMux.HandleFunc("/api/cge/learned-behaviors", handleCGELearnedBehaviors(core))
	apiMux.HandleFunc("/api/cge/critical-seeds", handleCGECriticalSeeds(core))
	apiMux.HandleFunc("/api/cge/critical-seeds/", handleCGECriticalSeed(core))
	apiMux.HandleFunc("/api/cge/danger-assessments", handleCGEDangerAssessments(core))
	apiMux.HandleFunc("/api/cge/danger-assessments/", handleCGEDangerAssessment(core))
	apiMux.HandleFunc("/api/cge/", handleCGEDetail(core))
	apiMux.HandleFunc("/api/validations", handleValidationCollection(core))
	apiMux.HandleFunc("/api/validations/", handleValidationItem(core))
	apiMux.HandleFunc("/api/devices", handleDeviceCollection(core))
	apiMux.HandleFunc("/api/devices/pairing/start", handlePairingStart(core))
	apiMux.HandleFunc("/api/devices/pairing/complete", handlePairingComplete(core))
	apiMux.HandleFunc("/api/devices/", handleDeviceItem(core))
	apiMux.HandleFunc("/api/residents", handleResidentCollection(core))
	apiMux.HandleFunc("/api/residents/", handleResidentItem(core))
	apiMux.HandleFunc("/api/automations", handleAutomationCollection(core))
	apiMux.HandleFunc("/api/automations/catalog", handleAutomationCatalog(core))
	apiMux.HandleFunc("/api/automations/", handleAutomationItem(core))
	apiMux.HandleFunc("/api/topology", handleTopologyConfiguration(core))
	apiMux.HandleFunc("/api/topology/", handleTopologySubroute())
	apiMux.HandleFunc("/api/system/health", handleSystemHealth(core))
	apiMux.HandleFunc("/api/snapshot", handleSnapshot(core))

	server := &http.Server{
		Addr:              addr,
		Handler:           buildServerHandler(securityConfig, apiMux, wsHub, webEnabled, webServer),
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("synora-api listening on", addr)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeRouteNotFound(w, "API")
		return
	}

	response := map[string]any{
		"service": "synora-api",
		"status":  "running",
		"message": "Synora API online",
	}

	writeJSON(w, http.StatusOK, response)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {

	response := healthResponse{
		Service:   "synora-api",
		Status:    "ok",
		Timestamp: time.Now().UTC(),
	}

	writeJSON(w, http.StatusOK, response)
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	payload any,
) {

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Println("json encode error:", err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {

	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		started := time.Now()

		next.ServeHTTP(w, r)

		log.Printf(
			"%s %s %s",
			r.Method,
			r.URL.Path,
			time.Since(started),
		)
	})
}

func corsMiddleware(cfg *security.Config, next http.Handler) http.Handler {

	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		origin := r.Header.Get("Origin")
		if cfg != nil && cfg.AllowsOrigin(origin) {
			if origin == "" {
				origin = "*"
			}
			w.Header().Set(
				"Access-Control-Allow-Origin",
				origin,
			)
			w.Header().Add("Vary", "Origin")
		}

		w.Header().Set(
			"Access-Control-Allow-Headers",
			"Content-Type, Authorization",
		)

		w.Header().Set(
			"Access-Control-Allow-Methods",
			"GET, POST, PATCH, PUT, DELETE, OPTIONS",
		)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func apiAuthMiddleware(cfg *security.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if r.URL.Path == "/api/system/health" && cfg != nil && cfg.PublicSystemHealth {
			next.ServeHTTP(w, r)
			return
		}

		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok && (r.URL.Path == "/api/ws" || r.URL.Path == "/ws") {
			token = strings.TrimSpace(r.URL.Query().Get("token"))
			ok = token != ""
		}
		if !ok || cfg == nil || !cfg.VerifyAPIToken(token) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func buildServerHandler(
	cfg *security.Config,
	apiMux http.Handler,
	wsHub http.Handler,
	webEnabled bool,
	webServer *webapi.Server,
) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", apiAuthMiddleware(cfg, apiMux))
	if wsHub != nil {
		mux.Handle("/api/ws", apiAuthMiddleware(cfg, wsHub))
		mux.Handle("/ws", apiAuthMiddleware(cfg, wsHub))
	}
	mux.HandleFunc("/health", handleHealth)
	if webEnabled && webServer != nil {
		mux.Handle("/", webServer.WebHandler())
	} else {
		mux.HandleFunc("/", handleIndex)
	}
	return loggingMiddleware(corsMiddleware(cfg, mux))
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func handleSnapshot(
	core snapshotProvider,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		snapshot, err := core.Snapshot()

		if err != nil {

			writeError(w, err)
			return
		}

		writeJSON(
			w,
			http.StatusOK,
			snapshot,
		)
	}
}

func handleState(
	core stateProvider,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		state, err := core.State()

		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, state)
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getenvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func handleDevices(
	core *coreclient.Client,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		devices, err := core.Devices()

		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, devices)
	}
}

func handleTopology(
	core *coreclient.Client,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		topology, err := core.Topology()

		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, topology)
	}
}

func handleSystemHealth(
	core systemHealthProvider,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		health, err := core.SystemHealth()

		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, health)
	}
}

func handleValidations(
	core validationProvider,
) http.HandlerFunc {
	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		validations, err := core.Validations()
		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, validations)
	}
}

func handleValidation(
	core validationProvider,
) http.HandlerFunc {
	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		path := strings.TrimPrefix(r.URL.Path, "/api/validations/")
		id, actionPath, ok := strings.Cut(path, "/")
		id = strings.TrimSpace(id)
		if id == "" || !ok || actionPath != "resolve" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "validation route not found"})
			return
		}
		if !requireMethod(w, r, http.MethodPost) {
			return
		}

		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}

		validation, err := core.ResolveValidation(id, body)
		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, validation)
	}
}

func handlePairingStart(
	core pairingProvider,
) http.HandlerFunc {
	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}

		response, err := core.StartPairing()
		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func handlePairingComplete(
	core pairingProvider,
) http.HandlerFunc {
	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}

		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}

		response, err := core.CompletePairing(body)
		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func handleDevice(
	core *coreclient.Client,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		id := strings.TrimPrefix(r.URL.Path, "/api/devices/")
		id = strings.TrimSpace(id)
		if id == "" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "device id required"})
			return
		}

		switch r.Method {
		case http.MethodPatch:
			body, ok := readJSONObject(w, r, true)
			if !ok {
				return
			}

			devices, err := core.UpdateDevice(id, body)
			if err != nil {
				writeError(w, err)
				return
			}

			writeJSON(w, http.StatusOK, devices)

		case http.MethodDelete:
			result, err := core.DeleteDevice(id)
			if err != nil {
				writeError(w, err)
				return
			}

			writeJSON(w, http.StatusOK, result)

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func handleTopologyReset(
	core *coreclient.Client,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		if !requireMethod(w, r, http.MethodPost) {
			return
		}

		body, ok := readJSONObject(w, r, false)
		if !ok {
			return
		}

		topology, err := core.ResetTopology(body)
		if err != nil {
			writeError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, topology)
	}
}

func requireMethod(
	w http.ResponseWriter,
	r *http.Request,
	method string,
) bool {

	if r.Method == method {
		return true
	}

	writeMethodNotAllowed(w, method)
	return false
}

func writeError(
	w http.ResponseWriter,
	err error,
) {
	if err == nil {
		err = contract.NewAPIError(contract.ErrorInternal, "internal server error")
	}
	code := contract.APIErrorCode(err)
	message := err.Error()
	if code == contract.ErrorInternal {
		message = "internal server error"
	}
	writeJSON(w, apiErrorStatus(code), map[string]any{
		"error":   code,
		"message": message,
	})
}

func apiErrorStatus(code string) int {
	switch code {
	case contract.ErrorInvalidJSON:
		return http.StatusBadRequest
	case contract.ErrorNotFound:
		return http.StatusNotFound
	case contract.ErrorDuplicateID, contract.ErrorTopologyRequired:
		return http.StatusConflict
	case contract.ErrorValidationFailed, contract.ErrorUnsafeAutomation:
		return http.StatusUnprocessableEntity
	case contract.ErrorForbiddenAction:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
