package types_test

import "testing"

// usageFiles split a body inside a loop, after a variable declared in the loop and before its uses, as
// internal/format/arena/slab.go's Reset does under cohere_poison.
var usageFiles = []fixtureFile{
	{"shared.go", everyBuild, `package fork

type Slab[T any] struct {
	chunks [][]T
	chunk  int
	Poison T
}

func (slab *Slab[T]) Reset() {
	for index := 0; index <= slab.chunk && index < len(slab.chunks); index++ {
		released := slab.chunks[index]
		if poisonReleased {
			for element := range released {
				released[element] = slab.Poison
			}
			continue
		}
		clear(released)
	}
}
`},
	{"release.go", buildDarwin | buildLinux, "package fork\n\nconst poisonReleased = false\n"},
	{"poison.go", buildWindows, "package fork\n\nconst poisonReleased = true\n"},
}

// TestVariationalCheckSplitsABodyWithoutFalseErrors is the projection test over usageFiles, which must check clean.
func TestVariationalCheckSplitsABodyWithoutFalseErrors(t *testing.T) {
	// Not parallel: the fork holds a variational check's unit state in package variables, one check at a time.
	projectAndCompare(t, usageFiles)
}
