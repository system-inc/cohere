package estree

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
)

/*
 * typescript-estree's convert.js, ported over typescript-go's AST.
 *
 * Upstream is @typescript-eslint/typescript-estree 8.65.0, dist/convert.js, the version the Prettier
 * fork bundles. Each case below is the upstream case of the same SyntaxKind, in upstream's order, and
 * builds the same properties. Properties nobody reads still get built where upstream builds them,
 * because the printer reads nodes generically (a visitor-key walk, `key in node`), and a property that
 * is missing here but present upstream is the kind of difference nobody finds by reading.
 *
 * Where typescript-go's tree differs from TypeScript's, the difference is absorbed here and named:
 *
 *   - Modifiers and decorators share one ModifierList. Upstream asks ts.getModifiers and
 *     ts.getDecorators separately; modifiers() and decorators() below split the list the same way.
 *   - A nested namespace (`namespace A.B {}`) carries a zero-width export modifier the parser
 *     synthesizes and flags Reparsed. TypeScript's tree has no such modifier, so reparsed modifiers
 *     are dropped.
 *   - `declare global` is a ModuleDeclaration whose Keyword is GlobalKeyword, where TypeScript sets
 *     NodeFlags.GlobalAugmentation; `namespace` versus `module` is the Keyword too.
 *
 * Upstream's syntax checks (check-syntax-errors.js) are ported for one case, an update of something
 * that cannot be assigned (`1++`, Converter.checkSyntaxError), which the tsc corpus found the formatter
 * printing where Prettier refuses. The rest are not. They reject programs TypeScript parsed but ESTree
 * cannot represent, the oracle fails on those files, and a file the oracle fails on leaves the
 * differential's denominator. Parse errors themselves are refused before conversion.
 */

// ConvertForBabel is Convert for a file Prettier would parse with babel (see ParseJavaScript). The
// one difference in the tree is postprocess's: Babel keeps a parenthesized expression as a node
// when a type cast comment, `/** @type {T} */ (value)`, comes right before it.
func ConvertForBabel(sourceFile *ast.SourceFile, nodes *Arena) (*Node, []*Node, error) {
	// postprocess merges nestled JSDoc comments before it looks for casts, so this does too.
	ends := []int{}
	for _, comment := range mergeNestledJsdocComments(collectComments(sourceFile)) {
		if IsTypeCastComment(comment) {
			ends = append(ends, LocEnd(comment))
		}
	}
	return convert(sourceFile, ends, nodes)
}

var typeCastCommentPattern = regexp.MustCompile(`@(?:type|satisfies)\b`)

// IsTypeCastComment is upstream's isTypeCastComment, utilities/is-type-cast-comment.js. Upstream
// memoizes it per comment; the answer reads only the comment's value, so computing it each time is
// the same.
func IsTypeCastComment(comment *Node) bool {
	return comment.Is("Block", "CommentBlock", "MultiLine") &&
		strings.HasPrefix(comment.String("value"), "*") &&
		// TypeScript expects the type to be enclosed in curly brackets, however
		// Closure Compiler accepts types in parens and even without any delimiters at all.
		// That's why we just search for "@type" and "@satisfies".
		typeCastCommentPattern.MatchString(comment.String("value"))
}

// Convert parses nothing: it converts a parsed source file, and returns the Program with its comments.
//
// The file must have parsed without diagnostics. Upstream throws the first parse diagnostic before
// converting, and so does this. The tree's nodes come from nodes, or the heap when it is nil.
func Convert(sourceFile *ast.SourceFile, nodes *Arena) (program *Node, comments []*Node, convertError error) {
	return convert(sourceFile, nil, nodes)
}

func convert(
	sourceFile *ast.SourceFile,
	typeCastCommentEnds []int,
	nodes *Arena,
) (program *Node, comments []*Node, convertError error) {
	if diagnostics := sourceFile.Diagnostics(); len(diagnostics) > 0 {
		return nil, nil, fmt.Errorf("parse error at %d: %s", diagnostics[0].Pos(), diagnostics[0].String())
	}
	converter := &Converter{
		sourceFile:          sourceFile,
		text:                sourceFile.Text(),
		typeCastCommentEnds: typeCastCommentEnds,
		nodes:               nodes,
	}
	converter.scan = scanner.NewScanner()
	converter.scan.SetText(converter.text)

	defer func() {
		if recovered := recover(); recovered != nil {
			program, comments, convertError = nil, nil, fmt.Errorf("converting to ESTree: %v", recovered)
		}
	}()

	program = converter.converter(sourceFile.AsNode(), nil, false)
	// Upstream throws a syntax error as it meets it, before any node it cannot convert later in the file,
	// so the syntax error is the answer when both happen.
	if converter.invalid != nil {
		return nil, nil, converter.invalid
	}
	if converter.unsupported != nil {
		return nil, nil, converter.unsupported
	}
	comments = collectComments(sourceFile)
	return program, comments, nil
}

// token is one scanned token: what findNextToken and getFirstToken return in upstream.
type token struct {
	kind  ast.Kind
	start int
	end   int
}

// noNodes is an empty node list made a property value once. A Nodes{} given to createNode is boxed into its
// value on every node that has one, which was 1.5M of the conversion's allocations on ahra for identifiers'
// decorators alone (#fyw36kf). Sharing one is safe: nothing can be stored in an empty list, and appending to
// it moves the result to new memory.
var noNodes Value = Nodes{}

// fixParentLocation is upstream's fixParentLocation.
func fixParentLocation(result *Node, childRange [2]int) {
	if childRange[0] < result.Range[0] {
		result.Range[0] = childRange[0]
	}
	if childRange[1] > result.Range[1] {
		result.Range[1] = childRange[1]
	}
}

// modifiers is getModifiers(node): the modifier keywords, without decorators.
func modifiers(node *ast.Node) []*ast.Node {
	if node == nil || !ast.CanHaveModifiers(node) {
		return nil
	}
	list := node.Modifiers()
	if list == nil {
		return nil
	}
	var result []*ast.Node
	for _, modifier := range list.Nodes {
		if modifier.Kind == ast.KindDecorator || modifier.Flags&ast.NodeFlagsReparsed != 0 {
			continue
		}
		result = append(result, modifier)
	}
	return result
}

// decorators is getDecorators(node).
func decorators(node *ast.Node) []*ast.Node {
	if node == nil || !ast.CanHaveDecorators(node) {
		return nil
	}
	list := node.Modifiers()
	if list == nil {
		return nil
	}
	var result []*ast.Node
	for _, modifier := range list.Nodes {
		if modifier.Kind == ast.KindDecorator {
			result = append(result, modifier)
		}
	}
	return result
}

// hasModifier is upstream's hasModifier.
func hasModifier(kind ast.Kind, node *ast.Node) bool {
	for _, modifier := range modifiers(node) {
		if modifier.Kind == kind {
			return true
		}
	}
	return false
}

// getTsNodeAccessibility is upstream's getTsNodeAccessibility. Upstream returns undefined when there
// is none, so nil here, which a Node stores as a present property with no value.
func getTsNodeAccessibility(node *ast.Node) Value {
	for _, modifier := range modifiers(node) {
		switch modifier.Kind {
		case ast.KindPublicKeyword:
			return String("public")
		case ast.KindProtectedKeyword:
			return String("protected")
		case ast.KindPrivateKeyword:
			return String("private")
		}
	}
	return nil
}

// getDeclarationKind is upstream's getDeclarationKind.
func getDeclarationKind(node *ast.Node) string {
	switch {
	case node.Flags&ast.NodeFlagsLet != 0:
		return "let"
	case node.Flags&ast.NodeFlagsAwaitUsing == ast.NodeFlagsAwaitUsing:
		return "await using"
	case node.Flags&ast.NodeFlagsConst != 0:
		return "const"
	case node.Flags&ast.NodeFlagsUsing != 0:
		return "using"
	}
	return "var"
}

func isComputedProperty(node *ast.Node) bool {
	return node != nil && node.Kind == ast.KindComputedPropertyName
}

func isOptional(node *ast.Node) bool {
	return node.QuestionToken() != nil
}

// canContainDirective is upstream's canContainDirective.
func canContainDirective(node *ast.Node) bool {
	if node.Kind == ast.KindBlock {
		switch node.Parent.Kind {
		case ast.KindConstructor, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindArrowFunction,
			ast.KindFunctionExpression, ast.KindFunctionDeclaration, ast.KindMethodDeclaration:
			return true
		default:
			return false
		}
	}
	return true
}

// isValidAssignmentTarget is upstream's isValidAssignmentTarget, node-utils.js: an identifier, or a member
// access that is not an optional chain, under any parentheses, type assertions and non-null assertions.
func isValidAssignmentTarget(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindIdentifier:
		return true
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		return node.Flags&ast.NodeFlagsOptionalChain == 0
	case ast.KindParenthesizedExpression, ast.KindTypeAssertionExpression, ast.KindAsExpression,
		ast.KindSatisfiesExpression, ast.KindExpressionWithTypeArguments, ast.KindNonNullExpression:
		return isValidAssignmentTarget(node.Expression())
	}
	return false
}

// getNamespaceModifiers is upstream's getNamespaceModifiers: nested namespaces use the topmost
// namespace's modifiers.
func getNamespaceModifiers(node *ast.Node) []*ast.Node {
	nodeModifiers := modifiers(node)
	moduleDeclaration := node
	for len(nodeModifiers) == 0 &&
		moduleDeclaration.Parent != nil &&
		moduleDeclaration.Parent.Kind == ast.KindModuleDeclaration {
		if parentModifiers := modifiers(moduleDeclaration.Parent); len(parentModifiers) > 0 {
			nodeModifiers = parentModifiers
		}
		moduleDeclaration = moduleDeclaration.Parent
	}
	return nodeModifiers
}

// isThisInTypeQuery is upstream's isThisInTypeQuery.
func isThisInTypeQuery(node *ast.Node) bool {
	if node.Kind != ast.KindIdentifier || scanner.IdentifierToKeywordKind(node.AsIdentifier()) != ast.KindThisKeyword {
		return false
	}
	current := node
	for current.Parent.Kind == ast.KindQualifiedName && current.Parent.AsQualifiedName().Left == current {
		current = current.Parent
	}
	return current.Parent.Kind == ast.KindTypeQuery
}

// keywordTypeNames are the keyword types upstream converts to `TS${SyntaxKind[kind]}`.
var keywordTypeNames = map[ast.Kind]string{
	ast.KindAnyKeyword:       "TSAnyKeyword",
	ast.KindBigIntKeyword:    "TSBigIntKeyword",
	ast.KindBooleanKeyword:   "TSBooleanKeyword",
	ast.KindNeverKeyword:     "TSNeverKeyword",
	ast.KindNumberKeyword:    "TSNumberKeyword",
	ast.KindObjectKeyword:    "TSObjectKeyword",
	ast.KindStringKeyword:    "TSStringKeyword",
	ast.KindSymbolKeyword:    "TSSymbolKeyword",
	ast.KindUnknownKeyword:   "TSUnknownKeyword",
	ast.KindVoidKeyword:      "TSVoidKeyword",
	ast.KindUndefinedKeyword: "TSUndefinedKeyword",
	ast.KindIntrinsicKeyword: "TSIntrinsicKeyword",
}

// isValidEscape is upstream's #isValidEscape.
func isValidEscape(text string) bool {
	if !strings.Contains(text, `\x`) && !strings.Contains(text, `\u`) {
		return true
	}
	for index := 0; index+1 < len(text); index++ {
		if text[index] != '\\' {
			continue
		}
		switch text[index+1] {
		case 'u':
			rest := text[index+2:]
			if !strings.HasPrefix(rest, "{") && !(len(rest) >= 4 && isHexDigits(rest[:4])) {
				return false
			}
		case 'x':
			rest := text[index+2:]
			if !(len(rest) >= 2 && isHexDigits(rest[:2])) {
				return false
			}
		}
	}
	return true
}

func isHexDigits(text string) bool {
	for index := 0; index < len(text); index++ {
		character := text[index]
		if !(character >= '0' && character <= '9' ||
			character >= 'a' && character <= 'f' ||
			character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

// numberValue is JavaScript's Number(text) for a numeric literal's source.
func numberValue(raw string) float64 {
	text := strings.ReplaceAll(raw, "_", "")
	lower := strings.ToLower(text)
	var base int
	switch {
	case strings.HasPrefix(lower, "0x"):
		base = 16
	case strings.HasPrefix(lower, "0o"):
		base = 8
	case strings.HasPrefix(lower, "0b"):
		base = 2
	}
	if base != 0 {
		value, parseError := strconv.ParseUint(text[2:], base, 64)
		if parseError != nil {
			return math.NaN()
		}
		return float64(value)
	}
	value, parseError := strconv.ParseFloat(text, 64)
	if parseError != nil {
		return math.NaN()
	}
	return value
}

// bigintString is String(BigInt(text)): the decimal digits of a bigint literal's value.
func bigintString(text string) string {
	lower := strings.ToLower(text)
	base := 10
	digits := text
	switch {
	case strings.HasPrefix(lower, "0x"):
		base, digits = 16, text[2:]
	case strings.HasPrefix(lower, "0o"):
		base, digits = 8, text[2:]
	case strings.HasPrefix(lower, "0b"):
		base, digits = 2, text[2:]
	}
	value, ok := new(big.Int).SetString(digits, base)
	if !ok {
		return text
	}
	return value.String()
}
