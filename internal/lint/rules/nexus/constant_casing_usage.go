package nexus

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/jsx"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// The usage-evidence half of consistency-require-constant-casing.
//
// The TypeScript original reads ESLint's scope analysis: it resolves a binding to a variable and
// walks that variable's references. There is no scope graph here, so these answer the same questions
// by scanning the file once and indexing what they find by name.
//
// That is an over-approximation: a name shadowed in an inner function is read as the same binding.
// The direction is deliberate and it is the safe one for this rule, because every one of these
// predicates grants an exemption. Over-approximating grants an exemption slightly too often, which
// costs a report the rule would have made; under-approximating would deny an exemption and demand a
// rename that breaks the code. `Icon` used as a JSX tag inside one function and declared as data in
// another is rare; a component told to become `<icon />` is a build failure.
//
// The scan is per file and shared across every declaration in it, since the alternative is walking
// the file once per constant.

// fileUsageIndex is what one walk of a file learns about how its names are used.
type fileUsageIndex struct {
	// usedAsJsxElementName holds names that appear in element position, or handed to a JSX
	// attribute. JSX takes the casing slot for its own semantics: `<icon />` is the HTML tag and
	// `<Icon />` is the component, so a name in element position must keep its capital.
	usedAsJsxElementName map[string]bool

	// calledLikeAFunction holds names that appear as the callee of a call. This is the shape no
	// initializer check can see: `const createEsLintRule = RuleCreator(...)` has an ordinary call on
	// the right, and only the fact that the result is later invoked proves it holds a function.
	calledLikeAFunction map[string]bool

	// functionNames holds names the file declares as functions, whether by the function keyword or
	// by a const holding a function literal. It answers `export const formatNumber = numberCompact`,
	// where the value is a function only if the thing it aliases is one.
	functionNames map[string]bool

	// functionsReturningAClass holds locally declared functions whose declared return type names a
	// class: `buildRegistry(): EventSourceChannelRegistry` returns an instance and its constant belongs
	// to the instance clause. A factory returning a function needs no entry here, because the
	// constant's own type says so through the checker.
	functionsReturningAClass map[string]bool
}

// usageIndexFor returns the index for the file being linted, computing it at most once per file.
//
// The cache is the shared per-file store, so a second rule wanting the same scan would not pay for
// it twice. Without it this rule would walk the file once per constant declaration in it.
func usageIndexFor(ctx rule.Context) *fileUsageIndex {
	return rule.Cached(ctx.FileCache, "nexus.constantCasingUsage", func() *fileUsageIndex {
		index := &fileUsageIndex{
			usedAsJsxElementName:     map[string]bool{},
			calledLikeAFunction:      map[string]bool{},
			functionNames:            map[string]bool{},
			functionsReturningAClass: map[string]bool{},
		}
		if ctx.SourceFile != nil {
			index.collect(ctx.SourceFile.AsNode())
		}
		return index
	})
}

// collect walks the whole file once, recording every usage shape the predicates below ask about.
func (index *fileUsageIndex) collect(node *ast.Node) {
	if node == nil {
		return
	}

	switch node.Kind {
	case ast.KindJsxOpeningElement, ast.KindJsxSelfClosingElement:
		tagName, _ := jsx.ElementParts(node)
		index.recordJsxTag(tagName)
	case ast.KindJsxClosingElement:
		// A closing tag is the one element form `jsx.ElementParts` does not answer, because a rule
		// asking what an element is reads the opening form and a closing tag carries no attributes.
		// This index wants every mention of the name, including the closing one.
		index.recordJsxTag(node.AsJsxClosingElement().TagName)

	case ast.KindJsxAttribute:
		// Handed to something else to render: `icon={Icon}`. The binding holds a component rather
		// than a value, and React's convention capitalizes a component wherever it travels, because
		// the receiver will put it in element position where the casing becomes load bearing again.
		attribute := node.AsJsxAttribute()
		if attribute.Initializer != nil && attribute.Initializer.Kind == ast.KindJsxExpression {
			inner := unwrapAssertions(attribute.Initializer.AsJsxExpression().Expression)
			if inner != nil && inner.Kind == ast.KindIdentifier {
				index.usedAsJsxElementName[inner.Text()] = true
			}
		}

	case ast.KindCallExpression:
		if callee := unwrapAssertions(node.AsCallExpression().Expression); callee != nil &&
			callee.Kind == ast.KindIdentifier {
			index.calledLikeAFunction[callee.Text()] = true
		}

	case ast.KindFunctionDeclaration:
		declaration := node.AsFunctionDeclaration()
		if name := declaration.Name(); name != nil {
			index.functionNames[name.Text()] = true
			index.recordClassReturn(name.Text(), declaration.Type)
		}

	case ast.KindVariableDeclaration:
		declaration := node.AsVariableDeclaration()
		name := declaration.Name()
		if name != nil && name.Kind == ast.KindIdentifier && isFunctionNode(declaration.Initializer) {
			index.functionNames[name.Text()] = true
		}
	}

	node.ForEachChild(func(child *ast.Node) bool {
		index.collect(child)
		return false
	})
}

// recordJsxTag records a tag written in element position. A qualified tag like `<Menu.Item />` is
// rooted at the binding, so the root is what gets recorded.
func (index *fileUsageIndex) recordJsxTag(tagName *ast.Node) {
	for tagName != nil {
		switch tagName.Kind {
		case ast.KindIdentifier:
			index.usedAsJsxElementName[tagName.Text()] = true
			return
		case ast.KindPropertyAccessExpression:
			tagName = tagName.AsPropertyAccessExpression().Expression
		default:
			return
		}
	}
}

// recordClassReturn records a locally declared function whose declared return type is class-shaped,
// excluding the built-in containers. A singleton routed through a function to survive module
// re-evaluation is still a singleton.
func (index *fileUsageIndex) recordClassReturn(name string, returnType *ast.Node) {
	if returnType == nil || returnType.Kind != ast.KindTypeReference {
		return
	}
	reference := returnType.AsTypeReferenceNode()
	if reference.TypeName != nil && reference.TypeName.Kind == ast.KindIdentifier {
		typeName := reference.TypeName.Text()
		if startsUppercase(typeName) && !dataStructureConstructors[typeName] {
			index.functionsReturningAClass[name] = true
		}
	}
}

// isUsedAsJsxElementName reports whether the binding appears in element position anywhere in the
// file. This resolves against real usage rather than trusting the file extension, so a PascalCase
// local in a .tsx file that is never rendered is still reported.
func (index *fileUsageIndex) isUsedAsJsxElementName(name string) bool {
	return index.usedAsJsxElementName[name]
}

// isCalledLikeAFunction reports whether the binding is invoked anywhere in the file. Usage is the
// evidence, so read the usage: only being invoked exempts a name, while a value merely passed around
// stays subject to the casing rule.
func (index *fileUsageIndex) isCalledLikeAFunction(name string) bool {
	return index.calledLikeAFunction[name]
}

// isFunctionValuedIdentifier reports whether an identifier initializer names a function this file
// declares.
//
// Split from the syntactic check because it needs names from elsewhere in the file: `export const
// formatNumber = numberCompact` is only a function alias if `numberCompact` is itself a function.
//
// An imported binding is not read here. This used to guess from its casing, a camelCase import read
// as a function and a PascalCase one not, because nothing else could follow an import; the
// constant's own type now does, in `holdsAFunctionType`, and it is right where the guess was wrong:
// a camelCase import of a configuration object is data.
func (index *fileUsageIndex) isFunctionValuedIdentifier(initializer *ast.Node) bool {
	expression := unwrapAssertions(initializer)
	if expression == nil || expression.Kind != ast.KindIdentifier {
		return false
	}
	return index.functionNames[expression.Text()]
}

// isResolverReturningAClass reports the singleton-through-a-resolver shape:
// `resolveAgentManagerSingleton()`, whose declared return type names a class.
func (index *fileUsageIndex) isResolverReturningAClass(initializer *ast.Node) bool {
	name := calledFactoryName(initializer)
	return name != "" && index.functionsReturningAClass[name]
}

// calledFactoryName reads the bare identifier a call is made through, or "" when the initializer is
// not a call through a plain name.
func calledFactoryName(initializer *ast.Node) string {
	expression := unwrapAssertions(initializer)
	if expression == nil || expression.Kind != ast.KindCallExpression {
		return ""
	}
	callee := unwrapAssertions(expression.AsCallExpression().Expression)
	if callee == nil || callee.Kind != ast.KindIdentifier {
		return ""
	}
	return callee.Text()
}
