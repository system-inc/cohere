package typescript

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	shimcore "github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// UnifiedSignatures reports two overloads that could be written as one.
//
//	valid:   function f(x: number): void; function f(x: string, y: number): void;
//	invalid: function f(x: number): void; function f(x: string): void;      -> f(x: number | string)
//	invalid: function f(): void; function f(x: number): void;               -> f(x?: number)
//	invalid: function f(x: number): void; function f(x: number): void;      -> identical
//
// Overloads are grouped per scope and per key, then every pair within a group is compared. A pair
// unifies when it agrees on return type, type parameters, and outer-type-parameter usage, and then
// differs in at most one place: one parameter's type (report a union), or one trailing parameter
// that could be optional or rest.
//
// # The rule compares types as source text, not through the checker
//
// Upstream's `typesAreEqual` is `sourceCode.getText(a) === sourceCode.getText(b)`, and the rule
// makes zero `getTypeAtLocation` calls. That is a deliberate choice upstream, not an oversight: it
// is why `string` and `String` are different here, and why the reported union is spelled with the
// author's own words rather than the checker's normalisation. Ported as-is, which means this rule
// declares no type checker and costs nothing on the type-graph side.
//
// # Our parser answers the parameter questions differently, and more simply
//
// Every property upstream reads off TSESTree was measured against our parser before this file was
// written, because the parameter shapes are exactly where the two trees diverge:
//
//	upstream (TSESTree)                    ours (measured)
//	p.type === RestElement                 DotDotDotToken != nil
//	p.optional                             QuestionToken != nil
//	p.typeAnnotation.typeAnnotation        Type
//	TSParameterProperty wrapping p         the SAME node, carrying a modifier
//	ExportNamedDeclaration wrapping f      the SAME node, carrying an export modifier
//
// The last two are why this file is shorter than upstream's without deciding anything differently.
// TSESTree wraps a parameter property in a `TSParameterProperty` and an exported declaration in an
// `ExportNamedDeclaration`, so upstream carries `isTSParameterProperty` unwrapping at five call
// sites and a `getExportingNode`/`containingNode` pair through the whole overload-collection path.
// Our parser puts both on the node as modifiers, so the unwrapping has nothing to unwrap. Measured,
// not assumed: `export declare function f` reports parent `SourceFile` with modifiers
// `[ExportKeyword DeclareKeyword]`, and `constructor(private a?: string)` is one parameter node with
// one modifier and a question token.
//
// Getters and setters fall out the same way. Upstream filters them with `isGetterOrSetter` because
// TSESTree gives them the same node type as a method with a `kind` discriminator; our parser gives
// them `KindGetAccessor` and `KindSetAccessor`, distinct kinds, so they never enter the listeners
// this rule installs and there is no filter to write.
//
// # `this` parameters
//
// Two separate exemptions, both upstream's. A pair where one signature declares `this` and the other
// does not cannot unify at all. And a signature taking `this: void` is exempt outright, because
// `this: void` is how a callback declares it must not be called as a method, which is a distinction
// unification would erase. Our parser gives `this` as an ordinary parameter named `this`, so both
// tests read the name.
//
// # Cost
//
// One map per scope, holding the signatures seen in it, and a pairwise comparison within each key's
// group. A group of n signatures costs n(n-1)/2 comparisons, and n is the number of overloads
// sharing one name, which is small in practice.
var UnifiedSignatures = rule.Rule[UnifiedSignaturesOptions]{
	Name: "@typescript-eslint/unified-signatures",

	Run: func(ruleContext rule.Context, options rule.Given[UnifiedSignaturesOptions]) rule.Listeners {
		if ruleContext.SourceFile == nil {
			return nil
		}
		// The value type, because that is what `DecodeOptionsInto` hands over. This line asserted a
		// pointer once, the comma-ok form swallowed the mismatch, and both options read false for
		// every configuration; `TestUnifiedSignaturesOptionsArriveThroughTheDecoder` holds it.
		settings, _ := options.Read()

		// One listener that walks the file itself, rather than a listener per scope kind with an
		// exit hook. Upstream pairs each `X` with an `X:exit` and checks a scope when it closes;
		// there is no exit listener here and the walk is pre-order, so the scope tree is walked
		// directly. That is also the clearer shape for this rule, because a scope's overloads are
		// exactly the signatures among its own members and nesting is containment.
		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				checker := &unifiedSignaturesChecker{ruleContext: ruleContext, settings: settings}
				checker.checkScope(node, nil)
			},
		}
	},
}

// UnifiedSignaturesOptions is upstream's option object. Both fields default to false, which are Go
// zero values, so the generic decoder serves this rule and no hand-written decode is needed.
type UnifiedSignaturesOptions struct {
	// IgnoreDifferentlyNamedParameters treats two parameters at the same index as different when
	// their names differ, even if their types match.
	IgnoreDifferentlyNamedParameters bool `json:"ignoreDifferentlyNamedParameters"`

	// IgnoreOverloadsWithDifferentJsDoc treats two overloads carrying different block comments as
	// different, on the reading that the prose documents a distinction the types do not carry.
	IgnoreOverloadsWithDifferentJsDoc bool `json:"ignoreOverloadsWithDifferentJSDoc"`
}

// unifiedSignaturesScopeMembers returns the declarations directly inside a scope.
//
// The four member-bearing kinds each expose their own list, and a source file or module block
// exposes statements. Every accessor here is one our parser panics on when called against the wrong
// kind, so the switch is exhaustive by kind rather than by a shared interface.
func unifiedSignaturesScopeMembers(scope *ast.Node) []*ast.Node {
	switch scope.Kind {
	case ast.KindSourceFile:
		if statements := scope.AsSourceFile().Statements; statements != nil {
			return statements.Nodes
		}
	case ast.KindModuleBlock:
		if statements := scope.AsModuleBlock().Statements; statements != nil {
			return statements.Nodes
		}
	case ast.KindClassDeclaration:
		if members := scope.AsClassDeclaration().Members; members != nil {
			return members.Nodes
		}
	case ast.KindClassExpression:
		if members := scope.AsClassExpression().Members; members != nil {
			return members.Nodes
		}
	case ast.KindInterfaceDeclaration:
		if members := scope.AsInterfaceDeclaration().Members; members != nil {
			return members.Nodes
		}
	case ast.KindTypeLiteral:
		if members := scope.AsTypeLiteralNode().Members; members != nil {
			return members.Nodes
		}
	}
	return nil
}

// unifiedSignaturesIsOverload is upstream's collection filter.
//
// A method or function with a body is an implementation rather than an overload signature, and
// upstream skips it: `if (!node.value.body ...)`. Getters and setters need no filter here, unlike
// upstream, because our parser gives them their own kinds and they never reach this switch.
func unifiedSignaturesIsOverload(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindCallSignature, ast.KindConstructSignature, ast.KindMethodSignature:
		return true
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().Body == nil
	case ast.KindFunctionDeclaration:
		return node.AsFunctionDeclaration().Body == nil
	case ast.KindConstructor:
		// A constructor overload in a class. Upstream reaches these through `MethodDefinition`,
		// whose `kind` is `'constructor'`; our parser gives them their own kind, so they need naming
		// here or a `declare class C { constructor(); constructor(x: number); }` reports nothing.
		return node.AsConstructorDeclaration().Body == nil
	}
	return false
}

// unifiedSignaturesNestedScopes returns the scopes contained directly within one scope.
//
// Only the containers this rule groups within are followed. Deeper nesting is reached by recursion
// through them, and a function body is deliberately not a scope here: upstream does not collect
// overloads declared inside a function body against ones outside it, because they are not overloads
// of each other.
func unifiedSignaturesNestedScopes(scope *ast.Node) []*ast.Node {
	var found []*ast.Node
	visit := func(node *ast.Node) bool {
		switch node.Kind {
		case ast.KindClassDeclaration, ast.KindClassExpression, ast.KindInterfaceDeclaration,
			ast.KindTypeLiteral, ast.KindModuleBlock:
			found = append(found, node)
			// Not descended into here: the recursion in checkScope handles what is inside it, so
			// stopping keeps each scope's members attributed to exactly one scope.
			return false
		}
		return true
	}
	unifiedSignaturesForEachDescendant(scope, visit)
	return found
}

// unifiedSignaturesForEachDescendant walks children, letting the visitor stop a branch by returning
// false. `ForEachChild` returns a value to stop the whole walk, so the stop is expressed by simply
// not recursing rather than by propagating.
func unifiedSignaturesForEachDescendant(node *ast.Node, visit func(*ast.Node) bool) {
	node.ForEachChild(func(child *ast.Node) bool {
		if visit(child) {
			unifiedSignaturesForEachDescendant(child, visit)
		}
		return false
	})
}

// unifiedSignaturesOverloadKey is upstream's `getOverloadKey`: two signatures are candidates for
// unification only when this string matches.
//
// The two leading digits encode computed-ness and static-ness, so a computed member never groups
// with a plain one of the same name and a static method never groups with an instance method. A
// call signature and a construct signature get fixed keys of their own.
//
// The name is read as source text for anything that is not a plain identifier, which is upstream's
// `key.raw`. That keeps `'f'` and `f` in separate groups, matching upstream, and it is why the
// corpus's quoted-name case reports the way it does.
func unifiedSignaturesOverloadKey(ruleContext rule.Context, node *ast.Node) string {
	switch node.Kind {
	case ast.KindConstructSignature:
		return "11constructor"
	case ast.KindCallSignature:
		return "11()"
	case ast.KindConstructor:
		// Upstream keys a class constructor through the identifier branch, since TSESTree gives it a
		// key node named `constructor`. Matching that spelling keeps a constructor grouped with
		// other constructors and apart from a method that happens to be named `constructor`, which
		// our parser would otherwise let collide.
		return "11identifier_constructor"
	}

	// The computed digit is upstream's and is kept, but in our tree it is subsumed and a mutant
	// flipping it survives. Upstream's non-computed info is `identifier_<text>`, while a computed
	// member's info is the source text of its `ComputedPropertyName`, which always begins with `[`.
	// Those two strings can never be equal, so the members already land in different groups before
	// the digit is consulted. Verified rather than reasoned: with the digit forced to a constant,
	// `interface I { ['a'](x: number): void; a(x: string): void }` still reports nothing, and the
	// two-computed control still reports.
	//
	// Kept because the argument rests on how our parser spells a computed key, which is a property
	// of the parser rather than of this rule.
	computed := "1"
	staticness := "1"
	name := node.Name()
	if name != nil && name.Kind == ast.KindComputedPropertyName {
		computed = "0"
	}
	if modifiers := node.Modifiers(); modifiers != nil {
		for _, modifier := range modifiers.Nodes {
			if modifier.Kind == ast.KindStaticKeyword {
				staticness = "0"
			}
		}
	}

	info := ""
	switch {
	case name == nil:
		info = "identifier_"
	case name.Kind == ast.KindIdentifier:
		info = "identifier_" + name.AsIdentifier().Text
	case name.Kind == ast.KindPrivateIdentifier:
		info = "private_identifier_" + name.AsPrivateIdentifier().Text
	default:
		info = unifiedSignaturesNodeText(ruleContext, name)
	}
	return computed + staticness + info
}

// unifiedSignaturesNodeText is upstream's `sourceCode.getText(node)`.
//
// `rule.TokenRange` trims the leading trivia our parser includes in a node's start, so the text is
// the node itself rather than the node plus whatever comments and whitespace preceded it. That
// matters here because this text is compared for equality: without the trim, two identical types
// would differ whenever one of them happened to follow a comment.
func unifiedSignaturesNodeText(ruleContext rule.Context, node *ast.Node) string {
	if node == nil {
		return ""
	}
	nodeRange := rule.TokenRange(ruleContext.SourceFile, node)
	return ruleContext.SourceFile.Text()[nodeRange.Pos():nodeRange.End()]
}

// unifiedSignaturesTypeParameterNames adds a scope's own type parameter names to the set inherited
// from its enclosing scopes.
//
// Returns a fresh map rather than mutating the one passed in, so sibling scopes do not see each
// other's type parameters through a shared map.
func unifiedSignaturesTypeParameterNames(scope *ast.Node, inherited map[string]bool) map[string]bool {
	var list *ast.NodeList
	switch scope.Kind {
	case ast.KindClassDeclaration:
		list = scope.AsClassDeclaration().TypeParameters
	case ast.KindClassExpression:
		list = scope.AsClassExpression().TypeParameters
	case ast.KindInterfaceDeclaration:
		list = scope.AsInterfaceDeclaration().TypeParameters
	}
	if list == nil || len(list.Nodes) == 0 {
		return inherited
	}

	combined := make(map[string]bool, len(inherited)+len(list.Nodes))
	for name := range inherited {
		combined[name] = true
	}
	for _, parameter := range list.Nodes {
		if name := parameter.Name(); name != nil && name.Kind == ast.KindIdentifier {
			combined[name.AsIdentifier().Text] = true
		}
	}
	return combined
}

// unifiedSignaturesParameters returns a signature's parameter list.
//
// Every kind this rule collects carries its own `Parameters` field, and reading the wrong accessor
// panics, so the switch is by kind. A signature with no parameter list yields nil, which the callers
// treat as an empty list.
func unifiedSignaturesParameters(node *ast.Node) []*ast.Node {
	var list *ast.NodeList
	switch node.Kind {
	case ast.KindCallSignature:
		list = node.AsCallSignatureDeclaration().Parameters
	case ast.KindConstructSignature:
		list = node.AsConstructSignatureDeclaration().Parameters
	case ast.KindMethodSignature:
		list = node.AsMethodSignatureDeclaration().Parameters
	case ast.KindMethodDeclaration:
		list = node.AsMethodDeclaration().Parameters
	case ast.KindFunctionDeclaration:
		list = node.AsFunctionDeclaration().Parameters
	case ast.KindConstructor:
		list = node.AsConstructorDeclaration().Parameters
	}
	if list == nil {
		return nil
	}
	return list.Nodes
}

// unifiedSignaturesReturnType returns the declared return type, or nil when there is none.
func unifiedSignaturesReturnType(node *ast.Node) *ast.Node {
	switch node.Kind {
	case ast.KindCallSignature:
		return node.AsCallSignatureDeclaration().Type
	case ast.KindConstructSignature:
		return node.AsConstructSignatureDeclaration().Type
	case ast.KindMethodSignature:
		return node.AsMethodSignatureDeclaration().Type
	case ast.KindMethodDeclaration:
		return node.AsMethodDeclaration().Type
	case ast.KindFunctionDeclaration:
		return node.AsFunctionDeclaration().Type
	case ast.KindConstructor:
		return node.AsConstructorDeclaration().Type
	}
	return nil
}

// unifiedSignaturesSignatureTypeParameters returns a signature's own type parameters, which are the
// ones written on the signature rather than inherited from an enclosing declaration.
func unifiedSignaturesSignatureTypeParameters(node *ast.Node) []*ast.Node {
	var list *ast.NodeList
	switch node.Kind {
	case ast.KindCallSignature:
		list = node.AsCallSignatureDeclaration().TypeParameters
	case ast.KindConstructSignature:
		list = node.AsConstructSignatureDeclaration().TypeParameters
	case ast.KindMethodSignature:
		list = node.AsMethodSignatureDeclaration().TypeParameters
	case ast.KindMethodDeclaration:
		list = node.AsMethodDeclaration().TypeParameters
	case ast.KindFunctionDeclaration:
		list = node.AsFunctionDeclaration().TypeParameters
	case ast.KindConstructor:
		list = node.AsConstructorDeclaration().TypeParameters
	}
	if list == nil {
		return nil
	}
	return list.Nodes
}

// unifiedSignaturesParameterType is upstream's `getParameterTypeAnnotation`.
//
// Upstream needs a branch here to reach through a `TSParameterProperty` wrapper. Ours does not: a
// parameter property is the same parameter node carrying a modifier, as measured, so the annotation
// is read the same way for both.
func unifiedSignaturesParameterType(parameter *ast.Node) *ast.Node {
	if parameter == nil || parameter.Kind != ast.KindParameter {
		return nil
	}
	return parameter.AsParameterDeclaration().Type
}

// unifiedSignaturesIsRest is upstream's `p.type === RestElement`, which our parser answers with the
// presence of the spread token on the parameter itself.
func unifiedSignaturesIsRest(parameter *ast.Node) bool {
	return parameter != nil && parameter.Kind == ast.KindParameter &&
		parameter.AsParameterDeclaration().DotDotDotToken != nil
}

// unifiedSignaturesIsOptional is upstream's `p.optional`, which our parser answers with the question
// token. Upstream reads a different property for a parameter property; ours does not need to.
func unifiedSignaturesIsOptional(parameter *ast.Node) bool {
	return parameter != nil && parameter.Kind == ast.KindParameter &&
		parameter.AsParameterDeclaration().QuestionToken != nil
}

// unifiedSignaturesStaticParameterName is upstream's `getStaticParameterName`: the name of a
// parameter that has a static name, which a destructured parameter does not.
func unifiedSignaturesStaticParameterName(parameter *ast.Node) (string, bool) {
	if parameter == nil || parameter.Kind != ast.KindParameter {
		return "", false
	}
	name := parameter.AsParameterDeclaration().Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return "", false
	}
	return name.AsIdentifier().Text, true
}

// unifiedSignaturesUsesTypeParameter is upstream's `signatureUsesTypeParameter`: does any parameter's
// type mention a type parameter declared by an enclosing scope?
//
// Upstream walks only `typeAnnotation` and `elementType`, so it sees `T` and `T[]` but not `Foo<T>`.
// This walks every child instead, which is a strictly wider net over the same question, and the
// corpus agrees with the wider reading on all 112 rows.
func unifiedSignaturesUsesTypeParameter(signature *ast.Node, outer map[string]bool) bool {
	if len(outer) == 0 {
		return false
	}
	for _, parameter := range unifiedSignaturesParameters(signature) {
		parameterType := unifiedSignaturesParameterType(parameter)
		if parameterType == nil {
			continue
		}
		if unifiedSignaturesTypeMentions(parameterType, outer) {
			return true
		}
	}
	return false
}

// unifiedSignaturesTypeMentions reports whether a type node names one of the given type parameters.
func unifiedSignaturesTypeMentions(node *ast.Node, outer map[string]bool) bool {
	if node == nil {
		return false
	}
	if node.Kind == ast.KindTypeReference {
		name := node.AsTypeReferenceNode().TypeName
		if name != nil && name.Kind == ast.KindIdentifier && outer[name.AsIdentifier().Text] {
			return true
		}
	}
	found := false
	node.ForEachChild(func(child *ast.Node) bool {
		if unifiedSignaturesTypeMentions(child, outer) {
			found = true
			return true
		}
		return false
	})
	return found
}

// unifiedSignaturesAt indexes a parameter list without panicking past its end.
func unifiedSignaturesAt(parameters []*ast.Node, index int) *ast.Node {
	if index < 0 || index >= len(parameters) {
		return nil
	}
	return parameters[index]
}

// unifiedSignaturesIsThisParameter is upstream's `isThisParam`.
//
// TSESTree and our parser agree here: `this` is an ordinary parameter whose name is the identifier
// `this`, measured rather than assumed.
func unifiedSignaturesIsThisParameter(parameter *ast.Node) bool {
	name, ok := unifiedSignaturesStaticParameterName(parameter)
	return ok && name == "this"
}

// unifiedSignaturesIsThisVoid is upstream's `isThisVoidParam`: a `this: void` declaration, which
// says the function must not be called as a method.
func unifiedSignaturesIsThisVoid(parameter *ast.Node) bool {
	if !unifiedSignaturesIsThisParameter(parameter) {
		return false
	}
	parameterType := unifiedSignaturesParameterType(parameter)
	return parameterType != nil && parameterType.Kind == ast.KindVoidKeyword
}

// unifiedSignaturesMayBeMissing is upstream's `parameterMayBeMissing`: a parameter a caller can
// leave out, which is an optional or a rest.
func unifiedSignaturesMayBeMissing(parameter *ast.Node) bool {
	return unifiedSignaturesIsRest(parameter) || unifiedSignaturesIsOptional(parameter)
}

// unifiedSignaturesHaveEqualSigils is upstream's `parametersHaveEqualSigils`: two parameters agree
// on rest-ness and on optional-ness. One of each cannot be unified with the other.
func unifiedSignaturesHaveEqualSigils(left *ast.Node, right *ast.Node) bool {
	return unifiedSignaturesIsRest(left) == unifiedSignaturesIsRest(right) &&
		unifiedSignaturesIsOptional(left) == unifiedSignaturesIsOptional(right)
}

// unifiedSignaturesFailureStart is upstream's `failureStringStart`.
//
// With exactly two overloads in the group there is no ambiguity about which other one is meant, so
// the message says "These overloads". With three or more it names the other one's line, which is why
// the other signature has to be threaded this far down.
func unifiedSignaturesFailureStart(ruleContext rule.Context, onlyTwo bool, other *ast.Node) string {
	if onlyTwo {
		return "These overloads can be combined into one signature"
	}
	line := scanner.GetECMALineOfPosition(ruleContext.SourceFile, rule.TokenRange(ruleContext.SourceFile, other).Pos())
	return fmt.Sprintf("This overload and the one on line %d can be combined into one signature",
		line+1)
}

// unifiedSignaturesUnionMembers flattens a union type into its members, leaving anything else as a
// single member.
func unifiedSignaturesUnionMembers(node *ast.Node) []*ast.Node {
	unparenthesized := unifiedSignaturesWithoutParentheses(node)
	if unparenthesized == nil {
		return nil
	}
	if unparenthesized.Kind != ast.KindUnionType {
		return []*ast.Node{unparenthesized}
	}
	types := unparenthesized.AsUnionTypeNode().Types
	if types == nil {
		return []*ast.Node{unparenthesized}
	}
	var members []*ast.Node
	for _, member := range types.Nodes {
		members = append(members, unifiedSignaturesUnionMembers(member)...)
	}
	return members
}

// unifiedSignaturesValueRange is upstream's `node.value ?? node`, expressed as a range.
//
// TSESTree splits a class method into a `MethodDefinition` holding the key and a `FunctionExpression`
// holding the parameters and return type, and upstream reports the second of those. Our parser has
// one node for both, so the equivalent range starts where the name ends: for
// `f(a: number): void;` upstream's span covers `(a: number): void;`, which the corpus asserts at
// column 4 rather than column 3.
//
// Only a method declaration is unwrapped. A function declaration has no such split upstream and is
// reported whole, which the corpus also asserts, so widening this to every kind would move three
// spans that are currently correct.
func unifiedSignaturesValueRange(ruleContext rule.Context, signature *ast.Node) shimcore.TextRange {
	full := rule.TokenRange(ruleContext.SourceFile, signature)
	if signature.Kind != ast.KindMethodDeclaration {
		return full
	}
	name := signature.Name()
	if name == nil {
		return full
	}
	return full.WithPos(name.End())
}

// unifiedSignaturesWithoutParentheses strips parenthesised type wrappers.
//
// TSESTree has no node for a parenthesised type: the parser folds it away, so upstream never sees
// one. Our parser keeps `KindParenthesizedType`, which changes two answers if left alone. The union
// text would carry the author's parentheses and any comments inside them, so
// `(/* before */ string /* after */)` printed as itself rather than as `string`; and a parenthesised
// union would count as one member instead of being flattened into its parts, so
// `(string | boolean)` against `number` printed `number | (string | boolean)` where upstream prints
// `number | string | boolean`.
//
// Both measured against the installed build over four shapes, including a nested `((string))`, which
// is why this loops rather than unwrapping once.
func unifiedSignaturesWithoutParentheses(node *ast.Node) *ast.Node {
	unwrapped := node
	for unwrapped != nil && unwrapped.Kind == ast.KindParenthesizedType {
		unwrapped = unwrapped.AsParenthesizedTypeNode().Type
	}
	return unwrapped
}
