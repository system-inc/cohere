// Command testnames renames the testing handles of cohere's Go code to full words: a local `t *testing.T`
// or `t`/`tb testing.TB` becomes `test`, `b *testing.B` becomes `benchmark`, `f *testing.F` becomes `fuzz`.
// Every rename goes through the rename engine, which refuses any that would change what an identifier
// resolves to; after writing, the tree is reloaded and every identifier's resolution compared.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
	"github.com/system-inc/cohere/internal/streams"

	"golang.org/x/tools/go/packages"
)

// skipFlags collects repeated -skip values.
type skipFlags []string

func (skips *skipFlags) String() string { return strings.Join(*skips, ",") }

func (skips *skipFlags) Set(value string) error {
	*skips = append(*skips, value)
	return nil
}

// testingTypes are the testing package's handle types, as type-checked objects.
type testingTypes struct {
	pointerT types.Type
	pointerB types.Type
	pointerF types.Type
	tb       types.Type
}

// main is the one way out: it exits with the code finishedExitCode returns, once that has run its deferred show.
func main() {
	os.Exit(finishedExitCode())
}

// finishedExitCode returns the code testnames exits with: runCommandLine returns its exit code here, and
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

// runCommandLine reads the flags and runs testnames, returning the code it exits with.
func runCommandLine() int {
	dryRun := flag.Bool("dry-run", false, "print counts and every rejection, write nothing")
	root := flag.String("root", ".", "the worktree root, holding go.work")
	var skips skipFlags
	flag.Var(&skips, "skip", "a path prefix, relative to the root, never edited (repeatable)")
	flag.Parse()
	return run(*root, skips, *dryRun)
}

func run(root string, skips []string, dryRun bool) int {
	loaded, loadError := workspace.Load(root, skips)
	if loadError != nil {
		fmt.Fprintf(streams.Errors, "testnames: loading: %v\n", loadError)
		return 1
	}
	fmt.Fprintf(
		streams.Output,
		"loaded %d root packages, analyzing %d variants\n",
		len(loaded.Roots),
		len(loaded.Analyzed),
	)
	// The analysis takes a while on a whole worktree, so the load's count is seen before it.
	streams.Show(streams.Output)
	editableFiles, skippedFiles := 0, 0
	for _, analyzedPackage := range loaded.Analyzed {
		editable := len(loaded.RenamePackage(analyzedPackage).Files)
		editableFiles += editable
		skippedFiles += len(analyzedPackage.Syntax) - editable
	}
	fmt.Fprintf(streams.Output, "files in analyzed variants: %d editable, %d skipped\n", editableFiles, skippedFiles)
	if editableFiles == 0 {
		fmt.Fprintln(streams.Errors, "testnames: no editable files; the root or skip rules are wrong")
		return 1
	}

	var allEdits []rename.Edit
	var commentEdits []rename.Edit
	commentsSkippedInRejectedDeclarations := 0
	var allRejections []rename.Rejection
	var allUnresolved []string
	proposalCount := 0
	acceptedDeclarations := map[string]string{}
	unmatchedLookalikes := []string{}
	for _, analyzedPackage := range loaded.Analyzed {
		renamePackage := loaded.RenamePackage(analyzedPackage)
		handles, hasTesting := findTesting(analyzedPackage)
		var proposals []rename.Proposal
		for _, file := range renamePackage.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if !isIdentifier {
					return true
				}
				variable, isVariable := analyzedPackage.TypesInfo.Defs[identifier].(*types.Var)
				if !isVariable {
					return true
				}
				newName := ""
				if hasTesting {
					newName = handleName(variable, handles)
				}
				if newName == "" {
					if looksLikeHandle(variable) {
						unmatchedLookalikes = append(unmatchedLookalikes, fmt.Sprintf(
							"%s: %s %s",
							analyzedPackage.Fset.Position(variable.Pos()),
							variable.Name(),
							variable.Type(),
						))
					}
					return true
				}
				proposals = append(proposals, rename.Proposal{Object: variable, NewName: newName})
				return true
			})
			proposals = append(proposals, testCaseProposals(analyzedPackage, file)...)
		}
		proposalCount += len(proposals)
		plan := rename.Check(renamePackage, proposals)
		allRejections = append(allRejections, plan.Rejections...)
		for _, unresolved := range plan.Unresolved {
			allUnresolved = append(allUnresolved, analyzedPackage.ID+": "+unresolved)
		}
		for object, newName := range plan.Accepted {
			position := analyzedPackage.Fset.PositionFor(object.Pos(), false)
			acceptedDeclarations[fmt.Sprintf(
				"%s:%d",
				position.Filename,
				position.Offset,
			)] = object.Name() + " -> " + newName
		}
		allEdits = append(allEdits, rename.Edits(renamePackage, plan.Accepted)...)
		if hasTesting {
			edits, skipped := handleCommentEdits(analyzedPackage, renamePackage, plan, handles)
			commentEdits = append(commentEdits, edits...)
			commentsSkippedInRejectedDeclarations += skipped
		}
	}
	allEdits = append(allEdits, commentEdits...)

	edits, disagreements := rename.Deduplicate(allEdits)
	editsByFile := rename.GroupByFile(edits)

	fmt.Fprintf(streams.Output, "proposals: %d\n", proposalCount)
	fmt.Fprintf(streams.Output, "accepted declarations: %d\n", len(acceptedDeclarations))
	acceptedByKind := map[string]int{}
	for _, kind := range acceptedDeclarations {
		acceptedByKind[kind]++
	}
	for _, kind := range sortedKeys(acceptedByKind) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", kind, acceptedByKind[kind])
	}
	fmt.Fprintf(streams.Output, "rejected: %d\n", len(allRejections))
	rejectedByReason := map[string]int{}
	for _, rejection := range allRejections {
		rejectedByReason[reasonClass(rejection.Reason)]++
	}
	for _, reason := range sortedKeys(rejectedByReason) {
		fmt.Fprintf(streams.Output, "  %s: %d\n", reason, rejectedByReason[reason])
	}
	fmt.Fprintf(
		streams.Output,
		"edits: %d in %d files, of them %d comment edits (t. to test.)\n",
		len(edits),
		len(editsByFile),
		len(commentEdits),
	)
	fmt.Fprintf(
		streams.Output,
		"handle comments left alone in declarations with a rejected t: %d\n",
		commentsSkippedInRejectedDeclarations,
	)
	fmt.Fprintf(streams.Output, "variant disagreements: %d\n", len(disagreements))
	fmt.Fprintf(streams.Output, "unresolved occurrences (scope model gaps): %d\n", len(allUnresolved))
	fmt.Fprintf(
		streams.Output,
		"variables named like a handle whose type did not match: %d\n",
		len(unmatchedLookalikes),
	)

	fmt.Fprintln(streams.Output, "\nrejections:")
	for _, rejection := range allRejections {
		fmt.Fprintf(
			streams.Output,
			"  %s: %s -> %s: %s\n",
			relativePosition(loaded, rejection.Position),
			rejection.Object.Name(),
			rejection.NewName,
			strings.ReplaceAll(rejection.Reason, loaded.Root+"/", ""),
		)
	}
	fmt.Fprintln(streams.Output, "\ncomment edits:")
	for _, edit := range commentEdits {
		source, readError := os.ReadFile(edit.Filename)
		if readError != nil {
			fmt.Fprintf(streams.Output, "  %s: %v\n", edit.Filename, readError)
			continue
		}
		lineStart := strings.LastIndexByte(string(source[:edit.Offset]), '\n') + 1
		lineEnd := strings.IndexByte(string(source[edit.Offset:]), '\n')
		if lineEnd < 0 {
			lineEnd = len(source) - edit.Offset
		}
		relative, _ := loaded.Relative(edit.Filename)
		fmt.Fprintf(streams.Output, "  %s:%d: %s\n", relative, strings.Count(string(source[:edit.Offset]), "\n")+1,
			strings.TrimSpace(string(source[lineStart:edit.Offset+lineEnd])))
	}
	fmt.Fprintln(streams.Output, "\nvariant disagreements:")
	for _, disagreement := range disagreements {
		fmt.Fprintln(streams.Output, "  "+disagreement)
	}
	fmt.Fprintln(streams.Output, "\nunresolved occurrences:")
	for _, unresolved := range allUnresolved {
		fmt.Fprintln(streams.Output, "  "+strings.ReplaceAll(unresolved, loaded.Root+"/", ""))
	}
	fmt.Fprintln(streams.Output, "\nhandle-named variables with another type:")
	sort.Strings(unmatchedLookalikes)
	for _, lookalike := range unmatchedLookalikes {
		fmt.Fprintln(streams.Output, "  "+lookalike)
	}

	reportComments(loaded, editsByFile, commentEdits)

	if len(disagreements) > 0 {
		fmt.Fprintln(streams.Errors, "testnames: variants disagree on some edits; those spans were dropped")
	}
	if dryRun {
		return 0
	}
	return apply(loaded, root, skips, edits, editsByFile)
}

// apply hands the edits to workspace.ApplyVerified, which proves the rewrite in memory and writes only a
// proven result.
func apply(
	loaded *workspace.Workspace,
	root string,
	skips []string,
	edits []rename.Edit,
	editsByFile map[string][]rename.Edit,
) int {
	return workspace.ApplyVerified(loaded, root, skips, edits, editsByFile, "testnames", streams.Output, streams.Errors)
}

// findTesting returns the testing package's handle types as pkg sees them.
func findTesting(goPackage *packages.Package) (testingTypes, bool) {
	var testingPackage *types.Package
	if imported, found := goPackage.Imports["testing"]; found && imported.Types != nil {
		testingPackage = imported.Types
	} else {
		visited := map[*types.Package]bool{}
		var visit func(current *types.Package)
		visit = func(current *types.Package) {
			if current == nil || visited[current] || testingPackage != nil {
				return
			}
			visited[current] = true
			if current.Path() == "testing" {
				testingPackage = current
				return
			}
			for _, imported := range current.Imports() {
				visit(imported)
			}
		}
		visit(goPackage.Types)
	}
	if testingPackage == nil {
		return testingTypes{}, false
	}
	handleTypes := map[string]types.Type{}
	for _, name := range []string{"T", "B", "F", "TB"} {
		typeName, isTypeName := testingPackage.Scope().Lookup(name).(*types.TypeName)
		if !isTypeName {
			// A testing package without the handle types is not one whose handles this codemod knows, so none
			// of its handles is renamed.
			return testingTypes{}, false
		}
		handleTypes[name] = typeName.Type()
	}
	return testingTypes{
		pointerT: types.NewPointer(handleTypes["T"]),
		pointerB: types.NewPointer(handleTypes["B"]),
		pointerF: types.NewPointer(handleTypes["F"]),
		tb:       handleTypes["TB"],
	}, true
}

// handleName is the new name for variable, or "" when it is not a testing handle this codemod renames.
func handleName(variable *types.Var, handles testingTypes) string {
	switch {
	case variable.Name() == "t" && types.Identical(variable.Type(), handles.pointerT):
		return "test"
	case (variable.Name() == "t" || variable.Name() == "tb") && types.Identical(variable.Type(), handles.tb):
		return "test"
	case variable.Name() == "b" && types.Identical(variable.Type(), handles.pointerB):
		return "benchmark"
	case variable.Name() == "f" && types.Identical(variable.Type(), handles.pointerF):
		return "fuzz"
	}
	return ""
}

// looksLikeHandle flags a variable named like a handle whose type mentions testing, so a type the match
// missed shows up in the report instead of silently staying.
func looksLikeHandle(variable *types.Var) bool {
	switch variable.Name() {
	case "t", "tb", "b", "f":
		return strings.Contains(variable.Type().String(), "testing.")
	}
	return false
}

// reasonClass groups rejection reasons for counting.
func reasonClass(reason string) string {
	if strings.HasPrefix(reason, "conflict:") {
		return "conflict"
	}
	return reason
}

// relativePosition prints a position relative to the root.
func relativePosition(loaded *workspace.Workspace, position token.Position) string {
	if relative, inside := loaded.Relative(position.Filename); inside {
		return fmt.Sprintf("%s:%d", relative, position.Line)
	}
	return position.String()
}

// commentHandle finds prose that still names a handle method, like "t.Fatal" or "t.Parallel".
var commentHandle = regexp.MustCompile(`\bt\.[A-Za-z_]\w*`)

// reportComments counts comments in the edited files that still say `t.` once the comment edits are made,
// and lists the ten most common.
func reportComments(loaded *workspace.Workspace, editsByFile map[string][]rename.Edit, commentEdits []rename.Edit) {
	rewritten := map[rename.EditKey]bool{}
	for _, edit := range commentEdits {
		rewritten[rename.EditKey{Filename: edit.Filename, Offset: edit.Offset}] = true
	}
	counts := map[string]int{}
	filesWith := map[string]bool{}
	total := 0
	for filename := range editsByFile {
		fileSet := token.NewFileSet()
		file, parseError := parser.ParseFile(fileSet, filename, nil, parser.ParseComments)
		if parseError != nil {
			fmt.Fprintf(streams.Output, "comment scan: %v\n", parseError)
			continue
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				commentOffset := fileSet.PositionFor(comment.Pos(), false).Offset
				for _, location := range commentHandle.FindAllStringIndex(comment.Text, -1) {
					if rewritten[rename.EditKey{Filename: filename, Offset: commentOffset + location[0]}] {
						continue
					}
					match := comment.Text[location[0]:location[1]]
					counts[match]++
					total++
					relative, _ := loaded.Relative(filename)
					filesWith[relative] = true
				}
			}
		}
	}
	type entry struct {
		text  string
		count int
	}
	var entries []entry
	for text, count := range counts {
		entries = append(entries, entry{text: text, count: count})
	}
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].count != entries[right].count {
			return entries[left].count > entries[right].count
		}
		return entries[left].text < entries[right].text
	})
	fmt.Fprintf(
		streams.Output,
		"\ncomments still saying `t.` in edited files after comment edits: %d occurrences, %d distinct, in %d files\n",
		total,
		len(entries),
		len(filesWith),
	)
	for index, entry := range entries {
		if index == 10 {
			break
		}
		fmt.Fprintf(streams.Output, "  %5d  %s\n", entry.count, entry.text)
	}
	fmt.Fprintln(streams.Output, "files:")
	for _, filename := range sortedKeys(filesWith) {
		fmt.Fprintln(streams.Output, "  "+filename)
	}
}

// sortedKeys returns a map's keys in order.
func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// testCaseProposals proposes `testCase` for every range loop value named exactly `test` over a slice, array,
// pointer to array, or map: the table-test case, which otherwise blocks `t` from becoming `test`.
func testCaseProposals(goPackage *packages.Package, file *ast.File) []rename.Proposal {
	var proposals []rename.Proposal
	ast.Inspect(file, func(node ast.Node) bool {
		rangeStatement, isRange := node.(*ast.RangeStmt)
		if !isRange || rangeStatement.Tok != token.DEFINE {
			return true
		}
		value, isIdentifier := rangeStatement.Value.(*ast.Ident)
		if !isIdentifier || value.Name != "test" {
			return true
		}
		variable, isVariable := goPackage.TypesInfo.Defs[value].(*types.Var)
		if !isVariable {
			return true
		}
		rangedType := goPackage.TypesInfo.TypeOf(rangeStatement.X)
		if rangedType == nil {
			return true
		}
		switch underlying := rangedType.Underlying().(type) {
		case *types.Slice, *types.Array, *types.Map:
		case *types.Pointer:
			if _, isArray := underlying.Elem().Underlying().(*types.Array); !isArray {
				return true
			}
		default:
			return true
		}
		proposals = append(proposals, rename.Proposal{Object: variable, NewName: "testCase"})
		return true
	})
	return proposals
}

// handleCommentReference finds `t.` followed by a name, in a comment's text.
var handleCommentReference = regexp.MustCompile(`t\.([A-Za-z_][A-Za-z0-9_]*)`)

// handleCommentEdits rewrites `t.<Method>` to `test.<Method>` in the comments of each top-level declaration
// (its doc and everything inside it) in which a `t` handle was renamed to `test`, where Method is a method of
// *testing.T. A declaration that also holds a `t` handle the engine rejected is left alone, since its comments
// may name that one. It returns the edits and how many such references it left in rejected declarations.
func handleCommentEdits(
	goPackage *packages.Package,
	renamePackage rename.Package,
	plan rename.Plan,
	handles testingTypes,
) ([]rename.Edit, int) {
	methods := map[string]bool{}
	methodSet := types.NewMethodSet(handles.pointerT)
	for index := 0; index < methodSet.Len(); index++ {
		methods[methodSet.At(index).Obj().Name()] = true
	}
	isRenamedHandle := func(object types.Object, newName string) bool {
		return object.Name() == "t" && newName == "test"
	}
	var edits []rename.Edit
	skipped := 0
	for _, file := range renamePackage.Files {
		for _, declaration := range file.Decls {
			start, end := declaration.Pos(), declaration.End()
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if typed.Doc != nil {
					start = typed.Doc.Pos()
				}
			case *ast.GenDecl:
				if typed.Doc != nil {
					start = typed.Doc.Pos()
				}
			}
			inside := func(position token.Pos) bool { return position >= start && position <= end }
			renamed, rejected := false, false
			for object, newName := range plan.Accepted {
				if isRenamedHandle(object, newName) && inside(object.Pos()) {
					renamed = true
				}
			}
			for _, rejection := range plan.Rejections {
				if isRenamedHandle(rejection.Object, rejection.NewName) && inside(rejection.Object.Pos()) {
					rejected = true
				}
			}
			if !renamed {
				continue
			}
			for _, group := range file.Comments {
				if group.Pos() < start || group.End() > end {
					continue
				}
				for _, comment := range group.List {
					commentOffset := goPackage.Fset.PositionFor(comment.Pos(), false).Offset
					for _, location := range handleCommentReference.FindAllStringSubmatchIndex(comment.Text, -1) {
						if location[0] > 0 {
							previous := comment.Text[location[0]-1]
							if previous == '.' || previous == '_' || (previous >= '0' && previous <= '9') ||
								(previous >= 'a' && previous <= 'z') || (previous >= 'A' && previous <= 'Z') {
								continue
							}
						}
						if !methods[comment.Text[location[2]:location[3]]] {
							continue
						}
						if rejected {
							skipped++
							continue
						}
						edits = append(edits, rename.Edit{
							Filename: goPackage.Fset.PositionFor(comment.Pos(), false).Filename,
							Offset:   commentOffset + location[0],
							OldText:  "t",
							NewText:  "test",
						})
					}
				}
			}
		}
	}
	return edits, skipped
}
