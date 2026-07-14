package main

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	"synora/internal/discovery/network"
)

type StreamDescriptor struct {
	DeviceID       string `json:"device_id"`
	RTSPPublishURL string `json:"rtsp_publish_url"`
	WebRTCURL      string `json:"webrtc_url,omitempty"`
	HLSURL         string `json:"hls_url,omitempty"`
	Status         string `json:"status"`
	LiveAvailable  bool   `json:"live_available"`
}

func handleStreams(core deviceConfigurationProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		items, err := core.Devices()
		if err != nil {
			writeError(w, err)
			return
		}
		pathID := strings.TrimPrefix(r.URL.Path, "/api/streams/")
		if pathID == r.URL.Path {
			pathID = ""
		}
		if pathID != "" {
			pathID, err = url.PathUnescape(strings.TrimSuffix(pathID, "/"))
			if err != nil {
				writeError(w, err)
				return
			}
		}
		result := make([]StreamDescriptor, 0)
		for _, item := range items {
			id := streamStringValue(item["id"])
			if id == "" || (pathID == "" && !isCameraDevice(item)) || (pathID != "" && id != pathID) {
				continue
			}
			result = append(result, streamDescriptor(id))
		}
		if pathID != "" && len(result) == 0 {
			http.NotFound(w, r)
			return
		}
		if pathID != "" {
			writeJSON(w, http.StatusOK, result[0])
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func isCameraDevice(item map[string]any) bool {
	for _, key := range []string{"type", "device_type", "role"} {
		if value := strings.ToLower(streamStringValue(item[key])); value == "camera" || strings.Contains(value, "camera") {
			return true
		}
	}
	return false
}

func streamDescriptor(deviceID string) StreamDescriptor {
	cfg, _ := network.LoadConfig(os.Getenv("SYNORA_NETWORK_CONFIG"))
	baseRTSP := cfg.SynoraNet.Services.RTSPURL
	if baseRTSP == "" {
		baseRTSP = "rtsp://10.77.0.1:8554"
	}
	baseWebRTC := cfg.SynoraNet.Services.WebRTCBaseURL
	baseHLS := cfg.SynoraNet.Services.HLSBaseURL
	if value := strings.TrimSpace(os.Getenv("SYNORA_WEBRTC_BASE_URL")); value != "" {
		baseWebRTC = value
	}
	if value := strings.TrimSpace(os.Getenv("SYNORA_HLS_BASE_URL")); value != "" {
		baseHLS = value
	}
	path := url.PathEscape(deviceID)
	descriptor := StreamDescriptor{DeviceID: deviceID, RTSPPublishURL: strings.TrimRight(baseRTSP, "/") + "/" + path, Status: "unknown"}
	if baseWebRTC != "" {
		descriptor.WebRTCURL = strings.TrimRight(baseWebRTC, "/") + "/" + path + "/whep"
	}
	if baseHLS != "" {
		descriptor.HLSURL = strings.TrimRight(baseHLS, "/") + "/" + path + "/index.m3u8"
	}
	descriptor.LiveAvailable = descriptor.WebRTCURL != "" || descriptor.HLSURL != ""
	return descriptor
}

func streamStringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}
