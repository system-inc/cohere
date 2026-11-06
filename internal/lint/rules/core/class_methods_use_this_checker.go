package core

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// classMethodsUseThisChecker carries the walk state that upstream keeps on its stack.
type classMethodsUseThisChecker struct {
	ctx                   rule.Context
	exceptMethods         map[string]bool
	enforceForClassFields bool
	ignoreOverrideMethods bool
	ignoreWithImplements  string

	// stack mirrors upstream's array of booleans: one frame per function-like construct that has
	// its own `this`. An arrow function deliberately pushes no frame; see the rule doc.
	stack []bool
}

// walk recurses the file, opening a frame for each construct that has its own `this`.
func (checker *classMethodsUseThisChecker) walk(node *ast.Node) {
	if node == nil {
		return
	}

	if classMethodsUseThisMarksThisUsed(node) {
		checker.markThisUsed()
	}

	// A property declaration opens a frame of its own, unconditionally, and this is the arm that is
	// easiest to leave out because it is not a function.
	//
	// Upstream spells it `"PropertyDefinition > *.key:exit": pushContext` and `"PropertyDefinition:exit":
	// popContext`, so the frame opens after the key and closes with the member. Its purpose is
	// containment rather than judgment: a class field's initializer is an implicit function with its
	// own `this`, so a `this` written there must not satisfy whatever encloses the class.
	//
	// The corpus case that needs it is `class A { foo () { return class { foo = this }; } }`, where
	// upstream reports the outer method. Without this frame the inner field's `this` bubbles up and
	// marks the outer method as satisfied, and that one finding is lost.
	//
	// The `> *.key:exit` half of upstream's selector is equally load-bearing, and getting it wrong
	// costs a finding in the other direction. The frame opens after the key, so a `this` written in
	// a computed key belongs to whatever encloses the class rather than to the field. That is
	// `class A { foo() { return class { [this.foo] = 1 }; } }`, upstream's valid[21], which is clean
	// precisely because the key's `this` still reaches the outer method. A frame opened at the
	// property instead of after its key swallows it and the outer method reports.
	//
	// Rather than model "after the key", the key is walked outside the frame and the rest inside,
	// which is the same thing expressed as a traversal.
	opensFrame := classMethodsUseThisOpensFrame(node) ||
		(checker.enforceForClassFields && classMethodsUseThisIsClassFieldArrow(node))

	if node.Kind == ast.KindPropertyDeclaration {
		checker.walkPropertyDeclaration(node)
		return
	}
	if opensFrame {
		checker.stack = append(checker.stack, false)
	}

	node.ForEachChild(func(child *ast.Node) bool {
		checker.walk(child)
		return false
	})

	if !opensFrame {
		return
	}

	usedThis := checker.stack[len(checker.stack)-1]
	checker.stack = checker.stack[:len(checker.stack)-1]

	if checker.isIncludedInstanceMember(node) && !usedThis {
		checker.ctx.ReportNode(
			classMethodsUseThisReportAnchor(node),
			classMethodsUseThisMissingThis(classMethodsUseThisNameWithKind(node)),
		)
	}
}

// markThisUsed sets the innermost frame, matching upstream's `markThisUsed`.
//
// Upstream guards with `if (stack.length)`, so a `this` at module scope is simply dropped rather
// than crashing. Same guard here, and it is reachable: a top-level `this` is legal in a script.
func (checker *classMethodsUseThisChecker) markThisUsed() {
	if len(checker.stack) == 0 {
		return
	}
	checker.stack[len(checker.stack)-1] = true
}

// isIncludedInstanceMember answers upstream's `isIncludedInstanceMethod`.
//
// The order of the filters is upstream's and matters: `override` and `implements` are consulted
// before the name, and a computed member returns true before `exceptMethods` is consulted at all,
// because a computed key has no name to compare.
func (checker *classMethodsUseThisChecker) isIncludedInstanceMember(node *ast.Node) bool {
	member := classMethodsUseThisMemberOf(node)
	if member == nil {
		return false
	}
	// A member with no body cannot use `this`, and judging it is a false positive rather than a
	// finding. Two TypeScript shapes reach here and upstream's JavaScript corpus contains neither:
	//
	//	abstract parse(value: unknown): TOutput;    an abstract method
	//	is(a: T): this;                             an overload signature, declared beside its
	//	is(a: T): unknown { ... }                   implementation, which IS judged
	//
	// Both are declarations rather than definitions, so `this` is not merely unused but
	// unmentionable. Measured against ESLint on `BaseSchema.ts`: it reports 2 and an earlier draft
	// here reported 8, with the six extra all bodiless. The overload case is the sharper one,
	// because the same name then reports twice and reads as a duplicate rather than as a wrong
	// verdict.
	if classMethodsUseThisHasNoBody(member) {
		return false
	}
	if !checker.isInstanceMember(member) {
		return false
	}

	if checker.ignoreOverrideMethods && classMethodsUseThisHasOverrideModifier(member) {
		return false
	}

	if checker.ignoreWithImplements != "" && classMethodsUseThisClassImplements(member) {
		if checker.ignoreWithImplements == "all" {
			return false
		}
		// `public-fields` exempts only members that are publicly visible: not private-named, and
		// either unannotated or explicitly `public`.
		if checker.ignoreWithImplements == "public-fields" &&
			!classMethodsUseThisHasPrivateName(member) &&
			classMethodsUseThisIsPublic(member) {
			return false
		}
	}

	if classMethodsUseThisIsComputed(member) {
		return true
	}

	name, readable := classMethodsUseThisMemberName(member)
	if !readable {
		return true
	}
	return !checker.exceptMethods[name]
}

// isInstanceMember answers upstream's `isInstanceMethod`.
//
// A constructor is exempt by kind, and a static member is exempt by modifier. A property
// declaration counts only when `enforceForClassFields` is on.
func (checker *classMethodsUseThisChecker) isInstanceMember(member *ast.Node) bool {
	if ast.HasStaticModifier(member) {
		return false
	}
	switch member.Kind {
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		return true
	case ast.KindPropertyDeclaration:
		return checker.enforceForClassFields
	}
	return false
}

// walkPropertyDeclaration walks a class field the way upstream's two selectors do.
//
// Upstream opens the field's frame at `"PropertyDefinition > *.key:exit"` and closes it at
// `"PropertyDefinition:exit"`, so the key is visited outside the frame and everything after it
// inside. Reproduced here as a traversal rather than as a position test: the key is walked first
// with the enclosing frame still current, then the frame opens for the remainder.
//
// Both halves are pinned by the corpus, in opposite directions. `foo = this` inside a nested class
// must not satisfy the enclosing method (invalid[19]), and `[this.foo] = 1` inside one must
// (valid[21]).
func (checker *classMethodsUseThisChecker) walkPropertyDeclaration(node *ast.Node) {
	declaration := node.AsPropertyDeclaration()
	if declaration == nil {
		node.ForEachChild(func(child *ast.Node) bool {
			checker.walk(child)
			return false
		})
		return
	}

	name := node.Name()
	if name != nil {
		checker.walk(name)
	}

	checker.stack = append(checker.stack, false)
	node.ForEachChild(func(child *ast.Node) bool {
		if child != name {
			checker.walk(child)
		}
		return false
	})
	usedThis := checker.stack[len(checker.stack)-1]
	checker.stack = checker.stack[:len(checker.stack)-1]

	// The field's own frame is opened for containment rather than for judgment: it keeps a `this`
	// written in the initializer from satisfying whatever encloses the class. A field whose value is
	// a function is judged by that function's own frame, inside the walk above, so there is nothing
	// to report here and `usedThis` is deliberately discarded.
	//
	// Upstream is the same shape: its `PropertyDefinition:exit` is a bare `popContext` with no
	// report, while every finding comes from `exitFunction`.
	_ = usedThis
	_ = declaration
}
