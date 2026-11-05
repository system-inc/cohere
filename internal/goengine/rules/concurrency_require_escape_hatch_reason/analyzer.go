// Package concurrency_require_escape_hatch_reason is the Go engine's rule that every escape hatch from Go's checks
// says why it is safe, the twin of cohere-swift/concurrency-require-escape-hatch-reason.
//
// Go checks that memory is reached through types and that a package's unexported names stay its own. Converting to or
// from unsafe.Pointer, the calls in package unsafe that build a slice or string from a raw pointer or move one, and a
// //go:linkname directive each tell the compiler to stop checking and trust the code instead. That trust is a
// suppression, and every suppression states its reason, so the next reader can check the reasoning the compiler no
// longer does: what keeps the memory alive and its layout fixed, or what the linked symbol promises.
//
// The reason opens a comment line with the marker `Unsafe:`. A //nolint or //lint:ignore line is not judged, because
// cohere honors neither, so neither suppresses anything here.
package concurrency_require_escape_hatch_reason

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/concurrency-require-escape-hatch-reason"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "concurrency_require_escape_hatch_reason",
	Doc: "Flags a conversion to or from unsafe.Pointer, a call to unsafe.Add, unsafe.Slice, unsafe.SliceData, " +
		"unsafe.String or unsafe.StringData, and a //go:linkname directive, unless a // Unsafe: <reason> comment " +
		"says why it is safe: for an expression, directly above it or above a statement or declaration holding it, " +
		"or at the end of its line or its statement's; for a directive, directly above it. unsafe.Sizeof, " +
		"unsafe.Alignof and unsafe.Offsetof are not judged, since they only measure a type.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var escapeHatchWithoutReasonMessage = policy.MessageOf(Name, "escapeHatchWithoutReason")

var unsafeMarkerWithoutReasonMessage = policy.MessageOf(Name, "unsafeMarkerWithoutReason")

// unsafeMarker opens the comment that says why an escape hatch is safe, matched as written.
const unsafeMarker = "Unsafe:"

// linknameDirective opens a //go:linkname line. The compiler reads a directive only where the comment's text starts
// with it, with no space after the slashes, so `// go:linkname` and a block comment are prose.
const linknameDirective = "//go:linkname"

// pointerEscapingFunctions are the functions of package unsafe that make or move a pointer the compiler cannot
// check. Sizeof, Alignof and Offsetof are left out: they are constants about a type and reach no memory.
var pointerEscapingFunctions = []string{"Add", "Slice", "SliceData", "String", "StringData"}

func run(pass *analysis.Pass) (any, error) {
	escaping := map[types.Object]bool{}
	for _, name := range pointerEscapingFunctions {
		escaping[types.Unsafe.Scope().Lookup(name)] = true
	}
	for _, file := range pass.Files {
		judgeExpressions(pass, file, escaping)
		judgeLinknames(pass, file)
	}
	return nil, nil
}

// judgeExpressions reports every unsafe conversion and call in file whose comments give no reason. A hatch is judged
// once with everything inside it, so `uintptr(unsafe.Pointer(&value))` is one finding and one reason covers it.
func judgeExpressions(pass *analysis.Pass, file *ast.File, escaping map[types.Object]bool) {
	comments := marker.NewIndex(pass.Fset, file)
	// enclosing is the path from the file down to the node being visited, so a hatch finds the statements and
	// declarations that hold it.
	enclosing := []ast.Node{}
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			enclosing = enclosing[:len(enclosing)-1]
			return true
		}
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			enclosing = append(enclosing, node)
			return true
		}
		spelling := hatchSpelling(pass, call, escaping)
		if spelling == "" {
			enclosing = append(enclosing, node)
			return true
		}
		report(pass, call, spelling, excuseForExpression(comments, call, enclosing))
		// Returning false skips the children and the nil visit that would pop this node, so it is never pushed.
		return false
	})
}

// hatchSpelling is how the message names the escape hatch call is, or "" when it is none. A conversion is found by
// its types, so a named type whose underlying type is unsafe.Pointer is one too, and a call by the object it
// resolves to, so a function or field named Pointer or String elsewhere is never mistaken for package unsafe's.
func hatchSpelling(pass *analysis.Pass, call *ast.CallExpr, escaping map[types.Object]bool) string {
	if function, isTypeAndValue := pass.TypesInfo.Types[call.Fun]; isTypeAndValue && function.IsType() {
		if len(call.Args) != 1 {
			return ""
		}
		target := pass.TypesInfo.TypeOf(call)
		source := pass.TypesInfo.TypeOf(call.Args[0])
		if target == nil || source == nil {
			return ""
		}
		// unsafe.Pointer(nil) holds no address, and a conversion between two unsafe pointer types, such as a named
		// one and unsafe.Pointer itself, crosses nothing: the hatch was where the pointer was made.
		if isUntypedNil(source) || isUnsafePointer(target) == isUnsafePointer(source) {
			return ""
		}
		if isUnsafePointer(target) {
			return "Converting to `unsafe.Pointer`"
		}
		return "Converting from `unsafe.Pointer`"
	}
	callee := typeutil.Callee(pass.TypesInfo, call)
	if callee == nil || !escaping[callee] {
		return ""
	}
	return "Calling `unsafe." + callee.Name() + "`"
}

// isUnsafePointer is whether candidate's underlying type is unsafe.Pointer. A type parameter's underlying type is its
// constraint, an interface, so a generic T is never one.
func isUnsafePointer(candidate types.Type) bool {
	basic, isBasic := candidate.Underlying().(*types.Basic)
	return isBasic && basic.Kind() == types.UnsafePointer
}

func isUntypedNil(candidate types.Type) bool {
	basic, isBasic := candidate.(*types.Basic)
	return isBasic && basic.Kind() == types.UntypedNil
}

// excuseForExpression is the strongest excuse the comments give hatch: one directly above the hatch or above any
// statement, declaration or specification holding it, a function's doc comment among them, or one at the end of the
// last line of the hatch or of the innermost statement holding it, which may end lines below the hatch.
func excuseForExpression(comments *marker.Index, hatch ast.Node, enclosing []ast.Node) marker.Excuse {
	found := comments.ExcuseFor(hatch, unsafeMarker)
	innermostFound := false
	for index := len(enclosing) - 1; index >= 0; index-- {
		switch holder := enclosing[index].(type) {
		case ast.Stmt, ast.Decl, ast.Spec:
			found = max(found, marker.Read(comments.Above(holder), unsafeMarker))
			if !innermostFound {
				innermostFound = true
				for _, group := range comments.Trailing(holder) {
					found = max(found, marker.Read(group, unsafeMarker))
				}
			}
		}
	}
	return found
}

// judgeLinknames reports every //go:linkname directive in file with no reason directly above it.
func judgeLinknames(pass *analysis.Pass, file *ast.File) {
	for _, group := range file.Comments {
		for index, comment := range group.List {
			arguments, isDirective := strings.CutPrefix(comment.Text, linknameDirective)
			if !isDirective || !(strings.HasPrefix(arguments, " ") || strings.HasPrefix(arguments, "\t")) {
				continue
			}
			// The parser joins comments on consecutive lines into one group, so the lines directly above the directive
			// are the group's comments before it. A comment a blank line away, or trailing the code on the line
			// above, is a group of its own and speaks for nothing here.
			above := &ast.CommentGroup{List: group.List[:index]}
			report(pass, comment, "Linking to another package's symbol with `//go:linkname`",
				marker.Read(above, unsafeMarker))
		}
	}
}

// report flags node unless its excuse gives a reason, with the message that names what is missing.
func report(pass *analysis.Pass, node ast.Node, spelling string, given marker.Excuse) {
	message := escapeHatchWithoutReasonMessage
	switch given {
	case marker.WithReason:
		return
	case marker.WithoutReason:
		message = unsafeMarkerWithoutReasonMessage
	case marker.None:
	}
	pass.Report(analysis.Diagnostic{
		Pos:      node.Pos(),
		End:      node.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{"spelling": spelling}),
	})
}
