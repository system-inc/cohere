package typescript

import (
	"encoding/json"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/verify/internal/rule"
)

var messageNoRedeclare = rule.Message{
	Id: "redeclared",
	Description: "This name is already declared in the same scope. The second declaration does not " +
		"create a second binding, it overwrites what the name reaches, so every use of the name " +
		"after this point gets whatever the last declaration bound and the earlier one becomes " +
		"unreachable without ever being deleted. The two spellings that look intentional are the " +
		"dangerous ones: a value and a type sharing a name compile fine and diverge later, and a " +
		"second class silently replaces the first for every `new`, every `extends`, and every " +
		"`instanceof` in the file. Rename one of them, or delete the declaration that is dead.",
}

// NoRedeclareOptions tunes whether TypeScript's legitimate declaration merges are exempt.
//
// The wire field is a pointer because the default is TRUE. A plain bool cannot tell an absent key
// from an explicit `false`, and a zero-valued struct would turn every merge exemption off, which
// silently converts thirteen of upstream's clean cases into findings.
type NoRedeclareOptions struct {
	IgnoreDeclarationMerge bool
}

// noRedeclareWire is the shape the config layer actually delivers, before defaults are applied.
type noRedeclareWire struct {
	IgnoreDeclarationMerge *bool `json:"ignoreDeclarationMerge"`
}

// DefaultNoRedeclareOptions is the configuration upstream applies when the rule is written bare.
func DefaultNoRedeclareOptions() NoRedeclareOptions {
	return NoRedeclareOptions{IgnoreDeclarationMerge: true}
}

// DecodeNoRedeclareOptions reads this rule's configuration from the config layer.
//
// Hand-rolled rather than `rule.DecodeOptionsInto`, because that helper reports an error on empty
// input and the config layer turns the error into nil, so a rule reached through it can never see
// the difference between "no options were written" and "the option was written as false". For an
// option defaulting to false that distinction is invisible. This one defaults to true, so the
// generic helper's zero value inverts the rule rather than merely weakening it.
func DecodeNoRedeclareOptions(raw []byte) (any, error) {
	options := DefaultNoRedeclareOptions()
	if len(raw) == 0 {
		return options, nil
	}
	var wire noRedeclareWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return options, err
	}
	if wire.IgnoreDeclarationMerge != nil {
		options.IgnoreDeclarationMerge = *wire.IgnoreDeclarationMerge
	}
	return options, nil
}

// NoRedeclare flags a name declared more than once in the same scope.
//
//	valid:   var a = 3; var b = function () { var a = 10; };
//	valid:   if (true) { let b = 2; } else { let b = 3; }
//	valid:   function a(): string; function a(): number; function a() {}
//	valid:   interface A {} class A {} namespace A {}
//	valid:   function A<T>() {} interface B<T> {} type C<T> = Array<T>; class D<T> {}
//	invalid: var a = 3; var a = 10;
//	invalid: var a; function a() {}
//	invalid: type T = 1; type T = 2;
//	invalid: type something = string; const something = 2;
//	invalid: class A {} class A {} namespace A {}
//
// A second declaration of a name does not add a binding, it replaces what the name reaches. In
// plain JavaScript that is a typo with no diagnostic; in TypeScript it is worse, because the
// language deliberately allows some of these pairs to MERGE into one thing and the merging ones are
// spelled exactly like the mistaken ones. `class A {} interface A {}` is one type with extra
// members and is correct. `class A {} class A {}` is one class silently replacing another and is
// not. The whole judgment this rule makes is telling those apart.
//
// # Why this reads the checker
//
// Upstream is a thin caller over `eslint-scope`: it asks `sourceCode.getScope(node)` for a scope,
// walks `scope.variables`, and reports any variable holding more than one declaration identifier.
// The scope partition IS the rule. Without one, `function f1() { var x = 1; } function f2() { var x
// = 2; }` and `var x = 1; var x = 2;` are the same two declarations of the same name and no
// syntactic test separates them.
//
// The equivalent partition our tree already has is TypeScript's own binder. `ast.IsLocalsContainer`
// names the nodes that own a scope and `ast.GetLocals` hands back that scope's symbol table, so
// walking from a declaration to its nearest locals container reproduces exactly the grouping
// upstream's scope manager builds. Measured on ten shapes against the installed rule, including the
// three that must produce no group at all: two function bodies, the arms of an if/else, and four
// generic parameters all named `T` across a function, an interface, a type alias and a class.
//
// # Why the obvious predicate does not work, which is the part worth reading
//
// The reflex is to ask the checker how many declarations a name has and report when it holds more
// than one. That answers wrongly in BOTH directions and a port built on it fails quietly:
//
//	class E {} class E {}                      symbol.Declarations is 1, and this REPORTS
//	interface I {} interface I {}              symbol.Declarations is 2, and this is CLEAN
//
// The two duplicate classes do not share a symbol at all. They are two symbols competing for one
// name, and the binder keeps one of them in the scope table and drops the other, so the count
// collapses to 1 on exactly the input that must report. The legitimately merging pair does share
// one symbol carrying both declarations, so it counts 2 on exactly the input that must not. A
// previous attempt at this rule measured that pair of facts, concluded the substrate was absent,
// and stopped. The measurement was right and the conclusion did not follow: the count is the wrong
// question, not a missing answer.
//
// So this rule never asks the checker for a count. It groups the declaration NODES by scope and
// name from the tree, which cannot lose one, and then applies upstream's merge arithmetic to the
// kinds in the group. The checker is still needed for the scope partition itself, which is what
// `ast.GetLocals` reads.
//
// # The merge arithmetic, which is upstream's and reproduced rather than reasoned about
//
// With `ignoreDeclarationMerge` on, a group is exempt when it is all interfaces, all namespaces, or
// drawn from one of three merge sets with at most one member from the set's "primary" kind: class
// with interface and namespace, function with namespace, enum with namespace. Past one primary the
// exemption lapses and only the PRIMARY declarations report, which is why `class A {} class A {}
// namespace A {}` reports once rather than twice: the namespace is still a legitimate merge partner
// and is not what went wrong.
//
// Overload signatures are filtered before any of this. Upstream drops `TSDeclareFunction`, its node
// for a function declaration with no body, so `function a(): string; function a(): number; function
// a() {}` is one implementation and two signatures rather than three declarations. Our parser gives
// all three `KindFunctionDeclaration` and separates them by whether a body is present.
//
// # What this port does not do, measured rather than assumed
//
// Upstream's `builtinGlobals` option reports a declaration that shadows a global, and this port has
// no such option. That is not a gap in our checker. Probed with a control: a local `var Object`
// SHADOWS rather than merges, so the scope enumeration returns only the local declaration and the
// standard library's `Object` is not in scope at all, answering identically to a name that is not a
// builtin. The option's verdict comes from ESLint's own environment model rather than from
// resolution, and upstream's corpus shows it: `var Object = 0;` appears as a CLEAN case and as a
// REPORTING case with the same option value, separated only by whether the file is a module. We
// have no configured-globals surface for that question to read, so the option is absent rather than
// stubbed. Sixteen of upstream's 52 cases carry it and are omitted from the fixtures, pinned by
// `TestNoRedeclareBuiltinGlobalsIsOutOfScope` rather than left as silence.
//
// The same reasoning covers upstream's `/*global b:false*/` case, which reports a declaration
// against a name a comment directive introduced. We have no directive-globals surface either.
//
// No fix. The repair is a rename or a deletion, and choosing which declaration is the dead one, and
// what to call the survivor, is exactly what the rule cannot know.
var NoRedeclare = rule.Rule{
	Name: "no-redeclare",

	// See the doc above: the scope partition comes from the binder, which the program builds.
	NeedsTypeChecker: true,

	Run: func(ctx rule.Context, options any) rule.Listeners {
		settings, ok := options.(NoRedeclareOptions)
		if !ok {
			// A rule configured as a bare "error" is handed nil, and `options.(T)` on nil yields the
			// zero value, whose false `IgnoreDeclarationMerge` inverts this rule rather than
			// weakening it. Falling back to the real defaults is what keeps the bare configuration,
			// which is how this rule is actually written down, from reporting every legitimate
			// declaration merge in the tree.
			settings = DefaultNoRedeclareOptions()
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				// The engine hands every rule a nil checker when the program could not be built.
				// This rule reads the binder's scope tables, which only exist on a bound program,
				// so without one there is nothing to group and the honest answer is silence.
				if ctx.TypeChecker == nil {
					return
				}
				reportRedeclarationsIn(ctx, node, settings)
			},
		}
	},
}

// declarationGroup is every declaration of one name inside one scope, in source order.
type declarationGroup struct {
	declarations []*ast.Node
}

// reportRedeclarationsIn walks the file once, partitions every declaration by the scope it binds
// into, and reports the groups upstream would.
//
// One pass over the file rather than a listener per scope-bearing kind. Upstream registers eight
// listeners and asks each for its scope, which is the shape ESLint's API forces; we have the whole
// tree in hand, so the same partition falls out of a single walk that asks each declaration which
// scope owns it. Fidelity is to which inputs report, not to how upstream obtained its scopes.
func reportRedeclarationsIn(ctx rule.Context, sourceFile *ast.Node, settings NoRedeclareOptions) {
	// Keyed by the scope-owning node and the name, so two scopes holding the same name never meet.
	groups := map[scopedName]*declarationGroup{}
	var order []scopedName

	var walk func(*ast.Node)
	walk = func(current *ast.Node) {
		if current == nil {
			return
		}
		if name := redeclarableName(current); name != "" {
			container := nearestScopeContainer(current)
			key := scopedName{container: container, name: name}
			group, seen := groups[key]
			if !seen {
				group = &declarationGroup{}
				groups[key] = group
				order = append(order, key)
			}
			group.declarations = append(group.declarations, current)
		}
		current.ForEachChild(func(child *ast.Node) bool {
			walk(child)
			return false
		})
	}
	walk(sourceFile)

	// Iterate the recorded order rather than the map. Go randomises map iteration, so reporting
	// straight from it would emit findings in a different order on every run, and the harness
	// asserts findings in order.
	for _, key := range order {
		for _, declaration := range redeclarationsToReport(groups[key].declarations, settings) {
			name := declaration.Name()
			if name == nil {
				continue
			}
			ctx.ReportNode(name, messageNoRedeclare)
		}
	}
}

// scopedName identifies one name inside one scope.
type scopedName struct {
	container *ast.Node
	name      string
}

// redeclarationsToReport applies upstream's merge arithmetic to one group and returns the
// declarations that should report, which is every one after the first that survives the exemptions.
func redeclarationsToReport(declarations []*ast.Node, settings NoRedeclareOptions) []*ast.Node {
	// Overload signatures are not declarations for this rule's purposes. Upstream filters its
	// `TSDeclareFunction` nodes before counting, so a function with two signatures and one body is
	// a single declaration rather than three.
	var considered []*ast.Node
	for _, declaration := range declarations {
		if isFunctionOverloadSignature(declaration) {
			continue
		}
		considered = append(considered, declaration)
	}
	// A cost guard rather than a discrimination, and measured as one: neutralising it survives the
	// whole corpus because every path below returns `slice[1:]`, which is already empty for a group
	// of one, so no input can distinguish the two versions. Kept because it skips the merge
	// arithmetic for the overwhelmingly common case of a name declared once, and the inverse
	// mutation, widening it to swallow real groups, fails 38 lines, so the statement is reached and
	// the fixtures do see it.
	if len(considered) < 2 {
		return nil
	}

	if settings.IgnoreDeclarationMerge {
		if exempt, primaries := mergeExemption(considered); exempt {
			// Every declaration merges legitimately, so nothing reports.
			if primaries == nil {
				return nil
			}
			// More than one declaration of the merge set's primary kind, so those report and the
			// legitimate merge partners beside them do not. The first primary is the one being
			// redeclared, so it is not itself a finding.
			return primaries[1:]
		}
	}

	return considered[1:]
}

// mergeExemption reports whether a group is one of TypeScript's legitimate declaration merges, and
// when the group holds more than one declaration of the merging kind, which ones those are.
//
// The three merge sets and their primary kinds are upstream's, at
// `typescript-eslint/packages/eslint-plugin/src/rules/no-redeclare.ts`. A group qualifies when every
// declaration in it is drawn from one set. If the set's primary kind appears once, the merge is safe
// and nothing reports. If it appears more than once, the extra copies report and the other members
// of the set do not, which is what makes `class A {} class A {} namespace A {}` one finding.
func mergeExemption(declarations []*ast.Node) (bool, []*ast.Node) {
	// Upstream tests all-interfaces and all-namespaces before the three merge sets, and that test is
	// deliberately absent here because the sets already answer it. An all-interface group is drawn
	// from the class set, whose members are class, interface and namespace, and supplies none of its
	// primary, so it takes the `len(primaries) <= 1` exit and comes back exempt. Namespaces are in
	// all three sets and do the same. Measured rather than argued: a mutant neutralising an explicit
	// arm for these two survived the whole corpus, which is what identified it as subsumed rather
	// than as untested, and upstream is silent on three interfaces, three namespaces, and the two
	// mixed, all of which this reaches through the sets.
	for _, set := range mergeSets {
		drawnFromSet := true
		var primaries []*ast.Node
		for _, declaration := range declarations {
			if !set.members[declaration.Kind] {
				drawnFromSet = false
				break
			}
			if declaration.Kind == set.primary {
				primaries = append(primaries, declaration)
			}
		}
		if !drawnFromSet {
			continue
		}
		if len(primaries) <= 1 {
			return true, nil
		}
		return true, primaries
	}

	return false, nil
}

// mergeSet is one of TypeScript's legitimate declaration merges: the kinds that may share a name,
// and the one of them that may only appear once.
type mergeSet struct {
	primary ast.Kind
	members map[ast.Kind]bool
}

// mergeSets is upstream's three sets, in upstream's order.
//
// Order is load-bearing rather than incidental. A group of one class and one namespace is drawn
// from the class set, and a group of one function and one namespace is drawn from the function set;
// nothing is drawn from two, because each set's members differ past the shared namespace. Reading
// them in a fixed order keeps the answer stable regardless.
var mergeSets = []mergeSet{
	{
		primary: ast.KindClassDeclaration,
		members: map[ast.Kind]bool{
			ast.KindClassDeclaration:     true,
			ast.KindInterfaceDeclaration: true,
			ast.KindModuleDeclaration:    true,
		},
	},
	{
		primary: ast.KindFunctionDeclaration,
		members: map[ast.Kind]bool{
			ast.KindFunctionDeclaration: true,
			ast.KindModuleDeclaration:   true,
		},
	},
	{
		primary: ast.KindEnumDeclaration,
		members: map[ast.Kind]bool{
			ast.KindEnumDeclaration:   true,
			ast.KindModuleDeclaration: true,
		},
	},
}

// redeclarableName returns the text of a declaration's name when the node binds that name into an
// enclosing scope, and "" otherwise.
//
// The kinds are the ones that can collide. A parameter and a class member are deliberately absent:
// a parameter belongs to the function's own scope where upstream's scope manager puts it too, and a
// member belongs to the class rather than to any scope this rule partitions. A binding pattern's
// elements are absent for a different reason, and it is a divergence worth naming: upstream reports
// `var { a = 0, b: Object = 0 } = {};` against an earlier `var a;`, so a destructured name IS a
// declaration to it. Reproducing that needs the binding pattern walked into, which is reachable,
// and it is left out here only because every corpus case exercising it also carries the
// `builtinGlobals` option and could not be asserted either way.
func redeclarableName(node *ast.Node) string {
	switch node.Kind {
	case ast.KindClassDeclaration, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration,
		ast.KindEnumDeclaration, ast.KindModuleDeclaration, ast.KindFunctionDeclaration,
		ast.KindVariableDeclaration:
		name := node.Name()
		if name != nil && ast.IsIdentifier(name) {
			return name.Text()
		}
	}
	return ""
}

// isFunctionOverloadSignature reports whether a function declaration is a signature with no body.
//
// Upstream's parser gives these their own node type and filters on it. Ours gives every form
// `KindFunctionDeclaration` and distinguishes them by the body, so the test is the body rather than
// the kind. An ambient `declare function` and a plain overload signature both land here, which
// matches upstream: both are `TSDeclareFunction` to it.
//
// Named for functions rather than sharing `isOverloadSignature` with `no-dupe-class-members`, which
// asks the same-sounding question about a class MEMBER and answers for method and accessor kinds
// this rule never sees. One name over two domains would have to widen to cover both and would then
// answer for kinds neither caller wants.
func isFunctionOverloadSignature(node *ast.Node) bool {
	return node.Kind == ast.KindFunctionDeclaration && node.Body() == nil
}

// nearestScopeContainer walks up to the first ancestor that owns a scope, which is the scope the
// declaration binds into.
//
// A nil container means the declaration sits outside any locals-bearing node, which the tree should
// not produce because the source file itself owns one. It is returned rather than guarded so that
// two such declarations would still group together under the same nil key rather than being
// silently dropped.
func nearestScopeContainer(node *ast.Node) *ast.Node {
	for current := node.Parent; current != nil; current = current.Parent {
		if ast.IsLocalsContainer(current) && ast.GetLocals(current) != nil {
			return current
		}
	}
	return nil
}
