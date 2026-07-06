package graph

import "math"

func calculateWeight(
	count int,
) float64 {

	x := float64(count)

	return 1.0 -
		math.Exp(-x/10.0)
}