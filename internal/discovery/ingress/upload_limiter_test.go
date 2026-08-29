package ingress

import "testing"

func TestUploadLimiterReservesCapacityPerCamera(t *testing.T) {
	limiter := newUploadLimiter()
	if !limiter.tryAcquire("cam-a") || !limiter.tryAcquire("cam-b") || !limiter.tryAcquire("cam-c") {
		t.Fatal("three distinct cameras should receive one upload slot each")
	}
	if limiter.tryAcquire("cam-a") || limiter.tryAcquire("cam-d") {
		t.Fatal("a camera or the global budget exceeded its bound")
	}
	limiter.release("cam-b")
	if !limiter.tryAcquire("cam-d") {
		t.Fatal("capacity did not return after release")
	}
	if limiter.active != 3 || len(limiter.perCamera) != 3 {
		t.Fatalf("limiter state active=%d cameras=%d", limiter.active, len(limiter.perCamera))
	}
}
