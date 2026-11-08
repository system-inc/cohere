// Command octalprefix writes each legacy octal integer literal, `0644`, with Go's `0o` prefix, `0o644`, the spelling
// cohere-go/consistency-require-octal-prefix asks for. It finds the literals by running that rule over every package
// of the workspace and takes the rule's own suggested fix for each, so it rewrites exactly what the rule flags, with
// the digits and separators as written: never a bare `0`, a float, an imaginary literal or a literal that already
// names its base. A literal in a generated file, a copy of upstream source or a skipped path is left alone, as every
// pass leaves those files, and counted with the reason. The rewrite is proven before anything is written: the tree
// is reloaded with it in memory and must type-check, every identifier must resolve as before, and every literal of
// each rewritten file must keep the type and constant value the type checker gave it, the rewritten ones spelled as
// the rule suggested. Any mismatch writes nothing.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"os"
	"sort"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_require_octal_prefix"
	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
	"github.com/system-inc/cohere/internal/streams"
)

// passName is what the verified apply calls this pass in a refusal.
const passName = "octalprefix"

// declinedLiteral is a literal the rule flags that the pass leaves as written, and why.
type declinedLiteral struct {
	position token.Position
	text     string
	reason   string
}

// planned is what plan finds: an edit for each literal to rewrite, possibly twice when two variants read its file,
// with its position for the report, and each literal left as written, keyed by its place.
type planned struct {
	edits     []rename.Edit
	positions map[rename.EditKey]token.Position
	declined  map[rename.EditKey]declinedLiteral
}

// literalRecord is one literal of a rewritten file as a load reads it: its spelling, whether the rewrite spells it,
// and the type and constant value the type checker gave it.
type literalRecord struct {
	text      string
	rewritten bool
	value     string
}

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code octalprefix exits with: runCommandLine returns its exit code here, and
// streams.Finish delivers what was printed and turns a clean exit into 1 when it did not arrive.
func finishedExitCode() int {
	// Deferred calls run while a panic unwinds, though not on os.Exit, so this delivers what was written before
	// the panic's own report, and main exits only after it has run.
	defer func() {
		streams.Show(streams.Errors)
		streams.Show(streams.Output)
	}()
	return streams.Finish(runCommandLine())
}

// runCommandLine reads the flags and runs octalprefix, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print the counts and every literal found, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	return run(*root, skips, *dryRun, streams.Output, streams.Errors)
}

// run finds every literal the rule flags and, unless dryRun, writes the rewrite proven, writing its report to output
// and its errors to errorOutput, and returns the code octalprefix exits with. main hands it streams.Output and
// streams.Errors, a test writers of its own. Each goes through buffered, whose error is sticky, so the Flush of each
// here says whether all of it arrived, and a run whose output did not arrive did not finish.
func run(root string, skips []string, dryRun bool, output io.Writer, errorOutput io.Writer) int {
	bufferedOutput, bufferedErrors := buffered(output), buffered(errorOutput)
	exitCode := rewrite(root, skips, dryRun, bufferedOutput, bufferedErrors)
	outputError, errorsError := bufferedOutput.Flush(), bufferedErrors.Flush()
	if outputError != nil || errorsError != nil {
		return max(exitCode, 1)
	}
	return exitCode
}

// buffered is writer itself when it already buffers, as streams.Output and streams.Errors do, so a Show of it
// reaches the terminal, and a new buffer over writer otherwise.
func buffered(writer io.Writer) *bufio.Writer {
	if stream, isBuffered := writer.(*bufio.Writer); isBuffered {
		return stream
	}
	return bufio.NewWriter(writer)
}

// rewrite is run with its writers buffered.
func rewrite(root string, skips []string, dryRun bool, output *bufio.Writer, errorOutput *bufio.Writer) int {
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(errorOutput, "%s: loading: %v\n", passName, loadError)
		return 1
	}
	fmt.Fprintf(output, "loaded %d root packages, analyzing %d variants\n", len(loaded.Roots), len(loaded.Analyzed))
	// The rule runs over every package, so the load's count is seen before it.
	streams.Show(output)

	found, planError := plan(loaded)
	if planError != nil {
		fmt.Fprintf(errorOutput, "%s: %v\n", passName, planError)
		return 1
	}
	edits, disagreements := rename.Deduplicate(found.edits)
	editsByFile := rename.GroupByFile(edits)
	report(loaded, edits, editsByFile, found, output)
	for _, disagreement := range disagreements {
		fmt.Fprintln(output, "  disagreement: "+disagreement)
	}
	if len(disagreements) > 0 {
		fmt.Fprintf(errorOutput, "%s: two variants of a file disagree on a literal's rewrite, so nothing is written\n",
			passName)
		return 1
	}
	if dryRun {
		return 0
	}
	// The proof reloads the whole workspace, several gigabytes on a large tree, and with nothing to rewrite there is
	// nothing to prove, so a second run over a rewritten tree ends here.
	if len(edits) == 0 {
		fmt.Fprintln(output, "wrote 0 files, nothing to rewrite")
		return 0
	}
	// The write reloads the whole workspace to prove it, so the plan is seen before that wait.
	streams.Show(output)
	return workspace.ApplyVerifiedChecked(
		loaded,
		root,
		skips,
		edits,
		editsByFile,
		literalCheck(loaded, edits),
		passName,
		output,
		errorOutput,
	)
}

// plan runs the rule over every analyzed variant's files and sorts each literal it flags: an edit holding the rule's
// own fix when the literal is in a file the workspace lets a pass edit and the type checker gave it a value to
// prove, and a reason it is left as written otherwise. A literal one variant edits is not declined by another.
func plan(loaded *workspace.Workspace) (planned, error) {
	result := planned{
		positions: map[rename.EditKey]token.Position{},
		declined:  map[rename.EditKey]declinedLiteral{},
	}
	for _, analyzedPackage := range loaded.Analyzed {
		diagnostics, findError := findings(analyzedPackage)
		if findError != nil {
			return planned{}, fmt.Errorf("running the rule over %s: %w", analyzedPackage.ID, findError)
		}
		fileOf := map[*token.File]*ast.File{}
		literalAt := map[token.Pos]*ast.BasicLit{}
		for _, file := range analyzedPackage.Syntax {
			fileOf[analyzedPackage.Fset.File(file.Pos())] = file
			ast.Inspect(file, func(node ast.Node) bool {
				if literal, isLiteral := node.(*ast.BasicLit); isLiteral {
					literalAt[literal.Pos()] = literal
				}
				return true
			})
		}
		for _, diagnostic := range diagnostics {
			position := analyzedPackage.Fset.PositionFor(diagnostic.Pos, false)
			key := rename.EditKey{Filename: position.Filename, Offset: position.Offset}
			literal := literalAt[diagnostic.Pos]
			file := fileOf[analyzedPackage.Fset.File(diagnostic.Pos)]
			if literal == nil || file == nil {
				result.declined[key] = declinedLiteral{position: position, reason: "the finding is at no literal"}
				continue
			}
			reason := loaded.Uneditable(analyzedPackage, file)
			newText, fixed := onlyFix(diagnostic, literal)
			if reason == "" && !fixed {
				reason = "the rule's fix is not one edit of the literal"
			}
			if typeAndValue := analyzedPackage.TypesInfo.Types[literal]; reason == "" && typeAndValue.Value == nil {
				reason = "the type checker gave it no constant value to prove"
			}
			if reason != "" {
				result.declined[key] = declinedLiteral{position: position, text: literal.Value, reason: reason}
				continue
			}
			result.positions[key] = position
			result.edits = append(result.edits, rename.Edit{
				Filename: position.Filename,
				Offset:   position.Offset,
				OldText:  literal.Value,
				NewText:  newText,
			})
		}
	}
	for key := range result.positions {
		delete(result.declined, key)
	}
	return result, nil
}

// findings runs the rule over goPackage's files and returns what it reports. The rule reads only the files and
// reports, so the pass needs nothing else of an analysis driver.
func findings(goPackage *packages.Package) ([]analysis.Diagnostic, error) {
	var diagnostics []analysis.Diagnostic
	pass := &analysis.Pass{
		Analyzer:  consistency_require_octal_prefix.Analyzer,
		Fset:      goPackage.Fset,
		Files:     goPackage.Syntax,
		Pkg:       goPackage.Types,
		TypesInfo: goPackage.TypesInfo,
		ResultOf:  map[*analysis.Analyzer]any{},
		Report:    func(diagnostic analysis.Diagnostic) { diagnostics = append(diagnostics, diagnostic) },
	}
	_, runError := consistency_require_octal_prefix.Analyzer.Run(pass)
	return diagnostics, runError
}

// onlyFix is the text of diagnostic's fix when it is one edit spanning literal exactly, as the rule offers it.
func onlyFix(diagnostic analysis.Diagnostic, literal *ast.BasicLit) (string, bool) {
	if len(diagnostic.SuggestedFixes) != 1 || len(diagnostic.SuggestedFixes[0].TextEdits) != 1 {
		return "", false
	}
	edit := diagnostic.SuggestedFixes[0].TextEdits[0]
	if edit.Pos != literal.Pos() || edit.End != literal.End() {
		return "", false
	}
	return string(edit.NewText), true
}

// report prints the counts, every literal to rewrite and every literal left as written with its reason.
func report(
	loaded *workspace.Workspace,
	edits []rename.Edit,
	editsByFile map[string][]rename.Edit,
	found planned,
	output *bufio.Writer,
) {
	place := func(position token.Position) string {
		if relative, inside := loaded.Relative(position.Filename); inside {
			return fmt.Sprintf("%s:%d:%d", relative, position.Line, position.Column)
		}
		return position.String()
	}
	reasons := map[string]int{}
	declinedLines := make([]string, 0, len(found.declined))
	for _, literal := range found.declined {
		reasons[literal.reason]++
		declinedLines = append(declinedLines, fmt.Sprintf("  declined: %s %s: %s",
			place(literal.position), literal.text, literal.reason))
	}
	sort.Strings(declinedLines)
	fmt.Fprintf(output, "candidates: %d legacy octal literals the rule flags\n", len(edits)+len(found.declined))
	fmt.Fprintf(output, "accepted: %d literals to rewrite in %d files\n", len(edits), len(editsByFile))
	fmt.Fprintf(output, "rejected: %d literals left as written\n", len(found.declined))
	reasonNames := make([]string, 0, len(reasons))
	for reason := range reasons {
		reasonNames = append(reasonNames, reason)
	}
	sort.Strings(reasonNames)
	for _, reason := range reasonNames {
		fmt.Fprintf(output, "rejected, %s: %d\n", reason, reasons[reason])
	}
	for _, edit := range edits {
		position := found.positions[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}]
		fmt.Fprintf(output, "  rewrite: %s %s -> %s\n", place(position), edit.OldText, edit.NewText)
	}
	for _, line := range declinedLines {
		fmt.Fprintln(output, line)
	}
}

// literalCheck is the proof the identifiers cannot give: every literal of each rewritten file, in each package
// variant that compiles the file, in source order, which a rewrite of spellings does not change. Each must keep the
// type and constant value the type checker gave it before, and each rewritten one must read as the rule suggested.
// Any other literal is held to its value alone, since gofmt may respell one, `0X1F` as `0x1F`, without changing it.
func literalCheck(loaded *workspace.Workspace, edits []rename.Edit) workspace.Verify {
	newTextAt := map[rename.EditKey]string{}
	files := map[string]bool{}
	for _, edit := range edits {
		newTextAt[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}] = edit.NewText
		files[edit.Filename] = true
	}
	expected := literalsOf(loaded.Roots, files, newTextAt)
	return func(reloaded *workspace.Workspace, output *bufio.Writer) int {
		actual := literalsOf(reloaded.Roots, files, nil)
		mismatches, compared, respelled := 0, 0, 0
		keys := make([]string, 0, len(expected))
		for key := range expected {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			expectedRecords := expected[key]
			actualRecords, found := actual[key]
			if !found || len(actualRecords) != len(expectedRecords) {
				fmt.Fprintf(output, "mismatch: %s: %d literals before, %d after\n", key, len(expectedRecords),
					len(actualRecords))
				mismatches++
				continue
			}
			for index, record := range expectedRecords {
				compared++
				after := actualRecords[index]
				if after.value != record.value || (record.rewritten && after.text != record.text) {
					fmt.Fprintf(output, "mismatch: %s, literal %d: expected %s (%s), got %s (%s)\n", key, index,
						record.text, record.value, after.text, after.value)
					mismatches++
					continue
				}
				if record.rewritten {
					respelled++
				}
			}
		}
		for key := range actual {
			if _, found := expected[key]; !found {
				fmt.Fprintf(output, "mismatch: %s holds rewritten literals only after the rewrite\n", key)
				mismatches++
			}
		}
		fmt.Fprintf(output, "re-verification: %d literals compared in %d package files, %d spelled as the rule "+
			"suggested, %d mismatches\n", compared, len(keys), respelled, mismatches)
		return mismatches
	}
}

// literalsOf records every literal of each file named in files, as each package of roots compiles it, keyed by the
// package and the file. A literal at a place newTextAt names is recorded with its new spelling, as rewritten.
func literalsOf(
	roots []*packages.Package,
	files map[string]bool,
	newTextAt map[rename.EditKey]string,
) map[string][]literalRecord {
	records := map[string][]literalRecord{}
	for _, rootsPackage := range roots {
		for _, file := range rootsPackage.Syntax {
			filename := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			if !files[filename] {
				continue
			}
			key := rootsPackage.ID + " " + filename
			records[key] = []literalRecord{}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, isLiteral := node.(*ast.BasicLit)
				if !isLiteral {
					return true
				}
				record := literalRecord{text: literal.Value, value: valueOf(rootsPackage.TypesInfo, literal)}
				offset := rootsPackage.Fset.PositionFor(literal.Pos(), false).Offset
				if newText, isRewritten := newTextAt[rename.EditKey{Filename: filename, Offset: offset}]; isRewritten {
					record.text, record.rewritten = newText, true
				}
				records[key] = append(records[key], record)
				return true
			})
		}
	}
	return records
}

// valueOf is the type and exact constant value the type checker gave literal, or a placeholder when it gave none, as
// it gives none for an import path or a struct tag.
func valueOf(info *types.Info, literal *ast.BasicLit) string {
	typeAndValue, found := info.Types[literal]
	if !found || typeAndValue.Value == nil {
		return "no constant value"
	}
	return typeAndValue.Type.String() + " " + typeAndValue.Value.ExactString()
}
