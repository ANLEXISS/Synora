package rpc

import (
	"log"
	"strings"

	"synora/internal/state"
	"synora/pkg/contract"
)

const incidentListLimitMax = 100

func (s *Server) incidentsList(msg contract.Message) (any, error) {
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
		limit = state.DefaultIncidentListLimit
	}
	if limit < 0 || limit > incidentListLimitMax {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "incident limit must be between 1 and 100")
	}
	return s.state.IncidentsList(limit), nil
}

func (s *Server) incidentGet(msg contract.Message) (any, error) {
	var request cgeIDRequest
	if err := decodePayload(msg.Payload, &request); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(request.ID)
	if id == "" {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "incident id is required")
	}
	value, ok := s.state.Incident(id)
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "incident not found")
	}
	return value, nil
}

func (s *Server) incidentMarkViewed(msg contract.Message) (any, error) {
	var request cgeIDRequest
	if err := decodePayload(msg.Payload, &request); err != nil {
		return nil, err
	}
	value, changed, err := s.state.MarkIncidentViewed(request.ID)
	if err != nil {
		return nil, err
	}
	if changed {
		if s.publishEvent != nil {
			s.publishEvent("incident.updated", contract.RealtimeIncidentUpdatedPayload{
				IncidentID: value.ID, Revision: value.Revision, Status: value.Status,
				Reason: "viewed", Incident: value,
			}, contract.PriorityNormal)
		}
		log.Printf("core: incident viewed id=%s", value.ID)
	}
	return value, nil
}

func (s *Server) incidentAcknowledge(msg contract.Message) (any, error) {
	var request cgeIDRequest
	if err := decodePayload(msg.Payload, &request); err != nil {
		return nil, err
	}
	value, changed, err := s.state.AcknowledgeIncident(request.ID)
	if err != nil {
		return nil, err
	}
	if changed {
		if s.publishEvent != nil {
			s.publishEvent("incident.updated", contract.RealtimeIncidentUpdatedPayload{
				IncidentID: value.ID, Revision: value.Revision, Status: value.Status,
				Reason: "acknowledged", Incident: value,
			}, contract.PriorityHigh)
		}
		log.Printf("core: incident acknowledged id=%s", value.ID)
	}
	return value, nil
}

func (s *Server) incidentResolve(msg contract.Message) (any, error) {
	var request cgeIDRequest
	if err := decodePayload(msg.Payload, &request); err != nil {
		return nil, err
	}
	value, changed, err := s.state.ResolveIncident(request.ID)
	if err != nil {
		return nil, err
	}
	if changed && s.publishEvent != nil {
		s.publishEvent("incident.updated", contract.RealtimeIncidentUpdatedPayload{
			IncidentID: value.ID, Revision: value.Revision, Status: value.Status,
			Reason: "resolved", Incident: value,
		}, contract.PriorityHigh)
	}
	return value, nil
}
