package type_checking

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
)

/*
 * A type is printed within stableTypeTextBudget characters, as TypeToString keeps one short: first with the widest
 * limits, then with each narrower pair in turn, until it fits or the narrowest is used. Members past the member
 * limit become `... N more ...`, and nesting past the depth limit becomes the type's name, or `...`. The steps are
 * fixed, so the text is the same in every run. Without a budget, one union of column definitions printed 6,247
 * characters where TypeToString had printed at most 1,484.
 */
const stableTypeTextBudget = 400

var stableTypeLimits = []struct{ members, depth int }{{8, 5}, {4, 4}, {3, 3}, {2, 2}, {1, 1}}

/*
 * StableTypeText prints a type the same way whichever checker holds it and whatever that checker did before, so
 * a message that names a type reads the same in every run (#dq881as).
 *
 * TypeToString can't promise that. It prints a union's members and an object's members in the checker's own
 * order, and that order moves with the checker's history:
 *   - a union's members are ordered by type id, which follows creation order;
 *   - an anonymous object's members are ordered by declaration, and the `name?: undefined` members TypeScript
 *     adds when it normalizes a union of object literals borrow the declaration of whichever sibling property the
 *     checker first made one from (getUndefinedProperty caches it by name, per checker).
 * With the walk and the type check sharing checkers concurrently, the same literal printed its members in
 * another order from run to run on www-phi-health at `GOMAXPROCS=16`, while single-threaded runs agreed.
 *
 * The order here uses no type id and no checker history, all the way down:
 *   - an anonymous object's own members (declared directly in the literal, namespace or file it came from) by
 *     their position, then every other member (the synthesized ones) by name;
 *   - a union's and an intersection's members by their own printed text, null and undefined last, which is a
 *     total order: two members that print alike are interchangeable in the text;
 *   - a named type by its name, with its type arguments printed by these same rules.
 *
 * This is not TypeScript's single-threaded order, which no type can recover (the cache's first entry is a fact
 * about the whole program's check order). So it is for messages that answer to nobody else's text: the adamic
 * rules. A ported rule's message is held to ESLint's, which prints the checker's own order.
 */
func StableTypeText(typeChecker *checker.Checker, subjectType *checker.Type) string {
	var text string
	for _, limit := range stableTypeLimits {
		printer := stableTypePrinter{typeChecker: typeChecker, maximumMembers: limit.members, maximumDepth: limit.depth}
		if text = printer.print(subjectType, 0); len(text) <= stableTypeTextBudget {
			break
		}
	}
	return text
}

// memberHome is the declaration a member is declared directly in: its parent, or for a namespace's and a file's
// statements the namespace or file around their block and variable statement.
func memberHome(declaration *ast.Node) *ast.Node {
	home := declaration.Parent
	for home != nil && (home.Kind == ast.KindModuleBlock ||
		home.Kind == ast.KindVariableStatement ||
		home.Kind == ast.KindVariableDeclarationList) {
		home = home.Parent
	}
	return home
}

// StablePropertyName prints a property's name as a member is written: bare when it's an identifier, quoted when
// it isn't, and `[name]` for a member keyed by a symbol.
//
// typescript-go names a symbol-keyed member with the raw byte 0xFE, which is not UTF-8 so no identifier can hold
// it: `\xfe@name@id` for a unique symbol (getESSymbolLikeTypeForNode), whose id is the symbol's creation order and
// moves from run to run, and `\xfe@name` for a known symbol the library doesn't declare. It is matched as bytes
// (#z9jcxp1): a regexp reads the invalid byte as U+FFFD, and the pattern this replaced matched U+00FE, so no
// name ever matched and the id was printed.
func StablePropertyName(name string) string {
	if symbolName, ok := strings.CutPrefix(name, ast.InternalSymbolNamePrefix+"@"); ok {
		if at := strings.LastIndexByte(symbolName, '@'); at >= 0 && isDecimal(symbolName[at+1:]) {
			symbolName = symbolName[:at]
		}
		return "[" + symbolName + "]"
	}
	if !isIdentifierName(name) {
		return strconv.Quote(name)
	}
	return name
}

// identifierName is a property name TypeScript prints bare.
var identifierName = regexp.MustCompile(`^[\p{L}_$][\p{L}\p{N}_$]*$`)

func isIdentifierName(name string) bool {
	return identifierName.MatchString(name)
}

// isDecimal is a symbol id: one or more ASCII digits.
func isDecimal(text string) bool {
	return text != "" && strings.Trim(text, "0123456789") == ""
}
