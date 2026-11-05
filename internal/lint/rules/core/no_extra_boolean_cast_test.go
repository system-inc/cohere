package core

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"

	"github.com/system-inc/cohere/internal/lint/testing"
)

// extraBooleanCastFile is where the fixtures pretend to live.
const extraBooleanCastFile = "/repository/source/ExtraBooleanCast.ts"

// The corpus is oxc's, copied rather than rewritten.
//
// Every case below is verbatim from `oxc/crates/oxc_linter/src/rules/eslint/no_extra_boolean_cast.rs`:
// 52 pass and 441 fail from the single Tester block, plus 382 entries from its `expect_fix` vector.
//
// # The counts do not line up and that is the point
//
// The snapshot records 507 diagnostics from those 441 fail inputs, so 66 inputs report twice and a
// fixture asserting one finding per input would be wrong on every one of them. The per-input counts
// below were recovered by aligning each snapshot diagnostic against the source line it prints,
// rather than by walking the two lists in order: 104 fail sources appear more than once under
// different options, and an in-order walk cannot tell those copies apart.
//
// # The option spellings
//
// Upstream runs 104 cases under `enforceForLogicalOperands` and 118 under
// `enforceForInnerExpressions`. Both are carried here under the single field they decode to, which
// is what oxc does, and the two blocks are kept separate below so the divergence stays visible: the
// 14 extra inner cases are exactly the shapes ESLint's legacy option would decline.

func TestNoExtraBooleanCastFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		options    any
		messageIds []string
	}{
		// --- defaults (219 cases) ---
		{"if (!!foo) {}", nil, []string{"redundantDoubleNegation"}},
		{"do {} while (!!foo)", nil, []string{"redundantDoubleNegation"}},
		{"while (!!foo) {}", nil, []string{"redundantDoubleNegation"}},
		{"!!foo ? bar : baz", nil, []string{"redundantDoubleNegation"}},
		{"for (; !!foo;) {}", nil, []string{"redundantDoubleNegation"}},
		{"!!!foo", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(!!foo)", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!foo)", nil, []string{"redundantDoubleNegation"}},
		{"if (Boolean(foo)) {}", nil, []string{"redundantBooleanCall"}},
		{"do {} while (Boolean(foo))", nil, []string{"redundantBooleanCall"}},
		{"while (Boolean(foo)) {}", nil, []string{"redundantBooleanCall"}},
		{"Boolean(foo) ? bar : baz", nil, []string{"redundantBooleanCall"}},
		{"for (; Boolean(foo);) {}", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(foo)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(foo && bar)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(foo + bar)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(+foo)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(foo())", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(foo = bar)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(...foo);", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(foo, bar());", nil, []string{"redundantBooleanCall"}},
		{"!Boolean((foo, bar()));", nil, []string{"redundantBooleanCall"}},
		{"!Boolean();", nil, []string{"redundantBooleanCall"}},
		{"!(Boolean());", nil, []string{"redundantBooleanCall"}},
		{"if (!Boolean()) { foo() }", nil, []string{"redundantBooleanCall"}},
		{"while (!Boolean()) { foo() }", nil, []string{"redundantBooleanCall"}},
		{"var foo = Boolean() ? bar() : baz()", nil, []string{"redundantBooleanCall"}},
		{"if (Boolean()) { foo() }", nil, []string{"redundantBooleanCall"}},
		{"while (Boolean()) { foo() }", nil, []string{"redundantBooleanCall"}},
		{"Boolean(Boolean(foo))", nil, []string{"redundantBooleanCall"}},
		{"Boolean(!!foo, bar)", nil, []string{"redundantDoubleNegation"}},
		{"function *foo() { yield!!a ? b : c }", nil, []string{"redundantDoubleNegation"}},
		{"function *foo() { yield!! a ? b : c }", nil, []string{"redundantDoubleNegation"}},
		{"function *foo() { yield! !a ? b : c }", nil, []string{"redundantDoubleNegation"}},
		{"function *foo() { yield !!a ? b : c }", nil, []string{"redundantDoubleNegation"}},
		{"function *foo() { yield(!!a) ? b : c }", nil, []string{"redundantDoubleNegation"}},
		{"function *foo() { yield/**/!!a ? b : c }", nil, []string{"redundantDoubleNegation"}},
		{"x=!!a ? b : c ", nil, []string{"redundantDoubleNegation"}},
		{"void!Boolean()", nil, []string{"redundantBooleanCall"}},
		{"void! Boolean()", nil, []string{"redundantBooleanCall"}},
		{"typeof!Boolean()", nil, []string{"redundantBooleanCall"}},
		{"(!Boolean())", nil, []string{"redundantBooleanCall"}},
		{"+!Boolean()", nil, []string{"redundantBooleanCall"}},
		{"void !Boolean()", nil, []string{"redundantBooleanCall"}},
		{"void(!Boolean())", nil, []string{"redundantBooleanCall"}},
		{"void/**/!Boolean()", nil, []string{"redundantBooleanCall"}},
		{"!/**/!!foo", nil, []string{"redundantDoubleNegation"}},
		{"!!/**/!foo", nil, []string{"redundantDoubleNegation"}},
		{"!!!/**/foo", nil, []string{"redundantDoubleNegation"}},
		{"!!!foo/**/", nil, []string{"redundantDoubleNegation"}},
		{"if(!/**/!foo);", nil, []string{"redundantDoubleNegation"}},
		{"(!!/**/foo ? 1 : 2)", nil, []string{"redundantDoubleNegation"}},
		{"!/**/Boolean(foo)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean/**/(foo)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(/**/foo)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(foo/**/)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(foo)/**/", nil, []string{"redundantBooleanCall"}},
		{"if(Boolean/**/(foo));", nil, []string{"redundantBooleanCall"}},
		{"(Boolean(foo/**/) ? 1 : 2)", nil, []string{"redundantBooleanCall"}},
		{"/**/!Boolean()", nil, []string{"redundantBooleanCall"}},
		{"!/**/Boolean()", nil, []string{"redundantBooleanCall"}},
		{"!Boolean/**/()", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(/**/)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean()/**/", nil, []string{"redundantBooleanCall"}},
		{"if(!/**/Boolean());", nil, []string{"redundantBooleanCall"}},
		{"(!Boolean(/**/) ? 1 : 2)", nil, []string{"redundantBooleanCall"}},
		{"if(/**/Boolean());", nil, []string{"redundantBooleanCall"}},
		{"if(Boolean/**/());", nil, []string{"redundantBooleanCall"}},
		{"if(Boolean(/**/));", nil, []string{"redundantBooleanCall"}},
		{"if(Boolean()/**/);", nil, []string{"redundantBooleanCall"}},
		{"(Boolean/**/() ? 1 : 2)", nil, []string{"redundantBooleanCall"}},
		{"Boolean(!!(a, b))", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(Boolean((a, b)))", nil, []string{"redundantBooleanCall"}},
		{"Boolean((!!(a, b)))", nil, []string{"redundantDoubleNegation"}},
		{"Boolean((Boolean((a, b))))", nil, []string{"redundantBooleanCall"}},
		{"Boolean(!(!(a, b)))", nil, []string{"redundantDoubleNegation"}},
		{"Boolean((!(!(a, b))))", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(!!(a = b))", nil, []string{"redundantDoubleNegation"}},
		{"Boolean((!!(a = b)))", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(Boolean(a = b))", nil, []string{"redundantBooleanCall"}},
		{"Boolean(Boolean((a += b)))", nil, []string{"redundantBooleanCall"}},
		{"Boolean(!!(a === b))", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(!!((a !== b)))", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(!!a.b)", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(Boolean((a)))", nil, []string{"redundantBooleanCall"}},
		{"Boolean((!!(a)))", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!(a, b))", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean(Boolean((a, b)))", nil, []string{"redundantBooleanCall"}},
		{"new Boolean((!!(a, b)))", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean((Boolean((a, b))))", nil, []string{"redundantBooleanCall"}},
		{"new Boolean(!(!(a, b)))", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean((!(!(a, b))))", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!(a = b))", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean((!!(a = b)))", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean(Boolean(a = b))", nil, []string{"redundantBooleanCall"}},
		{"new Boolean(Boolean((a += b)))", nil, []string{"redundantBooleanCall"}},
		{"new Boolean(!!(a === b))", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!((a !== b)))", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!a.b)", nil, []string{"redundantDoubleNegation"}},
		{"new Boolean(Boolean((a)))", nil, []string{"redundantBooleanCall"}},
		{"new Boolean((!!(a)))", nil, []string{"redundantDoubleNegation"}},
		{"if (!!(a, b));", nil, []string{"redundantDoubleNegation"}},
		{"if (Boolean((a, b)));", nil, []string{"redundantBooleanCall"}},
		{"if (!(!(a, b)));", nil, []string{"redundantDoubleNegation"}},
		{"if (!!(a = b));", nil, []string{"redundantDoubleNegation"}},
		{"if (Boolean(a = b));", nil, []string{"redundantBooleanCall"}},
		{"if (!!(a > b));", nil, []string{"redundantDoubleNegation"}},
		{"if (Boolean(a === b));", nil, []string{"redundantBooleanCall"}},
		{"if (!!f(a));", nil, []string{"redundantDoubleNegation"}},
		{"if (Boolean(f(a)));", nil, []string{"redundantBooleanCall"}},
		{"if (!!(f(a)));", nil, []string{"redundantDoubleNegation"}},
		{"if ((!!f(a)));", nil, []string{"redundantDoubleNegation"}},
		{"if ((Boolean(f(a))));", nil, []string{"redundantBooleanCall"}},
		{"if (!!a);", nil, []string{"redundantDoubleNegation"}},
		{"if (Boolean(a));", nil, []string{"redundantBooleanCall"}},
		{"while (!!(a, b));", nil, []string{"redundantDoubleNegation"}},
		{"while (Boolean((a, b)));", nil, []string{"redundantBooleanCall"}},
		{"while (!(!(a, b)));", nil, []string{"redundantDoubleNegation"}},
		{"while (!!(a = b));", nil, []string{"redundantDoubleNegation"}},
		{"while (Boolean(a = b));", nil, []string{"redundantBooleanCall"}},
		{"while (!!(a > b));", nil, []string{"redundantDoubleNegation"}},
		{"while (Boolean(a === b));", nil, []string{"redundantBooleanCall"}},
		{"while (!!f(a));", nil, []string{"redundantDoubleNegation"}},
		{"while (Boolean(f(a)));", nil, []string{"redundantBooleanCall"}},
		{"while (!!(f(a)));", nil, []string{"redundantDoubleNegation"}},
		{"while ((!!f(a)));", nil, []string{"redundantDoubleNegation"}},
		{"while ((Boolean(f(a))));", nil, []string{"redundantBooleanCall"}},
		{"while (!!a);", nil, []string{"redundantDoubleNegation"}},
		{"while (Boolean(a));", nil, []string{"redundantBooleanCall"}},
		{"do {} while (!!(a, b));", nil, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean((a, b)));", nil, []string{"redundantBooleanCall"}},
		{"do {} while (!(!(a, b)));", nil, []string{"redundantDoubleNegation"}},
		{"do {} while (!!(a = b));", nil, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean(a = b));", nil, []string{"redundantBooleanCall"}},
		{"do {} while (!!(a > b));", nil, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean(a === b));", nil, []string{"redundantBooleanCall"}},
		{"do {} while (!!f(a));", nil, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean(f(a)));", nil, []string{"redundantBooleanCall"}},
		{"do {} while (!!(f(a)));", nil, []string{"redundantDoubleNegation"}},
		{"do {} while ((!!f(a)));", nil, []string{"redundantDoubleNegation"}},
		{"do {} while ((Boolean(f(a))));", nil, []string{"redundantBooleanCall"}},
		{"do {} while (!!a);", nil, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean(a));", nil, []string{"redundantBooleanCall"}},
		{"for (; !!(a, b););", nil, []string{"redundantDoubleNegation"}},
		{"for (; Boolean((a, b)););", nil, []string{"redundantBooleanCall"}},
		{"for (; !(!(a, b)););", nil, []string{"redundantDoubleNegation"}},
		{"for (; !!(a = b););", nil, []string{"redundantDoubleNegation"}},
		{"for (; Boolean(a = b););", nil, []string{"redundantBooleanCall"}},
		{"for (; !!(a > b););", nil, []string{"redundantDoubleNegation"}},
		{"for (; Boolean(a === b););", nil, []string{"redundantBooleanCall"}},
		{"for (; !!f(a););", nil, []string{"redundantDoubleNegation"}},
		{"for (; Boolean(f(a)););", nil, []string{"redundantBooleanCall"}},
		{"for (; !!(f(a)););", nil, []string{"redundantDoubleNegation"}},
		{"for (; (!!f(a)););", nil, []string{"redundantDoubleNegation"}},
		{"for (; (Boolean(f(a))););", nil, []string{"redundantBooleanCall"}},
		{"for (; !!a;);", nil, []string{"redundantDoubleNegation"}},
		{"for (; Boolean(a););", nil, []string{"redundantBooleanCall"}},
		{"!!(a, b) ? c : d", nil, []string{"redundantDoubleNegation"}},
		{"(!!(a, b)) ? c : d", nil, []string{"redundantDoubleNegation"}},
		{"Boolean((a, b)) ? c : d", nil, []string{"redundantBooleanCall"}},
		{"!!(a = b) ? c : d", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(a -= b) ? c : d", nil, []string{"redundantBooleanCall"}},
		{"(Boolean((a *= b))) ? c : d", nil, []string{"redundantBooleanCall"}},
		{"!!(a ? b : c) ? d : e", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(a ? b : c) ? d : e", nil, []string{"redundantBooleanCall"}},
		{"!!(a || b) ? c : d", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(a && b) ? c : d", nil, []string{"redundantBooleanCall"}},
		{"!!(a === b) ? c : d", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(a < b) ? c : d", nil, []string{"redundantBooleanCall"}},
		{"!!((a !== b)) ? c : d", nil, []string{"redundantDoubleNegation"}},
		{"Boolean((a >= b)) ? c : d", nil, []string{"redundantBooleanCall"}},
		{"!!+a ? b : c", nil, []string{"redundantDoubleNegation"}},
		{"!!+(a) ? b : c", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(!a) ? b : c", nil, []string{"redundantBooleanCall"}},
		{"!!f(a) ? b : c", nil, []string{"redundantDoubleNegation"}},
		{"(!!f(a)) ? b : c", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(a.b) ? c : d", nil, []string{"redundantBooleanCall"}},
		{"!!a ? b : c", nil, []string{"redundantDoubleNegation"}},
		{"Boolean(a) ? b : c", nil, []string{"redundantBooleanCall"}},
		{"!!!(a, b)", nil, []string{"redundantDoubleNegation"}},
		{"!Boolean((a, b))", nil, []string{"redundantBooleanCall"}},
		{"!!!(a = b)", nil, []string{"redundantDoubleNegation"}},
		{"!!(!(a += b))", nil, []string{"redundantDoubleNegation"}},
		{"!(!!(a += b))", nil, []string{"redundantDoubleNegation"}},
		{"!Boolean(a -= b)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean((a -= b))", nil, []string{"redundantBooleanCall"}},
		{"!(Boolean(a -= b))", nil, []string{"redundantBooleanCall"}},
		{"!!!(a || b)", nil, []string{"redundantDoubleNegation"}},
		{"!Boolean(a || b)", nil, []string{"redundantBooleanCall"}},
		{"!!!(a && b)", nil, []string{"redundantDoubleNegation"}},
		{"!Boolean(a && b)", nil, []string{"redundantBooleanCall"}},
		{"!!!(a != b)", nil, []string{"redundantDoubleNegation"}},
		{"!!!(a === b)", nil, []string{"redundantDoubleNegation"}},
		{"var x = !Boolean(a > b)", nil, []string{"redundantBooleanCall"}},
		{"!!!(a - b)", nil, []string{"redundantDoubleNegation"}},
		{"!!!(a ** b)", nil, []string{"redundantDoubleNegation"}},
		{"!Boolean(a ** b)", nil, []string{"redundantBooleanCall"}},
		{"async function f() { !!!(await a) }", nil, []string{"redundantDoubleNegation"}},
		{"async function f() { !Boolean(await a) }", nil, []string{"redundantBooleanCall"}},
		{"!!!!a", nil, []string{"redundantDoubleNegation", "redundantDoubleNegation"}},
		{"!!(!(!a))", nil, []string{"redundantDoubleNegation", "redundantDoubleNegation"}},
		{"!Boolean(!a)", nil, []string{"redundantBooleanCall"}},
		{"!Boolean((!a))", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(!(a))", nil, []string{"redundantBooleanCall"}},
		{"!(Boolean(!a))", nil, []string{"redundantBooleanCall"}},
		{"!!!+a", nil, []string{"redundantDoubleNegation"}},
		{"!!!(+a)", nil, []string{"redundantDoubleNegation"}},
		{"!!(!+a)", nil, []string{"redundantDoubleNegation"}},
		{"!(!!+a)", nil, []string{"redundantDoubleNegation"}},
		{"!Boolean((-a))", nil, []string{"redundantBooleanCall"}},
		{"!Boolean(-(a))", nil, []string{"redundantBooleanCall"}},
		{"!!!(--a)", nil, []string{"redundantDoubleNegation"}},
		{"!Boolean(a++)", nil, []string{"redundantBooleanCall"}},
		{"!!!f(a)", nil, []string{"redundantDoubleNegation"}},
		{"!!!(f(a))", nil, []string{"redundantDoubleNegation"}},
		{"!!!a", nil, []string{"redundantDoubleNegation"}},
		{"!Boolean(a)", nil, []string{"redundantBooleanCall"}},
		{"if (Boolean?.(foo)) {};", nil, []string{"redundantBooleanCall"}},
		{"if (!Boolean(a as any)) { }", nil, []string{"redundantBooleanCall"}},

		// --- the deprecated enforceForLogicalOperands spelling (104 cases) ---
		{
			"if (!!foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (!!foo && bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if ((!!foo || bar) && bat) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (foo && !!bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"do {} while (!!foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"while (!!foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!foo && bat ? bar : baz",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"for (; !!foo || bar;) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!!foo || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"Boolean(!!foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"new Boolean(!!foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (Boolean(foo) || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"do {} while (Boolean(foo) || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"while (Boolean(foo) || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"Boolean(foo) || bat ? bar : baz",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"for (; Boolean(foo) || bar;) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo && bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo + bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(+foo)  || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo()) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo = bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(...foo) || bar;",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo, bar()) || bar;",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean((foo, bar()) || bat);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean() || bar;",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!(Boolean()) || bar;",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (!Boolean() || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"while (!Boolean() || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"var foo = Boolean() || bar ? bar() : baz()",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (Boolean() || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"while (Boolean() || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"function *foo() { yield(!!a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield(!! a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield(! !a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield (!!a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield/**/(!!a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"x=!!a || d ? b : c ",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"void(!Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"void(! Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"typeof(!Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"void/**/(!Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!/**/(!!foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!/**/!foo || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!!/**/foo || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!(!!foo || bar)/**/",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if(!/**/!foo || bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"(!!/**/foo || bar ? 1 : 2)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!/**/(Boolean(foo) || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean/**/(foo) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(/**/foo) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo/**/) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!(Boolean(foo)|| bar)/**/",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean/**/(foo) || bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(Boolean(foo/**/)|| bar ? 1 : 2)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"/**/!Boolean()|| bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!/**/Boolean()|| bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean/**/()|| bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(/**/)|| bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean()|| bar)/**/",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(!/**/Boolean()|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean(/**/) || bar ? 1 : 2)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(/**/Boolean()|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean/**/()|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean(/**/)|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean()|| bar/**/);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(Boolean/**/()|| bar ? 1 : 2)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a && !!(b ? c : d)){}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield!!a || d ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (!!(a, b) || !!(c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean((a, b)) || Boolean((c, d))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if ((!!((a, b))) || (!!((c, d)))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a, b) && !!(c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean((a, b)) && Boolean((c, d))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if ((!!((a, b))) && (!!((c, d)))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a = b) || !!(c = d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a /= b) || Boolean(c /= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a >>= b) && !!(c >>= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a **= b) && Boolean(c **= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a ? b : c) || !!(d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a ? b : c) || Boolean(d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a ? b : c) && !!(d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a ? b : c) && Boolean(d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a || b) || !!(c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a || b) || Boolean(c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a || b) && !!(c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a || b) && Boolean(c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a && b) || !!(c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a && b) || Boolean(c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a && b) && !!(c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a && b) && Boolean(c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a !== b) || !!(c !== d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a != b) || Boolean(c != d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a === b) && !!(c === d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a > b) || !!(c < d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(!a) || Boolean(+b)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!f(a) && !!b.c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a) || !!b) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantDoubleNegation"},
		},
		{
			"if (!!a && Boolean(b)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantBooleanCall"},
		},
		{
			"if ((!!a) || (Boolean(b))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantBooleanCall"},
		},
		{
			"if (Boolean(a ?? b) || c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (Boolean?.(a ?? b) || c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},

		// --- enforceForInnerExpressions (118 cases) ---
		{
			"if (!!foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (!!foo && bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if ((!!foo || bar) && bat) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (foo && !!bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"do {} while (!!foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"while (!!foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!foo && bat ? bar : baz",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"for (; !!foo || bar;) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!!foo || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"Boolean(!!foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"new Boolean(!!foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (Boolean(foo) || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"do {} while (Boolean(foo) || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"while (Boolean(foo) || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"Boolean(foo) || bat ? bar : baz",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"for (; Boolean(foo) || bar;) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo && bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo + bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(+foo)  || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo()) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo = bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(...foo) || bar;",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo, bar()) || bar;",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean((foo, bar()) || bat);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean() || bar;",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!(Boolean()) || bar;",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (!Boolean() || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"while (!Boolean() || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"var foo = Boolean() || bar ? bar() : baz()",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (Boolean() || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"while (Boolean() || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"function *foo() { yield(!!a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield(!! a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield(! !a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield (!!a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield/**/(!!a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"x=!!a || d ? b : c ",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"void(!Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"void(! Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"typeof(!Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"void/**/(!Boolean() || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!/**/(!!foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!/**/!foo || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!!/**/foo || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!(!!foo || bar)/**/",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if(!/**/!foo || bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"(!!/**/foo || bar ? 1 : 2)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"!/**/(Boolean(foo) || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean/**/(foo) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(/**/foo) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo/**/) || bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!(Boolean(foo)|| bar)/**/",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean/**/(foo) || bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(Boolean(foo/**/)|| bar ? 1 : 2)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"/**/!Boolean()|| bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!/**/Boolean()|| bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean/**/()|| bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(/**/)|| bar",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean()|| bar)/**/",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(!/**/Boolean()|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean(/**/) || bar ? 1 : 2)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(/**/Boolean()|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean/**/()|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean(/**/)|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean()|| bar/**/);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"(Boolean/**/()|| bar ? 1 : 2)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a && !!(b ? c : d)){}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield!!a || d ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"if ((1, 2, Boolean(3))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ?? Boolean(b)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ?? Boolean(b || c)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if ((a, b, c ?? (d, e, f ?? Boolean(g)))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (!!(a, b) || !!(c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean((a, b)) || Boolean((c, d))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if ((!!((a, b))) || (!!((c, d)))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a, b) && !!(c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean((a, b)) && Boolean((c, d))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if ((!!((a, b))) && (!!((c, d)))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a = b) || !!(c = d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a /= b) || Boolean(c /= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a >>= b) && !!(c >>= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a **= b) && Boolean(c **= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a ? b : c) || !!(d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a ? b : c) || Boolean(d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a ? b : c) && !!(d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a ? b : c) && Boolean(d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a || b) || !!(c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a || b) || Boolean(c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a || b) && !!(c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a || b) && Boolean(c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a && b) || !!(c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a && b) || Boolean(c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a && b) && !!(c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a && b) && Boolean(c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a !== b) || !!(c !== d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a != b) || Boolean(c != d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a === b) && !!(c === d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a > b) || !!(c < d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(!a) || Boolean(+b)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!f(a) && !!b.c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a) || !!b) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantDoubleNegation"},
		},
		{
			"if (!!a && Boolean(b)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantBooleanCall"},
		},
		{
			"if ((!!a) || (Boolean(b))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation", "redundantBooleanCall"},
		},
		{
			"if (Boolean(a ?? b) || c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (Boolean?.(a ?? b) || c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ? Boolean(b) : c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ? b : Boolean(c)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ? b : Boolean(c ? d : e)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"const ternary = Boolean(bar ? !!baz : bat);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"const commaOperator = Boolean((bar, baz, !!bat));",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantDoubleNegation"},
		},
		{
			"for (let i = 0; (console.log(i), Boolean(i < 10)); i++) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"const nullishCoalescingOperator = Boolean(bar ?? Boolean(baz));",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ? Boolean(b = c) : Boolean(d = e));",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (a ? Boolean((b, c)) : Boolean((d, e)));",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"\n\tfunction * generator() {\n\t    if (a ? Boolean(yield y) : x) {\n\t        return a;\n\t    };\n\t}\n\t",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"redundantBooleanCall"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFindings(test,
				rule_testing.RunWithOptions(test, NoExtraBooleanCast, extraBooleanCastFile,
					testCase.sourceText, testCase.options), testCase.messageIds...)
		})
	}
}

// The clean cases, which are where the discrimination actually lives.
//
// `if (new Boolean(foo)) {}` is the sharpest: a construction is a coercing *context* for whatever it
// wraps, and is never itself a redundant cast, so a rule that treated the two symmetrically reports
// it. `if ((Boolean(1), 2)) {}` is next: the call is in a sequence but not its last element, so its
// value is discarded rather than tested. And every `var foo = bar || !!baz` case pins the recursion
// terminating: the `||` is not itself in a coercing position, so nothing inside it is either.
func TestNoExtraBooleanCastStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		options    any
	}{
		{"Boolean(bar, !!baz);", nil},
		{"var foo = !!bar;", nil},
		{"function foo() { return !!bar; }", nil},
		{"var foo = bar() ? !!baz : !!bat", nil},
		{"for(!!foo;;) {}", nil},
		{"for(;; !!foo) {}", nil},
		{"var foo = Boolean(bar);", nil},
		{"function foo() { return Boolean(bar); }", nil},
		{"var foo = bar() ? Boolean(baz) : Boolean(bat)", nil},
		{"for(Boolean(foo);;) {}", nil},
		{"for(;; Boolean(foo)) {}", nil},
		{"if (new Boolean(foo)) {}", nil},
		{"if ((Boolean(1), 2)) {}", nil},
		{"var foo = bar || !!baz", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = bar && !!baz", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = bar || (baz && !!bat)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"function foo() { return (!!bar || baz); }", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"var foo = bar() ? (!!baz && bat) : (!!bat && qux)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"for(!!(foo && bar);;) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"for(;; !!(foo || bar)) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = Boolean(bar) || baz;", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = bar || Boolean(baz);", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = Boolean(bar) || Boolean(baz);", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"function foo() { return (Boolean(bar) || baz); }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"var foo = bar() ? Boolean(baz) || bat : Boolean(bat)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"for(Boolean(foo) || bar;;) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"for(;; Boolean(foo) || bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (new Boolean(foo) || bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (!!foo || bar) {}", nil},
		{"if (!!foo || bar) {}", NoExtraBooleanCastOptions{}},
		{"if (!!foo || bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: false}},
		{"if ((!!foo || bar) === baz) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (!!foo ?? bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = bar || !!baz", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = bar && !!baz", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = bar || (baz && !!bat)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"function foo() { return (!!bar || baz); }", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"var foo = bar() ? (!!baz && bat) : (!!bat && qux)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"for(!!(foo && bar);;) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"for(;; !!(foo || bar)) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = Boolean(bar) || baz;", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = bar || Boolean(baz);", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"var foo = Boolean(bar) || Boolean(baz);", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"function foo() { return (Boolean(bar) || baz); }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"var foo = bar() ? Boolean(baz) || bat : Boolean(bat)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"for(Boolean(foo) || bar;;) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"for(;; Boolean(foo) || bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (new Boolean(foo) || bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (!!foo || bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: false}},
		{"if ((!!foo || bar) === baz) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (!!foo ?? bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if ((1, Boolean(2), 3)) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test,
				rule_testing.RunWithOptions(test, NoExtraBooleanCast, extraBooleanCastFile,
					testCase.sourceText, testCase.options))
		})
	}
}

// The span of every finding, which ExpectFindings cannot see.
//
// A rule that reports the right message at the wrong offsets passes the whole corpus above. The
// expected text here is sliced out of upstream's own snapshot, which prints a line, a column and an
// underline for every one of its 507 diagnostics, so these are measurements rather than guesses.
//
// The `!!` cases are the ones that would catch an off-by-one: the finding covers the outer `!` and
// everything under it, so reporting the inner negation instead is a span one byte to the right that
// still lands inside the same expression.
func TestNoExtraBooleanCastReportsTheRightSpan(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		options    any
		wantSpans  []string
	}{
		{"if (!!foo) {}", nil, []string{"!!foo"}},
		{"do {} while (!!foo)", nil, []string{"!!foo"}},
		{"!!!foo", nil, []string{"!!foo"}},
		{"if (Boolean(foo)) {}", nil, []string{"Boolean(foo)"}},
		// Only the inner negation. The outer call is not itself in a coercing position, so it is a
		// context rather than a finding, and a rule reporting both would be reporting the outer one
		// for no reason. Upstream's snapshot records exactly one diagnostic here, at column 9.
		{"Boolean(!!foo)", nil, []string{"!!foo"}},
		{"new Boolean(!!foo)", nil, []string{"!!foo"}},
		{"!!!!a", nil, []string{"!!!a", "!!a"}},
		{"!Boolean(foo)", nil, []string{"Boolean(foo)"}},
		// The same asymmetry for the call form: the inner call is the first argument of the outer
		// one, which coerces it, while the outer call sits in a statement position that does not.
		{"Boolean(Boolean(foo))", nil, []string{"Boolean(foo)"}},
		{"if (Boolean?.(foo)) {};", nil, []string{"Boolean?.(foo)"}},
		{"if ((Boolean(f(a))));", nil, []string{"Boolean(f(a))"}},
		{"if (!!a && Boolean(b)) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
			[]string{"!!a", "Boolean(b)"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(test, NoExtraBooleanCast, extraBooleanCastFile,
				testCase.sourceText, testCase.options)
			if len(result.Diagnostics) != len(testCase.wantSpans) {
				test.Fatalf("wanted %d findings, got %d", len(testCase.wantSpans),
					len(result.Diagnostics))
			}
			for index, diagnostic := range result.Diagnostics {
				reported := testCase.sourceText[diagnostic.Range.Pos():diagnostic.Range.End()]
				if reported != testCase.wantSpans[index] {
					test.Fatalf("finding %d pointed at %q, wanted %q", index, reported,
						testCase.wantSpans[index])
				}
			}
		})
	}
}

// The deprecated option spelling reaches the same field, which no verbatim case can show.
//
// Upstream's corpus runs both spellings but the harness above decodes neither: the cases arrive as
// an already-built options struct. This is the only place the JSON path is exercised, and without it
// a port that simply dropped `enforceForLogicalOperands` from the decoder passes all 441 fail cases.
func TestNoExtraBooleanCastAcceptsTheDeprecatedOptionSpelling(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name    string
		rawJson string
		want    bool
	}{
		{"the current spelling", `{"enforceForInnerExpressions": true}`, true},
		{"the deprecated spelling", `{"enforceForLogicalOperands": true}`, true},
		{"neither spelling", `{}`, false},
		{"the current spelling off", `{"enforceForInnerExpressions": false}`, false},
		{"the deprecated spelling off", `{"enforceForLogicalOperands": false}`, false},
		// serde resolves an alias only when the canonical name is absent, so the canonical name
		// wins rather than whichever was written last.
		{"both spellings, canonical wins",
			`{"enforceForInnerExpressions": false, "enforceForLogicalOperands": true}`, false},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, err := rule.DecodeOptionsInto[NoExtraBooleanCastOptions]()([]byte(testCase.rawJson))
			if err != nil {
				test.Fatalf("decoding %s: %v", testCase.rawJson, err)
			}
			options, ok := decoded.(NoExtraBooleanCastOptions)
			if !ok {
				test.Fatalf("decoded to %T, wanted NoExtraBooleanCastOptions", decoded)
			}
			if options.EnforceForInnerExpressions != testCase.want {
				test.Fatalf("EnforceForInnerExpressions was %v, wanted %v",
					options.EnforceForInnerExpressions, testCase.want)
			}
		})
	}
}
