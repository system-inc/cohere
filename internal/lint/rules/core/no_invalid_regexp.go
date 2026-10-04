package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"
	"github.com/system-inc/cohere/internal/lint/rule"
)

var messageInvalidRegexp = rule.Message{
	Id: "invalidRegexp",
	Description: "This RegExp constructor is handed a pattern or flags the engine cannot parse, so " +
		"the line throws a SyntaxError when it runs. A regex literal with the same mistake is caught " +
		"by the parser and never ships; written as a string it survives compilation, type checking " +
		"and review, and fails at the moment the constructor is reached.",
}

// validRegExpFlags are the flags the language defines. A flag outside this set is a SyntaxError
// rather than a no-op, which is why an unknown one is worth reporting rather than ignoring.
var validRegExpFlags = map[byte]bool{
	'd': true, 'g': true, 'i': true, 'm': true,
	's': true, 'u': true, 'v': true, 'y': true,
}

// NoInvalidRegexp flags a RegExp constructor whose pattern or flags cannot be parsed.
//
//	valid:   new RegExp('[a-z]', 'i')
//	valid:   new RegExp(somePattern)          // not a literal, nothing to check
//	valid:   RegExp()                         // produces /(?:)/
//	invalid: new RegExp('[')
//	invalid: new RegExp('.', 'z')
//	invalid: new RegExp('.', 'ii')
//	invalid: new RegExp('.', 'uv')
//
// Only a string literal is checked. `new RegExp(pattern)` where pattern is a variable could be
// anything at runtime, and reporting it would flag correct code; that is a deliberate narrowing
// rather than a gap, and it matches what ESLint does.
//
// The flags are validated before the pattern because a flag changes what the pattern means. `u`
// makes `\p{L}` a property escape and a lone `{` a syntax error, so validating a pattern against
// flags that are themselves invalid asks a question with no answer.
//
// When the flags are not a literal the pattern is only reported if it fails under every flag
// combination that could reach it. A pattern valid under `u` and invalid without it is not a defect
// here, because the flags argument may well supply the `u`.
//
// # A local named RegExp, and a pattern under `v`
//
// The constructor is checked only when `RegExp` is the global, asked of the checker, since
// `function foo(RegExp) { RegExp('['); }` calls whatever the caller passed. ESLint's corpus pins three
// such rows as clean, and they read as extra until the global was asked (#jjfa7qb).
//
// A pattern under `v` is not checked, only its flags. `v` has its own grammar (set operations like
// `[A--B]`, nested classes, `\q{...}`), and the engine this compiles with does not speak it, so
// reading the pattern as `u` reported valid `v` patterns, three of ESLint's rows, and passed invalid
// ones. Silence is the honest answer when cohere cannot parse what it is asked about. The full answer
// is an ECMAScript pattern validator in place of the engine (#4bgr3h2 tracks it).
//
// # The option we deliberately do not implement
//
// Upstream takes `allowConstructorFlags`, a case-sensitive list of flag characters that stop being
// errors in a constructor call. With `["a"]`, `new RegExp('.', 'a')` becomes clean. It is threaded
// through the flag walk rather than applied afterwards, because oxc's own comment notes the regex
// engine cannot take the option.
//
// **This port has no option surface and takes the default empty allow-list**, so every flag outside
// `validRegExpFlags` reports. Nothing in this repository configures the option, and a rule reading a
// value nobody sets is the inert shape this tool exists to catch. If a consumer needs it, it gets
// built then, against a real requirement.
//
// Matching the default is asserted rather than assumed, because a port with no option surface lands
// on one branch or the other and a fixture pair cannot tell which: the corpus only ever exercises
// the default. `new RegExp('.', 'a')` reports here and `new RegExp('.', 'i')` does not, which is
// upstream's behavior with an empty allow-list. Missing surface is wrong later; a mismatched default
// would be wrong now.
var NoInvalidRegexp = rule.Rule{
	Name: "no-invalid-regexp",

	// Whether `RegExp` is the global
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		check := func(node *ast.Node, callee *ast.Node, arguments *ast.NodeList) {
			callee = ast.SkipParentheses(callee)
			if callee == nil || callee.Kind != ast.KindIdentifier {
				return
			}
			if callee.AsIdentifier().Text != "RegExp" {
				return
			}
			if ctx.TypeChecker == nil || !rule.IsDeclaredOnlyInDeclarationFiles(ctx.TypeChecker.GetSymbolAtLocation(callee)) {
				return
			}
			// `RegExp()` with no arguments produces the empty pattern and cannot fail.
			if arguments == nil || len(arguments.Nodes) == 0 {
				return
			}

			// A nil flags pointer means the second argument is not a literal, so what the flags
			// will be is unknowable here. That is different from the empty string, which is what a
			// one-argument call passes, and the two lead to different checks below.
			var flags *string
			if len(arguments.Nodes) >= 2 {
				if flagsNode := arguments.Nodes[1]; flagsNode.Kind == ast.KindStringLiteral {
					text := flagsNode.AsStringLiteral().Text
					flags = &text
				}
			} else {
				empty := ""
				flags = &empty
			}

			if flags != nil {
				if message := invalidFlagsMessage(*flags); message != "" {
					ctx.ReportNode(node, rule.Message{Id: messageInvalidRegexp.Id, Description: message})
					return
				}
			}

			patternNode := arguments.Nodes[0]
			if patternNode.Kind != ast.KindStringLiteral {
				return
			}
			pattern := patternNode.AsStringLiteral().Text

			if flags != nil {
				// Under `v` the pattern has a grammar the engine does not parse; see the doc above
				if strings.ContainsRune(*flags, 'v') {
					return
				}
				if message := invalidPatternMessage(pattern, *flags); message != "" {
					ctx.ReportNode(node, rule.Message{Id: messageInvalidRegexp.Id, Description: message})
				}
				return
			}

			// Unknown flags: report only when no reachable flag combination accepts the pattern.
			// `v` could also reach it, and is unknowable here, so a pattern is reported only when
			// both readings the engine can check refuse it and `v` is the one left unasked
			withUnicode := invalidPatternMessage(pattern, "u")
			withNeither := invalidPatternMessage(pattern, "")
			if withUnicode != "" && withNeither != "" {
				ctx.ReportNode(node, rule.Message{Id: messageInvalidRegexp.Id, Description: withNeither})
			}
		}

		return rule.Listeners{
			ast.KindCallExpression: func(node *ast.Node) {
				call := node.AsCallExpression()
				check(node, call.Expression, call.Arguments)
			},
			ast.KindNewExpression: func(node *ast.Node) {
				expression := node.AsNewExpression()
				check(node, expression.Expression, expression.Arguments)
			},
		}
	},
}

// invalidFlagsMessage reports why a flags string is not accepted, or the empty string when it is.
//
// The order matters and follows the language rather than convenience: each valid flag is struck
// once, so what remains is either a duplicate or an unknown one. Checking `u` and `v` together
// first is what makes `uv` report as mutually exclusive rather than as two fine flags.
func invalidFlagsMessage(flags string) string {
	remaining := flags
	for flag := range validRegExpFlags {
		remaining = strings.Replace(remaining, string(flag), "", 1)
	}

	if strings.Contains(flags, "u") && strings.Contains(flags, "v") {
		return fmt.Sprintf("Invalid regular expression flags: %s. The u and v flags cannot both be set", flags)
	}
	for index := 0; index < len(remaining); index++ {
		if validRegExpFlags[remaining[index]] {
			return fmt.Sprintf("Invalid regular expression flags: %s. Duplicate flag %q", flags, remaining[index])
		}
	}
	if remaining != "" {
		return fmt.Sprintf("Invalid regular expression flags: %s. Unknown flag %q", flags, remaining[0])
	}
	return ""
}

// invalidPatternMessage reports why a pattern is not accepted under the given flags.
//
// Only the flags that change what a pattern means are passed to the parser: the rest affect
// matching rather than parsing, and handing them over would ask the parser about something it does
// not decide. `v` extends `u` rather than replacing it, so a pattern under `v` is parsed as `u`.
func invalidPatternMessage(pattern string, flags string) string {
	var parseFlags strings.Builder
	for _, flag := range "imsu" {
		if strings.ContainsRune(flags, flag) {
			parseFlags.WriteRune(flag)
		}
	}
	compileFlags := parseFlags.String()
	if !strings.Contains(compileFlags, "u") && strings.Contains(flags, "v") {
		compileFlags += "u"
	}

	if _, err := esregexp.Compile(pattern, compileFlags); err != nil {
		text := err.Error()
		// The underlying engine prefixes "error parsing regexp: ", which repeats what the message
		// already says. Trimming to the first ": " drops it. Our parser's own refusals put the
		// refused construct after that same separator, so trimming there would keep the construct
		// and throw away the explanation, which is why they are excluded.
		if !errors.Is(err, esregexp.ErrUnsupportedSyntax) && !errors.Is(err, esregexp.ErrUnsupportedFlag) {
			if separator := strings.Index(text, ": "); separator != -1 {
				text = text[separator+2:]
			}
		}
		return fmt.Sprintf("Invalid regular expression: /%s/: %s", pattern, text)
	}
	return ""
}
