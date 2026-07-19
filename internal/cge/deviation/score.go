package deviation

import "fmt"

// Score is a deterministic bounded deviation index. Zero means no measured
// deviation from a comparison baseline; 1000 is the maximum index.
type Score uint16

const MaxScore Score = 1000

func NewScore(value int) (Score, error) {
	if value < 0 || value > int(MaxScore) {
		return 0, fmt.Errorf("%w: %d", ErrInvalidDeviationScore, value)
	}
	return Score(value), nil
}

func (s Score) Validate() error {
	if s > MaxScore {
		return fmt.Errorf("%w: %d", ErrInvalidDeviationScore, s)
	}
	return nil
}

func clampScore(value int64) Score {
	if value <= 0 {
		return 0
	}
	if value >= int64(MaxScore) {
		return MaxScore
	}
	return Score(value)
}

func roundedRatio(numerator, denominator int64) Score {
	if denominator <= 0 {
		return 0
	}
	return clampScore((numerator*int64(MaxScore) + denominator/2) / denominator)
}

func weightedScore(score Score, weight Score) int64 {
	return int64(score) * int64(weight)
}
