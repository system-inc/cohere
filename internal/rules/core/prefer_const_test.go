package core

import (
	"testing"

	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/rule_testing"
)

// preferConstFile is where the fixtures pretend to live.
const preferConstFile = "/repository/source/PreferConst.ts"

// The corpus is oxc's, copied rather than rewritten.
//
// `oxc/crates/oxc_linter/src/rules/eslint/prefer_const.rs` carries FIVE Tester blocks, which the
// extractor reports and which reading only the last one would silently drop four of:
//
//	block 1   75 pass, 65 fail   test_and_snapshot
//	block 2    3 pass,  0 fail   test, NOT snapshotted   svelte files
//	block 3    2 pass,  0 fail   test, NOT snapshotted   vue files
//	block 4    0 pass,  1 fail   test, NOT snapshotted   astro file
//	block 5   13 pass, 11 fail   test_and_snapshot
//
// 93 pass and 77 fail in total, against 98 snapshot diagnostics: several inputs report more than
// once, so a fixture asserting one finding per input would be wrong and the counts below are
// stated per case.
//
// Blocks 2, 3 and 4 are framework single-file-component sources. Upstream skips `.svelte` and
// `.vue` entirely in `should_run`, because oxlint parses only their `<script>` block and a binding
// the template reassigns therefore looks never-reassigned. Our engine is fed TypeScript files and
// never sees those extensions, so those five cases are not portable and are recorded here rather
// than transcribed into fixtures that would assert nothing. Block 4's astro case is upstream's own
// known-wrong entry: it fails on a file oxlint cannot parse as JavaScript.

// TestPreferConstFires covers the inputs upstream reports on, with the snapshot's own count.
func TestPreferConstFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		findings   int
	}{
		// Block 1, the plain shapes.
		{"an initialized let that is only read", "let x = 1; foo(x);", 1},
		{"a for-in head nothing writes", "for (let i in [1,2,3]) { foo(i); }", 1},
		{"a for-of head nothing writes", "for (let x of [1,2,3]) { foo(x); }", 1},
		{"an uninitialized let with one write", "let x; x = 0;", 1},
		{"a write inside a switch case", "switch (a) { case 0: let x; x = 0; }", 1},
		{"the same inside a function expression", "(function() { let x; x = 1; })();", 1},
		{"an initialized let inside a function", "(function() { let x = 1; foo(x); })();", 1},
		{"a for-in head inside a function", "(function() { for (let i in [1,2,3]) { foo(i); } })();", 1},
		{"a for-of head inside a function", "(function() { for (let x of [1,2,3]) { foo(x); } })();", 1},

		// A shadow: only the inner binding is const-able. This is the case that separates node
		// identity from declaration kind, since both anchors are KindVariableDeclaration.
		{"an inner shadow while the outer is written", "let x = 0; { let x = 1; foo(x); } x = 0;", 1},
		{"a let inside a for body", "for (let i = 0; i < 10; ++i) { let x = 1; foo(x); }", 1},
		// Two findings, not one: the loop head's `i` and the body's `x` are both const-able
		// and upstream reports both. The count was read off the snapshot at
		// `eslint_prefer_const.snap` rather than inferred from the input, which is what the
		// extractor's discrepancy line is warning about.
		{"a let inside a for-in body", "for (let i in [1,2,3]) { let x = 1; foo(x); }", 2},

		// Destructuring under the default, where one binding is written and the other is not.
		{"a pattern with one written binding", "let [x = -1, y] = [1,2]; y = 0;", 1},
		{"an object pattern with one written binding", "let {a: x = -1, b: y} = {a:1,b:2}; y = 0;", 1},
		{"a whole object pattern nothing writes", "let { foo, bar } = baz;", 2},
		{"an array pattern with a hole", "const x = [1,2]; let [,y] = x;", 1},
		{"an array pattern with two holes", "const x = [1,2,3]; let [y,,z] = x;", 2},

		// Multiple declarators in one list. The fix is only offered when the whole list converts.
		{"two initialized declarators", "let x = 'x', y = 'y';", 2},
		{"two declarators where one is written", "let x = 'x', y = 'y'; x = 1", 1},
		{"two lists in sequence", "let x = 1, y = 'y'; let z = 1;", 3},
		{"two object patterns where one is written", "let { a, b, c} = obj; let { x, y, z} = anotherObj; x = 2;", 5},
		{"a pattern beside an uninitialized declarator", "let {a, b} = c, d;", 2},
		{"a pattern beside two uninitialized declarators", "let {a, b, c} = {}, e, f;", 3},
		{"an initializer that is undefined", "let foo = undefined;", 1},

		// Class static blocks are their own statement list.
		{"a static block initializer", "class C { static { let a = 1; } }", 1},
		{"a static block write with no initializer", "class C { static { let a; a = 1; } }", 1},
		{"a static block pattern", "class C { static { let { a, b } = foo; } }", 2},
		{"a static block write then a read", "class C { static { let a; a = 0; console.log(a); } }", 1},
		{"a let inside a static block's if", "class C { static { if (foo) { let a = 1; } } }", 1},
		{"a static block initializer read later", "class C { static { let a = 1; if (foo) { a; } } }", 1},
		{"a static block write inside an if", "class C { static { if (foo) { let a; a = 1; } } }", 1},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.RunTyped(t, PreferConst, preferConstFile, testCase.sourceText)
			wantIds := make([]string, testCase.findings)
			for index := range wantIds {
				wantIds[index] = "preferConst"
			}
			rule_testing.ExpectFindings(t, result, wantIds...)
		})
	}
}

// TestPreferConstStaysSilent is where this rule earns its keep.
//
// The inversion note on PreferConst applies to every case here: this rule reports the ABSENCE of a
// write, so each of these is a write category that, if the rule fails to see it, becomes a false
// positive on correct code carrying a fix that breaks the file. They are not "cases upstream also
// declines"; they are the rule's entire safety margin.
func TestPreferConstStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		// Not a `let` at all.
		{"a var", "var x = 0;"},
		{"a const", "const x = 0;"},
		{"a using binding", "using resource = fn();"},
		{"an await using binding", "await using resource = fn();"},

		// Declared and never written: there is nothing to convert to, since `const` needs a value.
		{"a bare let with no write", "let x;"},

		// The write categories, one per shape.
		{"a plain reassignment", "let x = 0; x = 1;"},
		{"a compound assignment after the write", "let x; x = 0; x += 1;"},
		{"a self-referential reassignment", "let x; x = 0; x = x + 1;"},
		{"a prefix update in a for head", "let a; for (;; ++a);"},
		{"a prefix update inside a call", "let a; for (const x of [1,2,3]) { foo(++a); }"},
		{"a prefix update inside a function", "let a; function foo() { bar(++a); }"},
		{"a write in a for-in head", "for (let i in [1,2,3]) { i = 0; }"},
		{"a write in a for-of head", "for (let x of [1,2,3]) { x = 0; }"},
		{"a bare for-of target", "let x; for (x of array) { x; }"},

		// Writes the enclosing construct makes conditional or repeated.
		{"a write in a nested block", "let x; { x = 0; } foo(x);"},
		{"a write in a nested block then read there", "let x; { x = 0; foo(x); }"},
		{"a write in a while condition", "let a; while (a = foo());"},
		{"a write in a do-while condition", "let a; do {} while (a = foo());"},
		{"a write in a for condition", "let a; for (; a = foo(); );"},
		{"a write inside an if", "let a; if (true) a = 0; foo(a);"},
		{"a write inside a for-of body", "let x; for (const a of [1,2,3]) { x = foo(); bar(x); }"},
		{"a guarded write inside a loop", "let a; for (const x of [1,2,3]) { if (a) {} a = foo(); }"},
		{"a logical write inside a loop", "let a; for (const x of [1,2,3]) { a = a || foo(); bar(a); }"},

		// Writes from a nested function, which may run any number of times.
		{"a write from a function declaration", "let a; function foo() { if (a) {} a = bar(); }"},
		{"a logical write from a function", "let a; function foo() { a = a || bar(); baz(a); }"},
		{"a deferred write from a function", "let id;\nfunction foo() {\n  if (typeof id !== 'undefined') {\n    return;\n  }\n  id = setInterval(() => {}, 250);\n}\nfoo();\n"},
		{"a write into a static block from outside", "let a; class C { static { a = 1; } }"},

		// Two writes, where neither can be the initializer.
		{"two writes in a static block", "class C { static { let a = 1; a = 2; } }"},
		{"an initializer and two writes", "class C { static { let a; a = 1; a = 2; } }"},
		{"a conditional write in a static block", "class C { static { let a; if (foo) { a = 1; } } }"},
		{"an unbraced conditional write in a static block", "class C { static { let a; if (foo) a = 1; } }"},
		{"a conditional destructuring write in a static block", "class C { static { let a, b; if (foo) { ({ a, b } = foo); } } }"},
		{"an unbraced conditional destructuring write", "class C { static { let a, b; if (foo) ({ a, b } = foo); } }"},

		// Function shapes that are not conversions.
		{"a var inside a function", "(function() { var x = 0; })();"},
		{"a bare let inside a function", "(function() { let x; })();"},
		{"a write in a nested block inside a function", "(function() { let x; { x = 0; } foo(x); })();"},
		{"a reassignment inside a function", "(function() { let x = 0; x = 1; })();"},
		{"a const inside a function", "(function() { const x = 0; })();"},
		{"a parameter default", "(function(x = 0) { })();"},
		{"a write in a nested block then read, inside a function", "(function() { let x; { x = 0; foo(x); } })();"},
		{"a write inside a loop inside a function", "(function() { let x; for (const a of [1,2,3]) { x = foo(); bar(x); } })();"},

		// A classic for head with a written counter.
		{"a for head whose counter is updated", "for (let i = 0, end = 10; i < end; ++i) {}"},
		{"the same inside a function", "(function() { for (let i = 0, end = 10; i < end; ++i) {} })();"},

		// Destructuring assignment writes. These are the shapes where a write detector goes short.
		{"a shorthand destructuring write", "let a; { let b; ({ a, b } = obj); }"},
		{"an array destructuring write", "let a; { let b; ([ a, b ] = obj); }"},
		{"a shorthand destructuring write with a var", "var a; { var b; ({ a, b } = obj); }"},
		{"an array destructuring write with a var", "var a; { var b; ([ a, b ] = obj); }"},
		{"a shorthand write reaching a parameter", "\n(function (a) {\n  let b;\n  ({ a, b } = obj);\n})();\n"},
		{"an array write reaching a parameter", "\n(function (a) {\n  let b;\n  ([ a, b ] = obj);\n})();\n"},

		// A destructuring target mixing a member expression with a binding. `const` cannot be
		// initialized from a pattern that also writes a property, so upstream leaves all of these.
		{"a member expression beside a binding", "let predicate; [typeNode.returnType, predicate] = foo();"},
		{"a member expression beside a rest binding", "let predicate; [typeNode.returnType, ...predicate] = foo();"},
		{"a member expression with a hole", "let predicate; [typeNode.returnType,, predicate] = foo();"},
		{"a member expression with a default", "let predicate; [typeNode.returnType=5, predicate] = foo();"},
		{"a nested member expression with a default", "let predicate; [[typeNode.returnType=5], predicate] = foo();"},
		{"a nested pattern holding both", "let predicate; [[typeNode.returnType, predicate]] = foo();"},
		{"a binding nested beside a member expression", "let predicate; [typeNode.returnType, [predicate]] = foo();"},
		{"both nested after a hole", "let predicate; [, [typeNode.returnType, predicate]] = foo();"},
		{"an object pattern nested after a hole", "let predicate; [, {foo:typeNode.returnType, predicate}] = foo();"},
		{"an object rest nested after a hole", "let predicate; [, {foo:typeNode.returnType, ...predicate}] = foo();"},
		{"a property write beside a binding", "let a; const b = {}; ({ a, c: b.c } = func());"},

		// Written after being destructured out.
		{"an array hole binding written later", "const x = [1,2]; let y; [,y] = x; y = 0;"},
		{"two hole bindings written later", "const x = [1,2,3]; let y, z; [y,,z] = x; y = 0; z = 0;"},

		// Block 5: shapes upstream added as regressions.
		{"a write in a logical expression the return depends on", "function example() {\n  let value;\n  return someCheck() && (value = getValue());\n}\n"},
		{"a write inside an array literal argument", "let t;\nconst r = await Promise.race([ a(), (t = b()) ]);\nt.cancel();\n"},
		{"a write in a for condition with no initializer", "for (let match; (match = REGEX.exec(line)); ) {\n  handle(match);\n}"},
		{"the same with a type annotation", "for (let match: string[]; (match = REGEX.exec(line)); ) {\n  handle(match);\n}"},

		// Ours, not upstream's. Each covers a write category the task named and the imported corpus
		// does not separate, because upstream's semantic layer classifies them all as is_write()
		// and so never has to enumerate them.
		//
		// A rest element in a destructuring assignment target. `ast.IsWriteAccess` returns FALSE
		// for these, measured; without the spread arms in writesToBinding the rule reports `w` as
		// never reassigned and offers a fix that produces a const it then writes to.
		// These carry an INITIALIZER, and that is the whole point of them. Without one the
		// binding takes the "no initializer, exactly one write" path, where unconditionalWrite
		// refuses every destructuring write anyway and so masks whether the rest arm in
		// writesToBinding works at all. With an initializer the test is `len(writes) == 0` and
		// nothing else runs, so the rest arm is the only thing standing between correct code and a
		// finding that rewrites it into a const it then writes to. A mutation stubbing
		// isRestTargetOfAssignment to false survived the whole suite until these existed.
		{"an initialized array rest target", "let w = 1; [...w] = [];"},
		{"an initialized object rest target", "let w = 1; ({...w} = {});"},
		{"an initialized rest after another element", "let w = 1; [a, ...w] = [];"},
		{"an initialized rest nested in a property value", "let w = 1; ({ x: [...w] } = {});"},

		// A parenthesis directly around the identifier, which is the shape the rule's own write
		// detector missed until the detector was lifted onto the shelf. Both are real writes: node
		// reports `w` holding [1,2] after `[...(w)] = [1,2]`, and the const form throws
		// `TypeError: Assignment to constant variable`. So the rule reporting these as convertible
		// proposed a fix that produces code which cannot run.
		//
		// The unparenthesized siblings three lines above were always correct, which is what made
		// this invisible: every fixture anyone thought to write used the bare form.
		{"an initialized parenthesized array rest target", "let w = 1; [...(w)] = [];"},
		{"an initialized parenthesized object rest target", "let w = 1; ({...(w)} = {});"},

		{"an array rest assignment target", "let w; [...w] = [];"},
		{"an object rest assignment target", "let w; ({...w} = {});"},
		{"an array rest after another element", "let w; [a, ...w] = [];"},
		{"an object rest after another property", "let w; ({a, ...w} = {});"},
		{"a rest nested inside a property value", "let w; ({ x: [...w] } = {});"},
		{"a parenthesized array rest target", "let w; [...(w)] = [];"},
		{"a parenthesized object rest target", "let w; ({...(w)} = {});"},

		// A postfix update, the shape no_ex_assign was missing entirely until today.
		{"a postfix increment", "let w = 1; w++;"},
		{"a postfix decrement", "let w = 1; w--;"},
		{"a prefix decrement", "let w = 1; --w;"},

		// Logical assignment operators, which are writes and are newer than most corpora.
		{"a nullish assignment", "let w = 1; w ??= 2;"},
		{"a logical or assignment", "let w = 1; w ||= 2;"},
		{"a logical and assignment", "let w = 1; w &&= 2;"},

		// Writes from every nesting shape a callback can take.
		{"a write from an arrow function", "let w = 1; foo(() => { w = 2; });"},
		{"a write from a nested arrow", "let w = 1; foo(() => bar(() => { w = 2; }));"},
		{"a write from a method", "let w = 1; const o = { m() { w = 2; } };"},
		{"a write from a getter", "let w = 1; const o = { get g() { w = 2; return w; } };"},
		{"a write from a class method", "let w = 1; class C { m() { w = 2; } }"},
		{"a write from a catch block", "let w = 1; try {} catch (e) { w = 2; }"},
		{"a write from a finally block", "let w = 1; try {} finally { w = 2; }"},
		{"a write from a labeled block", "let w = 1; outer: { w = 2; }"},
		{"a write from a nested destructuring pattern", "let w = 1; ({a: {b: w}} = obj);"},
		{"a write from a doubly nested array pattern", "let w = 1; [[w]] = [[2]];"},
		{"a write with a default in a shorthand target", "let w = 1; ({w = 3} = obj);"},

		// An ambient declaration introduces no storage.
		{"an ambient let", "declare let w: number;"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule_testing.ExpectClean(t,
				rule_testing.RunTyped(t, PreferConst, preferConstFile, testCase.sourceText))
		})
	}
}

// TestPreferConstDestructuringOption covers the option's two answers on the same inputs.
//
// The option surface was read from ESLint's `meta.schema` per the brief rather than from the
// inventory column: one object, `destructuring: { enum: ["any", "all"] }` and
// `ignoreReadBeforeAssign: { type: "boolean" }`, `additionalProperties: false`. oxc's config agrees.
//
// The cases are upstream's own, and they are the answer to the task's third question: a pattern
// where only some bindings are reassigned DOES report under the default, naming just the
// never-reassigned ones, and reports nothing under "all".
func TestPreferConstDestructuringOption(t *testing.T) {
	anyMode := PreferConstOptions{Destructuring: PreferConstDestructuringAny}
	allMode := PreferConstOptions{Destructuring: PreferConstDestructuringAll}

	t.Run("any reports the unwritten binding of a mixed pattern", func(t *testing.T) {
		result := rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			"let {a = 0, b} = obj; b = 0; foo(a, b);", anyMode)
		rule_testing.ExpectFindings(t, result, "preferConst")
		reported := reportedTextOf(t, result, 0)
		if reported != "a" {
			t.Fatalf("reported %q, want the unwritten binding \"a\"", reported)
		}
	})

	t.Run("all declines the same mixed pattern", func(t *testing.T) {
		rule_testing.ExpectClean(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			"let {a, b} = obj; b = 0;", allMode))
	})

	t.Run("all declines a mixed pattern written by an update", func(t *testing.T) {
		rule_testing.ExpectClean(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			"let a, b; ({a, b} = obj); b++;", allMode))
	})

	t.Run("all declines a rest binding that is written", func(t *testing.T) {
		rule_testing.ExpectClean(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			"let { name, ...otherStuff } = obj; otherStuff = {};", allMode))
	})

	t.Run("any reports the unwritten binding beside a written rest", func(t *testing.T) {
		result := rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			"let { name, ...otherStuff } = obj; otherStuff = {};", anyMode)
		rule_testing.ExpectFindings(t, result, "preferConst")
		if reported := reportedTextOf(t, result, 0); reported != "name" {
			t.Fatalf("reported %q, want \"name\"", reported)
		}
	})

	t.Run("all reports a pattern where every binding qualifies", func(t *testing.T) {
		rule_testing.ExpectFindings(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			"let {a = 0, b} = obj; foo(a, b);", allMode), "preferConst", "preferConst")
	})

	t.Run("all reports a nested pattern where every binding qualifies", func(t *testing.T) {
		rule_testing.ExpectFindings(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			"let {a: {b, c}} = {a: {b: 1, c: 2}}", allMode), "preferConst", "preferConst")
	})

	t.Run("any reports the unwritten binding of a nested pattern", func(t *testing.T) {
		result := rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			"let {a: {b, c}} = {a: {b: 1, c: 2}}; b = 3;", anyMode)
		rule_testing.ExpectFindings(t, result, "preferConst")
		if reported := reportedTextOf(t, result, 0); reported != "c" {
			t.Fatalf("reported %q, want \"c\"", reported)
		}
	})

	// The default is "any", so an unconfigured run and an explicitly-any run must agree. A mutant
	// flipping the zero value is otherwise invisible.
	t.Run("the default matches any", func(t *testing.T) {
		source := "let {a = 0, b} = obj; b = 0; foo(a, b);"
		rule_testing.ExpectFindings(t,
			rule_testing.RunTyped(t, PreferConst, preferConstFile, source), "preferConst")
	})
}

// TestPreferConstIgnoreReadBeforeAssignOption covers the second option.
//
// Upstream's block 5 exists almost entirely for this option and carries both polarities of the same
// nine inputs, which is the strongest possible fixture shape: the same source must report with the
// option off and stay silent with it on, so a rule ignoring the option fails one half whichever way
// it errs.
func TestPreferConstIgnoreReadBeforeAssignOption(t *testing.T) {
	ignoring := PreferConstOptions{IgnoreReadBeforeAssign: true}
	notIgnoring := PreferConstOptions{IgnoreReadBeforeAssign: false}

	// Both polarities of the same input, verbatim from block 5.
	bothWays := []struct {
		name       string
		sourceText string
		findings   int
	}{
		{"a read from a function declared earlier",
			"function square(n) { x * x }\nlet x = 5;\nconsole.log(square(4));", 1},
		{"a read from an arrow assigned earlier", "const fn = () => x;\nlet x = 5;", 1},
		{"a read from an inner function", "function outer() {\n  function inner() { return x; }\n  let x = 5;\n  return inner();\n}", 1},
		{"a read from a class method", "class C {\n  method() { return x; }\n}\nlet x = 5;", 1},
		{"a read from an async function", "async function fetchData() { return x; }\nlet x = 5;", 1},
		{"a read from a generator", "function* gen() { yield x; }\nlet x = 5;", 1},
		{"a read from the first of two functions", "function reader() { return x; }\nfunction setup() { console.log('setup'); }\nlet x = 5;", 1},
		{"a read from an arrow inside a static block", "class C {\n  static {\n    const fn = () => a;\n    let a = 1;\n  }\n}", 1},
	}

	for _, testCase := range bothWays {
		t.Run(testCase.name+", ignoring", func(t *testing.T) {
			rule_testing.ExpectClean(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
				testCase.sourceText, ignoring))
		})
		t.Run(testCase.name+", not ignoring", func(t *testing.T) {
			result := rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
				testCase.sourceText, notIgnoring)
			wantIds := make([]string, testCase.findings)
			for index := range wantIds {
				wantIds[index] = "preferConst"
			}
			rule_testing.ExpectFindings(t, result, wantIds...)
		})
	}

	// A read AFTER the declaration is not a read before assignment, so the option must not
	// suppress it. Upstream lists this one only under `ignoreReadBeforeAssign: false`, and it is
	// the case that stops the option from being implemented as "never report anything that is
	// read".
	t.Run("a read after the declaration reports either way", func(t *testing.T) {
		source := "let x = 0; function foo() { bar(x); }"
		rule_testing.ExpectFindings(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			source, ignoring), "preferConst")
		rule_testing.ExpectFindings(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			source, notIgnoring), "preferConst")
	})

	// The uninitialized shape. Upstream declines this under the option because the read in `foo`
	// precedes the write, and reports it otherwise.
	t.Run("an uninitialized binding read before its write", func(t *testing.T) {
		source := "let x; function foo() { bar(x); } x = 0;"
		rule_testing.ExpectClean(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			source, ignoring))
		rule_testing.ExpectFindings(t, rule_testing.RunTyped(t, PreferConst, preferConstFile, source),
			"preferConst")
	})

	// Upstream's static-block pair, both polarities.
	t.Run("a static block reading a later declaration", func(t *testing.T) {
		source := "class C { static { a; } } let a = 1;"
		rule_testing.ExpectClean(t, rule_testing.RunTypedWithOptions(t, PreferConst, preferConstFile,
			source, ignoring))
		rule_testing.ExpectFindings(t, rule_testing.RunTyped(t, PreferConst, preferConstFile, source),
			"preferConst")
	})
}

// reportedTextOf slices the source with a finding's own range.
//
// Brief step 8: ExpectFindings asserts message ids and count and nothing else, so a rule whose
// defect is WHERE it points passes a complete fixture pair while being wrong. A clone shipped 187
// green fixtures over a rule whose findings all pointed at the wrong place.
func reportedTextOf(t *testing.T, result rule_testing.Result, index int) string {
	t.Helper()
	if index >= len(result.Diagnostics) {
		t.Fatalf("wanted diagnostic %d, got %d", index, len(result.Diagnostics))
	}
	finding := result.Diagnostics[index]
	return finding.SourceFile.Text()[finding.Range.Pos():finding.Range.End()]
}

// TestPreferConstReportsAtTheBinding asserts the span of every finding shape.
//
// The finding points at the bound identifier rather than at the `let` keyword or the whole
// declaration, which is what makes an `eslint-disable-next-line` above the declaration work and
// what makes a multi-binding pattern's findings distinguishable from each other.
func TestPreferConstReportsAtTheBinding(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		want       []string
	}{
		{"a plain binding", "let x = 1; foo(x);", []string{"x"}},
		{"a binding with a longer name", "let counter = 1; foo(counter);", []string{"counter"}},
		{"an uninitialized binding", "let x; x = 0;", []string{"x"}},
		{"a for-of head", "for (let x of [1,2,3]) { foo(x); }", []string{"x"}},
		{"a for-in head", "for (let i in [1,2,3]) { foo(i); }", []string{"i"}},
		{"each binding of a pattern", "let { foo, bar } = baz;", []string{"foo", "bar"}},
		{"each declarator of a list", "let x = 'x', y = 'y';", []string{"x", "y"}},
		{"only the unwritten declarator", "let x = 'x', y = 'y'; x = 1", []string{"y"}},
		{"the inner binding of a shadow", "let x = 0; { let x = 1; foo(x); } x = 0;", []string{"x"}},
		{"a renamed pattern binding", "let {a: x = -1, b: y} = {a:1,b:2}; y = 0;", []string{"x"}},
		{"a static block binding", "class C { static { let a = 1; } }", []string{"a"}},
		// A declaration preceded by a comment. The finding must land on the identifier, not on the
		// comment: ReportNode anchors on the token rather than on Loc.Pos, and this is the shape
		// that would otherwise produce an unsuppressable finding.
		{"a binding under a comment", "/* note */ let x = 1; foo(x);", []string{"x"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.RunTyped(t, PreferConst, preferConstFile, testCase.sourceText)
			if len(result.Diagnostics) != len(testCase.want) {
				t.Fatalf("got %d findings, want %d", len(result.Diagnostics), len(testCase.want))
			}
			for index, want := range testCase.want {
				if got := reportedTextOf(t, result, index); got != want {
					t.Errorf("finding %d reported %q, want %q", index, got, want)
				}
			}
		})
	}
}

// TestPreferConstFixesTheKeyword applies the repair and compares the resulting source.
//
// Brief step 8 again: never by comparing the fix's text, because a fix writing the right string
// over the wrong span passes a text comparison and is a real defect this tree has shipped.
//
// The repair is a fix rather than a suggestion because it preserves what the code means: the rule
// only fires where nothing reassigns the binding, so `const` is a strictly more accurate spelling
// of the same program. It stays a fix, not a suggestion, because there is exactly one valid answer.
func TestPreferConstFixesTheKeyword(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		want       string
	}{
		{"a plain binding", "let x = 1; foo(x);", "const x = 1; foo(x);\n"},
		{"a for-of head", "for (let x of [1,2,3]) { foo(x); }", "for (const x of [1,2,3]) { foo(x); }\n"},
		{"a for-in head", "for (let i in [1,2,3]) { foo(i); }", "for (const i in [1,2,3]) { foo(i); }\n"},
		{"a whole pattern", "let { foo, bar } = baz;", "const { foo, bar } = baz;\n"},
		{"a two-declarator list where both convert", "let x = 'x', y = 'y';", "const x = 'x', y = 'y';\n"},
		{"a static block binding", "class C { static { let a = 1; } }", "class C { static { const a = 1; } }\n"},
		// The keyword is located by skipping leading trivia rather than by taking the list's Pos,
		// which sits before the comment. Taking Pos would rewrite "/* note */" into "const note */",
		// and the span was checked directly rather than inferred from this passing: the fix lands on
		// [11,14) here and [8,11) under the line comment, which are the `let` in each.
		{"a binding under a comment", "/* note */ let x = 1; foo(x);", "/* note */ const x = 1; foo(x);\n"},
		{"a binding under a line comment", "// note\nlet x = 1; foo(x);", "// note\nconst x = 1; foo(x);\n"},
	}

	// The expectations carry a trailing newline because the harness normalizes what it writes to
	// disk before parsing it, appending one and trimming leading whitespace. That is a property of
	// the fixture harness rather than of the repair, and it is stated here because the first
	// execution of these fixtures read as nine fix defects and was none. An "extra spacing before
	// the keyword" case was dropped for the same reason: the harness trims the spacing away, so the
	// case asserted the harness rather than the rule.

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.RunTyped(t, PreferConst, preferConstFile, testCase.sourceText)
			rule_testing.ExpectFixedSource(t, result, testCase.want)
		})
	}
}

// TestPreferConstWithholdsTheFix covers the two conditions upstream puts on the repair.
//
// The finding is still reported in both; only the fix is withheld. That is the honest split: the
// diagnosis is correct and the repair needs a human to split the declaration.
func TestPreferConstWithholdsTheFix(t *testing.T) {
	// A list where one declarator is reassigned. Rewriting the shared keyword would make the
	// reassigned one a const that is then written to, which is source that parses and does not
	// compile: the one failure the fix engine structurally cannot refuse.
	t.Run("a list where another declarator is written", func(t *testing.T) {
		source := "let x = 'x', y = 'y'; x = 1"
		result := rule_testing.RunTyped(t, PreferConst, preferConstFile, source)
		rule_testing.ExpectFindings(t, result, "preferConst")
		if len(result.Diagnostics[0].Fixes) != 0 {
			t.Fatalf("offered a fix that would rewrite `x` into a const it then writes to")
		}
	})

	// A list holding a declarator with no initializer. `const d;` does not parse.
	t.Run("a list holding an uninitialized declarator", func(t *testing.T) {
		source := "let {a, b} = c, d;"
		result := rule_testing.RunTyped(t, PreferConst, preferConstFile, source)
		rule_testing.ExpectFindings(t, result, "preferConst", "preferConst")
		for index, finding := range result.Diagnostics {
			if len(finding.Fixes) != 0 {
				t.Fatalf("finding %d offered a fix producing `const ..., d;` which does not parse", index)
			}
		}
	})

	// An uninitialized declarator whose single write stands in for the initializer. `const x;` does
	// not parse and the write is a separate statement, so there is a finding and no fix.
	t.Run("an uninitialized binding written separately", func(t *testing.T) {
		source := "let x; x = 0;"
		result := rule_testing.RunTyped(t, PreferConst, preferConstFile, source)
		rule_testing.ExpectFindings(t, result, "preferConst")
		if len(result.Diagnostics[0].Fixes) != 0 {
			t.Fatalf("offered a fix producing `const x;` which does not parse")
		}
	})
}

// TestPreferConstNeedsTheTypedHarness makes a revert to rule_testing.Run fail loudly.
//
// Brief step 9: a rule declaring NeedsTypeChecker gets a nil checker from the plain harness, goes
// completely silent, and every StaysSilent case then passes vacuously while every Fires case fails
// in a way that reads like a rule bug. This asserts the silence directly, so the failure names its
// own cause.
func TestPreferConstNeedsTheTypedHarness(t *testing.T) {
	if !PreferConst.NeedsTypeChecker {
		t.Fatal("PreferConst stopped declaring NeedsTypeChecker; the fixtures below assume it")
	}
	rule_testing.ExpectClean(t, rule_testing.Run(t, PreferConst, preferConstFile, "let x = 1; foo(x);"))
	rule_testing.ExpectFindings(t,
		rule_testing.RunTyped(t, PreferConst, preferConstFile, "let x = 1; foo(x);"), "preferConst")
}

// TestPreferConstUsesNodeIdentityNotDeclarationKind is the trap from brief step 8b.
//
// Both anchors here are KindVariableDeclaration, so comparing the resolved declaration's KIND
// instead of its identity cannot separate them at all. Under a kind comparison the inner write is
// seen as a write to the outer binding too, so the outer `a` is silenced and the finding count
// drops. Nothing in the imported corpus catches this, because upstream's shadow cases resolve to
// declarations that exit through a different path.
func TestPreferConstUsesNodeIdentityNotDeclarationKind(t *testing.T) {
	// The outer `a` is never written and must report; the inner `a` is written and must not.
	result := rule_testing.RunTyped(t, PreferConst, preferConstFile, "let a = 1; { let a = 1; a = 2; }")
	rule_testing.ExpectFindings(t, result, "preferConst")
	if got := reportedTextOf(t, result, 0); got != "a" {
		t.Fatalf("reported %q, want the outer binding", got)
	}
	// The span pins WHICH `a`: the outer one is at offset 4, the inner at offset 17.
	if position := result.Diagnostics[0].Range.Pos(); position != 4 {
		t.Fatalf("reported the `a` at offset %d, want the outer one at 4", position)
	}

	// The mirror image, so a rule that simply reports the first of two matching names fails one of
	// the pair whichever way it errs.
	mirror := rule_testing.RunTyped(t, PreferConst, preferConstFile, "let a = 1; a = 2; { let a = 1; foo(a); }")
	rule_testing.ExpectFindings(t, mirror, "preferConst")
	// Offset 24, counted off the literal rather than read back off the rule: `let a = 1; a = 2; { `
	// is twenty characters and `let ` is four more.
	if position := mirror.Diagnostics[0].Range.Pos(); position != 24 {
		t.Fatalf("reported the `a` at offset %d, want the inner one at 24", position)
	}
}

// TestPreferConstSeesRestTargets is the shelf-gap fixture.
//
// `ast.IsWriteAccess` returns false for an identifier under a KindSpreadElement or
// KindSpreadAssignment in a destructuring assignment target, measured on this corpus. The sibling
// rules built on that accessor under-report as a result; this rule would over-report, and the fix
// it offered would rewrite correct code into a const that is then written to. This asserts the
// patched detection directly so a revert to the bare accessor fails here rather than in production.
func TestPreferConstSeesRestTargets(t *testing.T) {
	written := []string{
		"let w; [...w] = [];",
		"let w; ({...w} = {});",
		"let w; [a, ...w] = [];",
		"let w; ({a, ...w} = {});",
		"let w; ({ x: [...w] } = {});",
		"let w; [, [...w]] = [];",
		"let w; for ([...w] of pairs) {}",
	}
	for _, source := range written {
		t.Run("written "+source, func(t *testing.T) {
			rule_testing.ExpectClean(t, rule_testing.RunTyped(t, PreferConst, preferConstFile, source))
		})
	}

	// The control. A spread that is not an assignment target is a read, so these must still report:
	// without it, "treat every spread as a write" would pass the block above while silencing real
	// findings.
	notWritten := []struct {
		name       string
		sourceText string
	}{
		{"a spread argument", "let w = [1]; foo(...w);"},
		{"a spread into an array literal", "let w = [1]; const o = [...w];"},
		{"a spread into an object literal", "let w = {}; const o = {...w};"},
		{"a spread in a new expression", "let w = [1]; new Foo(...w);"},
	}
	for _, testCase := range notWritten {
		t.Run("read "+testCase.name, func(t *testing.T) {
			rule_testing.ExpectFindings(t,
				rule_testing.RunTyped(t, PreferConst, preferConstFile, testCase.sourceText), "preferConst")
		})
	}
}

// TestPreferConstDecodesItsOptions covers the path between a configuration file and the struct.
//
// The registration decodes with a plain json.Unmarshal, and `destructuring` arrives as the JSON
// string "any" or "all" against an int-backed field. Without an UnmarshalJSON the whole option
// object fails to decode and the rule falls back to its zero value, which happens to be "any": a
// project asking for "all" would silently get the other judgment with nothing reported anywhere.
// This asserts the decode directly so that a revert fails here rather than in a user's tree.
func TestPreferConstDecodesItsOptions(t *testing.T) {
	decode := rule.DecodeOptionsInto[PreferConstOptions]()

	for _, testCase := range []struct {
		raw  string
		want PreferConstDestructuring
	}{
		{`{"destructuring":"any"}`, PreferConstDestructuringAny},
		{`{"destructuring":"all"}`, PreferConstDestructuringAll},
	} {
		decoded, err := decode([]byte(testCase.raw))
		if err != nil {
			t.Fatalf("decoding %s: %v", testCase.raw, err)
		}
		if got := decoded.(PreferConstOptions).Destructuring; got != testCase.want {
			t.Fatalf("decoding %s gave destructuring %d, want %d", testCase.raw, got, testCase.want)
		}
	}

	decoded, err := decode([]byte(`{"ignoreReadBeforeAssign":true}`))
	if err != nil {
		t.Fatalf("decoding the boolean option: %v", err)
	}
	if !decoded.(PreferConstOptions).IgnoreReadBeforeAssign {
		t.Fatal("ignoreReadBeforeAssign decoded as false")
	}

	// A typo is an error rather than a silent fallback to the default, which is the whole reason
	// the decoder exists.
	if _, err := decode([]byte(`{"destructuring":"al"}`)); err == nil {
		t.Fatal("a misspelled destructuring value decoded without complaint")
	}
}
