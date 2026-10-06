package discovery

import "testing"

func TestCameraHealthStatusFailsClosedWithoutLiveCamera(t *testing.T) {
	for _, test := range []struct {
		known, online int
		status        string
	}{
		{0, 0, "unknown"},
		{1, 0, "offline"},
		{2, 1, "degraded"},
		{1, 1, "ok"},
	} {
		status, _ := cameraHealthStatus(test.known, test.online)
		if status != test.status {
			t.Fatalf("cameraHealthStatus(%d,%d)=%q, want %q", test.known, test.online, status, test.status)
		}
	}
}
