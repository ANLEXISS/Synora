package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"synora/pkg/contract"
)

type streamDevicesFake struct{ items []map[string]any }

func (f streamDevicesFake) Devices() ([]map[string]any, error)                   { return f.items, nil }
func (f streamDevicesFake) Device(string) (map[string]any, error)                { return nil, nil }
func (f streamDevicesFake) CreateDevice(json.RawMessage) (map[string]any, error) { return nil, nil }
func (f streamDevicesFake) UpdateDevice(string, json.RawMessage) (map[string]any, error) {
	return nil, nil
}
func (f streamDevicesFake) DeleteDevice(string) (map[string]any, error) { return nil, nil }

func TestStreamDescriptorSeparatesRTSPFromBrowserURLs(t *testing.T) {
	descriptor := streamDescriptor("cam_03")
	if descriptor.RTSPPublishURL != "rtsp://10.77.0.1:8554/cam_03" {
		t.Fatalf("descriptor=%#v", descriptor)
	}
	if descriptor.RTSPPublishURL == descriptor.WebRTCURL || descriptor.RTSPPublishURL == descriptor.HLSURL {
		t.Fatalf("browser URL must not be RTSP: %#v", descriptor)
	}
}

func TestStreamsListsOnlyCamerasAndSupportsDeviceRoute(t *testing.T) {
	handler := handleStreams(streamDevicesFake{items: []map[string]any{{"id": "cam_03", "type": "camera"}, {"id": "sensor_01", "type": "sensor"}}})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/streams", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() == "" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/streams/cam_03", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() == "" {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

type streamHealthFake struct {
	streamDevicesFake
	health *contract.RuntimeHealth
}

func (f streamHealthFake) SystemHealth() (*contract.RuntimeHealth, error) { return f.health, nil }

func TestStreamsExposeReadyAndDegradedWithoutMakingMediaMTXAUnitDependency(t *testing.T) {
	ready := &contract.RuntimeHealth{MediaMTX: contract.RuntimeMediaMTXHealth{Status: "ok"}}
	handler := handleStreams(streamHealthFake{streamDevicesFake: streamDevicesFake{items: []map[string]any{{"id": "cam_03", "type": "camera"}}}, health: ready})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/streams/cam_03", nil))
	var descriptor StreamDescriptor
	if err := json.Unmarshal(recorder.Body.Bytes(), &descriptor); err != nil {
		t.Fatal(err)
	}
	if descriptor.Status != "ready" {
		t.Fatalf("ready descriptor=%#v status=%d", descriptor, recorder.Code)
	}

	degraded := &contract.RuntimeHealth{MediaMTX: contract.RuntimeMediaMTXHealth{Status: "degraded"}, Timestamp: time.Now().UTC()}
	handler = handleStreams(streamHealthFake{streamDevicesFake: streamDevicesFake{items: []map[string]any{{"id": "cam_03", "type": "camera"}}}, health: degraded})
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/streams/cam_03", nil))
	if !strings.Contains(recorder.Body.String(), `"status":"degraded"`) || strings.Contains(recorder.Body.String(), `"live_available":true`) {
		t.Fatalf("degraded descriptor=%s", recorder.Body.String())
	}
}

func TestPublicStreamBaseStripsCredentialsAndQuery(t *testing.T) {
	value := publicStreamBase("rtsp://user:secret@example.test:8554/live?token=hidden")
	if value != "rtsp://example.test:8554/live" || strings.Contains(value, "secret") || strings.Contains(value, "token") {
		t.Fatalf("unsafe public base=%q", value)
	}
}

func TestStreamsHideUnauthorizedCameraURLs(t *testing.T) {
	items := []map[string]any{{"id": "cam_03", "type": "camera"}}
	handler := handleStreamsWithAuthorization(streamDevicesFake{items: items}, func(string, map[string]any) bool { return false })
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/streams/cam_03", nil))
	var descriptor StreamDescriptor
	if err := json.Unmarshal(recorder.Body.Bytes(), &descriptor); err != nil {
		t.Fatal(err)
	}
	if descriptor.Status != "unauthorized" || descriptor.LiveAvailable || descriptor.RTSPPublishURL != "" || descriptor.WebRTCURL != "" || descriptor.HLSURL != "" {
		t.Fatalf("unauthorized stream exposed: %#v", descriptor)
	}
}
