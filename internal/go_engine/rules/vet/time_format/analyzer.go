// Package time_format is vet's timeformat pass, a call of (time.Time).Format or time.Parse whose layout holds
// 2006-02-01, year, day and month, which no common calendar standard writes, so 2006-01-02, year, month and day, was
// almost surely meant. Only a layout the compiler knows, a constant, is judged; a layout held in a variable is left
// alone. Where the layout is written as one string literal, the finding covers the mistake inside it and offers the
// fix that swaps in 2006-01-02. It sits on the engine's syntax dispatcher, hearing each call on the walk the
// dispatcher makes once for every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/timeformat/timeformat.go, whose
// logic it keeps and whose messages and fix it reports word for word. The registry runs the port in the original's
// place, and the original stays the oracle: the parity tests beside this file run both over the same packages.
//
// It differs in two places, where the original is wrong, and the differences fixtures hold each shape.
//
// The original finds the mistake in the literal's value, with its escapes decoded, and uses that index as an offset
// into the literal's source text. The two agree only while no escape comes before the mistake or inside it, and in a
// raw literal only while no carriage return does, since a raw literal's value drops them. Where they part, the
// original's finding covers the wrong bytes and its fix rewrites them: `"\t2006-02-01"` becomes `"\2006-01-021"`. The
// port walks the literal's source text, decoding as the compiler does, to the bytes that spell the mistake, so its
// finding covers them and its fix replaces them alone.
//
// The original reads the layout from the call's first argument, which in a method expression,
// `time.Time.Format(moment, "2006-02-01")`, is the receiver, so it never judges the layout there. The port reads the
// argument after the receiver.
package time_format

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// badFormat is the layout the pass looks for, year, day and month, and goodFormat the one it suggests, year, month and
// day, under the original's names.
const (
	badFormat  = "2006-02-01"
	goodFormat = "2006-01-02"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "timeformat"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for calls of (time.Time).Format or time.Parse with 2006-02-01\n\n" +
	"The timeformat checker looks for time formats with the 2006-02-01 (yyyy-dd-mm)\n" +
	"format. Internationally, \"yyyy-dd-mm\" does not occur in common calendar date\n" +
	"standards, and so it is more likely that 2006-01-02 (yyyy-mm-dd) was intended."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/timeformat"

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, as the original's
// Preorder visits them, of which it judges the calls of (time.Time).Format and time.Parse. Every package is heard,
// even one that does not import "time": a value of time.Time, returned by a function of another package, has its
// Format method called without the import.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.CallExpr)(nil)},
	Node:  checkCall,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	return analyzer
}

// checkCall reports node when it calls (time.Time).Format or time.Parse with a constant layout holding badFormat,
// over the mistake and with its fix where the layout is one string literal.
func checkCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	callee := typeutil.Callee(context.TypesInfo, call)
	if !isMethodNamed(callee, "time", "Time", "Format") && !isFunctionNamed(callee, "time", "Parse") {
		return
	}
	// Both take the layout first, after the receiver when Format is called as a method expression, so a call that
	// type-checks always has it. The original's guard is kept for a package with type errors, whose calls may be
	// missing arguments.
	layoutIndex := 0
	if isMethodExpression(context.TypesInfo, call.Fun) {
		layoutIndex = 1
	}
	if len(call.Args) <= layoutIndex {
		return
	}
	layout := call.Args[layoutIndex]
	badAt := badFormatAt(context.TypesInfo, layout)
	if badAt < 0 {
		return
	}
	// Only a layout written as one literal has source text the fix can rewrite. A named constant or a constant
	// expression is reported at its start, with no fix, as the original reports it.
	literal, isLiteral := layout.(*ast.BasicLit)
	if !isLiteral {
		context.Report(analysis.Diagnostic{Pos: layout.Pos(), Message: badFormat + " should be " + goodFormat})
		return
	}
	start, end, spanError := spanOf(context, literal, badAt)
	if spanError != nil {
		// A listener returns no error, so the one that ended the port's run on the package, a file that no longer
		// holds the literal its syntax does, is raised as it is, its text unchanged, with no finding, as a run that
		// returned it had none.
		// Invariant: the dispatcher's recover makes this panic the rule's crash on the package, so it never stops
		// the process.
		panic(spanError)
	}
	context.Report(analysis.Diagnostic{
		Pos:     start,
		End:     end,
		Message: badFormat + " should be " + goodFormat,
		SuggestedFixes: []analysis.SuggestedFix{{
			Message:   "Replace " + badFormat + " with " + goodFormat,
			TextEdits: []analysis.TextEdit{{Pos: start, End: end, NewText: []byte(goodFormat)}},
		}},
	})
}

// isMethodExpression reports whether callee, looking through parentheses, selects a method from a type,
// `time.Time.Format`, whose call takes the receiver as its first argument.
func isMethodExpression(information *types.Info, callee ast.Expr) bool {
	selector, isSelector := ast.Unparen(callee).(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	selection, selected := information.Selections[selector]
	return selected && selection.Kind() == types.MethodExpr
}

// badFormatAt is the index in expression's constant string value of the first badFormat it holds, and -1 when it
// holds none, is not a constant, or is not of a basic string type.
func badFormatAt(information *types.Info, expression ast.Expr) int {
	typeAndValue, recorded := information.Types[expression]
	if !recorded {
		// A pass records every expression of a package that type-checks, so this is only an expression the checker
		// gave up on, and the original assumes its layout is good.
		return -1
	}
	// The original reads the recorded type as it stands, without looking through an alias, so a constant of a type
	// declared `type text = string` is not judged. The port keeps that: a finding the original never makes would be
	// a difference, and an alias for string as a layout's type is not one this port was asked to rule on.
	basic, isBasic := typeAndValue.Type.(*types.Basic)
	if !isBasic || basic.Info()&types.IsString == 0 {
		return -1
	}
	if typeAndValue.Value == nil {
		return -1
	}
	return strings.Index(constant.StringVal(typeAndValue.Value), badFormat)
}

// spanOf is where literal's source text spells the badFormat its value holds at badAt: from the first byte of the
// source that decodes into the value at badAt to the byte after the last that decodes into it.
func spanOf(context *rule.Context, literal *ast.BasicLit, badAt int) (token.Pos, token.Pos, error) {
	source, sourceError := sourceOf(context, literal)
	if sourceError != nil {
		return token.NoPos, token.NoPos, sourceError
	}
	start, startError := sourceOffset(source, badAt)
	if startError != nil {
		return token.NoPos, token.NoPos,
			fmt.Errorf("time_format: %s: %w", context.Fset.Position(literal.Pos()), startError)
	}
	// The walk stops at the first byte after the value before the mistake, and in a raw literal the carriage returns
	// there decode into nothing, so they spell no part of the mistake. The mistake follows them, so the loop ends on
	// its first byte.
	for source[0] == '`' && source[start] == '\r' {
		start++
	}
	end, endError := sourceOffset(source, badAt+len(badFormat))
	if endError != nil {
		return token.NoPos, token.NoPos,
			fmt.Errorf("time_format: %s: %w", context.Fset.Position(literal.Pos()), endError)
	}
	return literal.Pos() + token.Pos(start), literal.Pos() + token.Pos(end), nil
}

// sourceOf is literal's source text, quotes included. An interpreted literal's is the text its syntax keeps. A raw
// literal's is read from its file, since the scanner drops a raw literal's carriage returns from the text it keeps,
// while the positions it records count them.
func sourceOf(context *rule.Context, literal *ast.BasicLit) (string, error) {
	if !strings.HasPrefix(literal.Value, "`") {
		return literal.Value, nil
	}
	file := context.Fset.File(literal.Pos())
	if file == nil {
		// Invariant: every position in a package's syntax lies in a file of the package's file set.
		panic("time_format: a layout literal lies in no file of the file set")
	}
	content, readError := context.ReadFile(file.Name())
	if readError != nil {
		return "", fmt.Errorf("time_format: reading %s for a layout literal: %w", file.Name(), readError)
	}
	// opening is the offset of the literal's opening quote, and closing the length of what follows it up to and
	// including the closing one, which a raw literal never holds within.
	opening := file.Offset(literal.Pos())
	closing := -1
	if opening < len(content) {
		closing = bytes.IndexByte(content[opening+1:], '`')
	}
	if closing < 0 {
		return "", fmt.Errorf("time_format: %s: %s no longer holds the raw literal the syntax holds there",
			context.Fset.Position(literal.Pos()), file.Name())
	}
	return string(content[opening : opening+1+closing+1]), nil
}

// sourceOffset is the offset in source, a string literal's text from its opening quote, of the text that decodes into
// the byte at valueIndex of its value. valueIndex must fall where one character of the value ends and the next begins,
// as the start and end of badFormat, which is ASCII, always do.
func sourceOffset(source string, valueIndex int) (int, error) {
	quote := source[0]
	// offset walks the source after the opening quote, and decoded counts the bytes of the value it has decoded.
	offset := 1
	decoded := 0
	for decoded < valueIndex {
		if offset >= len(source)-1 {
			return 0, fmt.Errorf("the literal's value ends before its byte %d", valueIndex)
		}
		if quote == '`' {
			// A raw literal's source is its value, less every carriage return, which the compiler drops.
			if source[offset] != '\r' {
				decoded++
			}
			offset++
			continue
		}
		character, multipleBytes, tail, unquoteError := strconv.UnquoteChar(source[offset:], quote)
		if unquoteError != nil {
			return 0, fmt.Errorf("decoding the literal at its byte %d: %w", offset, unquoteError)
		}
		offset = len(source) - len(tail)
		// A `\x` or octal escape, or an unescaped byte of invalid UTF-8, stands for one byte, and anything else for
		// the character's UTF-8 encoding.
		if multipleBytes {
			decoded += utf8.RuneLen(character)
		} else {
			decoded++
		}
	}
	if decoded != valueIndex {
		return 0, fmt.Errorf("the literal's byte %d falls inside one character", valueIndex)
	}
	return offset, nil
}

// isFunctionNamed reports whether object is a package-level function, not a method, of the package at packagePath,
// named one of names. x/tools' typesinternal.IsFunctionNamed.
func isFunctionNamed(object types.Object, packagePath string, names ...string) bool {
	function, isFunction := object.(*types.Func)
	return isFunction &&
		isPackageLevel(object) &&
		function.Pkg().Path() == packagePath &&
		function.Signature().Recv() == nil &&
		slices.Contains(names, function.Name())
}

// isMethodNamed reports whether object is a method, named one of names, whose receiver is the package-level type
// typeName of the package at packagePath, or a pointer to it. x/tools' typesinternal.IsMethodNamed.
func isMethodNamed(object types.Object, packagePath string, typeName string, names ...string) bool {
	function, isFunction := object.(*types.Func)
	if !isFunction {
		return false
	}
	receiver := function.Signature().Recv()
	if receiver == nil {
		return false
	}
	named := receiverNamed(receiver)
	return named != nil && isTypeNamed(named, packagePath, typeName) && slices.Contains(names, function.Name())
}

// receiverNamed is the named type of receiver, looking through one pointer and any aliases, and nil when there is
// none. x/tools' typesinternal.ReceiverNamed, less the report of whether a pointer was looked through, which no
// caller here reads.
func receiverNamed(receiver *types.Var) *types.Named {
	receiverType := receiver.Type()
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	named, isNamed := types.Unalias(receiverType).(*types.Named)
	if !isNamed {
		return nil
	}
	return named
}

// isTypeNamed reports whether typed is, or is an alias for, a package-level defined type of the package at
// packagePath, named one of names. x/tools' typesinternal.IsTypeNamed.
func isTypeNamed(typed types.Type, packagePath string, names ...string) bool {
	named, isNamed := types.Unalias(typed).(*types.Named)
	if !isNamed {
		return false
	}
	typeName := named.Obj()
	return typeName != nil &&
		isPackageLevel(typeName) &&
		typeName.Pkg().Path() == packagePath &&
		slices.Contains(names, typeName.Name())
}

// isPackageLevel reports whether object is declared at its package's top level. x/tools'
// typesinternal.IsPackageLevel.
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}
