package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
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

type systemHealthResponse struct {
	*contract.RuntimeHealth
	Web    webapi.WebHealth    `json:"web"`
	Server webapi.ServerHealth `json:"server"`
}

type authPrincipalContextKey struct{}

type snapshotProvider interface {
	Snapshot() (*contract.PublicSnapshot, error)
}

type stateProvider interface {
	State() (*contract.PublicSnapshot, error)
}

type systemHealthProvider interface {
	SystemHealth() (*contract.RuntimeHealth, error)
}

type runtimeControlProvider interface {
	ResetIntrusion(json.RawMessage) (map[string]any, error)
	ResetSystemState(json.RawMessage) (map[string]any, error)
	ManualRisk(json.RawMessage) (map[string]any, error)
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

	securityPath := getenv("SYNORA_SECURITY", security.DefaultPath)
	authPath := getenv("SYNORA_AUTH", webapi.DefaultAuthConfigPath)
	securityConfig, err := security.Load(securityPath)
	if err != nil {
		log.Fatal(err)
	}
	if securityConfig.APITokenHash == "" {
		log.Fatal("security config requires api_token_hash or api_token")
	}
	serverConfig := securityConfig.Server
	httpAddr := getenv("SYNORA_HTTP_ADDR", serverConfig.HTTPAddr)
	httpsEnabled := getenvBool("SYNORA_HTTPS_ENABLED", serverConfig.HTTPSEnabled)
	httpsAddr := getenv("SYNORA_HTTPS_ADDR", serverConfig.HTTPSAddr)
	tlsCertFile := getenv("SYNORA_TLS_CERT_FILE", serverConfig.TLSCertFile)
	tlsKeyFile := getenv("SYNORA_TLS_KEY_FILE", serverConfig.TLSKeyFile)

	sessionTTL := getenvDuration("SYNORA_SESSION_TTL", webapi.DefaultSessionTTL)
	sessions, err := webapi.NewSessionStore(
		getenv("SYNORA_SESSION_STORE", webapi.DefaultSessionPath),
		sessionTTL,
		security.HashSecret(securityConfig.APITokenHash),
	)
	if err != nil {
		log.Fatal("web auth session store: ", err)
	}
	auth := webapi.NewAuthService(sessions, securityConfig.VerifyAPIToken)
	authUsers, err := webapi.LoadUserDirectory(authPath)
	if err != nil {
		log.Printf("auth users load warning path=%s err=%v", authPath, err)
		authUsers = webapi.NewUserDirectory()
	}
	auth.Users = authUsers
	auth.CookieOriginAllowed = func(r *http.Request) bool {
		return sameOriginRequest(r, securityConfig)
	}
	log.Printf("auth users loaded=%d path=%s", authUsers.Count(), authPath)

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
	faceRoot := strings.TrimSpace(getenv("SYNORA_FACE_DATA_ROOT", ""))
	if faceRoot == "" {
		faceRoot = strings.TrimSpace(securityConfig.Vision.FaceDataRoot)
	}
	faceFiles := newFaceStore(faceRoot)
	webHealth := webServer.Health()
	log.Printf(
		"web enabled=%t root=%s index_present=%t",
		webHealth.Enabled,
		webHealth.Root,
		webHealth.IndexPresent,
	)

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/state", handleState(core))
	apiMux.HandleFunc("/api/events", handleEvents(core))
	apiMux.HandleFunc("/api/events/chains", handleEventChains(core))
	apiMux.HandleFunc("/api/events/chains/", handleEventChain(core))
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
	apiMux.HandleFunc("/api/cge/critical-chains", handleCriticalChains(core))
	apiMux.HandleFunc("/api/cge/critical-chains/", handleCriticalChain(core))
	apiMux.HandleFunc("/api/cge/security-profile", handleCGESecurityProfile(core))
	apiMux.HandleFunc("/api/cge/feedback", handleCGEFeedbackList(core))
	apiMux.HandleFunc("/api/cge/feedback/evaluation", handleCGEFeedbackEvaluation(core))
	apiMux.HandleFunc("/api/cge/feedback/chain", handleCGEFeedbackChain(core))
	apiMux.HandleFunc("/api/cge/", handleCGEDetail(core))
	apiMux.HandleFunc("/api/validations", handleValidationCollection(core))
	apiMux.HandleFunc("/api/validations/", handleValidationItem(core))
	apiMux.HandleFunc("/api/devices", handleDeviceCollection(core))
	apiMux.HandleFunc("/api/devices/pairing/start", handlePairingStart(core))
	apiMux.HandleFunc("/api/devices/pairing/complete", handlePairingComplete(core))
	synoraCameraPairing := newSynoraCameraPairingStore()
	apiMux.HandleFunc("/api/devices/pairing/capabilities", handleSynoraCameraPairingCapabilities())
	apiMux.HandleFunc("/api/devices/pairing/synora-camera/start", handleSynoraCameraPairingStart(core, synoraCameraPairing))
	apiMux.HandleFunc("/api/devices/pairing/synora-camera/confirm", handleSynoraCameraPairingConfirm(core, synoraCameraPairing))
	apiMux.HandleFunc("/api/devices/pairing/synora-camera/claim", handleSynoraCameraPairingClaim(synoraCameraPairing))
	apiMux.HandleFunc("/api/devices/", handleDeviceItem(core))
	registerResidentRoutes(apiMux, core, faceFiles)
	apiMux.HandleFunc("/api/automations", handleAutomationCollection(core))
	apiMux.HandleFunc("/api/automations/catalog", handleAutomationCatalog(core))
	apiMux.HandleFunc("/api/automations/", handleAutomationItem(core))
	apiMux.HandleFunc("/api/topology", handleTopologyConfiguration(core))
	apiMux.HandleFunc("/api/topology/", handleTopologySubroute())
	serverHealth := webapi.ServerHealth{
		HTTPAddr:       httpAddr,
		HTTPSEnabled:   httpsEnabled,
		HTTPSAddr:      httpsAddr,
		TLSCertPresent: regularFile(tlsCertFile),
		TLSKeyPresent:  regularFile(tlsKeyFile),
	}
	apiMux.HandleFunc("/api/system/health", handleSystemHealth(core, webServer, serverHealth))
	apiMux.HandleFunc("/api/intrusion/reset", handleIntrusionReset(core))
	apiMux.HandleFunc("/api/system/state/reset", handleSystemStateReset(core))
	apiMux.HandleFunc("/api/cge/manual-risk", handleManualRisk(core))
	apiMux.HandleFunc("/api/runtime/diagnostics", handleRuntimeDiagnostics(core))
	apiMux.HandleFunc("/api/cge/runtime-status", handleRuntimeDiagnostics(core))
	apiMux.HandleFunc("/api/snapshot", handleSnapshot(core))

	handler := buildServerHandlerWithAuth(
		securityConfig,
		apiMux,
		wsHub,
		webEnabled,
		webServer,
		auth,
		getenvBool("SYNORA_WS_QUERY_TOKEN", false),
	)
	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           handler,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("synora-api http listening addr=%s", httpAddr)
	log.Printf(
		"synora-api https enabled=%t addr=%s cert=%s key=%s",
		httpsEnabled,
		httpsAddr,
		tlsCertFile,
		tlsKeyFile,
	)
	var httpsServer *http.Server
	if httpsEnabled {
		httpsServer = &http.Server{
			Addr:              httpsAddr,
			Handler:           handler,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
		}
	}

	errCh := make(chan error, 2)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()
	if httpsServer != nil {
		go func() {
			if err := httpsServer.ListenAndServeTLS(tlsCertFile, tlsKeyFile); err != nil && err != http.ErrServerClosed {
				errCh <- fmt.Errorf("https server: %w", err)
			}
		}()
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		log.Fatal(err)
	case <-stop:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		if httpsServer != nil {
			_ = httpsServer.Shutdown(shutdownCtx)
		}
	}
}

func regularFile(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return err == nil && info.Mode().IsRegular()
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
			w.Header().Set("Access-Control-Allow-Credentials", "true")
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
	return apiAuthMiddlewareWithAuth(cfg, nil, true, next)
}

func apiAuthMiddlewareWithAuth(
	cfg *security.Config,
	auth *webapi.AuthService,
	allowQueryToken bool,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if r.URL.Path == "/api/system/health" && cfg != nil && cfg.PublicSystemHealth {
			next.ServeHTTP(w, r)
			return
		}

		token, bearerProvided := bearerToken(r.Header.Get("Authorization"))
		bearerOK := bearerProvided && cfg != nil && cfg.VerifyAPIToken(token)
		if !bearerProvided && allowQueryToken && (r.URL.Path == "/api/ws" || r.URL.Path == "/ws") {
			token = strings.TrimSpace(r.URL.Query().Get("token"))
			bearerProvided = token != ""
			bearerOK = bearerProvided && cfg != nil && cfg.VerifyAPIToken(token)
		}

		session, sessionOK := authSession(auth, r)
		if !bearerOK && !sessionOK {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		if !bearerOK && sessionOK && isMutatingMethod(r.Method) && !sameOriginRequest(r, cfg) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}

		principal := session.User
		if bearerOK {
			principal = webapi.AdminAuthUser()
		}
		permission := requiredAPIPermission(r)
		if permission != "" && !principal.HasPermission(permission) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authPrincipalContextKey{}, principal)))
	})
}

func authPrincipalFromRequest(r *http.Request) (webapi.AuthUser, bool) {
	if r == nil {
		return webapi.AuthUser{}, false
	}
	principal, ok := r.Context().Value(authPrincipalContextKey{}).(webapi.AuthUser)
	return principal, ok
}

func isAdminRequest(r *http.Request) bool {
	principal, ok := authPrincipalFromRequest(r)
	return ok && principal.Role == webapi.RoleAdmin
}

func authSession(auth *webapi.AuthService, r *http.Request) (webapi.AuthSession, bool) {
	if auth == nil {
		return webapi.AuthSession{}, false
	}
	return auth.SessionFromRequest(r)
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func sameOriginRequest(r *http.Request, cfg *security.Config) bool {
	if r == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
			if parsed, err := url.Parse(referer); err == nil {
				origin = parsed.Scheme + "://" + parsed.Host
			}
		}
	}
	if origin == "" || strings.EqualFold(origin, "null") {
		return false
	}
	if cfg != nil && cfg.AllowsOrigin(origin) {
		return true
	}
	scheme := "http"
	if webapi.RequestIsHTTPS(r) {
		scheme = "https"
	}
	return strings.EqualFold(origin, scheme+"://"+r.Host)
}

func buildServerHandler(
	cfg *security.Config,
	apiMux http.Handler,
	wsHub http.Handler,
	webEnabled bool,
	webServer *webapi.Server,
) http.Handler {
	return buildServerHandlerWithAuth(cfg, apiMux, wsHub, webEnabled, webServer, nil, true)
}

func buildServerHandlerWithAuth(
	cfg *security.Config,
	apiMux http.Handler,
	wsHub http.Handler,
	webEnabled bool,
	webServer *webapi.Server,
	auth *webapi.AuthService,
	allowQueryToken bool,
) http.Handler {
	mux := http.NewServeMux()
	if auth != nil {
		mux.HandleFunc("/api/auth/login", auth.LoginHandler)
		mux.HandleFunc("/api/auth/me", auth.MeHandler)
		mux.HandleFunc("/api/auth/logout", auth.LogoutHandler)
		mux.HandleFunc("/api/auth/refresh", auth.RefreshHandler)
	}
	mux.Handle("/api/", apiAuthMiddlewareWithAuth(cfg, auth, allowQueryToken, apiMux))
	if wsHub != nil {
		mux.Handle("/api/ws", apiAuthMiddlewareWithAuth(cfg, auth, allowQueryToken, wsHub))
		mux.Handle("/ws", apiAuthMiddlewareWithAuth(cfg, auth, allowQueryToken, wsHub))
	}
	mux.HandleFunc("/health", handleHealth)
	if webEnabled && webServer != nil {
		mux.Handle("/", webServer.WebHandler())
	} else {
		mux.HandleFunc("/", handleIndex)
	}
	// Keep this wrapper outside the router, auth middleware, CORS and logging
	// layers so every API response receives the same anti-cache policy,
	// regardless of route declaration order or router implementation.
	return withAPINoStore(loggingMiddleware(corsMiddleware(cfg, mux)))
}

func requiredAPIPermission(r *http.Request) string {
	if r == nil {
		return webapi.PermissionSecurityAdmin
	}
	path := r.URL.Path
	method := r.Method
	readOnly := method == http.MethodGet || method == http.MethodHead

	switch {
	case path == "/api/devices/pairing/capabilities" || strings.HasPrefix(path, "/api/devices/pairing/synora-camera/"):
		return webapi.PermissionSecurityAdmin
	case method == http.MethodDelete && strings.HasPrefix(path, "/api/devices/"):
		return webapi.PermissionSecurityAdmin
	case path == "/api/state" || path == "/api/snapshot" || path == "/api/ws" || path == "/ws":
		return webapi.PermissionStateRead
	case strings.HasPrefix(path, "/api/events"):
		return webapi.PermissionStateRead
	case path == "/api/system/health":
		return webapi.PermissionSettingsRead
	case path == "/api/intrusion/reset" || path == "/api/system/state/reset":
		return webapi.PermissionSecurityAdmin
	case path == "/api/cge/manual-risk":
		return webapi.PermissionSecurityAdmin
	case path == "/api/runtime/diagnostics" || path == "/api/cge/runtime-status":
		return webapi.PermissionCGERead
	case strings.HasPrefix(path, "/api/devices"):
		if readOnly {
			return webapi.PermissionDevicesRead
		}
		return webapi.PermissionSecurityAdmin
	case strings.HasPrefix(path, "/api/residents"):
		residentPath := strings.TrimPrefix(path, "/api/residents/")
		if strings.Contains(residentPath, "/face") {
			return webapi.PermissionResidentsWrite
		}
		if readOnly {
			return webapi.PermissionResidentsRead
		}
		return webapi.PermissionResidentsWrite
	case strings.HasPrefix(path, "/api/topology"):
		if readOnly {
			return webapi.PermissionTopologyRead
		}
		return webapi.PermissionTopologyWrite
	case strings.HasPrefix(path, "/api/automations"):
		if readOnly {
			return webapi.PermissionAutomationsRead
		}
		return webapi.PermissionAutomationsWrite
	case strings.HasPrefix(path, "/api/simulation"):
		return webapi.PermissionSimulationRun
	case strings.HasPrefix(path, "/api/cge"):
		if readOnly {
			return webapi.PermissionCGERead
		}
		return webapi.PermissionCGEWrite
	case strings.HasPrefix(path, "/api/validations"):
		if readOnly {
			return webapi.PermissionSettingsRead
		}
		return webapi.PermissionSettingsWrite
	case strings.HasPrefix(path, "/api/"):
		return webapi.PermissionSecurityAdmin
	default:
		return ""
	}
}

// withAPINoStore prevents browsers and intermediary proxies from reusing
// stale runtime snapshots. Static assets are served outside this path and
// retain their immutable cache policy.
func withAPINoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		next.ServeHTTP(w, r)
	})
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

func getenvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
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
	webServer *webapi.Server,
	serverHealth ...webapi.ServerHealth,
) http.HandlerFunc {

	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		if !requireMethod(w, r, http.MethodGet) {
			return
		}

		type healthResult struct {
			health *contract.RuntimeHealth
			err    error
		}
		resultCh := make(chan healthResult, 1)
		go func() {
			health, err := core.SystemHealth()
			resultCh <- healthResult{health: health, err: err}
		}()
		var health *contract.RuntimeHealth
		probeOK := false
		select {
		case result := <-resultCh:
			if result.err != nil || result.health == nil {
				health = degradedRuntimeHealth("runtime health unavailable: " + errorMessage(result.err))
			} else {
				health = result.health
				probeOK = true
			}
		case <-time.After(500 * time.Millisecond):
			health = degradedRuntimeHealth("runtime health probe timed out")
		}
		if health.Status == "" {
			health.Status = "degraded"
		}
		markServingHealth(health, probeOK)

		webHealth := webapi.WebHealth{Status: "disabled"}
		if webServer != nil {
			webHealth = webServer.Health()
		}
		transportHealth := webapi.ServerHealth{}
		if len(serverHealth) > 0 {
			transportHealth = serverHealth[0]
		}
		writeJSON(w, http.StatusOK, systemHealthResponse{
			RuntimeHealth: health,
			Web:           webHealth,
			Server:        transportHealth,
		})
	}
}

func markServingHealth(health *contract.RuntimeHealth, coreProbeOK bool) {
	if health == nil {
		return
	}
	now := time.Now().UTC()
	if health.Services == nil {
		health.Services = map[string]contract.RuntimeServiceHealth{}
	}
	if health.Components == nil {
		health.Components = map[string]contract.RuntimeServiceHealth{}
	}
	api := contract.RuntimeServiceHealth{Name: "synora-api", Status: "ok", Active: true, Checked: now, Message: "serving"}
	health.Services["synora-api"] = api
	health.Components["api"] = contract.RuntimeServiceHealth{Name: "api", Status: "ok", Active: true, Checked: now, Message: "serving"}
	if coreProbeOK {
		bus := contract.RuntimeServiceHealth{Name: "synora-bus", Status: "ok", Active: true, Checked: now, Message: "reachable through core RPC"}
		health.Services["synora-bus"] = bus
		health.Components["bus"] = contract.RuntimeServiceHealth{Name: "bus", Status: diagnosticStatus(bus.Status, bus.Active), Active: bus.Active, Checked: bus.Checked, Message: bus.Message}
		core := contract.RuntimeServiceHealth{Name: "synora-core", Status: "ok", Active: true, Checked: now, Message: "RPC responded"}
		health.Services["synora-core"] = core
		health.Components["core"] = contract.RuntimeServiceHealth{Name: "core", Status: diagnosticStatus(core.Status, core.Active), Active: core.Active, Checked: core.Checked, Message: core.Message}
	}
}

func errorMessage(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}

func degradedRuntimeHealth(message string) *contract.RuntimeHealth {
	now := time.Now().UTC()
	health := contract.RuntimeHealth{
		Status:      "degraded",
		GeneratedAt: now,
		Services: map[string]contract.RuntimeServiceHealth{
			"synora-api":  {Name: "synora-api", Status: "ok", Active: true, Checked: now},
			"synora-core": {Name: "synora-core", Status: "degraded", Active: false, Checked: now, Error: message},
		},
		Network:   contract.RuntimeNetworkHealth{Status: "unknown"},
		MediaMTX:  contract.RuntimeMediaMTXHealth{Status: "unknown"},
		Disk:      contract.RuntimeDiskHealth{Path: "/var/lib/synora", Status: "unavailable", Error: message},
		Timestamp: now,
	}
	normalized := contract.NormalizeRuntimeHealth(health, now)
	return &normalized
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
	case contract.ErrorInvalidRequest:
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
