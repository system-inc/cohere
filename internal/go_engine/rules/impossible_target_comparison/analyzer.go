// Package impossible_target_comparison is staticcheck's SA4032, a comparison of runtime.GOOS or runtime.GOARCH with
// a value the file's build constraints rule out, ported as a check on syntax that reports through the kit every port
// of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4032/sa4032.go, whose logic it keeps and
// whose messages it reports word for word, through staticcheck_report.Report with no option, as the original reports
// through report.Report: a generated file is reported too. It reads each file's constraint with
// staticcheck_code.BuildConstraints, the port of the code.BuildConstraints the original calls, and the known
// operating systems and architectures from staticcheck's knowledge package, as data. The original matches the
// patterns `(BinaryExpr (Symbol "runtime.GOOS") op@(Or "==" "!=") lit@(BasicLit "STRING" _))` and its GOARCH twin
// against every node of every constrained file; the port matches what they match without importing pattern: a
// comparison, equal or not equal, through any parentheses around it, whose left side is, through parentheses, the
// package-level runtime.GOOS or runtime.GOARCH however it is named, and whose right side is, through parentheses, a
// string literal. Every node is matched, so a parenthesized comparison is met twice, as the parentheses and as the
// comparison inside them, and both are reported, each over its own range; parity's parenthesized shows it, and the
// port keeps it. The original first asks code.CouldMatchAny whether the package's type index holds runtime.GOOS or
// runtime.GOARCH at all, and walks nothing when it does not; a package that names either imports runtime, which the
// index then holds, so the port walks every package and finds the same. The original keeps nothing from one
// comparison to the next, and stays the oracle: the parity tests beside this file run both over the same packages,
// loaded for several targets, and the two differ nowhere they hold.
package impossible_target_comparison

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/constant"
	"go/token"
	"go/types"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every file, as the walk enters it, which
// Check walks itself, as the original does.
var Listener = rule.Listener{FileEnter: Check}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4032",
	"SA4032",
	"Flags a comparison of runtime.GOOS or runtime.GOARCH with a value the file's build constraints rule out: "+
		"staticcheck's SA4032, ported as a check on syntax.",
	&Listener,
)

// maximumOtherTags is how many tags other than operating systems or architectures a constraint may name and still be
// judged, since judging it tries every combination of them, two to the power of their count.
const maximumOtherTags = 10

// Check judges one file: when it has a build constraint, each comparison in it of runtime.GOOS or runtime.GOARCH with
// a known value the constraint rules out.
func Check(context *rule.Context, file *ast.File) {
	expression, isConstrained := staticcheck_code.BuildConstraints(context, file)
	if !isConstrained {
		return
	}
	ast.Inspect(file, func(node ast.Node) bool {
		target, value, isComparison := comparedTarget(context, node)
		if !isComparison {
			return true
		}
		switch target {
		case "GOOS":
			// A value the knowledge does not hold may be a target of a newer Go or of a fork, and is not reasoned about.
			if _, isKnown := knowledge.KnownGOOS[value]; !isKnown {
				return true
			}
			satisfiable, isJudged := tagComparisonSatisfiable(expression, func(tag string) (bool, bool) {
				return operatingSystemTagMatches(tag, value)
			})
			if isJudged && !satisfiable {
				staticcheck_report.Report(context, node,
					fmt.Sprintf("due to the file's build constraints, runtime.GOOS will never equal %q", value))
			}
		case "GOARCH":
			if _, isKnown := knowledge.KnownGOARCH[value]; !isKnown {
				return true
			}
			satisfiable, isJudged := tagComparisonSatisfiable(expression, func(tag string) (bool, bool) {
				return architectureTagMatches(tag, value)
			})
			if isJudged && !satisfiable {
				staticcheck_report.Report(context, node,
					fmt.Sprintf("due to the file's build constraints, runtime.GOARCH will never equal %q", value))
			}
		}
		return true
	})
}

// comparedTarget is, for a node that is through any parentheses a comparison of runtime.GOOS or runtime.GOARCH, equal
// or not equal, with a string literal, which of the two it compares, "GOOS" or "GOARCH", and the literal's value.
func comparedTarget(context *rule.Context, node ast.Node) (string, string, bool) {
	expression, isExpression := node.(ast.Expr)
	if !isExpression {
		return "", "", false
	}
	comparison, isBinary := ast.Unparen(expression).(*ast.BinaryExpr)
	if !isBinary || (comparison.Op != token.EQL && comparison.Op != token.NEQ) {
		return "", "", false
	}
	literal, isLiteral := ast.Unparen(comparison.Y).(*ast.BasicLit)
	if !isLiteral || literal.Kind != token.STRING {
		return "", "", false
	}
	target, isTarget := runtimeTarget(context, comparison.X)
	if !isTarget {
		return "", "", false
	}
	return target, constant.StringVal(context.TypesInfo.Types[literal].Value), true
}

// runtimeTarget is the name of the runtime package's GOOS or GOARCH that expression names, as the pattern's Symbol
// reads it: through any parentheses and an index, an identifier or a selector whose recorded object is a constant or
// variable declared at the runtime package's top level.
func runtimeTarget(context *rule.Context, expression ast.Expr) (string, bool) {
	named := ast.Unparen(expression)
	switch indexed := named.(type) {
	case *ast.IndexExpr:
		named = ast.Unparen(indexed.X)
	case *ast.IndexListExpr:
		named = ast.Unparen(indexed.X)
	}
	var identifier *ast.Ident
	switch named := named.(type) {
	case *ast.Ident:
		identifier = named
	case *ast.SelectorExpr:
		identifier = named.Sel
	default:
		return "", false
	}
	object := context.TypesInfo.ObjectOf(identifier)
	switch object.(type) {
	case *types.Const, *types.Var:
	default:
		return "", false
	}
	if object.Pkg() == nil || object.Parent() != object.Pkg().Scope() || object.Pkg().Path() != "runtime" {
		return "", false
	}
	if object.Name() != "GOOS" && object.Name() != "GOARCH" {
		return "", false
	}
	return object.Name(), true
}

// operatingSystemTagMatches is whether tag holds when the operating system is operatingSystem, and whether tag names
// an operating system at all, the families android of linux, ios of darwin and illumos of solaris, and unix of them
// all, included.
func operatingSystemTagMatches(tag string, operatingSystem string) (bool, bool) {
	switch tag {
	case "aix", "android", "dragonfly", "freebsd", "hurd", "illumos", "ios", "js", "netbsd", "openbsd", "plan9",
		"wasip1", "windows":
		return operatingSystem == tag, true
	case "darwin":
		return operatingSystem == "darwin" || operatingSystem == "ios", true
	case "linux":
		return operatingSystem == "linux" || operatingSystem == "android", true
	case "solaris":
		return operatingSystem == "solaris" || operatingSystem == "illumos", true
	case "unix":
		switch operatingSystem {
		case "aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos", "ios", "linux", "netbsd",
			"openbsd", "solaris":
			return true, true
		}
		return false, true
	}
	return false, false
}

// architectureTagMatches is whether tag holds when the architecture is architecture, and whether tag names an
// architecture at all.
func architectureTagMatches(tag string, architecture string) (bool, bool) {
	switch tag {
	case "386", "amd64", "arm", "arm64", "loong64", "mips", "mipsle", "mips64", "mips64le", "ppc64", "ppc64le",
		"riscv64", "s390x", "sparc64", "wasm":
		return architecture == tag, true
	}
	return false, false
}

// tagComparisonSatisfiable is whether expression can hold with its target tags answered by matchTargetTag, and
// whether that was judged at all. The other tags it names are tried in every combination, unless there are more than
// maximumOtherTags of them, when it is not judged.
func tagComparisonSatisfiable(expression constraint.Expr, matchTargetTag func(tag string) (bool, bool)) (bool, bool) {
	otherTags := map[string]int{}
	// The first evaluation numbers every other tag, once each, since evaluating a constraint reads every tag in it.
	satisfied := expression.Eval(func(tag string) bool {
		matches, isTargetTag := matchTargetTag(tag)
		if !isTargetTag {
			if _, isNumbered := otherTags[tag]; !isNumbered {
				otherTags[tag] = len(otherTags)
			}
		}
		return matches
	})
	// It holds whatever the other tags are, or there are none to try.
	if satisfied || len(otherTags) == 0 {
		return satisfied, true
	}
	if len(otherTags) > maximumOtherTags {
		return false, false
	}
	for combination := 0; combination < 1<<len(otherTags); combination++ {
		holds := expression.Eval(func(tag string) bool {
			matches, isTargetTag := matchTargetTag(tag)
			if isTargetTag {
				return matches
			}
			return combination&(1<<otherTags[tag]) != 0
		})
		if holds {
			return true, true
		}
	}
	return false, true
}
