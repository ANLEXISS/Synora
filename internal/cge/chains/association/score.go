package association

import (
	"fmt"
	"strings"

	"synora/internal/cge/chains"
)

// ScoreFact is one stable, explainable component of a candidate score.
type ScoreFact struct {
	Code   string
	Score  int64
	Detail string
}

// CandidateScore is a detached explanation of one chain's compatibility.
type CandidateScore struct {
	ChainID        chains.ChainID
	SourceRevision uint64
	Status         chains.Status

	Eligible bool
	Score    int64

	RejectionCode string
	Facts         []ScoreFact
}

func (f ScoreFact) validate() error {
	if strings.TrimSpace(f.Code) == "" || strings.ContainsAny(f.Code, "\r\n") || len([]rune(f.Code)) > 64 {
		return fmt.Errorf("score fact code is invalid")
	}
	if f.Score < 0 || strings.ContainsAny(f.Detail, "\r\n") || len([]rune(f.Detail)) > 256 {
		return fmt.Errorf("score fact is invalid")
	}
	return nil
}

func (c CandidateScore) validate() error {
	if _, err := chains.NewChainID(string(c.ChainID)); err != nil || c.SourceRevision == 0 || c.Status.Validate() != nil || c.Score < 0 {
		return fmt.Errorf("candidate score identity is invalid")
	}
	if !c.Eligible && strings.TrimSpace(c.RejectionCode) == "" {
		return fmt.Errorf("ineligible candidate must have a rejection code")
	}
	if c.Eligible && c.RejectionCode != "" {
		return fmt.Errorf("eligible candidate must not have a rejection code")
	}
	var total int64
	for _, fact := range c.Facts {
		if err := fact.validate(); err != nil {
			return err
		}
		total += fact.Score
	}
	if total != c.Score {
		return fmt.Errorf("candidate score does not equal fact sum")
	}
	return nil
}

func (c CandidateScore) clone() CandidateScore {
	c.Facts = append([]ScoreFact(nil), c.Facts...)
	return c
}
