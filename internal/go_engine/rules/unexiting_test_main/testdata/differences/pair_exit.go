package differences

import "os"

// exitInAnotherFile calls os.Exit at package level, the first half of a pair across two files: the original counts it
// for the next TestMain, in pair_test_main.go.
var exitInAnotherFile = func() int {
	if len(os.Args) > 100 {
		os.Exit(2)
	}
	return 0
}()
