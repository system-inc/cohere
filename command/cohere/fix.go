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
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/suppression"
	"github.com/system-inc/cohere/internal/types/program"
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
	transform edit.Transform,
	formatCandidates []string,
	maxPasses int,
) (edit.Summary, program.Result, error) {
	// This walk is the run's FIRST walk over the program, and it is returned so the lint phase can
	// reuse it rather than repeat it.
	//
	// An earlier version of this comment said the opposite: that a reporting walk had already
	// happened and this one merely asked again, cheaply. Both halves were wrong. Fix is phase 2 and
	// lint runs after phase 3, so nothing walks before this; and measured on the ahra tree a run
	// costs roughly a fixed 0.9s plus 1.1s per walk, so this is about a third of a default run
	// rather than a cheap repeat. The judgment was disclosed at the call site, which is the only
	// reason it was checkable at all.
	//
	// Returning the result rather than dropping it is what lets the caller decide. That decision is
	// the caller's and not this function's, because only the caller knows whether anything was
	// rewritten afterward: a file rewritten here makes these diagnostics describe bytes that no
	// longer exist.
	result, err := graph.Walk(ctx, projectFiles, rules)
	if err != nil {
		return edit.Summary{}, program.Result{}, fmt.Errorf("collecting proposals: %w", err)
	}

	// Group proposals by the file they belong to. A diagnostic carries its source file, so the
	// grouping is exact rather than inferred from a range.
	byFileName := map[string][]edit.Proposal{}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.SourceFile == nil || len(diagnostic.Fixes) == 0 {
			continue
		}
		fileName := diagnostic.SourceFile.FileName()
		for _, proposed := range diagnostic.Fixes {
			byFileName[fileName] = append(byFileName[fileName], edit.Proposal{
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
	// The formattable half comes from the scope rather than from the program, and this is the second
	// half of the split the ruling made. A proposed fix comes from a rule that ran over the program,
	// so its candidate must be in the program. A format candidate comes from the disk.
	//
	// Taking them from projectFiles is what made css, markdown, json and yaml invisible: a tsconfig
	// enumerates TypeScript by construction, so a `.css` was never a candidate, and the coverage line
	// could not even report it as skipped. The scope already holds the right set, walked from the
	// project root with the ignore layers applied and filtered by what the engine handles.
	for _, fileName := range formatCandidates {
		candidates[fileName] = struct{}{}
	}

	if len(candidates) == 0 {
		// An empty run still reports its population, so "nothing proposed a fix" cannot be confused
		// with "the fixer never ran".
		return edit.Summarize(nil), result, nil
	}

	// Sorted so a run is reproducible and a diff of two runs is readable.
	fileNames := make([]string, 0, len(candidates))
	for fileName := range candidates {
		fileNames = append(fileNames, fileName)
	}
	sort.Strings(fileNames)

	results := make([]edit.FileResult, 0, len(fileNames))
	for _, fileName := range fileNames {
		// The first pass reuses the proposals already collected; later passes re-lint the rewritten
		// text. Reusing them for the first pass and only the first pass is what keeps offsets honest:
		// a proposal is valid exactly against the text it was computed from.
		firstPass := byFileName[fileName]
		used := false

		propose := func(_ string, text string) ([]edit.Proposal, error) {
			if !used {
				used = true
				return firstPass, nil
			}
			return proposalsForText(fileName, text, graph, rules)
		}

		fileResult, err := edit.FixAndTransformFile(fileName, propose, transform, maxPasses)
		if err != nil {
			// One file failing must not abandon the rest. The failure is reported rather than
			// swallowed, and the tree is left in a state where every other fix still landed.
			fmt.Fprintf(os.Stderr, "cohere: %v\n", err)

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

	return edit.Summarize(results), result, nil
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
) ([]edit.Proposal, error) {
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

	/*
	 * Options are resolved here, not left nil, and the omission was a real defect rather than a
	 * simplification.
	 *
	 * A rule handed nil options falls back to whatever it does unconfigured, and for several rules
	 * that is to decline entirely: `import-require-path-alias` reads its alias map from options, so
	 * with nil it proposes nothing. That made every pass after the first blind to it. The visible
	 * symptom was a file needing two separate `--fix` invocations to settle, because the first pass
	 * applied one fix and the second pass could no longer see the other one to apply it.
	 *
	 * It hid because the first pass reuses the proposals the lint phase already collected, which do
	 * have options. Only passes two and later go through here, and only a file with two overlapping
	 * fixes ever needs a second pass at all.
	 */
	applicable := rules
	ruleOptions := map[string]any{}
	if graph.LintConfig != nil {
		resolution := graph.LintConfig.Resolve(fileName)
		if resolution.Ignored {
			return nil, nil
		}
		applicable = applicable[:0:0]
		for _, subject := range rules {
			if !resolution.Enabled(subject.Name) {
				continue
			}
			decoded, decodeError := graph.RuleOptions.Decode(subject.Name, resolution.RawOptionsFor(subject.Name))
			if decodeError != nil {
				return nil, fmt.Errorf("decoding options for %s on %s: %w", subject.Name, fileName, decodeError)
			}
			ruleOptions[subject.Name] = decoded
			applicable = append(applicable, subject)
		}
	}

	/*
	 * A rule that needs the type checker cannot run here, and running it anyway crashed the whole
	 * command.
	 *
	 * This pass re-lints text that has just been rewritten, which by definition is not the text the
	 * program was built from, so no checker in the graph describes it: `ctx.TypeChecker` is nil and
	 * a rule declaring `NeedsTypeChecker` dereferences it on its first question.
	 * `react/no-danger-with-children` is the one that found it, at `resolvedDeclaration`.
	 *
	 * It stayed hidden because reaching this code takes two things at once: a fix that lands, and a
	 * second pass to check it. Every fixable rule in the tree happened to be syntactic, so until a
	 * fixer landed on a file that also carries a checker-needing rule, the pass simply never ran
	 * with one in the list.
	 *
	 * Skipping is the honest answer rather than a workaround. A type-aware rule has nothing true to
	 * say about text no program contains, so its verdict here would be a guess whichever way it
	 * came out. The cost is bounded and worth naming: a fix that introduces a type-aware violation
	 * is not caught by the pass that applied it, and is caught by the next real lint run.
	 */
	withoutTypeChecker := applicable[:0:0]
	for _, subject := range applicable {
		if subject.NeedsTypeChecker {
			continue
		}
		withoutTypeChecker = append(withoutTypeChecker, subject)
	}
	applicable = withoutTypeChecker

	/*
	 * The same suppression index the lint path builds, for the same reason.
	 *
	 * `internal/types/program/walk.go:500` filters a diagnostic through `directives.Suppresses`
	 * before reporting it. This path did not, so a rule's finding was correctly withheld and its
	 * FIX was applied anyway: both linters reported nothing on a disabled line and both fixers
	 * rewrote it. That is worse than either half alone, because the file changes with no diagnostic
	 * explaining why and the only evidence is in `git diff`.
	 *
	 * Demonstrated rather than theorised. `libraries/structure/source/router/hooks/useRouter.ts`
	 * carries a deliberate `eslint-disable-next-line nexus/import-no-forbidden-source`, and a fixer
	 * run reverted it, reintroducing a circular import worth 108 findings across five rules.
	 */
	directives := suppression.Build(sourceFile.Text())

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
				if directives.Suppresses(diagnostic.RuleName, diagnostic.Range.Pos()) {
					return
				}
				diagnostics = append(diagnostics, diagnostic)
			},
		}

		listeners := currentRule.Run(context, ruleOptions[currentRule.Name])
		if len(listeners) == 0 {
			continue
		}
		walkNode(sourceFile.AsNode(), listeners)
	}

	return edit.ProposalsFrom(diagnostics), nil
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
