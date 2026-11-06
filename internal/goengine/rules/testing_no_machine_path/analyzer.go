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
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-no-machine-path"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "testing_no_machine_path",
	Doc: "Flags a string literal in a _test.go file that names a home directory, /Users/<name>/ or /home/<name>/, " +
		"since a test reading one developer's machine covers nothing anywhere else, unless a comment opening with " +
		"// Not a machine path: <reason> ends the literal's line or sits on lines of its own directly above the " +
		"statement or declaration holding it.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads no types: a literal's text and the comments around
// it decide everything.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var machinePathMessage = policy.MessageOf(Name, "machinePath")

var machinePathWithoutReasonMessage = policy.MessageOf(Name, "machinePathWithoutReason")

// notAMachinePathMarker opens the comment that excuses a literal. It is matched as written.
const notAMachinePathMarker = "Not a machine path:"

// homeDirectory is a /Users/ or /home/ that no word character runs into, followed by a name and the slash after it,
// so /Users/ alone and /AllUsers/x/ are not home directories.
var homeDirectory = regexp.MustCompile(`(^|[^A-Za-z0-9_])/(Users|home)/[^/\s"']+/`)

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// The file's own name, not one a //line directive gives it, since only a _test.go file holds tests.
		if !strings.HasSuffix(pass.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
			continue
		}
		comments := marker.NewIndex(pass.Fset, file)
		coveredAbove := linesCoveredAbove(pass, file, comments)
		ast.Inspect(file, func(node ast.Node) bool {
			literal, isLiteral := node.(*ast.BasicLit)
			if !isLiteral || literal.Kind != token.STRING {
				return true
			}
			// A literal the parser accepted always unquotes; one that does not is left alone rather than guessed at.
			value, unquoteError := strconv.Unquote(literal.Value)
			if unquoteError != nil || !homeDirectory.MatchString(value) {
				return true
			}
			// The marker above the statement or declaration holding the literal, or one at the end of the literal's
			// own last line.
			given := coveredAbove[pass.Fset.PositionFor(literal.Pos(), false).Line]
			for _, group := range comments.Trailing(literal) {
				given = max(given, marker.Read(group, notAMachinePathMarker))
			}
			message := machinePathMessage
			switch given {
			case marker.WithReason:
				return true
			case marker.WithoutReason:
				message = machinePathWithoutReasonMessage
			case marker.None:
			}
			pass.Report(analysis.Diagnostic{
				Pos:      literal.Pos(),
				End:      literal.End(),
				Category: message.Id,
				Message:  message.Render(map[string]string{}),
			})
			return true
		})
	}
	return nil, nil
}

// linesCoveredAbove is the excuse each line of file takes from a marker directly above a node: every line of each node
// with a marker comment directly above it, so a marker above a statement or declaration speaks for every literal in
// it, over as many lines as it runs.
func linesCoveredAbove(pass *analysis.Pass, file *ast.File, comments *marker.Index) map[int]marker.Excuse {
	covered := map[int]marker.Excuse{}
	// Every node, not only the outermost, since a marker may sit above a statement inside a function; an inner node
	// lies within the outer one's lines, so the lines covered are the outermost marked node's.
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		given := marker.Read(comments.Above(node), notAMachinePathMarker)
		if given == marker.None {
			return true
		}
		lastLine := pass.Fset.PositionFor(node.End(), false).Line
		for line := pass.Fset.PositionFor(node.Pos(), false).Line; line <= lastLine; line++ {
			covered[line] = max(covered[line], given)
		}
		return true
	})
	return covered
}
