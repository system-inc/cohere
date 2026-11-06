package core

import (
	"sort"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// SortVariablesSettings is the decoded option surface.
type SortVariablesSettings struct {
	// IgnoreCase compares names case-insensitively. Upstream's default is false.
	IgnoreCase bool
}

// DefaultSortVariablesSettings is upstream's `defaultOptions: [{ ignoreCase: false }]`, which is also
// the zero value, so this exists for symmetry with its siblings rather than to correct anything.
func DefaultSortVariablesSettings() SortVariablesSettings {
	return SortVariablesSettings{}
}

// DecodeSortVariablesOptions reads the one flag off the config.
//
// The wire shape is a single object and every default is false, so the generic decoder would work.
// It is hand rolled anyway for one reason: `rule.DecodeOptionsInto` errors on empty input, and a
// rule configured as a bare "error" is handed nil. Upstream's default is a working configuration
// rather than a disabled one, so nil has to decode rather than fail.
func DecodeSortVariablesOptions(raw []byte) (SortVariablesSettings, error) {
	settings := DefaultSortVariablesSettings()
	if len(raw) == 0 {
		return settings, nil
	}
	var wire struct {
		IgnoreCase bool `json:"ignoreCase"`
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &wire); unmarshalOptionsError != nil {
		return settings, unmarshalOptionsError
	}
	settings.IgnoreCase = wire.IgnoreCase
	return settings, nil
}

// sortVariablesSettingsFrom recovers the settings from whatever the config layer handed over.
func sortVariablesSettingsFrom(options rule.Given[SortVariablesSettings]) SortVariablesSettings {
	if settings, ok := options.Read(); ok {
		return settings
	}
	return DefaultSortVariablesSettings()
}

var messageSortVariablesSortVariables = rule.Message{
	Id: "sortVars",
	Description: "A declaration block in a fixed order is one a reader can scan instead of search. " +
		"Sort the names, or split the block so each declaration stands where it is used.",
}

// SortVariables reports a variable in a declaration block that is out of alphabetical order.
//
//	valid:   var a, b, c;
//	valid:   var a, B, c;          (ignoreCase)
//	valid:   var {b} = x, a;       (a destructured name is not compared at all)
//	invalid: var b, a;             (fixed to `var a, b;`)
//	invalid: var b = foo(), a;     (reported, NOT fixed: the initializer is not a literal)
//
// # The rule compares only identifier declarators, and that filter runs before everything else
//
// `node.declarations.filter(decl => decl.id.type === "Identifier")` drops destructuring patterns
// entirely, so `var {b} = x, a;` compares nothing and is clean. The filtered list is also what the
// fixer rewrites, which matters: a pattern sitting between two identifiers is left where it is and
// only the identifiers move around it.
//
// # The fixer preserves the text between declarators, which is what makes it safe
//
// Upstream rebuilds the span from the first identifier declarator to the last, and for each sorted
// declarator appends the declarator's own text plus the original separator that followed the
// declarator in that position. So commas, newlines, indentation and comments stay where they were
// and only the names move through them. A fixer that joined with ", " would reflow the block and
// silently delete any comment inside it, which is the class of repair the standard warns about.
//
// # Two declines, both upstream's, both asserted by `output: null` in the corpus
//
// `unfixable` is true when any identifier declarator has a non-literal initializer, and it declines
// the whole block: reordering `var b = foo(), a = bar();` would change evaluation order, and only
// the author knows whether that is safe. A literal initializer has no side effect, so reordering is
// spelling rather than behaviour.
//
// `fixed` allows at most one repair per declaration block. Upstream reports every out-of-order
// declarator but attaches a fix only to the first, because the first fix already sorts the whole
// block and a second edit over the same span would be refused by the engine anyway.
//
// # Sorting is by the raw comparison upstream uses, not by a locale collation
//
// `aName > bName ? 1 : -1` is a JavaScript string comparison, which is by UTF-16 code unit. Go's
// `<` on strings compares by byte, and the two agree for every name below U+10000 encoded in UTF-8;
// they can disagree only where a surrogate pair meets a character in U+E000..U+FFFF, which no
// identifier in the corpus reaches. Recorded rather than papered over, since it is the one place
// this port could differ on an input nobody has written.
var SortVariables = rule.Rule[SortVariablesSettings]{
	Name: "sort-vars",

	Run: func(ruleContext rule.Context, options rule.Given[SortVariablesSettings]) rule.Listeners {
		settings := sortVariablesSettingsFrom(options)

		return rule.Listeners{
			ast.KindVariableDeclarationList: func(node *ast.Node) {
				checkSortVariablesDeclarationList(ruleContext, node, settings)
			},
		}
	},
}

// sortVariablesDeclarator is one identifier declarator and the span the fixer would move.
type sortVariablesDeclarator struct {
	node *ast.Node
	name string
	// start is upstream's `range[0]`: the declarator's first token, excluding the leading trivia
	// that `Pos()` includes. Using `Pos()` here would drag the preceding whitespace and any comment
	// into the moved text and duplicate it.
	start int
	end   int
}

// checkSortVariablesDeclarationList judges one declaration block.
func checkSortVariablesDeclarationList(ruleContext rule.Context, node *ast.Node, settings SortVariablesSettings) {
	list := node.AsVariableDeclarationList()
	if list == nil || list.Declarations == nil {
		return
	}

	declarators := make([]sortVariablesDeclarator, 0, len(list.Declarations.Nodes))
	unfixable := false
	for _, declaration := range list.Declarations.Nodes {
		typed := declaration.AsVariableDeclaration()
		if typed == nil {
			continue
		}
		name := typed.Name()
		// Upstream's `decl.id.type === "Identifier"` filter. A destructuring pattern is not
		// compared and not moved.
		if name == nil || name.Kind != ast.KindIdentifier {
			continue
		}
		if typed.Initializer != nil && !sortVariablesIsLiteral(typed.Initializer) {
			unfixable = true
		}
		declarators = append(declarators, sortVariablesDeclarator{
			node:  declaration,
			name:  name.Text(),
			start: scanner.GetRangeOfTokenAtPosition(ruleContext.SourceFile, declaration.Pos()).Pos(),
			end:   declaration.End(),
		})
	}
	if len(declarators) < 2 {
		return
	}

	// Upstream's reduce: compare each declarator against the last one that was in order, not
	// against its immediate predecessor. So `var c, b, a;` reports twice, on `b` and on `a`, both
	// against `c`.
	fixed := false
	previous := declarators[0]
	for _, current := range declarators[1:] {
		if sortVariablesNameFor(current.name, settings) >= sortVariablesNameFor(previous.name, settings) {
			previous = current
			continue
		}

		if unfixable || fixed {
			ruleContext.ReportNode(current.node, messageSortVariablesSortVariables)
		} else {
			ruleContext.ReportNodeWithFixes(current.node, messageSortVariablesSortVariables,
				sortVariablesFix(ruleContext, declarators, settings))
		}
		fixed = true
		// Deliberately not advancing `previous`. Upstream returns `memo` from this branch, so an
		// out-of-order declarator is not treated as the new baseline.
	}
}

// sortVariablesNameFor applies the ignoreCase option, which is upstream's `getSortableName`.
func sortVariablesNameFor(name string, settings SortVariablesSettings) string {
	if settings.IgnoreCase {
		return strings.ToLower(name)
	}
	return name
}

// sortVariablesIsLiteral answers upstream's `decl.init.type !== "Literal"`.
//
// ESTree's `Literal` covers a string, a number, a boolean, `null` and a regular expression, and it
// is one node type there. Our parser gives each its own kind, and `true`/`false`/`null` are
// keywords rather than literals, so the set has to be written out. A template literal is
// deliberately absent: ESTree models it as `TemplateLiteral`, not `Literal`, so upstream treats it
// as unfixable and so does this.
func sortVariablesIsLiteral(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindBigIntLiteral,
		ast.KindRegularExpressionLiteral, ast.KindTrueKeyword, ast.KindFalseKeyword,
		ast.KindNullKeyword:
		return true
	}
	return false
}

// sortVariablesFix rebuilds the span from the first identifier declarator to the last, with the
// declarators sorted and the original separators kept in place.
//
// The separator for position `index` is the text between declarator `index`'s end and declarator
// `index+1`'s start in the original order, which is upstream's `textAfterIdentifier`. That is what
// keeps a comment or a newline attached to its position in the block rather than to the name that
// happened to be there.
func sortVariablesFix(ruleContext rule.Context, declarators []sortVariablesDeclarator,
	settings SortVariablesSettings) rule.Fix {

	text := ruleContext.SourceFile.Text()

	sorted := make([]sortVariablesDeclarator, len(declarators))
	copy(sorted, declarators)
	sort.SliceStable(sorted, func(first, second int) bool {
		return sortVariablesNameFor(sorted[first].name, settings) <
			sortVariablesNameFor(sorted[second].name, settings)
	})

	var rebuilt strings.Builder
	for index, declarator := range sorted {
		rebuilt.WriteString(text[declarator.start:declarator.end])
		if index < len(declarators)-1 {
			rebuilt.WriteString(text[declarators[index].end:declarators[index+1].start])
		}
	}

	return rule.ReplaceRange(
		core.NewTextRange(declarators[0].start, declarators[len(declarators)-1].end),
		rebuilt.String())
}
