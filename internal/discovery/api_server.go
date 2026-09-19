package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"synora/internal/bus"
	"synora/internal/runtimeconfig"
	"synora/internal/security"
	"synora/pkg/contract"
)

type busEventPublisher struct{ client *bus.Client }

func (p busEventPublisher) Publish(event contract.Event) error {
	body, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}
	return p.client.Send(contract.Message{ID: event.ID, Type: event.Type, Kind: contract.KindEvent, Source: "discovery", Target: "core", Timestamp: event.Timestamp, Priority: event.Priority, Payload: body})
}

type externalAPIServer struct {
	http  *http.Server
	https *http.Server
}

func startExternalAPIServer(runtime runtimeconfig.Config, cfg *security.Config, api http.Handler) *externalAPIServer {
	if api == nil {
		return nil
	}
	server := &externalAPIServer{}
	server.http = &http.Server{Addr: runtime.Endpoints.HTTP, Handler: api, ReadTimeout: runtime.Timeouts.HTTPRead, WriteTimeout: runtime.Timeouts.HTTPWrite, IdleTimeout: runtime.Timeouts.HTTPIdle, ReadHeaderTimeout: runtime.Timeouts.HTTPReadHeader}
	go func() { _ = server.http.ListenAndServe() }()
	if cfg != nil && cfg.Server.HTTPSEnabled && regularFile(runtime.Paths.TLSCert) && regularFile(runtime.Paths.TLSKey) {
		server.https = &http.Server{Addr: runtime.Endpoints.HTTPS, Handler: api, ReadTimeout: runtime.Timeouts.HTTPRead, WriteTimeout: runtime.Timeouts.HTTPWrite, IdleTimeout: runtime.Timeouts.HTTPIdle, ReadHeaderTimeout: runtime.Timeouts.HTTPReadHeader}
		go func() { _ = server.https.ListenAndServeTLS(runtime.Paths.TLSCert, runtime.Paths.TLSKey) }()
	}
	return server
}

func (s *externalAPIServer) shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.https != nil {
		_ = s.https.Shutdown(ctx)
	}
	if s.http != nil {
		return s.http.Shutdown(ctx)
	}
	return nil
}

func NewExternalAPI(cfg *security.Config, boundary *Boundary, publisher EventPublisher, updates EventSubscription, health func() map[string]any) http.Handler {
	api := (&WebAPI{Boundary: boundary, Publisher: publisher, Updates: updates, Health: health}).Handler()
	return externalMiddleware(cfg, api)
}

func externalMiddleware(cfg *security.Config, api http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/health" || r.URL.Path == "/health/live" || (r.URL.Path == "/api/v1/health" && cfg != nil && cfg.PublicSystemHealth) {
			writeDiscoveryHealth(w, r)
			return
		}
		if !validDiscoveryToken(cfg, r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		api.ServeHTTP(w, r)
	})
}

func validDiscoveryToken(cfg *security.Config, r *http.Request) bool {
	if cfg == nil || r == nil {
		return false
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return cfg.VerifyAPIToken(strings.TrimSpace(value[len("Bearer "):]))
	}
	return false
}

func writeDiscoveryHealth(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{"service": "discovery", "status": "ok", "timestamp": time.Now().UTC()}
	if r.URL.Path == "/api/v1/health" {
		status["api_owner"] = "discovery"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func regularFile(path string) bool {
	info, err := os.Stat(strings.TrimSpace(path))
	return err == nil && info.Mode().IsRegular()
}
