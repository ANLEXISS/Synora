package mediamtx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeController struct {
	paths   []string
	added   []string
	removed []string
	err     error
}

func (f *fakeController) ListPaths(context.Context) ([]string, error) {
	return append([]string(nil), f.paths...), f.err
}
func (f *fakeController) AddPath(_ context.Context, path string) error {
	if f.err != nil {
		return f.err
	}
	f.added = append(f.added, path)
	return nil
}
func (f *fakeController) DeletePath(_ context.Context, path string) error {
	if f.err != nil {
		return f.err
	}
	f.removed = append(f.removed, path)
	return nil
}

func TestDesiredPathsDeduplicatesAndSortsCameraIDs(t *testing.T) {
	paths, err := DesiredPaths([]string{"cam_02", "cam_01", "cam_02"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"cam_01", "cam_02"}) {
		t.Fatalf("paths=%v", paths)
	}
	if _, err := DesiredPaths([]string{"../secret"}); err == nil {
		t.Fatal("unsafe path accepted")
	}
}

func TestReconcileAddsRemovesAndIsIdempotent(t *testing.T) {
	controller := &fakeController{paths: []string{"cam_old", "cam_02", "cam_02"}}
	report, err := Reconcile(context.Background(), controller, []string{"cam_01", "cam_02"}, time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Ready || report.Status != "ready" || !reflect.DeepEqual(controller.added, []string{"cam_01"}) || !reflect.DeepEqual(controller.removed, []string{"cam_old"}) {
		t.Fatalf("report=%#v controller=%#v", report, controller)
	}
	if _, err := Reconcile(context.Background(), controller, []string{"cam_01", "cam_02"}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileReportsDegradedOnTimeoutOrControllerFailure(t *testing.T) {
	controller := &fakeController{err: errors.New("timeout")}
	report, err := Reconcile(context.Background(), controller, []string{"cam_01"}, time.Now())
	if err == nil || report.Ready || report.Status != "degraded" || report.Error != "timeout" {
		t.Fatalf("report=%#v err=%v", report, err)
	}
}

func TestRenderConfigRemovesWildcardAndKeepsOnlyAuthorizedPaths(t *testing.T) {
	base := []byte("api: true\npaths:\n  all_others:\n    source: publisher\n")
	data, err := RenderConfig(base, []string{"cam_02", "cam_01"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if containsAny(text, "all_others", "secret", "password") || !containsAny(text, "cam_01", "cam_02") {
		t.Fatalf("rendered config=%s", text)
	}
}

func TestHTTPClientUsesMediaMTXAPIWithoutLoggingOrAcceptingSecrets(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/v3/paths/list":
			_, _ = w.Write([]byte(`{"items":[{"name":"cam_old"}]}`))
		case "/v3/config/paths/add/cam_new", "/v3/config/paths/delete/cam_old":
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	paths, err := client.ListPaths(context.Background())
	if err != nil || !reflect.DeepEqual(paths, []string{"cam_old"}) {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
	if err := client.AddPath(context.Background(), "cam_new"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeletePath(context.Background(), "cam_old"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"GET /v3/paths/list", "POST /v3/config/paths/add/cam_new", "DELETE /v3/config/paths/delete/cam_old"}) {
		t.Fatalf("API calls=%v", calls)
	}
	if _, err := NewClient("http://user:secret@127.0.0.1:9997", nil); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("credential-bearing URL was accepted or leaked: %v", err)
	}
}

func TestHTTPClientHonorsContextTimeoutAndNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/paths/list" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err = client.ListPaths(ctx)
	if err == nil || !strings.Contains(err.Error(), "MediaMTX request failed") {
		t.Fatalf("timeout err=%v", err)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
