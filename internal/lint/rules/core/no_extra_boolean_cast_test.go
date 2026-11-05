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
		options    rule.Given[NoExtraBooleanCastOptions]
		messageIds []string
	}{
		// --- defaults (219 cases) ---
		{"if (!!foo) {}", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"do {} while (!!foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"while (!!foo) {}", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!foo ? bar : baz", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"for (; !!foo;) {}", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!!foo", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(!!foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if (Boolean(foo)) {}", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"do {} while (Boolean(foo))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"while (Boolean(foo)) {}", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"Boolean(foo) ? bar : baz", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"for (; Boolean(foo);) {}", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(foo && bar)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(foo + bar)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(+foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(foo())", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(foo = bar)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(...foo);", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(foo, bar());", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean((foo, bar()));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean();", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!(Boolean());", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if (!Boolean()) { foo() }", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"while (!Boolean()) { foo() }", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{
			"var foo = Boolean() ? bar() : baz()",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantBooleanCall"},
		},
		{"if (Boolean()) { foo() }", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"while (Boolean()) { foo() }", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"Boolean(Boolean(foo))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"Boolean(!!foo, bar)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{
			"function *foo() { yield!!a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield!! a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield! !a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield !!a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield(!!a) ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield/**/!!a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantDoubleNegation"},
		},
		{"x=!!a ? b : c ", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"void!Boolean()", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"void! Boolean()", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"typeof!Boolean()", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"(!Boolean())", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"+!Boolean()", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"void !Boolean()", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"void(!Boolean())", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"void/**/!Boolean()", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!/**/!!foo", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!/**/!foo", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!!/**/foo", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!!foo/**/", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if(!/**/!foo);", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"(!!/**/foo ? 1 : 2)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!/**/Boolean(foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean/**/(foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(/**/foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(foo/**/)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(foo)/**/", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if(Boolean/**/(foo));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"(Boolean(foo/**/) ? 1 : 2)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"/**/!Boolean()", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!/**/Boolean()", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean/**/()", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(/**/)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean()/**/", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if(!/**/Boolean());", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"(!Boolean(/**/) ? 1 : 2)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if(/**/Boolean());", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if(Boolean/**/());", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if(Boolean(/**/));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if(Boolean()/**/);", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"(Boolean/**/() ? 1 : 2)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"Boolean(!!(a, b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(Boolean((a, b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"Boolean((!!(a, b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean((Boolean((a, b))))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"Boolean(!(!(a, b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean((!(!(a, b))))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(!!(a = b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean((!!(a = b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(Boolean(a = b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"Boolean(Boolean((a += b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"Boolean(!!(a === b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(!!((a !== b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(!!a.b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(Boolean((a)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"Boolean((!!(a)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!(a, b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean(Boolean((a, b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"new Boolean((!!(a, b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean((Boolean((a, b))))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"new Boolean(!(!(a, b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean((!(!(a, b))))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!(a = b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean((!!(a = b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean(Boolean(a = b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"new Boolean(Boolean((a += b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"new Boolean(!!(a === b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!((a !== b)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean(!!a.b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"new Boolean(Boolean((a)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"new Boolean((!!(a)))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if (!!(a, b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if (Boolean((a, b)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if (!(!(a, b)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if (!!(a = b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if (Boolean(a = b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if (!!(a > b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if (Boolean(a === b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if (!!f(a));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if (Boolean(f(a)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if (!!(f(a)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if ((!!f(a)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if ((Boolean(f(a))));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if (!!a);", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"if (Boolean(a));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"while (!!(a, b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"while (Boolean((a, b)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"while (!(!(a, b)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"while (!!(a = b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"while (Boolean(a = b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"while (!!(a > b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"while (Boolean(a === b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"while (!!f(a));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"while (Boolean(f(a)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"while (!!(f(a)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"while ((!!f(a)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"while ((Boolean(f(a))));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"while (!!a);", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"while (Boolean(a));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"do {} while (!!(a, b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean((a, b)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"do {} while (!(!(a, b)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"do {} while (!!(a = b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean(a = b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"do {} while (!!(a > b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean(a === b));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"do {} while (!!f(a));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean(f(a)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"do {} while (!!(f(a)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"do {} while ((!!f(a)));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"do {} while ((Boolean(f(a))));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"do {} while (!!a);", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"do {} while (Boolean(a));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"for (; !!(a, b););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"for (; Boolean((a, b)););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"for (; !(!(a, b)););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"for (; !!(a = b););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"for (; Boolean(a = b););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"for (; !!(a > b););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"for (; Boolean(a === b););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"for (; !!f(a););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"for (; Boolean(f(a)););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"for (; !!(f(a)););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"for (; (!!f(a)););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"for (; (Boolean(f(a))););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"for (; !!a;);", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"for (; Boolean(a););", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!(a, b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"(!!(a, b)) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean((a, b)) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!(a = b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(a -= b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"(Boolean((a *= b))) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!(a ? b : c) ? d : e", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(a ? b : c) ? d : e", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!(a || b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(a && b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!(a === b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(a < b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!((a !== b)) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean((a >= b)) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!+a ? b : c", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!+(a) ? b : c", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(!a) ? b : c", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!f(a) ? b : c", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"(!!f(a)) ? b : c", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(a.b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!a ? b : c", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"Boolean(a) ? b : c", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!!(a, b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!!(a = b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!(!(a += b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!(!!(a += b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!Boolean(a -= b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean((a -= b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!(Boolean(a -= b))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!!(a || b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!Boolean(a || b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!!(a && b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!Boolean(a && b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!!(a != b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!!(a === b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"var x = !Boolean(a > b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!!(a - b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!!(a ** b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!Boolean(a ** b)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{
			"async function f() { !!!(await a) }",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantDoubleNegation"},
		},
		{
			"async function f() { !Boolean(await a) }",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantBooleanCall"},
		},
		{
			"!!!!a",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"!!(!(!a))",
			rule.Given[NoExtraBooleanCastOptions]{},
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{"!Boolean(!a)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean((!a))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(!(a))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!(Boolean(!a))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!!+a", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!!(+a)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!(!+a)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!(!!+a)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!Boolean((-a))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!Boolean(-(a))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!!(--a)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!Boolean(a++)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"!!!f(a)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!!(f(a))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!!!a", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantDoubleNegation"}},
		{"!Boolean(a)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if (Boolean?.(foo)) {};", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},
		{"if (!Boolean(a as any)) { }", rule.Given[NoExtraBooleanCastOptions]{}, []string{"redundantBooleanCall"}},

		// --- the deprecated enforceForLogicalOperands spelling (104 cases) ---
		{
			"if (!!foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (!!foo && bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if ((!!foo || bar) && bat) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (foo && !!bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"do {} while (!!foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"while (!!foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!foo && bat ? bar : baz",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"for (; !!foo || bar;) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!!foo || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"Boolean(!!foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"new Boolean(!!foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (Boolean(foo) || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"do {} while (Boolean(foo) || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"while (Boolean(foo) || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"Boolean(foo) || bat ? bar : baz",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"for (; Boolean(foo) || bar;) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo && bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo + bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(+foo)  || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo()) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo = bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(...foo) || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo, bar()) || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean((foo, bar()) || bat);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean() || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!(Boolean()) || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (!Boolean() || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"while (!Boolean() || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"var foo = Boolean() || bar ? bar() : baz()",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (Boolean() || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"while (Boolean() || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"function *foo() { yield(!!a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield(!! a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield(! !a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield (!!a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield/**/(!!a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"x=!!a || d ? b : c ",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"void(!Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"void(! Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"typeof(!Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"void/**/(!Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!/**/(!!foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!/**/!foo || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!!/**/foo || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!(!!foo || bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if(!/**/!foo || bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"(!!/**/foo || bar ? 1 : 2)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!/**/(Boolean(foo) || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean/**/(foo) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(/**/foo) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo/**/) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!(Boolean(foo)|| bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean/**/(foo) || bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(Boolean(foo/**/)|| bar ? 1 : 2)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"/**/!Boolean()|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!/**/Boolean()|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean/**/()|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(/**/)|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean()|| bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(!/**/Boolean()|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean(/**/) || bar ? 1 : 2)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(/**/Boolean()|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean/**/()|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean(/**/)|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean()|| bar/**/);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(Boolean/**/()|| bar ? 1 : 2)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a && !!(b ? c : d)){}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield!!a || d ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (!!(a, b) || !!(c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean((a, b)) || Boolean((c, d))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if ((!!((a, b))) || (!!((c, d)))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a, b) && !!(c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean((a, b)) && Boolean((c, d))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if ((!!((a, b))) && (!!((c, d)))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a = b) || !!(c = d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a /= b) || Boolean(c /= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a >>= b) && !!(c >>= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a **= b) && Boolean(c **= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a ? b : c) || !!(d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a ? b : c) || Boolean(d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a ? b : c) && !!(d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a ? b : c) && Boolean(d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a || b) || !!(c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a || b) || Boolean(c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a || b) && !!(c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a || b) && Boolean(c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a && b) || !!(c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a && b) || Boolean(c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a && b) && !!(c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a && b) && Boolean(c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a !== b) || !!(c !== d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a != b) || Boolean(c != d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a === b) && !!(c === d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a > b) || !!(c < d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(!a) || Boolean(+b)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!f(a) && !!b.c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a) || !!b) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantDoubleNegation"},
		},
		{
			"if (!!a && Boolean(b)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantBooleanCall"},
		},
		{
			"if ((!!a) || (Boolean(b))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantBooleanCall"},
		},
		{
			"if (Boolean(a ?? b) || c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (Boolean?.(a ?? b) || c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},

		// --- enforceForInnerExpressions (118 cases) ---
		{
			"if (!!foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (!!foo && bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if ((!!foo || bar) && bat) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (foo && !!bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"do {} while (!!foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"while (!!foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!foo && bat ? bar : baz",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"for (; !!foo || bar;) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!!foo || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"Boolean(!!foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"new Boolean(!!foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if (Boolean(foo) || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"do {} while (Boolean(foo) || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"while (Boolean(foo) || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"Boolean(foo) || bat ? bar : baz",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"for (; Boolean(foo) || bar;) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo && bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo + bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(+foo)  || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo()) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo = bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(...foo) || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo, bar()) || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean((foo, bar()) || bat);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean() || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!(Boolean()) || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (!Boolean() || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"while (!Boolean() || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"var foo = Boolean() || bar ? bar() : baz()",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (Boolean() || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"while (Boolean() || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"function *foo() { yield(!!a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield(!! a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield(! !a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield (!!a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield/**/(!!a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"x=!!a || d ? b : c ",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"void(!Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"void(! Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"typeof(!Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"void/**/(!Boolean() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!/**/(!!foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!/**/!foo || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!!!/**/foo || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!(!!foo || bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if(!/**/!foo || bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"(!!/**/foo || bar ? 1 : 2)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"!/**/(Boolean(foo) || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean/**/(foo) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(/**/foo) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(foo/**/) || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!(Boolean(foo)|| bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean/**/(foo) || bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(Boolean(foo/**/)|| bar ? 1 : 2)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"/**/!Boolean()|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!/**/Boolean()|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean/**/()|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"!Boolean(/**/)|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean()|| bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(!/**/Boolean()|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(!Boolean(/**/) || bar ? 1 : 2)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(/**/Boolean()|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean/**/()|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean(/**/)|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if(Boolean()|| bar/**/);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"(Boolean/**/()|| bar ? 1 : 2)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a && !!(b ? c : d)){}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"function *foo() { yield!!a || d ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"if ((1, 2, Boolean(3))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ?? Boolean(b)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ?? Boolean(b || c)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if ((a, b, c ?? (d, e, f ?? Boolean(g)))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (!!(a, b) || !!(c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean((a, b)) || Boolean((c, d))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if ((!!((a, b))) || (!!((c, d)))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a, b) && !!(c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean((a, b)) && Boolean((c, d))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if ((!!((a, b))) && (!!((c, d)))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a = b) || !!(c = d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a /= b) || Boolean(c /= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a >>= b) && !!(c >>= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a **= b) && Boolean(c **= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a ? b : c) || !!(d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a ? b : c) || Boolean(d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a ? b : c) && !!(d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a ? b : c) && Boolean(d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a || b) || !!(c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a || b) || Boolean(c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a || b) && !!(c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a || b) && Boolean(c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a && b) || !!(c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a && b) || Boolean(c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a && b) && !!(c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a && b) && Boolean(c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a !== b) || !!(c !== d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a != b) || Boolean(c != d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!(a === b) && !!(c === d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (!!(a > b) || !!(c < d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(!a) || Boolean(+b)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (!!f(a) && !!b.c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantDoubleNegation"},
		},
		{
			"if (Boolean(a) || !!b) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantDoubleNegation"},
		},
		{
			"if (!!a && Boolean(b)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantBooleanCall"},
		},
		{
			"if ((!!a) || (Boolean(b))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation", "redundantBooleanCall"},
		},
		{
			"if (Boolean(a ?? b) || c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (Boolean?.(a ?? b) || c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ? Boolean(b) : c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ? b : Boolean(c)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ? b : Boolean(c ? d : e)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"const ternary = Boolean(bar ? !!baz : bat);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"const commaOperator = Boolean((bar, baz, !!bat));",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantDoubleNegation"},
		},
		{
			"for (let i = 0; (console.log(i), Boolean(i < 10)); i++) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"const nullishCoalescingOperator = Boolean(bar ?? Boolean(baz));",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
		{
			"if (a ? Boolean(b = c) : Boolean(d = e));",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"if (a ? Boolean((b, c)) : Boolean((d, e)));",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall", "redundantBooleanCall"},
		},
		{
			"\n\tfunction * generator() {\n\t    if (a ? Boolean(yield y) : x) {\n\t        return a;\n\t    };\n\t}\n\t",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"redundantBooleanCall"},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFindings(test,
				rule_testing.RunGiven(test, NoExtraBooleanCast, extraBooleanCastFile,
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
		options    rule.Given[NoExtraBooleanCastOptions]
	}{
		{"Boolean(bar, !!baz);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"var foo = !!bar;", rule.Given[NoExtraBooleanCastOptions]{}},
		{"function foo() { return !!bar; }", rule.Given[NoExtraBooleanCastOptions]{}},
		{"var foo = bar() ? !!baz : !!bat", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for(!!foo;;) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for(;; !!foo) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"var foo = Boolean(bar);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"function foo() { return Boolean(bar); }", rule.Given[NoExtraBooleanCastOptions]{}},
		{"var foo = bar() ? Boolean(baz) : Boolean(bat)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for(Boolean(foo);;) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for(;; Boolean(foo)) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (new Boolean(foo)) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if ((Boolean(1), 2)) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"var foo = bar || !!baz", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{"var foo = bar && !!baz", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{
			"var foo = bar || (baz && !!bat)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function foo() { return (!!bar || baz); }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = bar() ? (!!baz && bat) : (!!bat && qux)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"for(!!(foo && bar);;) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{"for(;; !!(foo || bar)) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{
			"var foo = Boolean(bar) || baz;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = bar || Boolean(baz);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = Boolean(bar) || Boolean(baz);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function foo() { return (Boolean(bar) || baz); }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = bar() ? Boolean(baz) || bat : Boolean(bat)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"for(Boolean(foo) || bar;;) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{
			"for(;; Boolean(foo) || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (new Boolean(foo) || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"if (!!foo || bar) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (!!foo || bar) {}", rule.Configured(NoExtraBooleanCastOptions{})},
		{"if (!!foo || bar) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: false})},
		{
			"if ((!!foo || bar) === baz) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"if (!!foo ?? bar) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{"var foo = bar || !!baz", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{"var foo = bar && !!baz", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{
			"var foo = bar || (baz && !!bat)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function foo() { return (!!bar || baz); }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = bar() ? (!!baz && bat) : (!!bat && qux)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"for(!!(foo && bar);;) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{"for(;; !!(foo || bar)) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{
			"var foo = Boolean(bar) || baz;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = bar || Boolean(baz);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = Boolean(bar) || Boolean(baz);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function foo() { return (Boolean(bar) || baz); }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = bar() ? Boolean(baz) || bat : Boolean(bat)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"for(Boolean(foo) || bar;;) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{
			"for(;; Boolean(foo) || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (new Boolean(foo) || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"if (!!foo || bar) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: false})},
		{
			"if ((!!foo || bar) === baz) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"if (!!foo ?? bar) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{"if ((1, Boolean(2), 3)) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test,
				rule_testing.RunGiven(test, NoExtraBooleanCast, extraBooleanCastFile,
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
		options    rule.Given[NoExtraBooleanCastOptions]
		wantSpans  []string
	}{
		{"if (!!foo) {}", rule.Given[NoExtraBooleanCastOptions]{}, []string{"!!foo"}},
		{"do {} while (!!foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"!!foo"}},
		{"!!!foo", rule.Given[NoExtraBooleanCastOptions]{}, []string{"!!foo"}},
		{"if (Boolean(foo)) {}", rule.Given[NoExtraBooleanCastOptions]{}, []string{"Boolean(foo)"}},
		// Only the inner negation. The outer call is not itself in a coercing position, so it is a
		// context rather than a finding, and a rule reporting both would be reporting the outer one
		// for no reason. Upstream's snapshot records exactly one diagnostic here, at column 9.
		{"Boolean(!!foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"!!foo"}},
		{"new Boolean(!!foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"!!foo"}},
		{"!!!!a", rule.Given[NoExtraBooleanCastOptions]{}, []string{"!!!a", "!!a"}},
		{"!Boolean(foo)", rule.Given[NoExtraBooleanCastOptions]{}, []string{"Boolean(foo)"}},
		// The same asymmetry for the call form: the inner call is the first argument of the outer
		// one, which coerces it, while the outer call sits in a statement position that does not.
		{"Boolean(Boolean(foo))", rule.Given[NoExtraBooleanCastOptions]{}, []string{"Boolean(foo)"}},
		{"if (Boolean?.(foo)) {};", rule.Given[NoExtraBooleanCastOptions]{}, []string{"Boolean?.(foo)"}},
		{"if ((Boolean(f(a))));", rule.Given[NoExtraBooleanCastOptions]{}, []string{"Boolean(f(a))"}},
		{"if (!!a && Boolean(b)) {}", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
			[]string{"!!a", "Boolean(b)"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunGiven(test, NoExtraBooleanCast, extraBooleanCastFile,
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
			decoded, decodeOptionsError := rule.DecodeOptionsInto[NoExtraBooleanCastOptions]()([]byte(testCase.rawJson))
			if decodeOptionsError != nil {
				test.Fatalf("decoding %s: %v", testCase.rawJson, decodeOptionsError)
			}
			if decoded.EnforceForInnerExpressions != testCase.want {
				test.Fatalf("EnforceForInnerExpressions was %v, wanted %v",
					decoded.EnforceForInnerExpressions, testCase.want)
			}
		})
	}
}

// A case written because a mutation survived the imported corpus, not because upstream has it.
//
// `??` cannot sit beside `&&` or `||` without parentheses. That is a grammar restriction rather than
// a precedence one, and in this tree it cannot be expressed as a precedence comparison at all:
// `ast.GetBinaryOperatorPrecedence` gives `??` and `||` the same value, 5, so the ordinary
// comparisons see them as equal and let both through. oxc's own precedence table separates them,
// which is why its version of this check reads as one more comparison and this one has to name the
// operators.
//
// Upstream's corpus reaches the guard only through `if (a ?? Boolean(b || c)) {}`, where the
// replacement is on the right at equal precedence, so the left-associativity branch above already
// returns true and the guard is never the reason. Deleting the guard therefore changed nothing any
// of the 382 repair comparisons could see.
//
// `&&` distinguishes them: it is precedence 6 against `??`'s 5, so neither earlier branch fires, and
// without the guard the repair emits `a ?? b && c`. That does not merely mean something different,
// it does not parse, which is the one failure an unattended fixer structurally cannot refuse.
func TestNoExtraBooleanCastParenthesisesLogicalOperandsUnderCoalescing(test *testing.T) {
	test.Parallel()

	const sourceText = "if (a ?? Boolean(b && c)) {}"

	result := rule_testing.RunWithOptions(test, NoExtraBooleanCast, extraBooleanCastFile, sourceText,
		NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})
	rule_testing.ExpectFixedSource(test, result, "if (a ?? (b && c)) {}")
}

// A second case written for a survivor, covering the other half of the token-boundary padding.
//
// Upstream's corpus exercises the *leading* half thoroughly, through the `yield!!a`, `void!Boolean()`
// and `typeof!Boolean()` family, where the cast being removed was the only thing separating a
// keyword from its operand. It never exercises the trailing half, so deleting that branch changed
// none of the 382 repair comparisons.
//
// `!Boolean()in x` reaches it. The whole `!Boolean()` collapses to the literal `true`, and the next
// character begins the `in` operator with no space in front of it, so without the trailing pad the
// repair writes `truein x`. That parses, as a reference to an identifier named `truein`, and then
// fails to compile against a name that does not exist. A fix producing source that parses and does
// not compile is the one failure the edit engine structurally cannot refuse, which is what makes
// this worth a fixture rather than a comment.
func TestNoExtraBooleanCastPadsTheTrailingTokenBoundary(test *testing.T) {
	test.Parallel()

	const sourceText = "!Boolean()in x"

	result := rule_testing.Run(test, NoExtraBooleanCast, extraBooleanCastFile, sourceText)
	rule_testing.ExpectFixedSource(test, result, "true in x")
}
