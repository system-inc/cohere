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
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/marker"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/concurrency-require-escape-hatch-reason"

// listener reads the file's comments as the syntax dispatcher's walk enters each file, hears every call in it, and
// judges the file's //go:linkname directives as the walk leaves it.
var listener = rule.Listener{
	Requires:  []*analysis.Analyzer{marker.Analyzer},
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.CallExpr)(nil)},
	Node:      judgeCall,
	FileExit:  judgeLinknames,
}

// Analyzer runs the rule over one package, reporting what the syntax dispatcher's walk found for it.
var Analyzer = dispatch.RuleAnalyzer(Name, "concurrency_require_escape_hatch_reason", "Flags a conversion to or "+
	"from unsafe.Pointer, a call to unsafe.Add, unsafe.Slice, unsafe.SliceData, unsafe.String or unsafe.StringData, "+
	"and a //go:linkname directive, unless a // Unsafe: <reason> comment says why it is safe: for an expression, "+
	"directly above it or above a statement or declaration holding it, or at the end of its line or its statement's; "+
	"for a directive, directly above it. unsafe.Sizeof, unsafe.Alignof and unsafe.Offsetof are not judged, since "+
	"they only measure a type.", &listener)

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer, Listener: &listener}

var escapeHatchWithoutReasonMessage = policy.MessageOf(Name, "escapeHatchWithoutReason")

var unsafeMarkerWithoutReasonMessage = policy.MessageOf(Name, "unsafeMarkerWithoutReason")

// linknameDirective opens a //go:linkname line. The compiler reads a directive only where the comment's text starts
// with it, with no space after the slashes, so `// go:linkname` and a block comment are prose.
const linknameDirective = "//go:linkname"

// pointerEscapingFunctions are the functions of package unsafe that make or move a pointer the compiler cannot
// check. Sizeof, Alignof and Offsetof are left out: they are constants about a type and reach no memory.
var pointerEscapingFunctions = []string{"Add", "Slice", "SliceData", "String", "StringData"}

// packageState is what the rule reads once for its package and once for each file, held in its context's state.
type packageState struct {
	// escaping is the objects of pointerEscapingFunctions in package unsafe, read as the walk enters its first file.
	escaping map[types.Object]bool
	// indexes is the package's comment indexes, read with escaping.
	indexes *marker.Indexes
	// comments is the comment index of the file being walked.
	comments *marker.Index
	// hatchStart and hatchEnd span the last hatch reported in the file being walked. A hatch is judged once with
	// everything inside it, so `uintptr(unsafe.Pointer(&value))` is one finding and one reason covers it: a call that
	// opens within the span is inside the hatch, since the walk is in preorder and whatever follows the hatch outside
	// it opens at or after its end.
	hatchStart token.Pos
	hatchEnd   token.Pos
}

// enterFile reads the package's escaping functions and comment indexes the first time, and file's comment index.
func enterFile(context *rule.Context, file *ast.File) {
	state := rule.State[packageState](context)
	if state.indexes == nil {
		state.escaping = map[types.Object]bool{}
		for _, name := range pointerEscapingFunctions {
			state.escaping[types.Unsafe.Scope().Lookup(name)] = true
		}
		indexes, isIndexes := context.ResultOf(marker.Analyzer).(*marker.Indexes)
		if !isIndexes {
			// Invariant: marker's result is always its Indexes, so another is a mistake in the engine, and the
			// dispatcher's recover makes it this rule's crash on the package, so it never stops the process.
			panic(fmt.Sprintf("reading the comment indexes: marker gave %T", context.ResultOf(marker.Analyzer)))
		}
		state.indexes = indexes
	}
	state.comments = state.indexes.ForFile(file)
	state.hatchStart, state.hatchEnd = token.NoPos, token.NoPos
}

// judgeCall reports node when it is an unsafe conversion or call whose comments give no reason, and is not inside a
// hatch already reported. stack is the path from the file down to it, so a hatch finds the statements and
// declarations that hold it.
func judgeCall(context *rule.Context, node ast.Node, stack []ast.Node) {
	state := rule.State[packageState](context)
	call, isCall := node.(*ast.CallExpr)
	if !isCall || (call.Pos() >= state.hatchStart && call.Pos() < state.hatchEnd) {
		return
	}
	spelling := hatchSpelling(context, call, state.escaping)
	if spelling == "" {
		return
	}
	report(context, call, spelling, excuseForExpression(state.comments, call, stack))
	state.hatchStart, state.hatchEnd = call.Pos(), call.End()
}

// hatchSpelling is how the message names the escape hatch call is, or "" when it is none. A conversion is found by
// its types, so a named type whose underlying type is unsafe.Pointer is one too, and a call by the object it
// resolves to, so a function or field named Pointer or String elsewhere is never mistaken for package unsafe's.
func hatchSpelling(context *rule.Context, call *ast.CallExpr, escaping map[types.Object]bool) string {
	if function, isTypeAndValue := context.TypesInfo.Types[call.Fun]; isTypeAndValue && function.IsType() {
		if len(call.Args) != 1 {
			return ""
		}
		target := context.TypesInfo.TypeOf(call)
		source := context.TypesInfo.TypeOf(call.Args[0])
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
	callee := typeutil.Callee(context.TypesInfo, call)
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
	found := comments.ExcuseFor(hatch, marker.Unsafe)
	innermostFound := false
	for index := len(enclosing) - 1; index >= 0; index-- {
		switch holder := enclosing[index].(type) {
		case ast.Stmt, ast.Decl, ast.Spec:
			found = max(found, marker.Read(comments.Above(holder), marker.Unsafe))
			if !innermostFound {
				innermostFound = true
				for _, group := range comments.Trailing(holder) {
					found = max(found, marker.Read(group, marker.Unsafe))
				}
			}
		}
	}
	return found
}

// judgeLinknames reports every //go:linkname directive in file with no reason directly above it.
func judgeLinknames(context *rule.Context, file *ast.File) {
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
			report(context, comment, "Linking to another package's symbol with `//go:linkname`",
				marker.Read(above, marker.Unsafe))
		}
	}
}

// report flags node unless its excuse gives a reason, with the message that names what is missing.
func report(context *rule.Context, node ast.Node, spelling string, given marker.Excuse) {
	message := escapeHatchWithoutReasonMessage
	switch given {
	case marker.WithReason:
		return
	case marker.WithoutReason:
		message = unsafeMarkerWithoutReasonMessage
	case marker.None:
	}
	context.Report(analysis.Diagnostic{
		Pos:      node.Pos(),
		End:      node.End(),
		Category: message.Id,
		Message:  message.Render(map[string]string{"spelling": spelling}),
	})
}
