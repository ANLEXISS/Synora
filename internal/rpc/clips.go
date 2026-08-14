package rpc

import (
	"strings"

	"synora/internal/state"
	"synora/pkg/contract"
)

func (s *Server) clipsList(msg contract.Message) (any, error) {
	var request struct {
		Limit int `json:"limit"`
	}
	if len(msg.Payload) > 0 {
		if err := decodePayload(msg.Payload, &request); err != nil {
			return nil, err
		}
	}
	limit := request.Limit
	if limit == 0 {
		limit = state.DefaultClipListLimit
	}
	if limit < 1 || limit > state.MaxClipListLimit {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "clip limit must be between 1 and 100")
	}
	values := s.state.ClipsList(limit)
	for index := range values {
		values[index].Path = ""
	}
	return values, nil
}

func (s *Server) clipGet(msg contract.Message) (any, error) {
	var request cgeIDRequest
	if err := decodePayload(msg.Payload, &request); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(request.ID)
	if id == "" {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "clip id is required")
	}
	value, ok := s.state.Clip(id)
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "clip not found")
	}
	public := contract.Clip(*value)
	public.Path = ""
	return public, nil
}
