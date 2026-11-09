package canonical_function_hash

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// errorsOnly passes a test to analysistest without its want-comment checks, which this analyzer, reporting nothing,
// has none of.
type errorsOnly struct{ *testing.T }

// hashedFixture is every function of the fixture, by `package.name`, as the analyzer left it.
type hashedFixture struct {
	functions map[string][]duplicate_functions.Function
	refused   map[string]duplicate_functions.Refusal
	unread    map[string]Unread
	packages  int
}

var (
	fixtureOnce sync.Once
	fixture     hashedFixture
)

// hashFixture runs the analyzer, through buildir as the engine runs it, over the fixture's packages and their test
// variants, once for every test in the package.
func hashFixture(test *testing.T) hashedFixture {
	test.Helper()
	fixtureOnce.Do(func() {
		fixture = hashedFixture{functions: map[string][]duplicate_functions.Function{},
			refused: map[string]duplicate_functions.Refusal{}, unread: map[string]Unread{}}
		for _, analyzed := range analysistest.Run(errorsOnly{test}, analysistest.TestData(), Analyzer, "fixture/...") {
			if analyzed.Err != nil {
				test.Fatalf("hashing %s: %v", analyzed.Action.Package.ID, analyzed.Err)
			}
			functions, isFunctions := analyzed.Result.(*PackageFunctions)
			if !isFunctions {
				test.Fatalf("the analyzer's result on %s is %T", analyzed.Action.Package.ID, analyzed.Result)
			}
			fixture.packages++
			packageName := analyzed.Action.Package.Types.Name()
			for _, function := range functions.Functions {
				key := packageName + "." + function.Name
				fixture.functions[key] = append(fixture.functions[key], function)
			}
			for _, refused := range functions.Refused {
				fixture.refused[packageName+"."+refused.Name] = refused
			}
			for _, unread := range functions.Unread {
				fixture.unread[packageName+"."+unread.Name] = unread
			}
		}
	})
	return fixture
}

// digestOf is the one digest the fixture gives a function, failing when it has none or, across the package and its
// test variant, two.
func digestOf(test *testing.T, hashed hashedFixture, name string) (duplicate_functions.Digest, bool) {
	test.Helper()
	functions := hashed.functions[name]
	if len(functions) == 0 {
		test.Errorf("%s was not hashed", name)
		return duplicate_functions.Digest{}, false
	}
	for _, function := range functions[1:] {
		if function.Digest != functions[0].Digest {
			test.Errorf("%s hashes differently in the package and its test variant", name)
		}
	}
	return functions[0].Digest, true
}

// Each pair is the one a normalization decides, so switching the normalization off turns exactly its pair red. The
// prototype's fixture on #6rj7jw7 is the start, its tables both ways, run here on buildir's IR, built in debug
// mode with no-return calls marked, which the prototype never built.
var samePairs = []struct{ normalization, left, right string }{
	{"values are named by structure, never by source name", "shapes.SameNamesA", "shapes.SameNamesB"},
	{"independent statements float free of source order", "shapes.SameOrderA", "shapes.SameOrderB"},
	{"independent loads float among themselves", "shapes.SameLoadOrderA", "shapes.SameLoadOrderB"},
	{"integer operands of a commutative operator are sorted", "shapes.SameCommuteA", "shapes.SameCommuteB"},
	{"constants are hashed by value", "shapes.SameLiteralA", "shapes.SameLiteralB"},
	{"an early return and an else agree", "shapes.SameEarlyA", "shapes.SameEarlyB"},
	{"a negated condition with its arms swapped agrees", "shapes.SameNegateA", "shapes.SameNegateB"},
	{"loop-carried values are named by refinement, not declaration order", "shapes.SameLoopA", "shapes.SameLoopB"},
	{"closures over different outer names agree", "shapes.SameClosureA", "shapes.SameClosureB"},
	{"a closure is hashed by its own body", "shapes.SameClosureA$1", "shapes.SameClosureB$1"},
	{"a local copy is transparent", "shapes.SameCopyA", "shapes.SameCopyB"},
	{"two packages' identical functions agree", "alpha.Name", "beta.Name"},
}

var differentPairs = []struct{ normalization, left, right string }{
	{"subtraction is never sorted", "shapes.DiffSubtractA", "shapes.DiffSubtractB"},
	{"calls keep their order", "shapes.DiffCallsA", "shapes.DiffCallsB"},
	{"a value's type is part of it", "shapes.DiffTypesA", "shapes.DiffTypesB"},
	{"a value's type is part of it, with no constant to tell them apart", "shapes.DiffDivisionA",
		"shapes.DiffDivisionB"},
	{"a callee is its package and name", "shapes.DiffPackagesA", "shapes.DiffPackagesB"},
	{"string concatenation is never sorted", "shapes.DiffConcatA", "shapes.DiffConcatB"},
	{"loop-carried values keep which is which", "shapes.DiffLoopA", "shapes.DiffLoopB"},
	{"a load stays on its side of a store", "shapes.DiffMemoryA", "shapes.DiffMemoryB"},
	{"a receiver's type is part of a method", "shapes.(Box).Doubled", "shapes.(Crate).Doubled"},
	{"constants differ by value", "shapes.DiffConstantA", "shapes.DiffConstantB"},
}

func TestPairsThatComputeTheSameThingHashAlike(test *testing.T) {
	test.Parallel()
	hashed := hashFixture(test)
	for _, pair := range samePairs {
		left, leftHashed := digestOf(test, hashed, pair.left)
		right, rightHashed := digestOf(test, hashed, pair.right)
		if leftHashed && rightHashed && left != right {
			test.Errorf("%s: %s and %s hash apart", pair.normalization, pair.left, pair.right)
		}
	}
}

func TestPairsThatComputeDifferentThingsHashApart(test *testing.T) {
	test.Parallel()
	hashed := hashFixture(test)
	for _, pair := range differentPairs {
		left, leftHashed := digestOf(test, hashed, pair.left)
		right, rightHashed := digestOf(test, hashed, pair.right)
		if leftHashed && rightHashed && left == right {
			test.Errorf("%s: %s and %s hash alike", pair.normalization, pair.left, pair.right)
		}
	}
}

// Nothing in the fixture holds an instruction the hash does not model: on buildir's IR, debug references included,
// every function with a body in a file of ours is hashed.
func TestEveryFixtureFunctionIsHashed(test *testing.T) {
	test.Parallel()
	hashed := hashFixture(test)
	for name, refused := range hashed.refused {
		test.Errorf("%s was refused as %s (%s)", name, refused.Reason, refused.Detail)
	}
}

// A generated file's function, a copied file's and one without a body are left unread, each by its reason, never
// hashed: the copied file's is withheld as every house rule withholds it.
func TestGeneratedCopiedAndBodylessFunctionsAreUnread(test *testing.T) {
	test.Parallel()
	hashed := hashFixture(test)
	expected := map[string]duplicate_functions.NotReadReason{
		"shapes.GeneratedA": duplicate_functions.NotReadGenerated,
		"shapes.CopiedA":    duplicate_functions.NotReadCopied,
		"shapes.Bodyless":   duplicate_functions.NotReadBodyless,
	}
	for name, reason := range expected {
		if unread, found := hashed.unread[name]; !found || unread.Reason != reason {
			test.Errorf(
				"%s was left unread as %q (found %t), where %s was expected",
				name,
				unread.Reason,
				found,
				reason,
			)
		}
		if _, wasHashed := hashed.functions[name]; wasHashed {
			test.Errorf("%s was hashed, where it was expected to be unread", name)
		}
	}
}

// The test variant reads the package's functions again beside its test files' own. The two readings agree, name the
// same file and line, and so are one function to the engine, which counts each place once.
func TestTheTestVariantReadsEachFunctionAtItsOwnPlace(test *testing.T) {
	test.Parallel()
	hashed := hashFixture(test)
	if hashed.packages < 4 {
		test.Fatalf("the analyzer ran on %d packages, where the fixture's three and shapes' test variant were expected",
			hashed.packages)
	}
	readings := hashed.functions["shapes.SameNamesA"]
	if len(readings) != 2 {
		test.Fatalf("shapes.SameNamesA was read %d times, where the package and its test variant were expected",
			len(readings))
	}
	if readings[0].File != readings[1].File || readings[0].Line != readings[1].Line {
		test.Errorf("the two readings of shapes.SameNamesA name %s:%d and %s:%d", readings[0].File, readings[0].Line,
			readings[1].File, readings[1].Line)
	}
	inTest := hashed.functions["shapes.helperInTest"]
	if len(inTest) != 1 || filepath.Base(inTest[0].File) != "shapes_test.go" {
		test.Errorf("the test file's helper was read as %v, where once in shapes_test.go was expected", inTest)
	}
}
