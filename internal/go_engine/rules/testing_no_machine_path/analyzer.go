// Package testing_no_machine_path is the Go engine's rule that a test names no home directory unless it says why.
//
// A test that reads a path on one developer's machine skips everywhere else, so its coverage exists on that machine
// alone and nobody can tell (#sycrdr6). Code outside the repository is asked for by a name the environment resolves.
// A literal that only looks like a home path, such as the input to a function that trims one, says so where a reader
// will see it.
//
// Its marker is read as every house rule reads one: the marker opens a comment line, spelled as written, and a comment
// trailing the code on the line above a statement speaks for that line alone.
package testing_no_machine_path

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/internal/go_engine/rules/positions"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-no-machine-path"

// listener reads the file's comments as the syntax dispatcher's walk enters each _test.go file, and hears every
// literal in it.
var listener = rule.Listener{
	FileLocal: true,
	Requires:  []*analysis.Analyzer{marker.Analyzer, positions.Analyzer},
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.BasicLit)(nil)},
	Node:      judgeLiteral,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "testing_no_machine_path", "Flags a string literal in a _test.go file "+
	"that names a home directory, /Users/<name>/ or /home/<name>/, since a test reading one developer's machine "+
	"covers nothing anywhere else, unless a comment opening with // Not a machine path: <reason> ends the literal's "+
	"line or sits on lines of its own directly above the statement or declaration holding it.", &listener)

// Rule is this rule's entry in the Go engine's registry. It reads no types: a literal's text and the comments around
// it decide everything.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer, Listener: &listener}

var machinePathMessage = policy.MessageOf(Name, "machinePath")

var machinePathWithoutReasonMessage = policy.MessageOf(Name, "machinePathWithoutReason")

// homeDirectory is a /Users/ or /home/ that no word character runs into, followed by a name and the slash after it,
// so /Users/ alone and /AllUsers/x/ are not home directories.
var homeDirectory = regexp.MustCompile(`(^|[^A-Za-z0-9_])/(Users|home)/[^/\s"']+/`)

// fileState is what the rule reads once for each file, held in its file state (rule.FileState).
type fileState struct {
	// table is the package's line table, read as the walk enters its first file.
	table *positions.Table
	// indexes is the package's comment indexes, read with table.
	indexes *marker.Indexes
	// isTest is whether the file being walked is a _test.go file, the only kind whose literals are judged.
	isTest bool
	// comments is the comment index of the file being walked.
	comments *marker.Index
	// coveredAbove is linesCoveredAbove of the file being walked, read at its first home directory, since it walks
	// every node of the file and nearly every file names none.
	coveredAbove map[int]marker.Excuse
}

// enterFile reads the package's comment indexes and line table the first time, and whether file holds tests.
func enterFile(context *rule.Context, file *ast.File) {
	state := rule.FileState[fileState](context)
	if state.table == nil {
		indexes, isIndexes := context.ResultOf(marker.Analyzer).(*marker.Indexes)
		if !isIndexes {
			// Invariant: marker's result is always its Indexes, so another is a mistake in the engine, and the
			// dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
			panic(fmt.Sprintf("reading the comment indexes: marker gave %T", context.ResultOf(marker.Analyzer)))
		}
		table, isTable := context.ResultOf(positions.Analyzer).(*positions.Table)
		if !isTable {
			// Invariant: positions' result is always its Table, for the reason above.
			panic(fmt.Sprintf("reading the line table: positions gave %T", context.ResultOf(positions.Analyzer)))
		}
		state.indexes = indexes
		state.table = table
	}
	// The file's own name, not one a //line directive gives it, since only a _test.go file holds tests.
	state.isTest = strings.HasSuffix(context.FileSet.PositionFor(file.Package, false).Filename, "_test.go")
	state.comments = nil
	state.coveredAbove = nil
	if state.isTest {
		state.comments = state.indexes.ForFile(file)
	}
}

// judgeLiteral reports node when it is a string literal in a _test.go file naming a home directory that no marker
// excuses with a reason.
func judgeLiteral(context *rule.Context, node ast.Node, stack []ast.Node) {
	state := rule.FileState[fileState](context)
	literal, isLiteral := node.(*ast.BasicLit)
	if !state.isTest || !isLiteral || literal.Kind != token.STRING || len(stack) == 0 {
		return
	}
	if !mayNameHomeDirectory(literal.Value) {
		return
	}
	// A literal the parser accepted always unquotes; one that does not is left alone rather than guessed at.
	value, unquoteError := strconv.Unquote(literal.Value)
	if unquoteError != nil || !homeDirectory.MatchString(value) {
		return
	}
	if state.coveredAbove == nil {
		file, isFile := stack[0].(*ast.File)
		if !isFile {
			return
		}
		state.coveredAbove = linesCoveredAbove(state.table, file, state.comments)
	}
	// The marker above the statement or declaration holding the literal, or one at the end of the literal's own last
	// line.
	given := state.coveredAbove[state.table.Line(literal.Pos())]
	for _, group := range state.comments.Trailing(literal) {
		given = max(given, marker.Read(group, marker.NotAMachinePath))
	}
	message := machinePathMessage
	switch given {
	case marker.WithReason:
		return
	case marker.WithoutReason:
		message = machinePathWithoutReasonMessage
	case marker.None:
	}
	context.Report(analysis.Diagnostic{
		Pos:      literal.Pos(),
		End:      literal.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{}),
	})
}

// mayNameHomeDirectory is whether a string literal, as its source spells it, could name a home directory once
// unquoted, read without unquoting it, since nearly no literal does and unquoting allocates. A match of homeDirectory
// holds /Users/ or /home/ as written. Unquoting changes the text only through an escape, which starts with a
// backslash, or, in a raw string, by dropping a carriage return, so a literal holding neither is its own value and
// holds those words only if its source does. One holding either is unquoted and matched as before.
func mayNameHomeDirectory(source string) bool {
	if strings.ContainsAny(source, "\\\r") {
		return true
	}
	return strings.Contains(source, "/Users/") || strings.Contains(source, "/home/")
}

// linesCoveredAbove is the excuse each line of file takes from a marker directly above a node: every line of each node
// with a marker comment directly above it, so a marker above a statement or declaration speaks for every literal in
// it, over as many lines as it runs. Each node's lines are read from table, without the FileSet's lock.
func linesCoveredAbove(table *positions.Table, file *ast.File, comments *marker.Index) map[int]marker.Excuse {
	covered := map[int]marker.Excuse{}
	// Every node, not only the outermost, since a marker may sit above a statement inside a function; an inner node
	// lies within the outer one's lines, so the lines covered are the outermost marked node's.
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		given := marker.Read(comments.Above(node), marker.NotAMachinePath)
		if given == marker.None {
			return true
		}
		lastLine := table.Line(node.End())
		for line := table.Line(node.Pos()); line <= lastLine; line++ {
			covered[line] = max(covered[line], given)
		}
		return true
	})
	return covered
}
