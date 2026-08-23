package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/verify/internal/fix"
	"github.com/system-inc/verify/internal/program"
	"github.com/system-inc/verify/internal/rule"
)

// applyProposedFixes rewrites every file whose rules proposed a repair, and reports what it did.
//
// Each file converges on its own rather than the tree converging as a whole. A fix is local to the
// file it edits, so a per-file fixpoint reaches the same result as a whole-tree loop while re-linting
// only the files that actually changed — and it means one pathological file cannot force every other
// file through another pass.
//
// Only files with at least one proposal are considered. A file nobody proposed anything for is never
// opened, never re-parsed, and never written, which is what keeps a fix run proportional to the work
// rather than to the tree.
func applyProposedFixes(
	ctx context.Context,
	graph *program.Graph,
	projectFiles []*ast.SourceFile,
	rules []rule.Rule,
	transform fix.Transform,
	maxPasses int,
) (fix.Summary, error) {
	// The first walk already happened for reporting; this asks again because the caller does not hand
	// the diagnostics down. Asking is cheap relative to the fix loop and keeps this function honest
	// about what it is fixing: the state of the tree right now.
	result, err := graph.Walk(ctx, projectFiles, rules)
	if err != nil {
		return fix.Summary{}, fmt.Errorf("collecting proposals: %w", err)
	}

	// Group proposals by the file they belong to. A diagnostic carries its source file, so the
	// grouping is exact rather than inferred from a range.
	byFileName := map[string][]fix.Proposal{}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.SourceFile == nil || len(diagnostic.Fixes) == 0 {
			continue
		}
		fileName := diagnostic.SourceFile.FileName()
		for _, proposed := range diagnostic.Fixes {
			byFileName[fileName] = append(byFileName[fileName], fix.Proposal{
				RuleName: diagnostic.RuleName,
				Fix:      proposed,
			})
		}
	}

	// The file set is the union of "something proposed a fix here" and "this file can be formatted",
	// and the union rather than the intersection is the whole point. Fixing is driven by findings, so
	// it only visits files a rule complained about. Formatting is not: a file can be perfectly correct
	// and badly formatted, and a formatter that only ran where a rule had already fired would never
	// touch most of a tree while reporting a clean run.
	//
	// When no transform is configured the union collapses back to the files with proposals, so a
	// fix-only run costs exactly what it did before.
	candidates := map[string]struct{}{}
	for fileName := range byFileName {
		candidates[fileName] = struct{}{}
	}
	if transform != nil {
		for _, sourceFile := range projectFiles {
			candidates[sourceFile.FileName()] = struct{}{}
		}
	}

	if len(candidates) == 0 {
		// An empty run still reports its population, so "nothing proposed a fix" cannot be confused
		// with "the fixer never ran".
		return fix.Summarize(nil), nil
	}

	// Sorted so a run is reproducible and a diff of two runs is readable.
	fileNames := make([]string, 0, len(candidates))
	for fileName := range candidates {
		fileNames = append(fileNames, fileName)
	}
	sort.Strings(fileNames)

	results := make([]fix.FileResult, 0, len(fileNames))
	for _, fileName := range fileNames {
		// The first pass reuses the proposals already collected; later passes re-lint the rewritten
		// text. Reusing them for the first pass and only the first pass is what keeps offsets honest:
		// a proposal is valid exactly against the text it was computed from.
		firstPass := byFileName[fileName]
		used := false

		propose := func(_ string, text string) ([]fix.Proposal, error) {
			if !used {
				used = true
				return firstPass, nil
			}
			return proposalsForText(fileName, text, graph, rules)
		}

		fileResult, err := fix.FixAndTransformFile(fileName, propose, transform, maxPasses)
		if err != nil {
			// One file failing must not abandon the rest. The failure is reported rather than
			// swallowed, and the tree is left in a state where every other fix still landed.
			fmt.Fprintf(os.Stderr, "verify: %v\n", err)

			// A failure is its own category rather than a flavor of "did not converge". The two send a
			// reader somewhere different: not-converged means the pass budget ran out with work still
			// landing, which points at two rules arguing, while a failure means the file never entered
			// the loop at all. A zero-value FileResult has Converged false, so leaving it alone would
			// file this under the wrong heading — a real number reported as the wrong finding.
			fileResult.FileName = fileName
			fileResult.Failed = true
			fileResult.Changed = false
		}
		results = append(results, fileResult)
	}

	return fix.Summarize(results), nil
}

// proposalsForText re-runs the rules against rewritten text.
//
// The file is re-parsed rather than re-read, because the text in hand is the candidate that has not
// been written yet. This pass has no type information: a re-parse produces a standalone file rather
// than a member of the program, and building a whole new program per pass would cost seconds. A
// type-aware rule that proposes a fix will therefore contribute on the first pass, where the real
// checker was present, and not on later ones. That is a real limitation and it is stated rather than
// hidden — the alternative is a fix run that rebuilds a 9,530-file program once per pass per file,
// which is precisely the cost this tool exists to remove.
func proposalsForText(
	fileName string,
	text string,
	graph *program.Graph,
	rules []rule.Rule,
) ([]fix.Proposal, error) {
	rooted := fileName
	if !tspath.IsRootedDiskPath(rooted) {
		rooted = "/" + rooted
	}
	rooted = tspath.NormalizePath(rooted)

	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: rooted,
		Path:     tspath.Path(rooted),
	}, text, core.GetScriptKindFromFileName(rooted))
	if sourceFile == nil {
		return nil, fmt.Errorf("re-parsing %s produced nothing", fileName)
	}

	applicable := rules
	if graph.LintConfig != nil {
		resolution := graph.LintConfig.Resolve(fileName)
		if resolution.Ignored {
			return nil, nil
		}
		applicable = applicable[:0:0]
		for _, subject := range rules {
			if resolution.Enabled(subject.Name) {
				applicable = append(applicable, subject)
			}
		}
	}

	var diagnostics []rule.Diagnostic
	for _, subject := range applicable {
		currentRule := subject
		context := rule.Context{
			SourceFile: sourceFile,
			Program:    graph.Program,
			Report: func(diagnostic rule.Diagnostic) {
				diagnostic.RuleName = currentRule.Name
				if diagnostic.SourceFile == nil {
					diagnostic.SourceFile = sourceFile
				}
				diagnostics = append(diagnostics, diagnostic)
			},
		}

		listeners := currentRule.Run(context, nil)
		if len(listeners) == 0 {
			continue
		}
		walkNode(sourceFile.AsNode(), listeners)
	}

	return fix.ProposalsFrom(diagnostics), nil
}

// walkNode visits every node, calling any listener registered for its kind.
func walkNode(node *ast.Node, listeners rule.Listeners) {
	if node == nil {
		return
	}
	if listener, isListening := listeners[node.Kind]; isListening {
		listener(node)
	}
	node.ForEachChild(func(child *ast.Node) bool {
		walkNode(child, listeners)
		return false
	})
}
