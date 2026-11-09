// Package duplicates holds one computation written three times, one copy kept in its shape, and a suppression that
// keeps nothing.
package duplicates

// ScoreOne weighs the readings above ten and subtracts the rest.
func ScoreOne(values []int, weight int) int {
	total := 0
	for _, value := range values {
		if value > 10 {
			total += value * weight
		} else {
			total -= value
		}
	}
	return total / (len(values) + 1)
}

// ScoreTwo is ScoreOne under other names.
func ScoreTwo(readings []int, factor int) int {
	sum := 0
	for _, reading := range readings {
		if reading > 10 {
			sum += factor * reading
		} else {
			sum -= reading
		}
	}
	return sum / (len(readings) + 1)
}

// ScorePort is ScoreOne as upstream wrote it.
//
//cohere:ignore consistency-no-duplicate-function port of upstream's score, kept in its shape
func ScorePort(values []int, weight int) int {
	total := 0
	for _, value := range values {
		if value > 10 {
			total += value * weight
		} else {
			total -= value
		}
	}
	return total / (len(values) + 1)
}

// Lonely duplicates nothing, so its suppression keeps nothing.
//
//cohere:ignore consistency-no-duplicate-function kept, though nothing duplicates it
func Lonely(value int) int {
	return value + 1
}
