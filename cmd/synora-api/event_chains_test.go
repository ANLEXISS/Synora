package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeEventChainsProvider struct {
	chains map[string]any
	chain  map[string]any
}

func (f fakeEventChainsProvider) EventChains(map[string]any) (map[string]any, error) {
	return f.chains, nil
}

func (f fakeEventChainsProvider) EventChain(string) (map[string]any, error) {
	return f.chain, nil
}

func TestEventChainsAPIListAndDetail(t *testing.T) {
	provider := fakeEventChainsProvider{
		chains: map[string]any{"chains": []map[string]any{{"id": "chain-1", "status": "open"}}, "generated_at": "now"},
		chain:  map[string]any{"id": "chain-1", "recent_events": []any{}, "evaluations": []any{}},
	}
	list := httptest.NewRecorder()
	handleEventChains(provider).ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/events/chains?status=open", nil))
	if list.Code != http.StatusOK || list.Body.String() == "" {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	detail := httptest.NewRecorder()
	handleEventChain(provider).ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/api/events/chains/chain-1", nil))
	if detail.Code != http.StatusOK || detail.Body.String() == "" {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
}
