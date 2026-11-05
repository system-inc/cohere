// Package testing_no_machine_path is the Go engine's rule that a test names no home directory unless it says why.
//
// A test that reads a path on one developer's machine skips everywhere else, so its coverage exists on that machine
// alone and nobody can tell (#sycrdr6). Code outside the repository is asked for by a name the environment resolves.
// A literal that only looks like a home path, such as the input to a function that trims one, says so where a reader
// will see it.
//
// It is the port of internal/testpolicy's HomePaths and finds what that finds, no more and no fewer: the same
// regular expressions, and the same reading of where a marker excuses.
package testing_no_machine_path

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/testing-no-machine-path"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "testing_no_machine_path",
	Doc: "Flags a string literal in a _test.go file that names a home directory, /Users/<name>/ or /home/<name>/, " +
		"since a test reading one developer's machine covers nothing anywhere else, unless a // Not a machine path: " +
		"<reason> comment ends the literal's line or sits directly above the statement or declaration holding it.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry. It reads no types: a literal's text and the comments around
// it decide everything.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var machinePathMessage = policy.MessageOf(Name, "machinePath")

var machinePathWithoutReasonMessage = policy.MessageOf(Name, "machinePathWithoutReason")

// notAMachinePathMarker opens the comment that excuses a literal, spelled as testpolicy's NotAMachinePathMarker.
const notAMachinePathMarker = "Not a machine path:"

var (
	// homeDirectory is testpolicy's expression as written: a /Users/ or /home/ that no word character runs into,
	// followed by a name and the slash after it, so /Users/ alone and /AllUsers/x/ are not home directories.
	homeDirectory = regexp.MustCompile(`(^|[^A-Za-z0-9_])/(Users|home)/[^/\s"']+/`)
	// reasonAfterPathMarker is testpolicy's reading of a reasoned marker: anywhere in the comment group's text, in
	// any case, with something other than space after the colon, which may sit on the group's next line.
	reasonAfterPathMarker = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(notAMachinePathMarker) + `\s*\S`)
	// pathMarker is the marker with or without a reason, so a group holding only the bare marker gets the message
	// that asks for the reason, while it still excuses nothing.
	pathMarker = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(notAMachinePathMarker))
)

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// The file's own name, not one a //line directive gives it, since testpolicy judges _test.go files by path.
		if !strings.HasSuffix(pass.Fset.PositionFor(file.Package, false).Filename, "_test.go") {
			continue
		}
		excused := linesCoveredBy(pass, file, func(text string) bool {
			return reasonAfterPathMarker.MatchString(text)
		})
		markedWithoutReason := linesCoveredBy(pass, file, func(text string) bool {
			return pathMarker.MatchString(text) && !reasonAfterPathMarker.MatchString(text)
		})
		ast.Inspect(file, func(node ast.Node) bool {
			literal, isLiteral := node.(*ast.BasicLit)
			if !isLiteral || literal.Kind != token.STRING {
				return true
			}
			// A literal the parser accepted always unquotes; the check is testpolicy's, kept so the two agree.
			value, unquoteError := strconv.Unquote(literal.Value)
			if unquoteError != nil || !homeDirectory.MatchString(value) {
				return true
			}
			line := pass.Fset.PositionFor(literal.Pos(), false).Line
			if excused[line] {
				return true
			}
			message := machinePathMessage
			if markedWithoutReason[line] {
				message = machinePathWithoutReasonMessage
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

// linesCoveredBy is every line of file that a comment group whose text matches speaks for, read as testpolicy reads
// it: the line the group starts on, for a comment at the end of a literal's line, and every line of each node that
// starts on the line below the group's end, for a comment directly above a statement or declaration.
func linesCoveredBy(pass *analysis.Pass, file *ast.File, matches func(text string) bool) map[int]bool {
	covered := map[int]bool{}
	nodesStartingOn := map[int]bool{}
	for _, group := range file.Comments {
		if !matches(group.Text()) {
			continue
		}
		covered[pass.Fset.PositionFor(group.Pos(), false).Line] = true
		nodesStartingOn[pass.Fset.PositionFor(group.End(), false).Line+1] = true
	}
	// Every node starting on that line, not only the outermost, as testpolicy walks it; an inner node lies within
	// the outer one's lines, so the lines covered are the outermost node's.
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		firstLine := pass.Fset.PositionFor(node.Pos(), false).Line
		if !nodesStartingOn[firstLine] {
			return true
		}
		for line := firstLine; line <= pass.Fset.PositionFor(node.End(), false).Line; line++ {
			covered[line] = true
		}
		return true
	})
	return covered
}
