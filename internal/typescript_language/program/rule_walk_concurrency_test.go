package program_test

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/locale"
	"github.com/system-inc/cohere/internal/typescript_language/program"
	"github.com/system-inc/cohere/internal/typescript_language/registry"
)

// ruleWalkCursorSource is the class the walk lints, loom's cursor's shape (#72kn826): a computed member, which a
// checker reads off the class's own member list when it late-binds it, and a duplicate, so no-dupe-class-members
// has a class to report on while it runs. A script, so the file beside it reads the class with no import.
const ruleWalkCursorSource = "class Cursor {\n" +
	"  *[Symbol.iterator](): IterableIterator<number> { yield 1; }\n" +
	"  read(): number { return 1; }\n" +
	"  read(): number { return 2; }\n" +
	"}\n"

// ruleWalkReaderSource iterates the cursor, so type-checking it resolves the cursor's `[Symbol.iterator]`, through
// the class's member list.
const ruleWalkReaderSource = "function total(cursor: Cursor): number {\n" +
	"  let sum = 0;\n" +
	"  for (const value of cursor) { sum += value; }\n" +
	"  return sum;\n" +
	"}\n"

// Every rule walks a file while, on other goroutines, a checker type-checks the file beside it, resolving the walked
// file's class as it does, and a reader reads the walked file's whole tree, over and over until the walk ends. The
// syntax tree is shared with every checker, so a rule that writes any of it is a data race here, red under -race
// (the land gate runs this package so, race_gated.go).
//
// no-dupe-class-members swapped a class's member list for one without its computed members while its listener ran,
// and a checker that resolved the class during the swap kept it without its iterator for the run: TS2488 on every
// `for ... of` over loom's cursor, in 2 of 60 cold runs (#72kn826). TestRulesNeverWriteTheSyntaxTree holds every
// rule to that statically; this runs them, every one the registry holds, so a write the scan cannot see (through a
// function value, or in a checker call a rule makes) is caught where it races (#qb5xxj1).
//
// The check alone is not enough of an instrument. A checker reads the class once and keeps what it resolved, and
// both it and the walk pass through the compiler's shared atomics (the node and symbol ids) between their reads and
// writes, which order them for the race detector: with the old swap planted, the check beside the walk passed under
// -race. The reader reads what a checker reads of every node, its children, with nothing between its reads that
// orders them, for as long as the walk runs, so any write to the tree meets a read the detector cannot order. The
// check is what shows the cost, a type error the tree does not have.
//
// The check is on a checker other than the one the walk holds, as the fused check hands a file to another file's
// checker (semanticDiagnosticsOn): two files this small share a checker in the pool, whose libraries outweigh them,
// and on one checker the walk and the check would take turns under its lock.
func TestEveryRuleWalksBesideReadersOfTheSameTreeAtOnce(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"Cursor.ts":     ruleWalkCursorSource,
		"Total.ts":      ruleWalkReaderSource,
	})
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   "tsconfig.json",
		CurrentDirectory: directory,
		Checkers:         2,
	})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}
	var cursorFile *ast.SourceFile
	var totalFile *ast.SourceFile
	for _, sourceFile := range graph.ProjectFiles() {
		switch {
		case strings.HasSuffix(sourceFile.FileName().AsString(), "/Cursor.ts"):
			cursorFile = sourceFile
		case strings.HasSuffix(sourceFile.FileName().AsString(), "/Total.ts"):
			totalFile = sourceFile
		}
	}
	if cursorFile == nil || totalFile == nil {
		test.Fatalf("the program holds %d project files, not the cursor and its reader", len(graph.ProjectFiles()))
	}

	// The walk holds the cursor's checker. The reader is checked on the checker of a file the other one owns.
	cursorChecker, releaseCursorChecker := graph.CheckerForFile(context.Background(), cursorFile)
	releaseCursorChecker()
	var otherCheckersFile *ast.SourceFile
	for _, candidate := range graph.Program.GetSourceFiles() {
		candidateChecker, releaseCandidateChecker := graph.CheckerForFile(context.Background(), candidate)
		releaseCandidateChecker()
		if candidateChecker != nil && candidateChecker != cursorChecker {
			otherCheckersFile = candidate
			break
		}
	}
	if cursorChecker == nil || otherCheckersFile == nil {
		test.Fatal("the program has one checker, so the walk and the check would never run at once")
	}

	rules := registry.All()
	start := make(chan struct{})
	var group sync.WaitGroup
	// The reader reads the tree once before the walk starts, and then again and again until it ends. Its first read
	// is ordered before the walk by start; none after it is.
	var walking atomic.Bool
	walking.Store(true)
	readerStarted := make(chan struct{})
	readerPasses := 0
	group.Go(func() {
		for readerPasses == 0 || walking.Load() {
			readEveryNode(cursorFile.AsNode())
			readerPasses++
			if readerPasses == 1 {
				close(readerStarted)
			}
		}
	})
	var walked program.Result
	var walkError error
	group.Go(func() {
		defer walking.Store(false)
		<-start
		walked, walkError = graph.Walk(context.Background(), []*ast.SourceFile{cursorFile}, rules)
	})
	var checked []*ast.Diagnostic
	group.Go(func() {
		<-start
		otherChecker, releaseOtherChecker := graph.CheckerForFile(context.Background(), otherCheckersFile)
		defer releaseOtherChecker()
		checked = compiler.FilterAndSortDiagnostics(compiler.Program_getSemanticDiagnosticsWithChecker(
			graph.Program,
			context.Background(),
			otherChecker,
			totalFile,
		))
	})
	<-readerStarted
	close(start)
	group.Wait()
	test.Logf("the reader read the cursor's tree %d times while %d rules walked it", readerPasses,
		walked.Coverage.RulesRun)

	if walkError != nil {
		test.Fatalf("walking the cursor: %v", walkError)
	}
	if walked.Coverage.RulesRun < len(rules)/2 {
		test.Fatalf("%d of %d rules ran on the cursor, too few to be the walk", walked.Coverage.RulesRun, len(rules))
	}
	duplicates := 0
	for _, diagnostic := range walked.Diagnostics {
		if strings.HasSuffix(diagnostic.RuleName, "no-dupe-class-members") {
			duplicates++
		}
	}
	if duplicates == 0 {
		test.Fatal("no-dupe-class-members reported nothing on the cursor, so its listener never ran beside the check")
	}
	for _, diagnostic := range checked {
		test.Errorf("checked beside the walk, the cursor's reader read TS%d: %s", diagnostic.Code(),
			diagnostic.Localize(locale.Locale{}))
	}
}

// readEveryNode reads every node under node through its children, as a checker reads what it resolves: a class's
// member list, every member in it. It reads nothing the compiler writes after the program is built.
func readEveryNode(node *ast.Node) {
	node.ForEachChild(func(child *ast.Node) bool {
		readEveryNode(child)
		return false
	})
}
