package whatsapp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"synora/internal/actions"
	"synora/pkg/contract"
)

type fakeHTTP struct {
	status  int
	request *http.Request
}

func (f *fakeHTTP) Do(request *http.Request) (*http.Response, error) {
	f.request = request
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
}

func TestDisabledProviderIsSkipped(t *testing.T) {
	result, err := (Adapter{Config: Config{Provider: "cloud_api"}}).Execute(context.Background(), contract.ActionRequest{Type: "notify.whatsapp"})
	if err != nil || result.Status != actions.StatusSkipped || result.Details["reason"] != "provider_disabled" {
		t.Fatalf("unexpected result=%#v err=%v", result, err)
	}
}

func TestDryRunDoesNotCallNetwork(t *testing.T) {
	fake := &fakeHTTP{status: 200}
	result, err := (Adapter{Config: Config{Enabled: true, DryRun: true, DefaultTo: "+33123456789", DefaultTemplate: "synora_security_alert"}, Client: fake}).Execute(context.Background(), contract.ActionRequest{Type: "notify.whatsapp", Data: map[string]any{"message": "test"}})
	if err != nil || result.Status != actions.StatusSimulatedSuccess || fake.request != nil {
		t.Fatalf("unexpected dry-run result=%#v err=%v request=%v", result, err, fake.request)
	}
	if strings.Contains(result.Details["to"].(string), "33123456789") {
		t.Fatal("recipient was not masked")
	}
}

func TestMissingTokenFailsWithoutSecret(t *testing.T) {
	t.Setenv("SYNORA_WHATSAPP_ACCESS_TOKEN", "")
	result, err := (Adapter{Config: Config{Enabled: true, DryRun: false, DefaultTo: "+33123456789", PhoneNumberID: "phone", AccessTokenFile: t.TempDir() + "/missing"}}).Execute(context.Background(), contract.ActionRequest{Type: "notify.whatsapp"})
	if err != nil || result.Status != actions.StatusError || result.Details["reason"] != "config_missing" {
		t.Fatalf("unexpected result=%#v err=%v", result, err)
	}
}

func TestHTTPStatusIsReturned(t *testing.T) {
	for _, status := range []int{200, 400} {
		fake := &fakeHTTP{status: status}
		adapter := Adapter{Config: Config{Enabled: true, DryRun: false, DefaultTo: "+33123456789", PhoneNumberID: "phone", AccessTokenFile: "unused", BaseURL: "https://graph.example"}, Client: fake}
		t.Setenv("SYNORA_WHATSAPP_ACCESS_TOKEN", "test-secret")
		result, err := adapter.Execute(context.Background(), contract.ActionRequest{Type: "notify.whatsapp", Data: map[string]any{"template": "synora_security_alert"}})
		if err != nil {
			t.Fatal(err)
		}
		want := actions.StatusSuccess
		if status >= 300 {
			want = actions.StatusError
		}
		if result.Status != want {
			t.Fatalf("status %d result=%#v", status, result)
		}
		if fake.request != nil && fake.request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("authorization header missing")
		}
	}
}
