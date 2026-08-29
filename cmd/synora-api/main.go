package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	webapi "synora/internal/api"
	"synora/internal/bus"
	"synora/internal/coreclient"
	"synora/internal/discovery/network"
	"synora/internal/runtimeconfig"
	"synora/internal/security"
	"synora/internal/version"
	"synora/pkg/contract"
)

type healthResponse struct {
	Service   string    `json:"service"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

type systemHealthResponse struct {
	*contract.RuntimeHealth
	Web                  webapi.WebHealth    `json:"web"`
	Server               webapi.ServerHealth `json:"server"`
	DangerDecay          map[string]any      `json:"danger_decay,omitempty"`
	DangerScoreCurrent   float64             `json:"danger_score_current"`
	DangerScorePeak      float64             `json:"danger_score_peak"`
	DangerScoreUpdatedAt time.Time           `json:"danger_score_updated_at,omitempty"`
	DangerReasonsCurrent []string            `json:"danger_reasons_current,omitempty"`
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
	ClearManualRisk(json.RawMessage) (map[string]any, error)
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Fatal("invalid runtime configuration: ", err)
	}
	paths := runtime.Paths
	securityPath := paths.Security
	authPath := paths.Auth
	securityConfig, err := security.Load(securityPath)
	if err != nil {
		log.Fatal(err)
	}
	if securityConfig.APITokenHash == "" {
		log.Fatal("security config requires api_token_hash or api_token")
	}
	serverConfig := securityConfig.Server
	httpAddr := runtime.Endpoints.HTTP
	if strings.TrimSpace(os.Getenv("SYNORA_HTTP_ADDR")) == "" && serverConfig.HTTPAddr != "" {
		httpAddr = serverConfig.HTTPAddr
	}
	httpsConfigured := getenvBool("SYNORA_HTTPS_ENABLED", serverConfig.HTTPSEnabled)
	httpsAddr := runtime.Endpoints.HTTPS
	if strings.TrimSpace(os.Getenv("SYNORA_HTTPS_ADDR")) == "" && serverConfig.HTTPSAddr != "" {
		httpsAddr = serverConfig.HTTPSAddr
	}
	tlsCertFile := runtime.Paths.TLSCert
	if strings.TrimSpace(os.Getenv("SYNORA_TLS_CERT_FILE")) == "" && serverConfig.TLSCertFile != "" {
		tlsCertFile = serverConfig.TLSCertFile
	}
	tlsKeyFile := runtime.Paths.TLSKey
	if strings.TrimSpace(os.Getenv("SYNORA_TLS_KEY_FILE")) == "" && serverConfig.TLSKeyFile != "" {
		tlsKeyFile = serverConfig.TLSKeyFile
	}
	httpsEnabled := httpsConfigured && regularFile(tlsCertFile) && regularFile(tlsKeyFile)
	if httpsConfigured && !httpsEnabled {
		log.Fatalf("synora-api refuses insecure startup: HTTPS is enabled but TLS certificate or key is missing")
	}

	sessionTTL := getenvDuration("SYNORA_SESSION_TTL", webapi.DefaultSessionTTL)
	sessionFingerprint := security.HashSecret(securityConfig.APITokenHash)
	if sessionSecretPath := getenv("SYNORA_SESSION_SECRET_FILE", securityConfig.SessionSecretFile); sessionSecretPath != "" {
		if sessionSecret, readErr := os.ReadFile(sessionSecretPath); readErr == nil && strings.TrimSpace(string(sessionSecret)) != "" {
			sessionFingerprint = security.HashSecret(strings.TrimSpace(string(sessionSecret)))
		}
	}
	sessions, err := webapi.NewSessionStore(
		paths.SessionStore,
		sessionTTL,
		sessionFingerprint,
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

	busClient, err := bus.ConnectContext(ctx,
		paths.BusSocket,
		"api",
	)

	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("api bus connection stopped: %v", err)
		}
		return
	}
	publishNetworkPairingEvent = func(event string, payload map[string]any) {
		sendNetworkPairingEvent(busClient, event, payload)
	}

	core := coreclient.New(busClient)
	wsHub := newWebSocketHubWithOrigin(core, func(r *http.Request) bool {
		return websocketOriginAllowed(r, securityConfig)
	})
	go wsHub.observeBus(busClient)
	simulationRunner := newSimulationRunner(busClient, wsHub)
	webEnabled := getenvBool("SYNORA_WEB_ENABLED", true)
	webRoot := paths.WebRoot
	webServer := &webapi.Server{
		WebEnabled: webEnabled,
		WebRoot:    webRoot,
	}
	faceRoot := paths.FaceDataRoot
	if strings.TrimSpace(os.Getenv("SYNORA_FACE_DATA_ROOT")) == "" && strings.TrimSpace(securityConfig.Vision.FaceDataRoot) != "" {
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
	features := securityConfig.Features
	apiMux.HandleFunc("/api/state", handleState(core))
	apiMux.HandleFunc("/api/events", handleEvents(core))
	apiMux.HandleFunc("/api/events/chains", handleEventChains(core))
	apiMux.HandleFunc("/api/events/chains/", handleEventChain(core))
	apiMux.HandleFunc("/api/incidents", handleIncidentCollection(core))
	apiMux.HandleFunc("/api/incidents/", handleIncidentRoute(core))
	apiMux.HandleFunc("/api/clips", handleClipCollection(core))
	apiMux.HandleFunc("/api/clips/", handleClipRoute(core))
	apiMux.HandleFunc("/api/simulation/scenarios", withFeature(features.Enabled(security.FeatureDevSimulation), security.FeatureDevSimulation, handleSimulationScenarios()))
	apiMux.HandleFunc("/api/simulation/run", withFeature(features.Enabled(security.FeatureDevSimulation), security.FeatureDevSimulation, handleSimulationRun(simulationRunner)))
	apiMux.HandleFunc("/api/simulation/runs/", withFeature(features.Enabled(security.FeatureDevSimulation), security.FeatureDevSimulation, handleSimulationRunStatus(simulationRunner)))
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
	apiMux.HandleFunc("/api/actions/policy", handleActionPolicy(core))
	apiMux.HandleFunc("/api/actions/policy/reset", handleActionPolicyReset(core))
	apiMux.HandleFunc("/api/actions/catalog", handleActionCatalog(core))
	apiMux.HandleFunc("/api/actions/test", handleActionTest(core))
	apiMux.HandleFunc("/api/cge/feedback", handleCGEFeedbackList(core))
	apiMux.HandleFunc("/api/cge/feedback/evaluation", handleCGEFeedbackEvaluation(core))
	apiMux.HandleFunc("/api/cge/feedback/chain", handleCGEFeedbackChain(core))
	labValidationEvents := withFeature(features.Enabled(security.FeatureSynoraLab), security.FeatureSynoraLab,
		withFeature(features.Enabled(security.FeatureCGEValidation), security.FeatureCGEValidation, handleCGEValidationEvents(core)))
	labValidationSequence := withFeature(features.Enabled(security.FeatureSynoraLab), security.FeatureSynoraLab,
		withFeature(features.Enabled(security.FeatureCGEValidation), security.FeatureCGEValidation, handleCGEValidationSequence(core)))
	labValidationHistory := withFeature(features.Enabled(security.FeatureSynoraLab), security.FeatureSynoraLab,
		withFeature(features.Enabled(security.FeatureCGEValidation), security.FeatureCGEValidation, handleCGEValidationHistory(core)))
	apiMux.HandleFunc("/api/cge/validation/events", labValidationEvents)
	apiMux.HandleFunc("/api/cge/validation/chain-sequence", labValidationSequence)
	apiMux.HandleFunc("/api/cge/validation/history", labValidationHistory)
	// Product-facing names for Synora Lab. The /api/cge/validation/* aliases
	// remain stable for existing clients and the current webapp.
	apiMux.HandleFunc("/api/lab/validation/events", labValidationEvents)
	apiMux.HandleFunc("/api/lab/validation/chain-sequence", labValidationSequence)
	apiMux.HandleFunc("/api/lab/validation/history", labValidationHistory)
	apiMux.HandleFunc("/api/cge/", handleCGEDetail(core))
	apiMux.HandleFunc("/api/validations", handleValidationCollection(core))
	apiMux.HandleFunc("/api/validations/", handleValidationItem(core))
	apiMux.HandleFunc("/api/devices", handleDeviceCollection(core))
	identityRegistry := security.NewIdentityRegistry(paths.IdentityRegistry)
	if err := identityRegistry.Load(); err != nil {
		log.Fatal("camera identity registry: ", err)
	}
	streamAuthorization := func(deviceID string, item map[string]any) bool {
		if err := identityRegistry.Reload(); err != nil {
			return false
		}
		record, ok := identityRegistry.Lookup(deviceID)
		if !ok || record.Kind != security.IdentityCamera || record.Status != security.IdentityActive {
			return false
		}
		trusted, trustedOK := item["trusted"].(bool)
		enabled, enabledOK := item["enabled"].(bool)
		if trustedOK && !trusted || enabledOK && !enabled {
			return false
		}
		if deleted, exists := item["deleted_at"]; exists && deleted != nil {
			return false
		}
		return true
	}
	apiMux.HandleFunc("/api/streams", handleStreamsWithAuthorization(core, streamAuthorization))
	apiMux.HandleFunc("/api/streams/", handleStreamsWithAuthorization(core, streamAuthorization))
	apiMux.HandleFunc("/api/devices/pairing/start", handlePairingStart(core))
	apiMux.HandleFunc("/api/devices/pairing/complete", handlePairingComplete(core))
	synoraCameraPairing := newSynoraCameraPairingStore(filepath.Join(filepath.Dir(paths.IdentityRegistry), "pairing-sessions.json"))
	synoraCameraPairing.securityPath = securityPath
	synoraCameraPairing.windowActive = network.PairingWindowActive
	synoraCameraPairing.identityRegistry = identityRegistry
	synoraCameraPairing.requirePublicKey = true
	synoraCameraPairing.requireObservedMAC = true
	apiMux.HandleFunc("/api/devices/pairing/capabilities", handleSynoraCameraPairingCapabilities())
	apiMux.HandleFunc("/api/devices/pairing/synora-camera/start", handleSynoraCameraPairingStart(core, synoraCameraPairing))
	apiMux.HandleFunc("/api/devices/pairing/synora-camera/confirm", handleSynoraCameraPairingConfirm(core, synoraCameraPairing))
	apiMux.HandleFunc("/api/devices/pairing/synora-camera/claim", handleSynoraCameraPairingClaimWithProvider(core, synoraCameraPairing))
	apiMux.HandleFunc("/api/devices/pairing/synora-camera/revoke", handleSynoraCameraPairingRevoke(core, synoraCameraPairing))
	apiMux.HandleFunc("/api/devices/pairing/synora-camera/reset", handleSynoraCameraPairingReset(core, synoraCameraPairing))
	apiMux.HandleFunc("/api/devices/pairing/status", handleSynoraNetPairingStatus())
	apiMux.HandleFunc("/api/devices/pairing/window/start", handleSynoraNetPairingWindowStart())
	apiMux.HandleFunc("/api/devices/pairing/window/stop", handleSynoraNetPairingWindowStop())
	apiMux.HandleFunc("/api/devices/", handleDeviceItem(core))
	registerResidentRoutes(apiMux, core, faceFiles)
	apiMux.HandleFunc("/api/automations", handleAutomationCollection(core))
	apiMux.HandleFunc("/api/automations/catalog", handleAutomationCatalog(core))
	apiMux.HandleFunc("/api/automations/", handleAutomationItem(core))
	apiMux.HandleFunc("/api/topology", handleTopologyConfiguration(core))
	apiMux.HandleFunc("/api/topology/", handleTopologySubroute())
	serverHealth := webapi.ServerHealth{
		HTTPAddr:       httpAddr,
		HTTPSEnabled:   httpsConfigured,
		HTTPSAddr:      httpsAddr,
		TLSCertPresent: regularFile(tlsCertFile),
		TLSKeyPresent:  regularFile(tlsKeyFile),
	}
	apiMux.HandleFunc("/api/system/health", handleSystemHealth(core, webServer, serverHealth))
	apiMux.HandleFunc("/api/system/version", handleSystemVersion(paths.VersionFile))
	apiMux.HandleFunc("/api/system/connectivity", handleConnectivityStatus(busClient))
	apiMux.HandleFunc("/api/intrusion/reset", handleIntrusionReset(core))
	apiMux.HandleFunc("/api/system/state/reset", handleSystemStateReset(core))
	apiMux.HandleFunc("/api/cge/manual-risk", handleManualRisk(core))
	apiMux.HandleFunc("/api/cge/manual-risk/clear", handleManualRiskClear(core))
	apiMux.HandleFunc("/api/security/mode", handleSecurityMode(core))
	apiMux.HandleFunc("/api/security/arm", handleSecurityArm(core))
	apiMux.HandleFunc("/api/security/disarm", handleSecurityDisarm(core))
	apiMux.HandleFunc("/api/runtime/diagnostics", withFeature(features.Enabled(security.FeatureDiagnostics), security.FeatureDiagnostics, handleRuntimeDiagnostics(core)))
	apiMux.HandleFunc("/api/cge/runtime-status", withFeature(features.Enabled(security.FeatureDiagnostics), security.FeatureDiagnostics, handleRuntimeDiagnostics(core)))
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
	httpHandler := handler
	if httpsEnabled {
		// HTTP is only the redirect listener. The TLS listener must receive
		// the real handler or HTTPS requests would redirect to themselves.
		httpHandler = redirectHTTPToHTTPS(handler, httpsAddr)
	}
	httpServer := &http.Server{
		Addr:              httpAddr,
		Handler:           httpHandler,
		ReadTimeout:       runtime.Timeouts.HTTPRead,
		WriteTimeout:      runtime.Timeouts.HTTPWrite,
		IdleTimeout:       runtime.Timeouts.HTTPIdle,
		ReadHeaderTimeout: runtime.Timeouts.HTTPReadHeader,
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
			ReadTimeout:       runtime.Timeouts.HTTPRead,
			WriteTimeout:      runtime.Timeouts.HTTPWrite,
			IdleTimeout:       runtime.Timeouts.HTTPIdle,
			ReadHeaderTimeout: runtime.Timeouts.HTTPReadHeader,
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

	select {
	case err := <-errCh:
		log.Fatal(err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), runtime.Timeouts.Shutdown)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		if httpsServer != nil {
			_ = httpsServer.Shutdown(shutdownCtx)
		}
		wsHub.Close()
		_ = busClient.Close()
	}
}

func websocketOriginAllowed(r *http.Request, cfg *security.Config) bool {
	if r == nil || strings.TrimSpace(r.Header.Get("Origin")) == "" {
		return true
	}
	return sameOriginRequest(r, cfg)
}

func redirectHTTPToHTTPS(next http.Handler, httpsAddr string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if hostname, _, err := net.SplitHostPort(host); err == nil {
			host = hostname
		}
		port := strings.TrimPrefix(strings.TrimSpace(httpsAddr), ":")
		if _, configuredPort, err := net.SplitHostPort(httpsAddr); err == nil {
			port = configuredPort
		}
		if port != "" {
			host = net.JoinHostPort(host, port)
		}
		location := "https://" + host + r.URL.RequestURI()
		http.Redirect(w, r, location, http.StatusPermanentRedirect)
	})
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

func handleSystemVersion(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, http.StatusOK, version.Current(path))
	}
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
			security.RedactSupportText(r.URL.Path),
			time.Since(started),
		)
	})
}

func corsMiddleware(cfg *security.Config, next http.Handler) http.Handler {

	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			if cfg == nil || !cfg.AllowsOrigin(origin) {
				if r.Method == http.MethodOptions {
					writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin_not_allowed"})
					return
				}
			} else if explicitOriginAllowed(cfg, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Add("Vary", "Origin")
			} else {
				// A wildcard origin is deliberately public and can never be
				// combined with credentialed browser requests.
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
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
		// The camera claim is the one intentionally narrow unauthenticated
		// pairing surface. It still requires an active, short-lived network
		// pairing window and a one-time setup token in the handler.
		path := canonicalAPIPath(r.URL.Path)
		if path == "/api/devices/pairing/synora-camera/claim" && r.Method == http.MethodPost && network.PairingWindowActive() {
			next.ServeHTTP(w, r)
			return
		}
		if path == "/api/system/health" && cfg != nil && cfg.PublicSystemHealth {
			next.ServeHTTP(w, r)
			return
		}

		token, bearerProvided := bearerToken(r.Header.Get("Authorization"))
		bearerOK := bearerProvided && cfg != nil && cfg.VerifyAPIToken(token)
		if !bearerProvided && allowQueryToken && (path == "/api/ws" || path == "/ws") {
			token = strings.TrimSpace(r.URL.Query().Get("token"))
			bearerProvided = token != ""
			bearerOK = bearerProvided && cfg != nil && cfg.VerifyAPIToken(token)
		}

		session, sessionOK := authSession(auth, r)
		if !bearerOK && !sessionOK {
			if isAPIV1Path(r.URL.Path) {
				writeAPIV1Error(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		if !bearerOK && sessionOK && isMutatingMethod(r.Method) && !sameOriginRequest(r, cfg) {
			if isAPIV1Path(r.URL.Path) {
				writeAPIV1Error(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}

		principal := session.User
		if bearerOK {
			principal = webapi.AdminAuthUser()
		}
		permission := requiredAPIPermission(r)
		if permission != "" && !principal.HasPermission(permission) {
			if isAPIV1Path(r.URL.Path) {
				writeAPIV1Error(w, http.StatusForbidden, "forbidden", "forbidden")
				return
			}
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
			return
		}

		requestContext := context.WithValue(r.Context(), authPrincipalContextKey{}, principal)
		if sessionOK && !bearerOK && auth != nil {
			requestContext = context.WithValue(requestContext, wsSessionValidatorContextKey{}, func() bool {
				_, valid := authSession(auth, r)
				return valid
			})
		}
		next.ServeHTTP(w, r.WithContext(requestContext))
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
	if explicitOriginAllowed(cfg, origin) {
		return true
	}
	scheme := "http"
	if webapi.RequestIsHTTPS(r) {
		scheme = "https"
	}
	return strings.EqualFold(origin, scheme+"://"+r.Host)
}

func explicitOriginAllowed(cfg *security.Config, origin string) bool {
	if cfg == nil {
		return false
	}
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}
	for _, allowed := range cfg.AllowedOrigins {
		if strings.TrimSpace(allowed) == origin {
			return true
		}
	}
	return false
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
		mux.HandleFunc("/api/auth/bootstrap", auth.BootstrapHandler)
		mux.HandleFunc("/api/auth/me", auth.MeHandler)
		mux.HandleFunc("/api/auth/logout", auth.LogoutHandler)
		mux.HandleFunc("/api/auth/refresh", auth.RefreshHandler)
		mux.HandleFunc("/api/auth/password", auth.ChangePasswordHandler)
		mux.HandleFunc("/api/auth/users", auth.UsersHandler)
		mux.HandleFunc("/api/auth/users/", auth.UserHandler)
	}
	apiV1 := newAPIV1Handler(apiMux)
	mux.Handle("/api/v1", apiAuthMiddlewareWithAuth(cfg, auth, allowQueryToken, apiV1))
	mux.Handle("/api/v1/", apiAuthMiddlewareWithAuth(cfg, auth, allowQueryToken, apiV1))
	if wsHub != nil {
		mux.Handle("/api/v1/ws", apiAuthMiddlewareWithAuth(cfg, auth, allowQueryToken, rewriteAPIPath("/api/ws", wsHub)))
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
	return securityHeadersMiddleware(withAPINoStore(loggingMiddleware(corsMiddleware(cfg, apiRateLimitMiddleware(newAPIRequestRateLimiter(), mux)))))
}

func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'; connect-src 'self' ws: wss:")
		next.ServeHTTP(w, r)
	})
}

func requiredAPIPermission(r *http.Request) string {
	if r == nil {
		return webapi.PermissionSecurityAdmin
	}
	path := canonicalAPIPath(r.URL.Path)
	method := r.Method
	readOnly := method == http.MethodGet || method == http.MethodHead

	switch {
	case strings.HasPrefix(path, "/api/devices/pairing/"):
		return webapi.PermissionSecurityAdmin
	case path == "/api/devices/pairing/capabilities" || strings.HasPrefix(path, "/api/devices/pairing/synora-camera/"):
		return webapi.PermissionSecurityAdmin
	case method == http.MethodDelete && strings.HasPrefix(path, "/api/devices/"):
		return webapi.PermissionSecurityAdmin
	case path == "/api/state" || path == "/api/snapshot" || path == "/api/ws" || path == "/ws":
		return webapi.PermissionStateRead
	case strings.HasPrefix(path, "/api/events"):
		return webapi.PermissionStateRead
	case strings.HasPrefix(path, "/api/incidents"):
		if readOnly {
			return webapi.PermissionStateRead
		}
		return webapi.PermissionSecurityAdmin
	case strings.HasPrefix(path, "/api/clips"):
		return webapi.PermissionStateRead
	case path == "/api/system/health":
		return webapi.PermissionSettingsRead
	case path == "/api/system/version":
		return webapi.PermissionSettingsRead
	case path == "/api/system/connectivity":
		return webapi.PermissionSettingsRead
	case path == "/api/intrusion/reset" || path == "/api/system/state/reset":
		return webapi.PermissionSecurityAdmin
	case path == "/api/security/mode" && readOnly:
		return webapi.PermissionStateRead
	case path == "/api/security/mode" || path == "/api/security/arm" || path == "/api/security/disarm" || path == "/api/cge/manual-risk" || path == "/api/cge/manual-risk/clear":
		return webapi.PermissionSecurityAdmin
	case path == "/api/cge/validation/events" || path == "/api/cge/validation/chain-sequence" || path == "/api/cge/validation/history":
		if readOnly {
			return webapi.PermissionCGERead
		}
		return webapi.PermissionSecurityAdmin
	case path == "/api/runtime/diagnostics" || path == "/api/cge/runtime-status":
		return webapi.PermissionCGERead
	case strings.HasPrefix(path, "/api/devices"):
		if readOnly {
			return webapi.PermissionDevicesRead
		}
		return webapi.PermissionSecurityAdmin
	case strings.HasPrefix(path, "/api/streams"):
		return webapi.PermissionDevicesRead
	case strings.HasPrefix(path, "/api/residents"):
		residentPath := strings.TrimPrefix(path, "/api/residents/")
		if strings.Contains(residentPath, "/face") || strings.Contains(residentPath, "/photos") {
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
	case strings.HasPrefix(path, "/api/lab"):
		return webapi.PermissionLabUse
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
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/v1" || strings.HasPrefix(r.URL.Path, "/api/v1/") {
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
		health.Components["https_api"] = httpsHealth(transportHealth, time.Now().UTC())
		var decay map[string]any
		var scoreCurrent, scorePeak float64
		var scoreUpdatedAt time.Time
		var reasons []string
		if stateReader, ok := core.(stateProvider); ok {
			if snapshot, err := stateReader.State(); err == nil && snapshot != nil {
				decay, scoreCurrent, scorePeak, scoreUpdatedAt, reasons = dangerDecayFromSnapshot(snapshot)
			}
		}
		writeJSON(w, http.StatusOK, systemHealthResponse{
			RuntimeHealth:        health,
			Web:                  webHealth,
			Server:               transportHealth,
			DangerDecay:          decay,
			DangerScoreCurrent:   scoreCurrent,
			DangerScorePeak:      scorePeak,
			DangerScoreUpdatedAt: scoreUpdatedAt,
			DangerReasonsCurrent: reasons,
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

func httpsHealth(server webapi.ServerHealth, now time.Time) contract.RuntimeServiceHealth {
	item := contract.RuntimeServiceHealth{Name: "https_api", Checked: now}
	if !server.HTTPSEnabled {
		item.Status, item.Message = "disabled", "HTTPS API disabled"
		return item
	}
	if !server.TLSCertPresent || !server.TLSKeyPresent {
		item.Status, item.Message = "degraded", "HTTPS configured but local certificate is missing"
		return item
	}
	item.Status, item.Active, item.Message = "ok", true, "HTTPS API available on 8443"
	return item
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
	response := map[string]any{
		"error":   code,
		"message": message,
	}
	if typed, ok := err.(*contract.APIError); ok && typed.Details != nil {
		response["details"] = typed.Details
	}
	writeJSON(w, apiErrorStatus(code), response)
}

func apiErrorStatus(code string) int {
	switch code {
	case contract.ErrorInvalidJSON:
		return http.StatusBadRequest
	case contract.ErrorInvalidRequest:
		return http.StatusBadRequest
	case contract.ErrorPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case contract.ErrorNotFound:
		return http.StatusNotFound
	case contract.ErrorRateLimited:
		return http.StatusTooManyRequests
	case contract.ErrorConflict, contract.ErrorDuplicateID, contract.ErrorTopologyRequired:
		return http.StatusConflict
	case contract.ErrorValidationFailed:
		return http.StatusBadRequest
	case contract.ErrorUnsafeAutomation:
		return http.StatusUnprocessableEntity
	case contract.ErrorForbiddenAction:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
