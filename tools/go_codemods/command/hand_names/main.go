// Command handnames applies renames a person chose, one per line of a list: `path:line:column newName`, where
// the position is the declaring identifier of a local variable (a parameter, named result or local) as the
// findings census reports it. The judgment is in the list; this command only finds each declaration, hands every
// rename to the rename engine, which refuses any that would change what an identifier resolves to, and writes
// through the verified apply, which reloads the tree with the rewrite in memory and compares every identifier's
// resolution before writing anything. A line whose position declares no local is reported, never guessed at.
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
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
	"github.com/system-inc/cohere/internal/streams"
)

// skipFlags collects repeated -skip values.
type skipFlags []string

func (skips *skipFlags) String() string { return strings.Join(*skips, ",") }

func (skips *skipFlags) Set(value string) error {
	*skips = append(*skips, value)
	return nil
}

// request is one line of the list: the declaration at a position, and the name chosen for it.
type request struct {
	position string
	newName  string
}

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code handnames exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs handnames, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print every request's outcome, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	list := flag.String("list", "", "the list of renames, one `path:line:column newName` per line")
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	if *list == "" {
		fmt.Fprintln(streams.Errors, "handnames: -list is required")
		return 2
	}
	return run(*root, *list, skips, *dryRun, streams.Output, streams.Errors)
}

// run checks every rename of the list and, unless dryRun, writes them proven, writing its report to output and its
// errors to errorOutput, and returns the code handnames exits with. main hands it streams.Output and
// streams.Errors, a test writers of its own, so two runs never share one. Each goes through buffered, whose error
// is sticky, so the Flush of each here says whether all of it arrived, and a run whose output did not arrive did
// not finish. A failure of the streams is said where streams.Finish runs.
func run(root string, list string, skips []string, dryRun bool, output io.Writer, errorOutput io.Writer) int {
	bufferedOutput, bufferedErrors := buffered(output), buffered(errorOutput)
	exitCode := applyList(root, list, skips, dryRun, bufferedOutput, bufferedErrors)
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

// applyList is run with its writers buffered.
func applyList(
	root string,
	list string,
	skips []string,
	dryRun bool,
	output *bufio.Writer,
	errorOutput *bufio.Writer,
) int {
	requests, readError := readList(list)
	if readError != nil {
		fmt.Fprintf(errorOutput, "handnames: reading the list: %v\n", readError)
		return 1
	}
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(errorOutput, "handnames: loading: %v\n", loadError)
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

	edits, outcomes, unresolved := plan(loaded, requests)
	for _, occurrence := range unresolved {
		fmt.Fprintln(output, occurrence)
	}
	edits, disagreements := rename.Deduplicate(edits)
	editsByFile := rename.GroupByFile(edits)

	counts := map[string]int{}
	for _, request := range requests {
		counts[outcomeClass(outcomes[request.position])]++
	}
	fmt.Fprintf(output, "requests: %d\n", len(requests))
	for _, class := range sortedKeys(counts) {
		fmt.Fprintf(output, "  %s: %d\n", class, counts[class])
	}
	fmt.Fprintf(output, "edits: %d in %d files\n", len(edits), len(editsByFile))
	fmt.Fprintf(output, "variant disagreements: %d\n", len(disagreements))
	fmt.Fprintln(output, "\noutcomes:")
	for _, request := range requests {
		fmt.Fprintf(
			output,
			"  %s -> %s: %s\n",
			request.position,
			request.newName,
			strings.ReplaceAll(outcomes[request.position], loaded.Root+"/", ""),
		)
	}
	for _, disagreement := range disagreements {
		fmt.Fprintln(output, "  disagreement: "+disagreement)
	}
	if counts["accepted"] != len(requests) || len(disagreements) > 0 {
		fmt.Fprintln(errorOutput, "handnames: not every request was accepted, so nothing is written; fix the list")
		return 1
	}
	if dryRun {
		return 0
	}
	return workspace.ApplyVerified(loaded, root, skips, edits, editsByFile, "handnames", output, errorOutput)
}

// readList reads the list, skipping blank lines and lines opening with #.
func readList(path string) ([]request, error) {
	file, openError := os.Open(path)
	if openError != nil {
		return nil, openError
	}
	// Ignored: the file was only read, so a failed close loses nothing.
	defer file.Close()
	var requests []request
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 2 {
			return nil, fmt.Errorf("line %d: want `path:line:column newName`, got %q", lineNumber, text)
		}
		if seen[fields[0]] {
			return nil, fmt.Errorf("line %d: %s is listed twice", lineNumber, fields[0])
		}
		seen[fields[0]] = true
		requests = append(requests, request{position: fields[0], newName: fields[1]})
	}
	return requests, scanner.Err()
}

// plan finds each request's declaration, checks every package's requests together through the rename engine,
// and returns the edits of the accepted renames, each request's outcome, and a line for each occurrence the
// engine could not resolve.
func plan(loaded *workspace.Workspace, requests []request) ([]rename.Edit, map[string]string, []string) {
	outcomes := map[string]string{}
	wanted := map[string]request{}
	for _, request := range requests {
		wanted[request.position] = request
		outcomes[request.position] = "not found: no identifier is declared at this position in an editable file"
	}
	var edits []rename.Edit
	var unresolvedLines []string
	for _, analyzedPackage := range loaded.Analyzed {
		renamePackage := loaded.RenamePackage(analyzedPackage)
		var proposals []rename.Proposal
		positionOf := map[types.Object]string{}
		constants := map[types.Object]string{}
		for _, file := range renamePackage.Files {
			clausesOf := typeSwitchClauses(analyzedPackage.TypesInfo, file)
			ast.Inspect(file, func(node ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if !isIdentifier {
					return true
				}
				position := analyzedPackage.Fset.Position(identifier.Pos())
				relative, inside := loaded.Relative(position.Filename)
				if !inside {
					return true
				}
				key := relative + ":" + strconv.Itoa(position.Line) + ":" + strconv.Itoa(position.Column)
				request, isWanted := wanted[key]
				if !isWanted {
					return true
				}
				if clauses, isGuard := clausesOf[identifier]; isGuard && request.newName != identifier.Name {
					// The guard of a type switch declares one variable per clause, renamed together.
					for _, clause := range clauses {
						positionOf[clause] = key
						proposals = append(proposals, rename.Proposal{Object: clause, NewName: request.newName})
					}
					return true
				}
				object := analyzedPackage.TypesInfo.Defs[identifier]
				if object == nil {
					outcomes[key] = "refused: the identifier here declares nothing (a use, not a declaration)"
					return true
				}
				if identifier.Name == request.newName {
					outcomes[key] = "refused: it already has that name"
					return true
				}
				positionOf[object] = key
				if isFunctionConstant(object, analyzedPackage.Types) {
					constants[object] = request.newName
					return true
				}
				proposals = append(proposals, rename.Proposal{Object: object, NewName: request.newName})
				return true
			})
		}
		for object, newName := range constants {
			edits = append(edits, constantEdits(analyzedPackage.Fset, analyzedPackage.TypesInfo, object, newName)...)
			outcomes[positionOf[object]] = "accepted"
		}
		if len(proposals) == 0 {
			continue
		}
		checked := rename.Check(renamePackage, proposals)
		for _, rejection := range checked.Rejections {
			outcomes[positionOf[rejection.Object]] = "refused: " + rejection.Reason
		}
		for object := range checked.Accepted {
			outcomes[positionOf[object]] = "accepted"
		}
		for _, unresolved := range checked.Unresolved {
			unresolvedLines = append(
				unresolvedLines,
				fmt.Sprintf("unresolved occurrence in %s: %s", analyzedPackage.ID, unresolved),
			)
		}
		edits = append(edits, rename.Edits(renamePackage, checked.Accepted)...)
	}
	return edits, outcomes, unresolvedLines
}

// typeSwitchClauses maps the guard identifier of each type switch in file, `value` in `switch value :=
// subject.(type)`, to the variables go/types declares for it, one per clause.
func typeSwitchClauses(info *types.Info, file *ast.File) map[*ast.Ident][]types.Object {
	clausesOf := map[*ast.Ident][]types.Object{}
	ast.Inspect(file, func(node ast.Node) bool {
		statement, isTypeSwitch := node.(*ast.TypeSwitchStmt)
		if !isTypeSwitch {
			return true
		}
		assignment, isAssignment := statement.Assign.(*ast.AssignStmt)
		if !isAssignment || len(assignment.Lhs) != 1 {
			return true
		}
		guard, isIdentifier := assignment.Lhs[0].(*ast.Ident)
		if !isIdentifier {
			return true
		}
		for _, clause := range statement.Body.List {
			if object := info.Implicits[clause]; object != nil {
				clausesOf[guard] = append(clausesOf[guard], object)
			}
		}
		return true
	})
	return clausesOf
}

// isFunctionConstant reports whether object is a constant declared inside a function, which the rename engine
// does not take (it renames variables) but which, like a local variable, is read only inside its own function.
func isFunctionConstant(object types.Object, packageTypes *types.Package) bool {
	constant, isConstant := object.(*types.Const)
	if !isConstant || constant.Pkg() != packageTypes || constant.Parent() == nil {
		return false
	}
	parent := constant.Parent()
	return parent != packageTypes.Scope() && parent.Parent() != packageTypes.Scope()
}

// constantEdits renames every identifier that declares or reads constant. The engine's scope check does not
// cover constants, so a constant's rename rests on the verified apply: the reload compares every identifier's
// resolution, and a new name that captured or was captured by another declaration is a mismatch that writes
// nothing.
func constantEdits(fileSet *token.FileSet, info *types.Info, constant types.Object, newName string) []rename.Edit {
	var edits []rename.Edit
	add := func(identifier *ast.Ident, object types.Object) {
		if object != constant {
			return
		}
		position := fileSet.PositionFor(identifier.Pos(), false)
		edits = append(edits, rename.Edit{
			Filename: position.Filename,
			Offset:   position.Offset,
			OldText:  identifier.Name,
			NewText:  newName,
		})
	}
	for identifier, object := range info.Defs {
		add(identifier, object)
	}
	for identifier, object := range info.Uses {
		add(identifier, object)
	}
	return edits
}

// outcomeClass is an outcome without its detail, for the counts.
func outcomeClass(outcome string) string {
	if class, _, found := strings.Cut(outcome, ":"); found {
		if class == "refused" {
			reason := strings.TrimSpace(strings.SplitN(outcome, ":", 3)[1])
			return "refused, " + reason
		}
		return class
	}
	return outcome
}

// sortedKeys is a map's keys in order.
func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
