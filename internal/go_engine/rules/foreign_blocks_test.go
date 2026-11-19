package rules

import (
	"maps"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// readsAnotherFunctionsBlocks are the rules whose listener on the representation's walk reads the blocks of a function
// the dispatcher did not hand it, without calling that function's Build first, each with what it reads. In an extra
// build where functions stand, the dispatcher builds the package less the functions that stand for every started run
// (#e4rd5dw), so such a listener can meet a function that was never built and read it as empty. That is sound only
// for a rule that is never confined, one the inputs table declares Composes, whose run hears every function and is
// never confined on a file. The code cannot tell such a read from one of the handed function's own, a closure's
// included, since both read a *Function's Blocks, so the list is kept by hand: a listener that comes to read another
// function's blocks joins it with its reason, or calls Build.
var readsAnotherFunctionsBlocks = map[string]string{
	rule.Namespace + "SA2002": "reads the body of the function a go statement starts (test_failure_in_goroutine.Check)",
}

// Every rule that reads another function's blocks without building it is a registered rule listening on the
// representation's walk, and one the inputs table declares Composes, so it is never confined and the dispatcher builds
// every function it may read: a rule declared PerUnit or FileSyntax added to the list goes red.
func TestARuleReadingAnotherFunctionsBlocksIsDeclaredComposes(test *testing.T) {
	test.Parallel()
	declared, declaredError := DeclaredInputs()
	if declaredError != nil {
		test.Fatal(declaredError)
	}
	registered := All()
	for _, ruleName := range slices.Sorted(maps.Keys(readsAnotherFunctionsBlocks)) {
		at := slices.IndexFunc(registered, func(each rule.Rule) bool { return each.Name == ruleName })
		if at < 0 || registered[at].Listener == nil || !registered[at].Listener.IsIntermediateRepresentation() {
			test.Errorf("%s, which %s, is no registered rule listening on the representation's walk", ruleName,
				readsAnotherFunctionsBlocks[ruleName])
			continue
		}
		if inputs := declared[ruleName].Inputs; inputs != InputsComposes {
			test.Errorf("%s %s without building it, and is declared %s, so it could be confined and read a function "+
				"the dispatcher left unbuilt: declare it Composes, or have it call Build", ruleName,
				readsAnotherFunctionsBlocks[ruleName], inputs)
		}
	}
}
