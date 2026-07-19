package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"synora/internal/cge/demo"
)

// The development UI is embedded so the default demonstrator never needs a
// build step, a CDN, a font download, or a network service other than itself.
//
//go:embed web/* scenarios/*.json
var webAssets embed.FS

func main() {
	var options demo.Options
	var port int
	var headless, technical, reset bool
	var exportDir string
	flag.StringVar(&options.Scenario, "scenario", "investor-core", "scenario id")
	flag.Uint64Var(&options.Seed, "seed", 3501, "deterministic seed")
	flag.StringVar(&options.Locale, "locale", "fr", "fr or en")
	flag.IntVar(&port, "port", 8091, "localhost port")
	flag.BoolVar(&headless, "headless", false, "run without starting HTTP server")
	flag.StringVar(&exportDir, "export", "", "write a self-contained static export")
	flag.BoolVar(&reset, "reset", false, "discard the in-memory run and prepare it again")
	flag.BoolVar(&technical, "technical", false, "start the UI in technical mode")
	flag.Parse()
	if options.Locale != "fr" && options.Locale != "en" {
		fatal("--locale must be fr or en")
	}
	library, err := embeddedScenarioLibrary()
	if err != nil {
		fatal(err.Error())
	}
	if _, ok := scenarioExists(options.Scenario); !ok {
		if _, ok := library.Get(options.Scenario); !ok {
			fatal(fmt.Sprintf("unknown scenario %q", options.Scenario))
		}
	}
	if reset {
		options.Seed++
		options.Seed--
	}
	result, err := demo.Run(context.Background(), options)
	if err != nil {
		fatal(err.Error())
	}
	if exportDir != "" {
		if err := exportStatic(exportDir, result, options.Locale, technical); err != nil {
			fatal(err.Error())
		}
	}
	if headless {
		data, err := demo.Encode(result)
		if err != nil {
			fatal(err.Error())
		}
		fmt.Println(string(data))
		return
	}
	live, err := demo.NewLiveSession(context.Background(), demo.LiveOptions{Seed: options.Seed, Locale: options.Locale})
	if err != nil {
		fatal(err.Error())
	}
	live.SetScenarioLibrary(library)
	defer live.Close()
	serve(options, port, result, live, technical)
}

func embeddedScenarioLibrary() (*demo.ScenarioLibrary, error) {
	files := map[string][]byte{}
	entries, err := fs.Glob(webAssets, "scenarios/*.json")
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		data, readErr := webAssets.ReadFile(name)
		if readErr != nil {
			return nil, readErr
		}
		files[name] = data
	}
	return demo.LoadScenarioLibrary(files)
}

func scenarioExists(id string) (demo.ScenarioInfo, bool) {
	for _, item := range demo.Scenarios() {
		if item.ID == id {
			return item, true
		}
	}
	return demo.ScenarioInfo{}, false
}

func serve(options demo.Options, port int, initial demo.RunResult, live *demo.LiveSession, technical bool) {
	assetFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		fatal(err.Error())
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(assetFS)))
	var current = initial
	var mode = technical
	mux.HandleFunc("/presentation", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/?presentation=1", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/api/scenario", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, current) })
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"locale": options.Locale, "technical": mode, "offline": true, "bind": "127.0.0.1"})
	})
	mux.HandleFunc("/api/claims", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, demo.Claims()) })
	mux.HandleFunc("/api/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fresh, runErr := demo.Run(r.Context(), options)
		if runErr != nil {
			http.Error(w, runErr.Error(), http.StatusInternalServerError)
			return
		}
		current = fresh
		writeJSON(w, current)
	})
	mux.HandleFunc("/api/live/state", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, live.State()) })
	mux.HandleFunc("/api/live/scenarios", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, live.ListScenarios())
	})
	mux.HandleFunc("/api/live/scenarios/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/live/scenarios/")
		scenario, ok := live.GetScenario(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, scenario)
	})
	// Scenario commands intentionally return only demonstrator state. Expected
	// properties are compared after real engine output has been produced.
	mux.HandleFunc("/api/live/scenario/load", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid scenario JSON", 400)
			return
		}
		value, err := live.LoadScenario(req.ID)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, value)
	})
	mux.HandleFunc("/api/live/scenario/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		value, err := live.StartScenario(r.Context(), req.ID)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, value)
	})
	mux.HandleFunc("/api/live/scenario/next", scenarioCommand(func(ctx context.Context) (any, error) { return live.ScenarioNext(ctx) }))
	mux.HandleFunc("/api/live/scenario/previous-view", scenarioCommand(func(context.Context) (any, error) { return live.ScenarioPreviousView() }))
	mux.HandleFunc("/api/live/scenario/pause", scenarioCommand(func(context.Context) (any, error) { return live.ScenarioStateAfter(live.ScenarioPause()) }))
	mux.HandleFunc("/api/live/scenario/resume", scenarioCommand(func(context.Context) (any, error) { return live.ScenarioStateAfter(live.ScenarioResume()) }))
	mux.HandleFunc("/api/live/scenario/cancel", scenarioCommand(func(context.Context) (any, error) { return live.ScenarioStateAfter(live.ScenarioCancel()) }))
	mux.HandleFunc("/api/live/scenario/reset", scenarioCommand(func(ctx context.Context) (any, error) {
		err := live.ScenarioReset(ctx)
		return live.ScenarioStateAfter(err)
	}))
	mux.HandleFunc("/api/live/scenario/run-to-end", scenarioCommand(func(ctx context.Context) (any, error) {
		err := live.ScenarioRunToEnd(ctx)
		return live.ScenarioStateAfter(err)
	}))
	mux.HandleFunc("/api/live/scenario/report", scenarioCommand(func(context.Context) (any, error) { return live.ScenarioReport() }))
	mux.HandleFunc("/api/live/scenario/compare", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var req struct {
			LeftID  string `json:"left_id"`
			RightID string `json:"right_id"`
			Seed    uint64 `json:"seed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid comparison JSON", 400)
			return
		}
		if req.Seed == 0 {
			req.Seed = options.Seed
		}
		library, _ := embeddedScenarioLibrary()
		value, err := demo.CompareScenarioRuns(r.Context(), library, req.LeftID, req.RightID, req.Seed)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, value)
	})
	mux.HandleFunc("/api/live/scenario/modify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var req struct {
			StepID string             `json:"step_id"`
			Event  demo.ScenarioEvent `json:"event"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid modification JSON", 400)
			return
		}
		if err := live.ModifyScenarioEvent(req.StepID, req.Event); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, live.ScenarioState())
	})
	mux.HandleFunc("/api/live/scenario/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := live.ImportScenario(data); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, live.ListScenarios())
	})
	mux.HandleFunc("/api/live/scenario/export", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		data, err := live.ExportScenario()
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/api/live/event", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var input demo.LiveEventInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid event JSON", http.StatusBadRequest)
			return
		}
		result, err := live.Submit(input)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("/api/live/batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request demo.LiveBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid batch JSON", http.StatusBadRequest)
			return
		}
		results, err := live.RunBatch(r.Context(), request, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"results": results, "state": live.State()})
	})
	mux.HandleFunc("/api/live/advance-time", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request demo.LiveAdvanceRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid clock JSON", http.StatusBadRequest)
			return
		}
		state, err := live.Advance(request)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, state)
	})
	mux.HandleFunc("/api/live/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := live.Reset(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, live.State())
	})
	mux.HandleFunc("/api/live/restart", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		result, err := live.Restart()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("/api/live/load-baseline", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Days int `json:"days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid baseline JSON", http.StatusBadRequest)
			return
		}
		if err := live.LoadBaseline(request.Days); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, live.State())
	})
	mux.HandleFunc("/api/live/trace", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, live.Trace()) })
	mux.HandleFunc("/api/live/wal", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, live.State().WAL) })
	mux.HandleFunc("/api/live/chains", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, live.State().Chains) })
	mux.HandleFunc("/api/live/hypotheses", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, live.State().Hypotheses) })
	mux.HandleFunc("/api/live/routines", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, live.State().Routines) })
	mux.HandleFunc("/api/live/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}
		ch, cancel := live.Subscribe()
		defer cancel()
		for {
			select {
			case <-r.Context().Done():
				return
			case data, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}
		for _, event := range current.Events {
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if r.Context().Err() != nil {
				return
			}
		}
	})
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	server := &http.Server{Addr: addr, Handler: withHeaders(mux), ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("Synora CGE demo: http://%s/  scénario=%s seed=%d\n", addr, options.Scenario, options.Seed)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fatal(err.Error())
	}
}

func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func scenarioCommand(run func(context.Context) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		value, err := run(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, value)
	}
}

func exportStatic(dir string, result demo.RunResult, locale string, technical bool) error {
	if filepath.IsAbs(dir) == false || filepath.Clean(dir) != dir {
		return fmt.Errorf("export directory must be a clean absolute path")
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o750); err != nil {
		return err
	}
	copyAsset := func(name string) error {
		data, err := webAssets.ReadFile("web/" + name)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, name), data, 0o640)
	}
	for _, name := range []string{"app.js", "live.js", "styles.css", "live.css", "live-extra.css"} {
		if err := copyAsset(name); err != nil {
			return err
		}
	}
	index, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		return err
	}
	resultData, err := demo.Encode(result)
	if err != nil {
		return err
	}
	claimsData, err := json.MarshalIndent(demo.Claims(), "", "  ")
	if err != nil {
		return err
	}
	manifestData, err := json.MarshalIndent(result.Manifest, "", "  ")
	if err != nil {
		return err
	}
	indexText := strings.Replace(string(index), "__SYNORA_EMBEDDED_DATA__", string(resultData), 1)
	indexText = strings.Replace(indexText, "__SYNORA_EMBEDDED_CLAIMS__", string(claimsData), 1)
	indexText = strings.Replace(indexText, "__SYNORA_LOCALE__", locale, 1)
	indexText = strings.Replace(indexText, "__SYNORA_TECHNICAL__", fmt.Sprintf("%t", technical), 1)
	// A static export replays the guided presentation. The live lab remains
	// attached to the local session APIs and is intentionally not faked.
	indexText = strings.Replace(indexText, `new URLSearchParams(window.location.search).get("presentation") === "1"`, "true", 1)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(indexText), 0o640); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "scenario.json"), resultData, 0o640); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "claims.json"), claimsData, 0o640); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestData, 0o640); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
