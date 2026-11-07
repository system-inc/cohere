// Command contextfields renames each struct field named `ctx` whose type, after one pointer, is a type named
// exactly Context declared in a package of the workspace: the field takes its type's package name in front,
// so `ctx rule.Context` becomes `ruleContext`, at its declaration and at every selector and composite literal
// key that names it, in every package. It is contextnames' rule for a local, applied to a field, which only
// the cross-package engine can rename. A field of any other type is left, and listed.
//
// A field is refused, never numbered, when some selector of it sits where the new name already means
// something else: a method holding a `ruleContext` parameter and reading `walker.ctx` would read
// `walker.ruleContext` beside `ruleContext`, two names for what may be two contexts. Every other rename goes
// through the global rename engine, which refuses any that could change what the program means; the rewrite
// is reloaded in memory and every identifier's resolution compared before anything is written.
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
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
	"github.com/system-inc/cohere/internal/streams"
)

// commandName prefixes every message this command prints to standard error.
const commandName = "contextfields"

// abbreviatedName is the field name this command spells out.
const abbreviatedName = "ctx"

// skipFlags collects repeated -skip values.
type skipFlags []string

func (skips *skipFlags) String() string { return strings.Join(*skips, ",") }

func (skips *skipFlags) Set(value string) error {
	*skips = append(*skips, value)
	return nil
}

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code contextfields exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs contextfields, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print the plan, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	return run(*root, skips, *dryRun, streams.Output, streams.Errors)
}

// field is one ctx field and what this command decided for it.
type field struct {
	proposal global_rename.Proposal
	position token.Position
	// typeName is the field's type as the report prints it.
	typeName string
}

// collision is a selector of a field where its new name is already in scope.
type collision struct {
	field field
	// selector is where the field is read; declared is what the new name means there.
	selector token.Position
	declared string
}

// run renames the fields and proves the rewrite, or with dryRun only plans and reports it, writing its report to
// output and its errors to errorOutput, and returns the code contextfields exits with. main hands it
// streams.Output and streams.Errors, a test writers of its own, so two runs never share one. Each goes through
// buffered, whose error is sticky, so the Flush of each here says whether all of it arrived, and a run whose output
// did not arrive did not finish. A failure of the streams is said where streams.Finish runs.
func run(root string, skips []string, dryRun bool, output io.Writer, errorOutput io.Writer) int {
	bufferedOutput, bufferedErrors := buffered(output), buffered(errorOutput)
	exitCode := renameFields(root, skips, dryRun, bufferedOutput, bufferedErrors)
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

// renameFields is run with its writers buffered.
func renameFields(root string, skips []string, dryRun bool, output *bufio.Writer, errorOutput *bufio.Writer) int {
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(errorOutput, "%s: loading: %v\n", commandName, loadError)
		return 1
	}
	fmt.Fprintf(
		output,
		"loaded %d root packages, analyzing %d variants\n",
		len(loaded.Roots),
		len(loaded.Analyzed),
	)
	// The analysis takes a while on a whole worktree, so the load's count is seen before it.
	streams.Show(output)

	taken, left := findFields(loaded)
	collisions := findCollisions(loaded, taken)
	collided := map[types.Object]bool{}
	for _, found := range collisions {
		collided[found.field.proposal.Object] = true
	}
	var proposals []global_rename.Proposal
	for _, candidate := range taken {
		if !collided[candidate.proposal.Object] {
			proposals = append(proposals, candidate.proposal)
		}
	}
	fmt.Fprintf(
		output,
		"candidates: %d ctx fields of a workspace Context type, %d refused for a collision in scope, "+
			"%d ctx fields of another type left\n",
		len(taken),
		len(collisions),
		len(left),
	)

	result := global_rename.Plan(loaded, proposals, global_rename.Options{ReviewedLiterals: reviewedLiterals})
	report(output, loaded, collisions, left, result)

	if dryRun {
		return 0
	}
	if len(result.Unresolved)+len(result.SelectionModelMismatches)+len(result.SatisfactionModelMismatches) > 0 {
		fmt.Fprintf(errorOutput, "%s: the engine's model disagreed with go/types somewhere; the renames it touched "+
			"were refused, the rest go on to the verified write\n", commandName)
	}
	// The write reloads the whole workspace to prove it, so the plan is seen before that wait.
	streams.Show(output)
	mismatches, applyError := global_rename.Apply(loaded, root, skips, result, output)
	if applyError != nil {
		fmt.Fprintf(errorOutput, "%s: %v\n", commandName, applyError)
		return 1
	}
	if mismatches > 0 {
		return 1
	}
	return 0
}

// findFields reads every analyzed variant and returns, once per declaration, the ctx fields it renames and the
// ctx fields of any other type, which it leaves.
func findFields(loaded *workspace.Workspace) ([]field, []field) {
	workspacePackages := map[string]bool{}
	for _, rootsPackage := range loaded.Roots {
		workspacePackages[rootsPackage.PkgPath] = true
	}
	var taken []field
	var left []field
	seen := map[string]bool{}
	for _, analyzedPackage := range loaded.Analyzed {
		for _, file := range analyzedPackage.Syntax {
			if _, inside := loaded.Relative(analyzedPackage.Fset.PositionFor(file.Package, false).Filename); !inside {
				continue
			}
			ast.Inspect(file, func(node ast.Node) bool {
				structType, isStruct := node.(*ast.StructType)
				if !isStruct {
					return true
				}
				for _, declared := range structType.Fields.List {
					for _, name := range declared.Names {
						if name.Name != abbreviatedName {
							continue
						}
						variable, isVariable := analyzedPackage.TypesInfo.Defs[name].(*types.Var)
						if !isVariable || !variable.IsField() {
							continue
						}
						position := analyzedPackage.Fset.PositionFor(variable.Pos(), false)
						identity := fmt.Sprintf("%s:%d", position.Filename, position.Offset)
						if seen[identity] {
							continue
						}
						seen[identity] = true
						candidate := field{
							proposal: global_rename.Proposal{Package: analyzedPackage, Object: variable},
							position: position,
							typeName: types.TypeString(variable.Type(), (*types.Package).Name),
						}
						candidate.proposal.NewName = contextName(variable.Type(), workspacePackages)
						if candidate.proposal.NewName == "" {
							left = append(left, candidate)
							continue
						}
						taken = append(taken, candidate)
					}
				}
				return true
			})
		}
	}
	return taken, left
}

// contextName is the name a field of fieldType takes: its package's name and Context, for a type named exactly
// Context in a workspace package, seen through one pointer. Anything else is "".
func contextName(fieldType types.Type, workspacePackages map[string]bool) string {
	dereferenced := fieldType
	if pointer, isPointer := types.Unalias(fieldType).(*types.Pointer); isPointer {
		dereferenced = pointer.Elem()
	}
	named, isNamed := types.Unalias(dereferenced).(*types.Named)
	if !isNamed || named.Obj().Pkg() == nil || named.Obj().Name() != "Context" ||
		!workspacePackages[named.Obj().Pkg().Path()] {
		return ""
	}
	return named.Obj().Pkg().Name() + "Context"
}

// findCollisions finds, for each field, the first selector of it, in source order across every root package,
// where its new name already resolves to a declaration: a parameter, a local, anything in scope there.
func findCollisions(loaded *workspace.Workspace, taken []field) []collision {
	byPosition := map[token.Position]field{}
	for _, candidate := range taken {
		byPosition[candidate.position] = candidate
	}
	found := map[token.Position]collision{}
	for _, rootsPackage := range loaded.Roots {
		for selector, selection := range rootsPackage.TypesInfo.Selections {
			if selection.Kind() != types.FieldVal {
				continue
			}
			candidate, isCandidate := byPosition[rootsPackage.Fset.PositionFor(selection.Obj().Pos(), false)]
			if !isCandidate {
				continue
			}
			scope := rootsPackage.Types.Scope().Innermost(selector.Pos())
			if scope == nil {
				continue
			}
			_, declared := scope.LookupParent(candidate.proposal.NewName, selector.Pos())
			if declared == nil {
				continue
			}
			where := rootsPackage.Fset.PositionFor(selector.Sel.Pos(), false)
			if earlier, recorded := found[candidate.position]; recorded && !positionBefore(where, earlier.selector) {
				continue
			}
			found[candidate.position] = collision{
				field:    candidate,
				selector: where,
				declared: fmt.Sprintf("%s %s declared at %s", kindName(declared), declared.Name(),
					rootsPackage.Fset.PositionFor(declared.Pos(), false)),
			}
		}
	}
	collisions := make([]collision, 0, len(found))
	for _, entry := range found {
		collisions = append(collisions, entry)
	}
	sort.Slice(collisions, func(left, right int) bool {
		return positionBefore(collisions[left].field.position, collisions[right].field.position)
	})
	return collisions
}

// kindName names what a declaration in scope is, for the report.
func kindName(object types.Object) string {
	switch object.(type) {
	case *types.Var:
		return "variable"
	case *types.Const:
		return "constant"
	case *types.TypeName:
		return "type"
	case *types.Func:
		return "function"
	case *types.PkgName:
		return "import"
	}
	return "declaration"
}

func positionBefore(left, right token.Position) bool {
	if left.Filename != right.Filename {
		return left.Filename < right.Filename
	}
	return left.Offset < right.Offset
}

// report prints the counts, then every list a reviewer needs.
func report(
	output *bufio.Writer,
	loaded *workspace.Workspace,
	collisions []collision,
	left []field,
	result *global_rename.Result,
) {
	relative := func(position token.Position) string {
		if path, inside := loaded.Relative(position.Filename); inside {
			return fmt.Sprintf("%s:%d", path, position.Line)
		}
		return position.String()
	}
	trim := func(text string) string { return strings.ReplaceAll(text, loaded.Root+"/", "") }

	fmt.Fprintf(output, "accepted: %d\n", len(result.Accepted))
	fmt.Fprintf(
		output,
		"refused: %d by the engine, %d for a collision in scope\n",
		len(result.Rejections),
		len(collisions),
	)
	byClass := map[string]int{}
	for _, rejection := range result.Rejections {
		byClass[rejection.Class]++
	}
	classes := make([]string, 0, len(byClass))
	for class := range byClass {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	for _, class := range classes {
		fmt.Fprintf(output, "  %s: %d\n", class, byClass[class])
	}
	editFiles := map[string]bool{}
	for _, edit := range result.Edits {
		editFiles[edit.Filename] = true
	}
	fmt.Fprintf(
		output,
		"edits: %d identifiers in %d files, %d comment respellings\n",
		len(result.Edits),
		len(editFiles),
		len(result.CommentEdits),
	)
	fmt.Fprintf(
		output,
		"model checks: %d unresolved scope walks, %d selection mismatches, %d satisfaction mismatches\n",
		len(result.Unresolved),
		len(result.SelectionModelMismatches),
		len(result.SatisfactionModelMismatches),
	)

	fmt.Fprintln(output, "\nrefused for a collision in scope:")
	for _, found := range collisions {
		fmt.Fprintf(
			output,
			"  %s: field %s -> %s: read at %s, where %s\n",
			relative(found.field.position),
			abbreviatedName,
			found.field.proposal.NewName,
			relative(found.selector),
			trim(found.declared),
		)
	}
	fmt.Fprintln(output, "\nrefused by the engine:")
	for _, rejection := range result.Rejections {
		fmt.Fprintf(
			output,
			"  %s: %s %s -> %s: [%s] %s\n",
			relative(rejection.Position),
			rejection.Kind,
			rejection.OldName,
			rejection.NewName,
			rejection.Class,
			trim(rejection.Reason),
		)
	}
	fmt.Fprintln(output, "\nleft, a ctx field of another type:")
	for _, entry := range left {
		fmt.Fprintf(output, "  %s: %s\n", relative(entry.position), entry.typeName)
	}
	fmt.Fprintln(output, "\naccepted:")
	for _, accepted := range result.Accepted {
		fmt.Fprintf(
			output,
			"  %s: %s %s -> %s\n",
			relative(accepted.Position),
			accepted.Kind,
			accepted.OldName,
			accepted.NewName,
		)
	}
	for _, section := range []struct {
		title   string
		entries []string
	}{
		{"unresolved scope walks", result.Unresolved},
		{"selection model mismatches", result.SelectionModelMismatches},
		{"satisfaction model mismatches", result.SatisfactionModelMismatches},
		{"notes", result.Notes},
	} {
		fmt.Fprintf(output, "\n%s:\n", section.title)
		for _, entry := range section.entries {
			fmt.Fprintln(output, "  "+trim(entry))
		}
	}
}
