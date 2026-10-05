package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"synora/internal/bus"
	"synora/internal/runtimeconfig"
	"synora/internal/security"
)

// synora-api observes the redacted Core decision stream and serves the
// Intelligence view. System tests use the central Unix-bus harness rather
// than an HTTP injection path.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Fatal("invalid runtime configuration: ", err)
	}
	serverConfig, err := loadAPIServerConfig(runtime)
	if err != nil {
		log.Fatal("invalid API server configuration: ", err)
	}
	client, err := bus.ConnectContext(ctx, runtime.Paths.BusSocket, "api")
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Fatal("API bus connection failed: ", err)
		}
		return
	}
	defer client.Close()

	hub := newWebSocketHub()
	go hub.observeBus(client)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/intelligence/topology", handleIntelligenceTopology(hub))
	mux.HandleFunc("/api/intelligence/traces", handleIntelligenceTraces(hub))
	mux.HandleFunc("/health", handleHealth)
	mux.Handle("/api/ws", hub)
	webRoot := strings.TrimSpace(os.Getenv("SYNORA_WEB_ROOT"))
	if webRoot == "" {
		webRoot = filepath.Join("synora-web", "dist")
	}
	mux.Handle("/", staticWebHandler(webRoot))

	server := &http.Server{
		Addr:              serverConfig.HTTPAddr,
		Handler:           mux,
		ReadTimeout:       runtime.Timeouts.HTTPRead,
		WriteTimeout:      runtime.Timeouts.HTTPWrite,
		IdleTimeout:       runtime.Timeouts.HTTPIdle,
		ReadHeaderTimeout: runtime.Timeouts.HTTPReadHeader,
	}
	var httpsServer *http.Server
	if serverConfig.HTTPSEnabled {
		httpsServer = &http.Server{
			Addr:              serverConfig.HTTPSAddr,
			Handler:           mux,
			ReadTimeout:       runtime.Timeouts.HTTPRead,
			WriteTimeout:      runtime.Timeouts.HTTPWrite,
			IdleTimeout:       runtime.Timeouts.HTTPIdle,
			ReadHeaderTimeout: runtime.Timeouts.HTTPReadHeader,
		}
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), runtime.Timeouts.Shutdown)
		defer cancel()
		hub.Close()
		_ = server.Shutdown(shutdownCtx)
		if httpsServer != nil {
			_ = httpsServer.Shutdown(shutdownCtx)
		}
	}()

	log.Printf("synora API V1 HTTP listening on %s web_root=%s", serverConfig.HTTPAddr, webRoot)
	log.Printf("synora API V1 HTTPS enabled=%t addr=%s cert=%s key=%s", serverConfig.HTTPSEnabled, serverConfig.HTTPSAddr, serverConfig.TLSCertFile, serverConfig.TLSKeyFile)
	errCh := make(chan error, 2)
	go func() {
		if listenErr := server.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", listenErr)
		}
	}()
	if httpsServer != nil {
		go func() {
			if listenErr := httpsServer.ListenAndServeTLS(serverConfig.TLSCertFile, serverConfig.TLSKeyFile); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
				errCh <- fmt.Errorf("https server: %w", listenErr)
			}
		}()
	}
	select {
	case listenErr := <-errCh:
		log.Fatal(listenErr)
	case <-ctx.Done():
	}
}

type apiServerConfig struct {
	HTTPAddr     string
	HTTPSEnabled bool
	HTTPSAddr    string
	TLSCertFile  string
	TLSKeyFile   string
}

func loadAPIServerConfig(runtime runtimeconfig.Config) (apiServerConfig, error) {
	cfg, err := security.Load(runtime.Paths.Security)
	if err != nil {
		return apiServerConfig{}, err
	}
	result := apiServerConfig{
		HTTPAddr:     runtime.Endpoints.HTTP,
		HTTPSEnabled: cfg.Server.HTTPSEnabled,
		HTTPSAddr:    runtime.Endpoints.HTTPS,
		TLSCertFile:  runtime.Paths.TLSCert,
		TLSKeyFile:   runtime.Paths.TLSKey,
	}
	if value := strings.TrimSpace(cfg.Server.HTTPAddr); value != "" && strings.TrimSpace(os.Getenv("SYNORA_HTTP_ADDR")) == "" {
		result.HTTPAddr = value
	}
	if value := strings.TrimSpace(cfg.Server.HTTPSAddr); value != "" && strings.TrimSpace(os.Getenv("SYNORA_HTTPS_ADDR")) == "" {
		result.HTTPSAddr = value
	}
	if value := strings.TrimSpace(cfg.Server.TLSCertFile); value != "" && strings.TrimSpace(os.Getenv("SYNORA_TLS_CERT_FILE")) == "" {
		result.TLSCertFile = value
	}
	if value := strings.TrimSpace(cfg.Server.TLSKeyFile); value != "" && strings.TrimSpace(os.Getenv("SYNORA_TLS_KEY_FILE")) == "" {
		result.TLSKeyFile = value
	}
	if value := strings.TrimSpace(os.Getenv("SYNORA_HTTPS_ENABLED")); value != "" {
		enabled, parseErr := strconv.ParseBool(value)
		if parseErr != nil {
			return apiServerConfig{}, fmt.Errorf("SYNORA_HTTPS_ENABLED: %w", parseErr)
		}
		result.HTTPSEnabled = enabled
	}
	if result.HTTPSEnabled && (!regularFile(result.TLSCertFile) || !regularFile(result.TLSKeyFile)) {
		log.Printf("synora-api HTTPS configured but certificate/key unavailable; HTTP remains available")
		result.HTTPSEnabled = false
	}
	return result, nil
}

func regularFile(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return err == nil && info.Mode().IsRegular()
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"service": "synora-api", "status": "ok"})
}

func staticWebHandler(root string) http.Handler {
	root = filepath.Clean(root)
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// The V1 webapp is a static read-only surface. FileServer performs the
		// path cleaning and refuses traversal outside root. Extensionless paths
		// are sent to the SPA entrypoint so /intelligence works on refresh.
		requested := filepath.Join(root, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(requested); err != nil || info.IsDir() {
			if filepath.Ext(r.URL.Path) == "" {
				r.URL.Path = "/"
			}
		}
		files.ServeHTTP(w, r)
	})
}
