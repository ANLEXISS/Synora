package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
