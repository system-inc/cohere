package canonical_function_hash

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/types/program"
)

// hashedFixture is every function of a fixture project, by `file#name` with the file's base name, hashed or refused.
type hashedFixture struct {
	functions map[string]duplicate_functions.Function
	refused   map[string]duplicate_functions.Refusal
	bodyless  int
}

// hashFixture builds the fixture project at directory and hashes every file on its own checker.
func hashFixture(test *testing.T, directory string) hashedFixture {
	test.Helper()
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   filepath.Join(directory, "tsconfig.json"),
		CurrentDirectory: directory,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatalf("building the fixture program: %v", buildError)
	}
	hashed := hashedFixture{
		functions: map[string]duplicate_functions.Function{},
		refused:   map[string]duplicate_functions.Refusal{},
	}
	for _, sourceFile := range graph.ProjectFiles() {
		if sourceFile.IsDeclarationFile {
			continue
		}
		typeChecker, release := graph.CheckerForFile(context.Background(), sourceFile)
		result := HashFile(sourceFile, typeChecker, directory)
		release()
		hashed.bodyless += result.Bodyless
		for _, function := range result.Functions {
			hashed.functions[filepath.Base(function.File)+"#"+function.Name] = function
		}
		for _, refused := range result.Refused {
			hashed.refused[filepath.Base(refused.File)+"#"+refused.Name] = refused
		}
	}
	return hashed
}

// pairsFixture is testdata/pairs, absolute, since program.Build resolves a tsconfig against a directory.
func pairsFixture(test *testing.T) string {
	test.Helper()
	directory, absoluteError := filepath.Abs(filepath.Join("testdata", "pairs"))
	if absoluteError != nil {
		test.Fatalf("resolving the fixture: %v", absoluteError)
	}
	return directory
}

// Each pair below is the one a normalization decides, so switching that normalization off turns exactly its pair red.
// The prototype's fixture on #6rj7jw7 is the start, its tables both ways, with what the ruling's conditions added.
var samePairs = []struct {
	normalization string
	left          string
	right         string
}{
	{"values are named by structure, never by source name", "pairs.ts#namesA", "pairs.ts#namesB"},
	{"independent statements float free of source order", "pairs.ts#orderA", "pairs.ts#orderB"},
	{"numeric operands of a commutative operator are sorted", "pairs.ts#commuteA", "pairs.ts#commuteB"},
	{"bigint operands of a commutative operator are sorted", "pairs.ts#bigintA", "pairs.ts#bigintB"},
	{"constants are hashed by value", "pairs.ts#literalA", "pairs.ts#literalB"},
	{"a numeric separator is no part of a constant", "pairs.ts#literalA", "pairs.ts#literalSeparated"},
	{"a decimal point with nothing after it is no part of a constant", "pairs.ts#literalA",
		"pairs.ts#literalDecimal"},
	{"a ternary and an if lower alike, by block fusion", "pairs.ts#ternaryA", "pairs.ts#ternaryB"},
	{"an early return and an else agree, by the hash's own block order", "pairs.ts#earlyA", "pairs.ts#earlyB"},
	{"`a && b` and `a ? b : a` are one branch", "pairs.ts#andA", "pairs.ts#andB"},
	{"copies are transparent", "pairs.ts#copiesA", "pairs.ts#copiesB"},
	{"an import is its declaring file and name, whatever its local name", "importOne.ts#importsA",
		"importThree.ts#importsSameA"},
	{"two files' identical helpers agree", "helpersOne.ts#format", "helpersTwo.ts#format"},
	{"a literal default is hashed by value, not by its parameter's name", "pairs.ts#defaultsSameA",
		"pairs.ts#defaultsSameB"},
	{
		"a parameter property is hashed by its field, modifiers and type, not its class",
		"pairs.ts#PropertyOne.constructor",
		"pairs.ts#PropertyTwo.constructor",
	},
}

var differentPairs = []struct {
	normalization string
	left          string
	right         string
}{
	{"subtraction is never sorted", "pairs.ts#subtractA", "pairs.ts#subtractB"},
	{"calls keep their order", "pairs.ts#callsA", "pairs.ts#callsB"},
	{"`this` is never a global", "pairs.ts#Holder.thisA", "pairs.ts#thisB"},
	{"string concatenation is never sorted", "pairs.ts#stringsA", "pairs.ts#stringsB"},
	{"`??` is not `||`", "pairs.ts#nullishA", "pairs.ts#nullishB"},
	{"two results of one call are two values", "pairs.ts#resultsA", "pairs.ts#resultsB"},
	{"a load stays on its side of a store", "pairs.ts#storeA", "pairs.ts#storeB"},
	{"two allocations are two objects", "pairs.ts#allocationsA", "pairs.ts#allocationsB"},
	{"loop-carried values are named by their back edges", "pairs.ts#loopsA", "pairs.ts#loopsB"},
	{"a number is not its string", "pairs.ts#literalKindA", "pairs.ts#literalKindB"},
	{"a closure's captures are hashed into it", "pairs.ts#closureA", "pairs.ts#closureB"},
	{"a write through a closure is an effect", "pairs.ts#counterA", "pairs.ts#counterB"},
	{"two imports under one local name are two callees", "importOne.ts#importsA", "importTwo.ts#importsB"},
	{"literal defaults differ by value", "pairs.ts#defaultsA", "pairs.ts#defaultsB"},
	{"parameter properties differ by field name", "pairs.ts#StoreOne.constructor", "pairs.ts#StoreTwo.constructor"},
	{"parameter properties differ by modifiers", "pairs.ts#PropertyOne.constructor",
		"pairs.ts#PropertyPublic.constructor"},
	{"parameter properties differ by type", "pairs.ts#PropertyOne.constructor", "pairs.ts#PropertyString.constructor"},
}

var refusedFunctions = map[string]duplicate_functions.RefusalReason{
	"pairs.ts#methodsA":                duplicate_functions.RefusalObjectMethodCapture,
	"pairs.ts#methodsB":                duplicate_functions.RefusalObjectMethodCapture,
	"pairs.ts#defaultsComputed":        duplicate_functions.RefusalParameterDefault,
	"pairs.ts#loadsLater":              duplicate_functions.RefusalDynamicImport,
	"pairs.ts#Initialized.constructor": duplicate_functions.RefusalFieldInitializers,
	"pairs.ts#holdsAClass":             duplicate_functions.RefusalUnsupportedNode,
}

func TestPairsThatComputeTheSameThingHashAlike(test *testing.T) {
	test.Parallel()
	hashed := hashFixture(test, pairsFixture(test))
	for _, pair := range samePairs {
		left, leftFound := hashed.functions[pair.left]
		right, rightFound := hashed.functions[pair.right]
		if !leftFound || !rightFound {
			test.Errorf("%s: %s hashed %t, %s hashed %t, where both were expected", pair.normalization, pair.left,
				leftFound, pair.right, rightFound)
			continue
		}
		if left.Digest != right.Digest {
			test.Errorf("%s: %s and %s hash apart", pair.normalization, pair.left, pair.right)
		}
	}
}

func TestPairsThatComputeDifferentThingsHashApart(test *testing.T) {
	test.Parallel()
	hashed := hashFixture(test, pairsFixture(test))
	for _, pair := range differentPairs {
		left, leftFound := hashed.functions[pair.left]
		right, rightFound := hashed.functions[pair.right]
		if !leftFound || !rightFound {
			test.Errorf("%s: %s hashed %t, %s hashed %t, where both were expected", pair.normalization, pair.left,
				leftFound, pair.right, rightFound)
			continue
		}
		if left.Digest == right.Digest {
			test.Errorf("%s: %s and %s hash alike", pair.normalization, pair.left, pair.right)
		}
	}
}

// A function the IR cannot read faithfully is refused by its reason and never hashed, and nothing else is refused.
func TestFunctionsTheIrCannotReadAreRefusedByName(test *testing.T) {
	test.Parallel()
	hashed := hashFixture(test, pairsFixture(test))
	for name, reason := range refusedFunctions {
		refused, found := hashed.refused[name]
		if !found {
			_, wasHashed := hashed.functions[name]
			test.Errorf("%s was not refused (hashed %t), where %s was expected", name, wasHashed, reason)
			continue
		}
		if refused.Reason != reason {
			test.Errorf(
				"%s was refused as %s (%s), where %s was expected",
				name,
				refused.Reason,
				refused.Detail,
				reason,
			)
		}
	}
	for name, refused := range hashed.refused {
		if _, expected := refusedFunctions[name]; !expected {
			test.Errorf(
				"%s was refused as %s (%s), where it was expected to hash",
				name,
				refused.Reason,
				refused.Detail,
			)
		}
	}
}

// The prototype's own groups on its fixture, which this fixture extends: every one of them still forms, with no
// other group among the prototype's functions.
func TestThePrototypesGroupsStillForm(test *testing.T) {
	test.Parallel()
	hashed := hashFixture(test, pairsFixture(test))
	expected := [][]string{
		{"pairs.ts#earlyA", "pairs.ts#earlyB"}, {"pairs.ts#orderA", "pairs.ts#orderB"},
		{"importOne.ts#importsA", "importThree.ts#importsSameA"}, {"pairs.ts#namesA", "pairs.ts#namesB"},
		{"pairs.ts#commuteA", "pairs.ts#commuteB"}, {"pairs.ts#ternaryA", "pairs.ts#ternaryB"},
		{"pairs.ts#literalA", "pairs.ts#literalB", "pairs.ts#literalDecimal", "pairs.ts#literalSeparated"},
		{"helpersOne.ts#format", "helpersTwo.ts#format"},
		{"pairs.ts#andA", "pairs.ts#andB"},
		{"pairs.ts#copiesA", "pairs.ts#copiesB"}, {"pairs.ts#defaultsSameA", "pairs.ts#defaultsSameB"},
		{"pairs.ts#PropertyOne.constructor", "pairs.ts#PropertyTwo.constructor"},
		{"pairs.ts#bigintA", "pairs.ts#bigintB"},
	}
	byDigest := map[duplicate_functions.Digest][]string{}
	for name, function := range hashed.functions {
		byDigest[function.Digest] = append(byDigest[function.Digest], name)
	}
	var found [][]string
	for _, names := range byDigest {
		if len(names) > 1 {
			slices.Sort(names)
			found = append(found, names)
		}
	}
	for _, group := range expected {
		slices.Sort(group)
	}
	sortGroups := func(groups [][]string) {
		slices.SortFunc(groups, func(left []string, right []string) int { return slices.Compare(left, right) })
	}
	sortGroups(found)
	sortGroups(expected)
	if !slices.EqualFunc(found, expected, slices.Equal[[]string]) {
		test.Errorf("the groups are %v, where %v were expected", found, expected)
	}
}

// A digest names an imported declaration by its path from the root, so the same project in another directory hashes
// every function the same: a cached digest survives a moved checkout, and two checkouts agree.
func TestDigestsDoNotDependOnWhereTheProjectSits(test *testing.T) {
	test.Parallel()
	original := pairsFixture(test)
	moved := filepath.Join(test.TempDir(), "elsewhere")
	if copyError := os.CopyFS(moved, os.DirFS(original)); copyError != nil {
		test.Fatalf("copying the fixture: %v", copyError)
	}
	here, there := hashFixture(test, original), hashFixture(test, moved)
	if len(here.functions) == 0 || len(here.functions) != len(there.functions) {
		test.Fatalf("hashed %d functions in place and %d moved", len(here.functions), len(there.functions))
	}
	for name, function := range here.functions {
		if there.functions[name].Digest != function.Digest {
			test.Errorf("%s hashes differently once the project moves", name)
		}
	}
}
