package ingress

import (
	"strings"
	"sync"

	"synora/internal/resourcebudget"
)

// uploadLimiter bounds both total disk work and work per camera. Rejecting a
// fourth upload is deliberate: a slow or hostile camera must not occupy all
// upload capacity and starve the other cameras.
type uploadLimiter struct {
	mu        sync.Mutex
	active    int
	perCamera map[string]int
	globalMax int
	cameraMax int
}

func newUploadLimiter() *uploadLimiter {
	return &uploadLimiter{
		perCamera: make(map[string]int),
		globalMax: resourcebudget.MaxConcurrentClipUploads,
		cameraMax: resourcebudget.MaxClipUploadsPerCamera,
	}
}

func (l *uploadLimiter) tryAcquire(cameraID string) bool {
	if l == nil || strings.TrimSpace(cameraID) == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active >= l.globalMax || l.perCamera[cameraID] >= l.cameraMax {
		return false
	}
	l.active++
	l.perCamera[cameraID]++
	return true
}

func (l *uploadLimiter) release(cameraID string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active > 0 {
		l.active--
	}
	if count := l.perCamera[cameraID]; count <= 1 {
		delete(l.perCamera, cameraID)
	} else {
		l.perCamera[cameraID] = count - 1
	}
}
