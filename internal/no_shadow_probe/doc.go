// Package no_shadow_probe measures whether this tree has the substrate to port
// @typescript-eslint/no-shadow, and records the answer so the next porter starts
// from measurement rather than a blank page.
//
// The rule was NOT ported. It is feasible and it is large; the sizing is in the
// handoff below.
//
// # What upstream needs
//
// no-shadow is built entirely on eslint-scope. Its spine is: walk every scope,
// enumerate `scope.variables`, and for each ask `findVariable(scope.upper, name)`.
// The API surface it reads, measured off the source, is scope.upper, scope.variables,
// scope.childScopes, scope.block, scope.type, variable.identifiers, variable.defs,
// variable.scope, variable.isTypeVariable, variable.isValueVariable, plus seven
// DefinitionType variants and two ScopeType variants.
//
// # What this tree has, measured
//
// The scope graph IS here, under a name a search for "scope" does not find it by:
// TypeScript's own binder populates a per-container symbol table reachable through
// `ast.IsLocalsContainer` plus `ast.GetLocals`. Ten of twelve shadowing shapes taken
// from upstream's corpus are directly visible in it (see coverage_test.go).
//
// Two facts about that substrate cost a cycle each to establish:
//
//   - Locals is EMPTY under rule_testing.Run and populated under RunTyped. The
//     binder runs with the program, so an untyped probe reports an absent scope
//     graph that is merely unbound. A rule built on this must declare
//     NeedsTypeChecker for that reason rather than for any type question.
//
//   - ast.GetLocals PANICS on a node that is not a locals container; it does not
//     return nil. Guard with ast.IsLocalsContainer first. An enum declaration is
//     the shape that reaches it (see gaps_test.go).
//
// # The value/type split is native, and better than upstream's
//
// isValueVariable / isTypeVariable are a boolean pair in eslint-scope. Here the
// symbol carries TypeScript's own SymbolFlags, measured in symbolflags_test.go:
//
//	0x1      FunctionScopedVariable   a parameter, a var
//	0x2      BlockScopedVariable      a let, a const
//	0x10     Function                 a function declaration
//	0x20     Class                    a class declaration
//	0x40     Interface                type-only
//	0x100    RegularEnum
//	0x200    ValueModule              a namespace
//	0x40000  TypeParameter            type-only
//	0x80000  TypeAlias                type-only
//	0x200000 Alias                    an import
//
// Every DefinitionType variant the rule switches on maps onto these, so the
// classification does not have to be rebuilt.
//
// # The two gaps, and how each is recoverable
//
//   - An enum declaration is NOT a locals container, so its member scope is absent
//     from the general walk. The members are reachable through the enum symbol's
//     Exports table instead. So noEnumShadow is portable, through a different
//     accessor than everything else, which is exactly the kind of thing a walk
//     written from upstream's shape would miss silently.
//
//   - A function expression IS a locals container but its own name is not in it,
//     so upstream's functionExpressionName scope has no equivalent. That scope is
//     what makes `var a = function a() {}` clean while `var a = wrap(function a() {})`
//     reports, so the self-reference exemption has to come from the AST shape.
//
// # What is genuinely absent
//
// builtinGlobals has no substrate, and this is the same wall no-redeclare stopped
// at. All 10 corpus cases producing noShadowGlobal need both builtinGlobals:true
// and a configured `globals` map, and this config declares no globals. Those 10
// cases cannot be expressed here today, and reporting them would need a globals
// pass rather than a rule.
//
// # Corpus, extracted and byte-verified
//
// 239 cases across the two upstream files (tests/rules/no-shadow/ is a DIRECTORY:
// no-shadow.test.ts 47 invalid + 99 valid, no-shadow-eslint.test.ts 53 invalid +
// 40 valid), carrying 103 expected findings: 91 noShadow, 10 noShadowGlobal,
// 2 noEnumShadow. All 239 verified present verbatim in the source files, with the
// checker controlled by corrupting one case and watching it fail.
//
// Cases by option axis, which is how the remaining work divides:
//
//	96  defaults only        42 findings   the tractable core
//	62  hoist                              needs temporal dead zone and hoisting
//	28  ignoreOnInitialization             needs reference-position dataflow
//	20  builtinGlobals                     no substrate, see above
//	19  ignoreFunctionTypeParameterNameValueShadow
//	 8  ignoreTypeValueShadow
//	 1  allow
//
// A scoped first port covering only the default option set is 96 cases and 42
// findings, and it is defensible on its own because the defaults are what the live
// config would run. hoist and ignoreOnInitialization are each a separate analysis
// and belong in later passes.
package no_shadow_probe
