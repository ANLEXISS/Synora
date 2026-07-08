package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"synora/internal/bus"
	"synora/internal/coreclient"
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

func main() {

	addr := ":8080"

	busClient, err := bus.NewClient(
		getenv("SYNORA_BUS", "/run/synora/bus.sock"),
		"api",
	)

	if err != nil {
		log.Fatal(err)
	}

	core := coreclient.New(busClient)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/state", handleState(core))
	mux.HandleFunc("/api/devices", handleDevices(core))
	mux.HandleFunc("/api/devices/", handleDevice(core))
	mux.HandleFunc("/api/topology", handleTopology(core))
	mux.HandleFunc("/api/topology/reset", handleTopologyReset(core))
	mux.HandleFunc("/api/system/health", handleSystemHealth(core))
	mux.HandleFunc(
		"/api/snapshot",
		handleSnapshot(core),
	)
	mux.HandleFunc("/", handleIndex)

	server := &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(corsMiddleware(mux)),
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

func corsMiddleware(next http.Handler) http.Handler {

	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {

		w.Header().Set(
			"Access-Control-Allow-Origin",
			"*",
		)

		w.Header().Set(
			"Access-Control-Allow-Headers",
			"Content-Type, Authorization",
		)

		w.Header().Set(
			"Access-Control-Allow-Methods",
			"GET, POST, PUT, DELETE, OPTIONS",
		)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
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
			body, err := io.ReadAll(r.Body)
			if err != nil {
				writeError(w, err)
				return
			}

			devices, err := core.UpdateDevice(id, json.RawMessage(body))
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

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, err)
			return
		}

		topology, err := core.ResetTopology(json.RawMessage(body))
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

	w.WriteHeader(http.StatusMethodNotAllowed)
	return false
}

func writeError(
	w http.ResponseWriter,
	err error,
) {

	writeJSON(
		w,
		http.StatusInternalServerError,
		map[string]any{
			"error": err.Error(),
		},
	)
}
