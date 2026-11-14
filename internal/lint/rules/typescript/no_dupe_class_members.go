package typescript

import (
	"fmt"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/class_members"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// NoDupeClassMembers is typescript-eslint's extension of the core rule of the same name.
//
//	valid:   class A { ['foo']() {} ['foo']() {} }     computed, so never a duplicate here
//	valid:   class A { foo(a: string): void; foo(a: number): void; foo(a: unknown): void {} }
//	invalid: class A { foo() {} foo() {} }
//	invalid: class A { foo: string; foo: string; }
//
// Upstream's wrapper is `baseRule.create(context)` with two filters laid over the member listener:
// a member whose key is computed is skipped, and one whose value is a bodiless function expression
// is skipped. Everything that reports comes from the core rule underneath, which is ported here as
// `core.NoDupeClassMembers`, so this file carries the difference and nothing else.
//
// # Only one of upstream's two filters actually narrows anything, and it is not the obvious one
//
// The bodiless-function filter exists so a TypeScript overload signature is not read as a duplicate
// of its implementation. Our core already declines those, and not by inheritance: a previous porter
// found four false positives on real source, added `isOverloadSignature`, and wrote the measurement
// at the line. Measured again here across four shapes, an overload pair, a `declare class`, an
// `abstract class`, and an overload set followed by two real implementations: the wrapper and the
// bare core agree on all four, because ESLint's own rule reads the value's type and declines a
// bodiless one itself once the TypeScript parser hands it one.
//
// The computed filter is the one that bites, and it bites in the direction that reads backwards.
// `[x]()` twice is silent in both, since neither can know what `x` is. But `['foo']()` twice is a
// resolvable key, and the core reports it while the wrapper does not:
//
//	['foo']() twice     core reports, wrapper silent
//	['foo']() and foo() core reports, wrapper silent
//	[1]() twice         core reports, wrapper silent
//
// All three measured on the installed 8.67.0 build, driving the wrapper and the core over identical
// inputs. Upstream's corpus for this rule writes no literal computed key anywhere, so nothing in it
// can see this, and running that corpus through both rules produces byte-identical output on all
// twenty-one cases. The filter is `if (node.computed) return;` with no condition on what the key
// resolves to, which is a deliberate line rather than a side effect.
//
// So this rule is the core rule minus every computed key. Whether that is the better judgment is not
// this file's question: our core matches ESLint's core exactly on all six probed shapes, and this
// one matches typescript-eslint's wrapper.
//
// # Cost
//
// One extra kind test per class member over the core rule, and only on members the core would
// otherwise key.
var NoDupeClassMembers = rule.Rule[rule.NoOptions]{
	Name: "@typescript-eslint/no-dupe-class-members",

	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		// The core rule's own listeners, which is where every finding comes from. Wrapping rather
		// than reimplementing is the point of an extension rule: re-deriving the collision logic
		// would give the two rules two chances to disagree about the same question.
		// The collision judgment, shared with the bare `no-dupe-class-members` rather than
		// re-derived. It used to reach for `core.NoDupeClassMembers.Run` directly, which broke the
		// leaf property a rule package is held to; the judgment now lives in `class_members` and both
		// rules call it. Behaviour is unchanged: the same members collide, this file still carries
		// only the difference.
		coreCheck := func(members *ast.NodeList) {
			class_members.ForEachDuplicate(members, func(name *ast.Node, key class_members.Key) {
				ruleContext.ReportNode(name, rule.Message{
					Id: "noDupeClassMembers",
					Description: fmt.Sprintf(
						"This class already declares a member named %s. The later declaration wins "+
							"silently, so the earlier one is dead code that reads as live, and "+
							"nothing in the language or at runtime tells the two apart.", key.Name),
				})
			})
		}
		// A class carrying a computed member is handed to the core with those members left out of the list
		// it reads, rather than having the core's findings filtered afterwards.
		//
		// Filtering the output was tried first and is wrong, in a way worth recording because it
		// looks right. Upstream skips a computed member before it is keyed, so the member never
		// enters the collision map and cannot make a later member look like a duplicate. Dropping
		// findings after the fact cannot express that: measured, `class A { ['foo']() {} foo() {} }`
		// then reported on `foo`, whose own key is not computed, while upstream is silent. The
		// distinction is which member seeds the collision, and only the input side knows.
		return rule.Listeners{
			ast.KindClassDeclaration: func(node *ast.Node) {
				coreCheck(withoutComputedClassMembers(node.AsClassDeclaration().Members))
			},
			ast.KindClassExpression: func(node *ast.Node) {
				coreCheck(withoutComputedClassMembers(node.AsClassExpression().Members))
			},
		}
	},
}

// withoutComputedClassMembers is a class's member list with its computed members left out, the list itself
// when it has none. The list is new, holding the class's own member nodes and the original list's Loc, so every
// node the core sees is one the parser produced and every span it reports is real.
//
// The class is never written. Its member list once was, swapped for this one and put back after the call, on
// the reasoning that the walk is single-threaded per file. The syntax tree is shared with every checker, though,
// and a checker on another goroutine that resolved the class during the swap saw it without its computed members
// and kept it that way for the run: loom's cursor lost its `*[Symbol.iterator]()`, and every `for ... of` over a
// cursor on that checker read TS2488 (#72kn826, caught by -race).
func withoutComputedClassMembers(members *ast.NodeList) *ast.NodeList {
	if members == nil {
		return nil
	}
	kept := make([]*ast.Node, 0, len(members.Nodes))
	for _, member := range members.Nodes {
		if isComputedClassMember(member) {
			continue
		}
		kept = append(kept, member)
	}
	if len(kept) == len(members.Nodes) {
		return members
	}
	return &ast.NodeList{Loc: members.Loc, Nodes: kept}
}

// isComputedClassMember is upstream's `node.computed`.
//
// estree puts a boolean on the member; our parser puts a `KindComputedPropertyName` in the name
// position, so the same question is a kind test on the name. A member that names nothing, a static
// block or an index signature, is not computed and is not this predicate's business: the core rule
// already declines those by having no name to key.
func isComputedClassMember(member *ast.Node) bool {
	switch member.Kind {
	case ast.KindMethodDeclaration, ast.KindPropertyDeclaration,
		ast.KindGetAccessor, ast.KindSetAccessor:
		name := member.Name()
		return name != nil && name.Kind == ast.KindComputedPropertyName
	}
	return false
}
