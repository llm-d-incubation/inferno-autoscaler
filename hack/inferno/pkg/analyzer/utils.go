package analyzer

import (
	"fmt"
	"math"
)

var epsilon float32 = 1e-6
var maxIterations int = 100

// A variable x is relatively within a given tolerance from a value
func WithinTolerance(x, value, tolerance float32) bool {
	if x == value {
		return true
	}
	if value == 0 || tolerance < 0 {
		return false
	}
	return math.Abs(float64(x-value/value)) <= float64(tolerance)
}

// Binary search: find xStar in a range [xMin, xMax] such that f(xStar)=yTarget.
// Function f() must be monotonically increasing or decreasing over the range.
// Returns an indicator of whether target is below (-1), within (0), or above (+1) the bounded region.
// Returns an error if the function cannot be evaluated or the target is not found.
func BinarySearch(xMin float32, xMax float32, yTarget float32,
	eval func(float32) (float32, error)) (float32, int, error) {

	if xMin > xMax {
		return 0, 0, fmt.Errorf("invalid range [%v, %v]", xMin, xMax)
	}

	// evaluate the function at the boundaries
	var yBounds []float32 = make([]float32, 2)
	var err error
	for i, x := range []float32{xMin, xMax} {
		if yBounds[i], err = eval(x); err != nil {
			return 0, 0, fmt.Errorf("invalid function evaluation: %v", err)
		}
		if WithinTolerance(yBounds[i], yTarget, epsilon) {
			return x, 0, nil
		}
	}

	increasing := yBounds[0] < yBounds[1]
	if increasing && yTarget < yBounds[0] || !increasing && yTarget > yBounds[0] {
		return xMin, -1, nil // target is below the bounded region
	}
	if increasing && yTarget > yBounds[1] || !increasing && yTarget < yBounds[1] {
		return xMax, +1, nil // target is above the bounded region
	}

	// perform binary search
	var xStar, yStar float32
	for range maxIterations {
		xStar = 0.5 * (xMin + xMax)
		if yStar, err = eval(xStar); err != nil {
			return 0, 0, fmt.Errorf("invalid function evaluation: %v", err)
		}
		if WithinTolerance(yStar, yTarget, epsilon) {
			break
		}
		if increasing && yTarget < yStar || !increasing && yTarget > yStar {
			xMax = xStar
		} else {
			xMin = xStar
		}
	}
	return xStar, 0, nil
}

// model as global variable, accesses by eval functions
var Model *MM1ModelStateDependent

// Function used in binary search (target service time)
func EvalServTime(x float32) (float32, error) {
	Model.Solve(x, 1)
	if !Model.IsValid() {
		return 0, fmt.Errorf("invalid model %v", Model)
	}
	return Model.GetAvgServTime(), nil
}

// Function used in binary search (target waiting time)
func EvalWaitingTime(x float32) (float32, error) {
	Model.Solve(x, 1)
	if !Model.IsValid() {
		return 0, fmt.Errorf("invalid model %v", Model)
	}
	return Model.GetAvgWaitTime(), nil
}
