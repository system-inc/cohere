package duplicates

import "testing"

func TestScores(test *testing.T) {
	test.Parallel()
	if ScoreOne([]int{11}, 2) != ScoreTwo([]int{11}, 2) {
		test.Fatal("the two scores differ")
	}
}
