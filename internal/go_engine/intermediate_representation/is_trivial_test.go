package intermediate_representation_test

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// enteringSource holds a walk for each way IsTrivial meets a callee. chained calls a second function that calls a
// third, past leaf, met twice, which calls nothing, and a function literal called in place. abroad stops at another
// package's function, which has no syntax here, before the third it would have called next. relayedTwice meets relay
// again, which calls, so the second meeting is refused. alone calls nothing.
const enteringSource = `package entering

import "errors"

var failure = errors.New("failure")

func chained() error {
	leaf()
	leaf()
	return second()
}

func second() error {
	func() { leaf() }()
	return third()
}

func third() error { return failure }

func leaf() {}

func abroad() error {
	leaf()
	made()
	return third()
}

func made() error { return errors.New("made") }

func relayedTwice() {
	relay()
	relay()
}

func relay() { leaf() }

func alone() error { return failure }
`

// IsTrivialEntering answers as IsTrivial does and names every function whose body the walk read, each once, in the
// order it first entered them, and none it refused at once. No function of the package is built before, so the walk
// builds what it enters: abroad, asked first, leaves third, which it never reached, unbuilt.
func TestIsTrivialEnteringNamesEveryFunctionTheWalkRead(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "entering", enteringSource)
	program := intermediate_representation.NewProgram(unit.fileSet)
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	for _, asked := range []struct {
		name    string
		trivial bool
		entered []string
	}{
		{"abroad", false, []string{"leaf", "made"}},
		{"chained", true, []string{"leaf", "second", "second$1", "third"}},
		{"relayedTwice", false, []string{"relay", "leaf"}},
		{"alone", true, nil},
	} {
		function := built.Function(asked.name)
		trivial, entered := intermediate_representation.IsTrivialEntering(function, nil)
		var names []string
		for _, read := range entered {
			names = append(names, read.Name())
		}
		if trivial != asked.trivial || !slices.Equal(names, asked.entered) {
			test.Errorf("%s: the walk answers %t and entered %q, want %t and %q", asked.name, trivial, names,
				asked.trivial, asked.entered)
		}
		if alone := intermediate_representation.IsTrivial(function); alone != trivial {
			test.Errorf("%s: IsTrivial answers %t, the walk that names what it entered %t", asked.name, alone, trivial)
		}
		for _, read := range entered {
			if read.Blocks == nil {
				test.Errorf("%s: the walk left %s, which it entered, unbuilt", asked.name, read.Name())
			}
		}
		if asked.name == "abroad" && built.Function("third").Blocks != nil {
			test.Error("abroad: the walk built third, which it never reached")
		}
	}
}

// A function the caller refuses is refused as one with no syntax is: the walk takes it as nontrivial at once, and
// neither enters nor builds it. chained is trivial through second, so with second refused it is not, the walk names
// only leaf, entered before it, and second and third, which only second calls, stay unbuilt.
func TestIsTrivialEnteringNeitherEntersNorBuildsAFunctionItsCallerRefuses(test *testing.T) {
	test.Parallel()
	unit := differentialCheck(test, "entering", enteringSource)
	program := intermediate_representation.NewProgram(unit.fileSet)
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	second := built.Function("second")
	trivial, entered := intermediate_representation.IsTrivialEntering(built.Function("chained"),
		func(met *intermediate_representation.Function) bool { return met == second })
	var names []string
	for _, read := range entered {
		names = append(names, read.Name())
	}
	if trivial || !slices.Equal(names, []string{"leaf"}) {
		test.Errorf("chained, with second refused: the walk answers %t and entered %q, want false and [\"leaf\"]",
			trivial, names)
	}
	if second.Blocks != nil || built.Function("third").Blocks != nil {
		test.Error("chained, with second refused: the walk built second, or third, which only second calls")
	}
}
