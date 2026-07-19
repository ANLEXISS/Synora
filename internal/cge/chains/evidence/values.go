package evidence

import (
	"fmt"
	"math"
)

// ResolutionValues are the fixed contribution values captured when an
// evaluation is produced. They are separate from evidence scores and are
// never derived from the chain confidence.
type ResolutionValues struct {
	SupportValue       float64
	ContradictionValue float64
	NeutralValue       float64
}

func (v ResolutionValues) Validate() error {
	for name, value := range map[string]float64{
		"support":       v.SupportValue,
		"contradiction": v.ContradictionValue,
		"neutral":       v.NeutralValue,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return fmt.Errorf("resolution value %s is outside [0,1]", name)
		}
	}
	if v.NeutralValue != 0 {
		return fmt.Errorf("neutral resolution value must be zero")
	}
	return nil
}
