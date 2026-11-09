package intermediate_representation_test

// Order-independence (#6pt3bt8, wave 3b): ours built lazily, Function.Build on each source function in a seeded
// shuffled order, one at a time and from many goroutines at once, against go/ir's eager build, naive and lifted.
// Every function must come out as it does built eagerly, whatever was built first.
//
// COHERE_GO_IR_DIFFERENTIAL names the corpus as for the other differentials; COHERE_GO_IR_LAZY_SEEDS, a comma
// list, the seeds (1,2,3 when unset); COHERE_GO_IR_LAZY_WORKERS, a comma list, the goroutine counts (1,8 when
// unset). Run the concurrent half under -race to hold the builder's shared state to its guards.

import (
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"honnef.co/go/tools/go/ir"

	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
)

// differentialLazySeedsVariable and differentialLazyWorkersVariable name the seeds the lazy differential shuffles
// with and the goroutine counts it builds from.
const (
	differentialLazySeedsVariable   = "COHERE_GO_IR_LAZY_SEEDS"
	differentialLazyWorkersVariable = "COHERE_GO_IR_LAZY_WORKERS"
)

// differentialLazyNumbers are the numbers variable lists, or fallback when it is unset.
func differentialLazyNumbers(test *testing.T, variable string, fallback []uint64) []uint64 {
	test.Helper()
	listed := os.Getenv(variable)
	if listed == "" {
		return fallback
	}
	var numbers []uint64
	for _, field := range strings.Split(listed, ",") {
		number, parseError := strconv.ParseUint(strings.TrimSpace(field), 10, 64)
		if parseError != nil {
			test.Fatalf("%s holds %q, not a number: %v", variable, field, parseError)
		}
		numbers = append(numbers, number)
	}
	return numbers
}

// TestLazyDifferential builds the corpus lazily, shuffled by each seed, from each goroutine count, and compares
// every function to go/ir's eager build, naive and lifted. Eight goroutines interleave builds even where the test
// runs on two threads.
func TestLazyDifferential(test *testing.T) {
	test.Parallel()
	corpus := os.Getenv(differentialCorpusVariable)
	units := differentialCorpusUnits(test, corpus)
	seeds := differentialLazyNumbers(test, differentialLazySeedsVariable, []uint64{1, 2, 3})
	for _, workerCount := range differentialLazyNumbers(test, differentialLazyWorkersVariable, []uint64{1, 8}) {
		workers := int(workerCount)
		for _, seed := range seeds {
			started := time.Now()
			var naive differentialTally
			var lifted differentialTally
			for _, unit := range units {
				naive = differentialMerge(naive, differentialCompareLazyUnit(test, unit, true, seed, workers))
				lifted = differentialMerge(lifted, differentialCompareLazyUnit(test, unit, false, seed, workers))
			}
			name := fmt.Sprintf("lazy %s, seed %d, %d goroutines", differentialCorpusName(corpus), seed, workers)
			differentialReport(test, name+", naive", naive, time.Since(started))
			differentialReport(test, name+", lifted", lifted, time.Since(started))
		}
	}
}

// differentialCompareLazyUnit builds unit with go/ir eagerly and ours lazily, unlifted or lifted, in seed's
// shuffled order from workers goroutines, and compares every source function.
func differentialCompareLazyUnit(test *testing.T, unit differentialUnit, unlifted bool, seed uint64,
	workers int) (tally differentialTally) {
	tally.packages = 1
	var mode ir.BuilderMode
	if unlifted {
		mode = ir.NaiveForm
	}
	var theirPackage *ir.Package
	theirPanic := differentialBuild(func() { theirPackage = differentialBuildTheirs(unit, mode) })
	ourPackage, ourPanic := differentialBuildOursLazily(test, unit, unlifted, seed, workers)
	if theirPanic != "" || ourPanic != "" {
		tally.differences = append(tally.differences, differentialDifference{cause: "a lazy build panicked",
			function: unit.path, where: "building the package", theirs: theirPanic, ours: ourPanic})
		return tally
	}
	theirFunctions := differentialTheirSourceFunctions(theirPackage)
	tally.aliasSpelledInstances = differentialAliasSpelledInstances(theirFunctions)
	ourFunctions := differentialOurSourceFunctions(ourPackage)
	if len(theirFunctions) != len(ourFunctions) {
		tally.differences = append(tally.differences, differentialDifference{cause: "source function count",
			function: unit.path, where: "the package's source functions",
			theirs: fmt.Sprint(len(theirFunctions)), ours: fmt.Sprint(len(ourFunctions))})
		return tally
	}
	for index, theirs := range theirFunctions {
		tally.functions++
		var difference differentialDifference
		var differs bool
		if unlifted {
			difference, differs = differentialCompareFunction(theirs, ourFunctions[index])
		} else {
			difference, differs = differentialCompareLiftedFunction(theirs, ourFunctions[index])
		}
		if differs {
			tally.differences = append(tally.differences, difference)
			continue
		}
		tally.identical++
		for _, block := range theirs.Blocks {
			tally.instructions += len(block.Instrs)
		}
	}
	return tally
}

// differentialBuildOursLazily creates unit's package and builds each of its source functions with Function.Build,
// twice, shuffled by seed, from workers goroutines, never calling Package.Build. It returns what the first build to
// panic panicked with, if one did.
func differentialBuildOursLazily(test *testing.T, unit differentialUnit, unlifted bool, seed uint64,
	workers int) (*intermediate_representation.Package, string) {
	program := intermediate_representation.NewProgram(unit.fileSet)
	if unlifted {
		intermediate_representation.BuildUnliftedForTest(test, program)
	}
	program.SetNoReturn(differentialNoReturn)
	for _, imported := range unit.types.Imports() {
		program.CreatePackage(imported, nil, nil, true)
	}
	built := program.CreatePackage(unit.types, unit.files, unit.typeInfo, false)
	// Each function twice, so with several goroutines two of them can ask for the same one at once.
	functions := slices.Concat(built.Functions, built.Functions)
	rand.New(rand.NewPCG(seed, seed)).Shuffle(len(functions), func(left int, right int) {
		functions[left], functions[right] = functions[right], functions[left]
	})

	queue := make(chan *intermediate_representation.Function, len(functions))
	for _, function := range functions {
		queue <- function
	}
	close(queue)
	var guard sync.Mutex
	panicked := ""
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for function := range queue {
				if site := differentialBuild(function.Build); site != "" {
					guard.Lock()
					if panicked == "" {
						panicked = function.RelativeString(nil) + ": " + site
					}
					guard.Unlock()
				}
			}
		})
	}
	group.Wait()
	return built, panicked
}
