package program

import (
	"context"
	"fmt"
	"sync"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/system-inc/verify/internal/rule"
)

// Coverage is what a run actually looked at.
//
// This exists because "what did you find" is the less important half of a verdict and the only half
// most tools report. A run that found nothing and a run that checked nothing print the same green,
// and this project's history is a list of times that ambiguity cost days: a gate that linted zero
// files and exited 0, three configs that loaded no plugins, a filter that stopped reporting. Every
// result this package returns carries what it covered, so a caller that prints a verdict without
// also printing the population had to work at it.
type Coverage struct {
	// FilesInProgram is the whole type graph — our code plus every declaration it pulled in.
	FilesInProgram int

	// FilesWalked is how many files the rules actually visited.
	FilesWalked int

	// NodesVisited is how many AST nodes the traversal touched, across every file.
	NodesVisited int

	// RulesRun is how many rules were dispatched.
	RulesRun int

	// RulesListening counts, per rule name, how many files that rule chose to listen to. A rule that
	// declined every file reads as zero here, which is the difference between "ran and found nothing"
	// and "never actually looked" — the distinction a bare finding count erases.
	RulesListening map[string]int
}

// Result is the findings of one walk, and the coverage that produced them.
type Result struct {
	Diagnostics []rule.Diagnostic
	Coverage    Coverage
}

// Walk visits every file in the given set once, dispatching every rule's listeners as it goes.
//
// One traversal serves every rule. This is the arrangement the whole tool exists to make possible:
// a rule that wanted its own pass would multiply the only unavoidable cost in the system by the
// number of rules, which is exactly how 53 rules of ordinary shape came to cost 1.81s. Here the
// five-hundredth rule costs a map lookup per node it asked about, and nothing at all for the nodes
// it did not.
//
// Files are walked in parallel, one goroutine per checker, because a file must be visited by the
// checker that owns it and there is no use in more workers than there are checkers.
func (g *Graph) Walk(ctx context.Context, files []*ast.SourceFile, rules []rule.Rule) (Result, error) {
	if len(files) == 0 {
		return Result{}, fmt.Errorf("nothing to walk: the file set is empty (the program holds %d files)", len(g.Program.GetSourceFiles()))
	}
	if len(rules) == 0 {
		return Result{}, fmt.Errorf("nothing to run: no rules were given for %d files", len(files))
	}

	workers := min(g.Workers(), len(files))

	var mutex sync.Mutex
	diagnostics := []rule.Diagnostic{}
	listeningCounts := make(map[string]int, len(rules))
	nodesVisited := 0

	// Files are handed out by index stride rather than by a queue: the compiler assigns a file to a
	// checker by its position in the program's file list, so striding keeps each worker mostly on one
	// checker instead of contending across all of them.
	var waitGroup sync.WaitGroup
	for worker := range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			localDiagnostics := []rule.Diagnostic{}
			localListening := make(map[string]int, len(rules))
			localNodes := 0

			for index := worker; index < len(files); index += workers {
				sourceFile := files[index]

				fileChecker, release := g.CheckerForFile(ctx, sourceFile)

				// A rule's Report closure captures the rule it belongs to, so a rule cannot report under
				// another rule's name even by accident.
				visited := dispatchFile(sourceFile, func(diagnostic rule.Diagnostic) {
					localDiagnostics = append(localDiagnostics, diagnostic)
				}, rules, g, fileChecker, localListening)

				release()

				localNodes += visited
			}

			mutex.Lock()
			diagnostics = append(diagnostics, localDiagnostics...)
			for name, count := range localListening {
				listeningCounts[name] += count
			}
			nodesVisited += localNodes
			mutex.Unlock()
		}()
	}
	waitGroup.Wait()

	return Result{
		Diagnostics: diagnostics,
		Coverage: Coverage{
			FilesInProgram: len(g.Program.GetSourceFiles()),
			FilesWalked:    len(files),
			NodesVisited:   nodesVisited,
			RulesRun:       len(rules),
			RulesListening: listeningCounts,
		},
	}, nil
}

// dispatchFile asks every rule what it wants to hear about in this file, merges those answers into
// one dispatch table, and walks the tree once against it. It returns how many nodes it visited.
//
// Merging before walking is what makes the cost per node independent of the rule count: the walk
// does one map lookup per node regardless of whether one rule or five hundred registered for that
// kind.
func dispatchFile(
	sourceFile *ast.SourceFile,
	report func(rule.Diagnostic),
	rules []rule.Rule,
	graph *Graph,
	fileChecker *checker.Checker,
	listeningCounts map[string]int,
) int {
	// A kind may have listeners from several rules, so the merged table maps a kind to a slice rather
	// than to one function.
	merged := map[ast.Kind][]func(node *ast.Node){}

	for _, subject := range rules {
		ruleName := subject.Name

		context := rule.Context{
			SourceFile:  sourceFile,
			Program:     graph.Program,
			TypeChecker: fileChecker,
			Report: func(diagnostic rule.Diagnostic) {
				diagnostic.RuleName = ruleName
				if diagnostic.SourceFile == nil {
					diagnostic.SourceFile = sourceFile
				}
				report(diagnostic)
			},
		}

		listeners := subject.Run(context, nil)
		if len(listeners) == 0 {
			// The rule looked at the file and declined it. That is the cheapest and most valuable thing
			// a rule can do, and it is counted rather than ignored so a rule that declines *everything*
			// is visible as a rule that never ran.
			continue
		}

		listeningCounts[ruleName]++
		for kind, listener := range listeners {
			merged[kind] = append(merged[kind], listener)
		}
	}

	if len(merged) == 0 {
		return 0
	}

	return walk(sourceFile.AsNode(), merged)
}

// walk visits every node once, calling whatever listeners registered for its kind.
func walk(node *ast.Node, listeners map[ast.Kind][]func(node *ast.Node)) int {
	if node == nil {
		return 0
	}

	visited := 1
	for _, listener := range listeners[node.Kind] {
		listener(node)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		visited += walk(child, listeners)
		return false
	})

	return visited
}
