// Package staticcheck_report is how a check ported from staticcheck reports a finding: staticcheck's analysis/report,
// ported onto rule.Context, so every port reports through this one copy and none imports staticcheck's framework.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 analysis/report/report.go, with the LanguageVersion,
// StdlibVersion and File of analysis/code/code.go that its version gates read. A finding lands where staticcheck's
// lands, a node's Pos and End or an instruction's Source(), each cut short by ShortRange the same way, and the options
// keep staticcheck's meaning and their order's. Where staticcheck reads a pass, the kit reads a rule.Context, the
// package's files from Files and staticcheck's generated-file judgment from IsGenerated, so a check on the engine's
// dispatcher reports through it as a check on an analyzer does (see PassContext).
//
// It differs in one place, adjudicated: staticcheck's MaximumLanguageVersion sets the minimum language version, so it
// gates the wrong way, and the kit's is the maximum its name says. No staticcheck check calls it, so no port's
// findings move. TestReportMatchesStaticcheck holds the rest finding for finding against staticcheck's, which stays
// in the tree as the oracle. Render and RenderArguments, which write code into a message, are staticcheck's Render and
// RenderArgs, held by TestRenderMatchesStaticcheck. HasRange, which TestReportMatchesStaticcheck asks at every node
// beside the reports, and Ordinal, held by TestOrdinalMatchesStaticcheck, are staticcheck's own.
package staticcheck_report

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/version"
	"path/filepath"
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// Positioner is what a finding can be reported at: an ast.Node, an instruction or value of staticcheck's intermediate
// representation, or a types.Object.
type Positioner interface {
	Pos() token.Pos
}

// fullPositioner is a Positioner that knows where it ends, as an ast.Node does.
type fullPositioner interface {
	Pos() token.Pos
	End() token.Pos
}

// sourcer is a Positioner built from syntax, as an instruction of the engine's intermediate representation is, and of
// staticcheck's, whose finding spans that syntax.
type sourcer interface {
	Source() ast.Node
}

// Report reports message at node through context, shaped by options, as staticcheck's report.Report does: nothing when
// a version gate or FilterGenerated holds it back, and otherwise a finding at node's range. It panics, as
// staticcheck's does, for a node outside the package's files, whose versions it cannot read, and for an instruction
// with no source, which has no range; a check reports neither.
func Report(context *rule.Context, node Positioner, message string, options ...Option) {
	shape := &reportOptions{}
	for _, option := range options {
		option(shape)
	}

	languageVersion := LanguageVersion(context, node)
	standardLibraryVersion := StandardLibraryVersion(context, node)
	// version.Compare orders an empty or malformed version before every valid one, so a file of no known version is
	// older than any minimum and newer than no maximum, as staticcheck reads it.
	if shape.maximumLanguageVersion != "" && version.Compare(shape.maximumLanguageVersion, languageVersion) == -1 {
		return
	}
	if shape.maximumStandardLibraryVersion != "" &&
		version.Compare(shape.maximumStandardLibraryVersion, standardLibraryVersion) == -1 {
		return
	}
	if shape.minimumLanguageVersion != "" && version.Compare(shape.minimumLanguageVersion, languageVersion) == 1 {
		return
	}
	if shape.minimumStandardLibraryVersion != "" &&
		version.Compare(shape.minimumStandardLibraryVersion, standardLibraryVersion) == 1 {
		return
	}

	if shape.filterGenerated {
		if context.IsGenerated == nil {
			// Invariant: every context a check reports through is built by PassContext or the engine's dispatcher,
			// both of which give it staticcheck's judgment; a context built without one is a bug in its builder.
			panic("staticcheck_report: FilterGenerated asked of a context with no IsGenerated judgment")
		}
		// The judgment is keyed by the name a file was parsed from, and staticcheck's report asks it by the name a
		// position is displayed in, as this does, so a generated file whose line directives point into another Go
		// file is asked by that file's name, found not generated, and its findings are not withheld.
		if context.IsGenerated(DisplayPosition(context.FileSet, node.Pos()).Filename) {
			return
		}
	}

	position, end, hasRange := rangeOf(node, shape.shortRange)
	if !hasRange {
		// Invariant: a check reports at an instruction only when it has a source, as staticcheck's checks do, so
		// this is a port's bug, which the dispatcher records under the rule's name as staticcheck's crash would be.
		panic(fmt.Sprintf("no valid position for reporting node %v", node))
	}
	context.Report(analysis.Diagnostic{
		Pos:            position,
		End:            end,
		Message:        message,
		SuggestedFixes: shape.fixes,
		Related:        shape.related,
	})
}

// HasRange is whether a finding or related information can be reported at node: false for an instruction or value of
// the intermediate representation that has no source, and true for anything else, as staticcheck's
// report.HasRange answers. A check asks it before Report or Related at a value that may have been built with no
// syntax, Report panicking there and Related adding nothing. Like staticcheck's, it asks for both the full and the
// short range, since it cannot know which the report will want, though the two agree on whether there is one.
func HasRange(node Positioner) bool {
	if _, _, hasRange := rangeOf(node, false); !hasRange {
		return false
	}
	_, _, hasRange := rangeOf(node, true)
	return hasRange
}

// rangeOf is where a finding at node spans, short or full, and false for an instruction with no source. A node that
// knows only where it starts, such as a types.Object, spans no end.
func rangeOf(node Positioner, short bool) (position token.Pos, end token.Pos, hasRange bool) {
	switch node := node.(type) {
	case sourcer:
		source := node.Source()
		if source == nil {
			return token.NoPos, token.NoPos, false
		}
		if short {
			position, end = shortRange(source)
			return position, end, true
		}
		return source.Pos(), source.End(), true
	case fullPositioner:
		if short {
			position, end = shortRange(node)
			return position, end, true
		}
		return node.Pos(), node.End(), true
	default:
		return node.Pos(), token.NoPos, true
	}
}

// shortRange is the main part of node: for a node with a body, its header without the body, and for any other node,
// all of it.
func shortRange(node ast.Node) (position token.Pos, end token.Pos) {
	switch node := node.(type) {
	case *ast.File:
		return node.Pos(), node.Name.End()
	case *ast.CaseClause:
		return node.Pos(), node.Colon + 1
	case *ast.CommClause:
		return node.Pos(), node.Colon + 1
	case *ast.DeferStmt:
		return node.Pos(), node.Defer + token.Pos(len("defer"))
	case *ast.ExprStmt:
		return shortRange(node.X)
	case *ast.ForStmt:
		switch {
		case node.Post != nil:
			return node.For, node.Post.End()
		case node.Cond != nil:
			return node.For, node.Cond.End()
		case node.Init != nil:
			// One past the initializer takes in the semicolon after it, where gofmt puts it.
			return node.Pos(), node.Init.End() + 1
		default:
			return node.Pos(), node.For + token.Pos(len("for"))
		}
	case *ast.FuncDecl:
		return node.Pos(), node.Type.End()
	case *ast.FuncLit:
		return node.Pos(), node.Type.End()
	case *ast.GoStmt:
		if _, isLiteral := ast.Unparen(node.Call.Fun).(*ast.FuncLit); isLiteral {
			return node.Pos(), node.Go + token.Pos(len("go"))
		}
		return node.Pos(), node.End()
	case *ast.IfStmt:
		return node.Pos(), node.Cond.End()
	case *ast.RangeStmt:
		return node.Pos(), node.X.End()
	case *ast.SelectStmt:
		return node.Pos(), node.Pos() + token.Pos(len("select"))
	case *ast.SwitchStmt:
		switch {
		case node.Tag != nil:
			return node.Pos(), node.Tag.End()
		case node.Init != nil:
			// One past the initializer takes in the semicolon after it, where gofmt puts it.
			return node.Pos(), node.Init.End() + 1
		default:
			return node.Pos(), node.Pos() + token.Pos(len("switch"))
		}
	case *ast.TypeSwitchStmt:
		return node.Pos(), node.Assign.End()
	default:
		return node.Pos(), node.End()
	}
}

// DisplayPosition is where position is shown: the position a line directive moves it to when the directive names a Go
// file, as cgo's do, and otherwise where it is in the file it was parsed from, so a finding in a file goyacc generated
// points into that file and never into the grammar.
func DisplayPosition(fileSet *token.FileSet, position token.Pos) token.Position {
	if position == token.NoPos {
		return token.Position{}
	}
	adjusted := fileSet.PositionFor(position, true)
	if filepath.Ext(adjusted.Filename) == ".go" {
		return adjusted
	}
	return fileSet.PositionFor(position, false)
}

// Ordinal is number written as an English ordinal, 1st, 2nd, 3rd, 4th, for a message that counts, such as which
// return value of a call it means: staticcheck's report.Ordinal. Like staticcheck's, it gives 11 to 20 their "th" by
// their range alone, so 111 reads "111st" and 112 "112nd", and a negative number takes the suffix of its last digit
// only where Go's remainder is positive, so -1 reads "-1th"; no staticcheck check counts that far or below one.
func Ordinal(number int) string {
	suffix := "th"
	if number < 10 || number > 20 {
		switch number % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(number) + suffix
}
