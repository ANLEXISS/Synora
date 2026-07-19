package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithFeatureDisabledReturnsNotFound(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/lab/validation/history", nil)
	recorder := httptest.NewRecorder()

	withFeature(false, "synora_lab_enabled", func(http.ResponseWriter, *http.Request) {
		t.Fatal("disabled feature must not invoke the handler")
	})(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestWithFeatureEnabledCallsHandler(t *testing.T) {
	called := false
	req := httptest.NewRequest(http.MethodGet, "/api/lab/validation/history", nil)
	recorder := httptest.NewRecorder()

	withFeature(true, "synora_lab_enabled", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})(recorder, req)

	if !called {
		t.Fatal("enabled feature must invoke the handler")
	}
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}
