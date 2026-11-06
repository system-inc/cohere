package tailwind

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// liveMatcher is one matcher alive in a walk that nests: one of the selector's own, or one an
// anonymousFunctionReturn matcher made, upstream's closures in getESMatcherFunctions.
type liveMatcher struct {
	kind liveMatcherKind
	// matcher is a selector's own matcher, for liveMatcherSelector.
	matcher *compiledMatcher
	// inner is what a made matcher hands on once it finds the returned value.
	inner []compiledMatcher
}

// evaluate is one matcher's answer about one node, and the matchers it makes when it nests.
func (matcher liveMatcher) evaluate(node esNode) (matcherResult, []liveMatcher) {
	switch matcher.kind {
	case liveMatcherArrowBody:
		// The returned value is the body of an arrow with no braces.
		if node.asFunction || node.asQuasi {
			return matcherNoMatch, nil
		}
		parent := esParent(node.node)
		if parent != nil && parent.Kind == ast.KindArrowFunction && parent.Body().Kind != ast.KindBlock &&
			skipOuter(parent.Body()) == node.node {
			return matcherNested, selectorMatchers(matcher.inner)
		}
		return matcherNoMatch, nil

	case liveMatcherReturnStatement:
		if node.asFunction {
			return matcherUncrossable, nil
		}
		if node.asQuasi {
			return matcherNoMatch, nil
		}
		switch node.node.Kind {
		case ast.KindArrowFunction, ast.KindFunctionExpression, ast.KindFunctionDeclaration,
			ast.KindVariableDeclaration:
			return matcherUncrossable, nil
		case ast.KindCallExpression:
			if isUncrossable(node.node) {
				return matcherUncrossable, nil
			}
		case ast.KindReturnStatement:
			return matcherNested, selectorMatchers(matcher.inner)
		}
		return matcherNoMatch, nil
	}

	selector := matcher.matcher
	if selector.matcherType == MatcherTypeAnonymousFunctionReturn {
		if !node.asFunction && !node.asQuasi &&
			(node.node.Kind == ast.KindVariableDeclaration ||
				node.node.Kind == ast.KindCallExpression && isUncrossable(node.node)) {
			return matcherUncrossable, nil
		}
		switch {
		case node.asFunction:
			return matcherNested, []liveMatcher{{kind: liveMatcherReturnStatement, inner: selector.inner}}
		case node.asQuasi:
			return matcherNoMatch, nil
		case node.node.Kind == ast.KindArrowFunction:
			if node.node.Body().Kind != ast.KindBlock {
				return matcherNested, []liveMatcher{{kind: liveMatcherArrowBody, inner: selector.inner}}
			}
			return matcherNested, []liveMatcher{{kind: liveMatcherReturnStatement, inner: selector.inner}}
		case node.node.Kind == ast.KindFunctionExpression && node.node.Name() == nil:
			return matcherNested, []liveMatcher{{kind: liveMatcherReturnStatement, inner: selector.inner}}
		}
		return matcherNoMatch, nil
	}

	// The three matchers that never nest stop at the same four kinds, a method's function among them.
	if node.asFunction || (!node.asQuasi && isUncrossable(node.node)) {
		return matcherUncrossable, nil
	}
	if node.asQuasi {
		switch selector.matcherType {
		case MatcherTypeStrings:
			if stringsMatcherReads(node.node, nil) {
				return matcherMatch, nil
			}
		case MatcherTypeObjectValues:
			if objectValuesMatcherReads(node.node, nil) && selector.matchesPath(objectPath(node.node, true)) {
				return matcherMatch, nil
			}
		}
		return matcherNoMatch, nil
	}
	if isMatch, _ := evaluateMatchers(node.node, 1, []compiledMatcher{*selector}); isMatch {
		return matcherMatch, nil
	}
	return matcherNoMatch, nil
}
