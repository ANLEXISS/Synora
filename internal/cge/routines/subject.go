package routines

import (
	"fmt"
	"strings"

	"synora/internal/cge/chains"
)

type SubjectKind string

const (
	SubjectEntity SubjectKind = "entity"
	SubjectChain  SubjectKind = "chain"
)

type Subject struct {
	Kind     SubjectKind
	EntityID string
	ChainID  chains.ChainID
}

func SubjectFromObservation(observation chains.ObservationRef, chainID chains.ChainID) (Subject, error) {
	if err := observation.Validate(); err != nil {
		return Subject{}, fmt.Errorf("%w: observation: %v", ErrInvalidSubject, err)
	}
	if strings.TrimSpace(observation.EntityID) != "" {
		if !validText(observation.EntityID, 256) {
			return Subject{}, fmt.Errorf("%w: entity id", ErrInvalidSubject)
		}
		return Subject{Kind: SubjectEntity, EntityID: observation.EntityID}, nil
	}
	if _, err := chains.NewChainID(string(chainID)); err != nil {
		return Subject{}, fmt.Errorf("%w: chain id: %v", ErrInvalidSubject, err)
	}
	return Subject{Kind: SubjectChain, ChainID: chainID}, nil
}

func (s Subject) Validate() error {
	switch s.Kind {
	case SubjectEntity:
		if !validText(s.EntityID, 256) || s.ChainID != "" {
			return fmt.Errorf("%w: entity subject", ErrInvalidSubject)
		}
	case SubjectChain:
		if s.EntityID != "" {
			return fmt.Errorf("%w: chain subject contains entity", ErrInvalidSubject)
		}
		if _, err := chains.NewChainID(string(s.ChainID)); err != nil {
			return fmt.Errorf("%w: chain subject: %v", ErrInvalidSubject, err)
		}
	default:
		return fmt.Errorf("%w: unknown subject kind %q", ErrInvalidSubject, s.Kind)
	}
	return nil
}

func (s Subject) key() string {
	return string(s.Kind) + "\x00" + s.EntityID + "\x00" + string(s.ChainID)
}
