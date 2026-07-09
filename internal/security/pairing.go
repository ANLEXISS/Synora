package security

import (
	"errors"
	"strings"
	"sync"
	"time"
)

type PairingService struct {
	Path string
	Now  func() time.Time

	mu       sync.Mutex
	sessions map[string]time.Time
}

type PairingStartResponse struct {
	PairingID string    `json:"pairing_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PairingCompleteRequest struct {
	PairingID string `json:"pairing_id"`
	DeviceID  string `json:"device_id"`
}

type PairingCompleteResponse struct {
	DeviceID   string `json:"device_id"`
	Secret     string `json:"secret"`
	SecretHash string `json:"secret_hash"`
}

func (s *PairingService) Start() (*PairingStartResponse, error) {
	cfg, err := Load(s.Path)
	if err != nil {
		return nil, err
	}
	if !cfg.PairingEnabled {
		return nil, errors.New("pairing disabled")
	}

	id, err := RandomHex(defaultPairingIDByteCount)
	if err != nil {
		return nil, err
	}
	now := s.now()
	response := &PairingStartResponse{
		PairingID: id,
		ExpiresAt: now.Add(defaultPairingTTL),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string]time.Time{}
	}
	s.sessions[id] = response.ExpiresAt
	return response, nil
}

func (s *PairingService) Complete(req PairingCompleteRequest) (*PairingCompleteResponse, error) {
	req.PairingID = strings.TrimSpace(req.PairingID)
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	if req.PairingID == "" {
		return nil, errors.New("pairing_id is required")
	}
	if req.DeviceID == "" {
		return nil, errors.New("device_id is required")
	}

	cfg, err := Load(s.Path)
	if err != nil {
		return nil, err
	}
	if !cfg.PairingEnabled {
		return nil, errors.New("pairing disabled")
	}

	s.mu.Lock()
	expiresAt, ok := s.sessions[req.PairingID]
	if !ok {
		s.mu.Unlock()
		return nil, errors.New("pairing session not found")
	}
	if s.now().After(expiresAt) {
		delete(s.sessions, req.PairingID)
		s.mu.Unlock()
		return nil, errors.New("pairing session expired")
	}
	delete(s.sessions, req.PairingID)
	s.mu.Unlock()

	secret, err := RandomHex(defaultSecretByteCount)
	if err != nil {
		return nil, err
	}
	secretHash := HashSecret(secret)

	cfg.DeviceSecrets[req.DeviceID] = secretHash
	if err := Save(s.Path, cfg); err != nil {
		return nil, err
	}

	return &PairingCompleteResponse{
		DeviceID:   req.DeviceID,
		Secret:     secret,
		SecretHash: secretHash,
	}, nil
}

func (s *PairingService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
