package situationfacts

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ProvenanceRef struct {
	SourceKind string
	SourceID   string

	SourceRevision uint64
	ObservedAt     time.Time

	AlgorithmID      string
	AlgorithmVersion string
}

func (p ProvenanceRef) Validate(max int) error {
	if !validText(p.SourceKind, false, max) || !validText(p.SourceID, false, max) || !validText(p.AlgorithmID, true, max) || !validText(p.AlgorithmVersion, true, max) || p.ObservedAt.IsZero() || p.ObservedAt.Location() != time.UTC {
		return fmt.Errorf("%w: provenance", ErrInvalidFact)
	}
	return nil
}

func (p ProvenanceRef) Compare(other ProvenanceRef) int {
	if result := strings.Compare(p.SourceKind, other.SourceKind); result != 0 {
		return result
	}
	if result := strings.Compare(p.SourceID, other.SourceID); result != 0 {
		return result
	}
	if result := compareRevisionText(p.SourceRevision, other.SourceRevision); result != 0 {
		return result
	}
	leftTime, rightTime := p.ObservedAt.UTC().Round(0), other.ObservedAt.UTC().Round(0)
	if leftTime.Before(rightTime) {
		return -1
	}
	if leftTime.After(rightTime) {
		return 1
	}
	if result := strings.Compare(p.AlgorithmID, other.AlgorithmID); result != 0 {
		return result
	}
	return strings.Compare(p.AlgorithmVersion, other.AlgorithmVersion)
}

// compareRevisionText preserves the historical lexical comparison of the
// decimal SourceRevision field without allocating formatted strings.
func compareRevisionText(left, right uint64) int {
	leftText, rightText := strconv.FormatUint(left, 10), strconv.FormatUint(right, 10)
	if len(leftText) != len(rightText) {
		if len(leftText) < len(rightText) {
			return -1
		}
		return 1
	}
	return strings.Compare(leftText, rightText)
}

func cloneProvenance(values []ProvenanceRef) []ProvenanceRef {
	return append([]ProvenanceRef(nil), values...)
}

func canonicalProvenance(values []ProvenanceRef) []ProvenanceRef {
	if provenanceAlreadyCanonical(values) {
		return values
	}
	out := append([]ProvenanceRef(nil), values...)
	for i := range out {
		out[i].ObservedAt = out[i].ObservedAt.UTC().Round(0)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Compare(out[j]) < 0 })
	result := out[:0]
	for _, value := range out {
		if len(result) == 0 || value.Compare(result[len(result)-1]) != 0 {
			result = append(result, value)
		}
	}
	return result
}

func mergeCanonicalProvenance(left, right []ProvenanceRef) []ProvenanceRef {
	if len(right) == 0 {
		return left
	}
	if len(left) == 0 {
		return right
	}
	leftIndex, rightIndex := 0, 0
	for leftIndex < len(left) && rightIndex < len(right) {
		comparison := left[leftIndex].Compare(right[rightIndex])
		if comparison == 0 {
			leftIndex++
			rightIndex++
			continue
		}
		if comparison < 0 {
			leftIndex++
		} else {
			return mergeCanonicalProvenanceSlow(left, right)
		}
	}
	if rightIndex == len(right) {
		return left
	}
	return mergeCanonicalProvenanceSlow(left, right)
}

func mergeCanonicalProvenanceSlow(left, right []ProvenanceRef) []ProvenanceRef {
	merged := make([]ProvenanceRef, 0, len(left)+len(right))
	leftIndex, rightIndex := 0, 0
	for leftIndex < len(left) || rightIndex < len(right) {
		if leftIndex == len(left) {
			merged = appendUniqueProvenance(merged, right[rightIndex])
			rightIndex++
			continue
		}
		if rightIndex == len(right) {
			merged = appendUniqueProvenance(merged, left[leftIndex])
			leftIndex++
			continue
		}
		if left[leftIndex].Compare(right[rightIndex]) <= 0 {
			merged = appendUniqueProvenance(merged, left[leftIndex])
			leftIndex++
		} else {
			merged = appendUniqueProvenance(merged, right[rightIndex])
			rightIndex++
		}
	}
	return merged
}

func appendUniqueProvenance(values []ProvenanceRef, value ProvenanceRef) []ProvenanceRef {
	if len(values) == 0 || values[len(values)-1].Compare(value) != 0 {
		return append(values, value)
	}
	return values
}

func provenanceAlreadyCanonical(values []ProvenanceRef) bool {
	for i, value := range values {
		canonicalTime := value.ObservedAt.UTC().Round(0)
		if value.ObservedAt.Location() != time.UTC || value.ObservedAt != canonicalTime || i > 0 && values[i-1].Compare(value) >= 0 {
			return false
		}
	}
	return true
}
