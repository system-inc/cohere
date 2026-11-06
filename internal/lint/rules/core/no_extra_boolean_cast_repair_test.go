package core

import (
	"sort"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// The Boolean-call repairs, asserted through the shared harness.
//
// These 199 entries are the ones whose only finding is a redundant `Boolean(...)`, so every repair
// is a Fix and `rule_testing.ExpectFixedSource` can apply them. It is the shelf's assertion and it
// refuses overlapping fixes rather than guessing which wins, which is stricter than anything written
// here would be.
//
// The expected output is upstream's own, from the `expect_fix` vector its Tester runs. That is the
// assertion the message-id cases structurally cannot make: a rule reporting the right finding at the
// wrong span, or writing the right text over the wrong bytes, passes all 441 of them and is broken.
func TestNoExtraBooleanCastFixesBooleanCalls(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		wantSource string
		options    rule.Given[NoExtraBooleanCastOptions]
	}{
		{"if (Boolean(foo)) {}", "if (foo) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (Boolean(foo))", "do {} while (foo)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (Boolean(foo)) {}", "while (foo) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(foo) ? bar : baz", "foo ? bar : baz", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; Boolean(foo);) {}", "for (; foo;) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(foo)", "!foo", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(foo && bar)", "!(foo && bar)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(foo + bar)", "!(foo + bar)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(+foo)", "!+foo", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(foo())", "!foo()", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(foo = bar)", "!(foo = bar)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean((foo, bar()));", "!(foo, bar());", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean();", "true;", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!(Boolean());", "true;", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (!Boolean()) { foo() }", "if (true) { foo() }", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (!Boolean()) { foo() }", "while (true) { foo() }", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"var foo = Boolean() ? bar() : baz()",
			"var foo = false ? bar() : baz()",
			rule.Given[NoExtraBooleanCastOptions]{},
		},
		{"if (Boolean()) { foo() }", "if (false) { foo() }", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (Boolean()) { foo() }", "while (false) { foo() }", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(Boolean(foo))", "Boolean(foo)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"void!Boolean()", "void true", rule.Given[NoExtraBooleanCastOptions]{}},
		{"void! Boolean()", "void true", rule.Given[NoExtraBooleanCastOptions]{}},
		{"typeof!Boolean()", "typeof true", rule.Given[NoExtraBooleanCastOptions]{}},
		{"(!Boolean())", "(true)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"+!Boolean()", "+true", rule.Given[NoExtraBooleanCastOptions]{}},
		{"void !Boolean()", "void true", rule.Given[NoExtraBooleanCastOptions]{}},
		{"void(!Boolean())", "void(true)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"void/**/!Boolean()", "void/**/true", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!/**/Boolean(foo)", "!/**/foo", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(foo)/**/", "!foo/**/", rule.Given[NoExtraBooleanCastOptions]{}},
		{"/**/!Boolean()", "/**/true", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean()/**/", "true/**/", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if(/**/Boolean());", "if(/**/false);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if(Boolean()/**/);", "if(false/**/);", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"if (Boolean(foo) || bar) {}",
			"if (foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"do {} while (Boolean(foo) || bar)",
			"do {} while (foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"while (Boolean(foo) || bar) {}",
			"while (foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"Boolean(foo) || bat ? bar : baz",
			"foo || bat ? bar : baz",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"for (; Boolean(foo) || bar;) {}",
			"for (; foo || bar;) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo) || bar",
			"!foo || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo && bar) || bat",
			"!(foo && bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo + bar) || bat",
			"!(foo + bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(+foo)  || bar",
			"!+foo  || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo()) || bar",
			"!foo() || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo() || bar)",
			"!(foo() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo = bar) || bat",
			"!(foo = bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean((foo, bar()) || bat);",
			"!((foo, bar()) || bat);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean() || bar;",
			"true || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!(Boolean()) || bar;",
			"true || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!Boolean() || bar) { foo() }",
			"if (true || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"while (!Boolean() || bar) { foo() }",
			"while (true || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = Boolean() || bar ? bar() : baz()",
			"var foo = false || bar ? bar() : baz()",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean() || bar) { foo() }",
			"if (false || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"while (Boolean() || bar) { foo() }",
			"while (false || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"void(!Boolean() || bar)",
			"void(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"void(! Boolean() || bar)",
			"void(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"typeof(!Boolean() || bar)",
			"typeof(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"(!Boolean() || bar)",
			"(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"void/**/(!Boolean() || bar)",
			"void/**/(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!/**/(Boolean(foo) || bar)",
			"!/**/(foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!(Boolean(foo)|| bar)/**/",
			"!(foo|| bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"/**/!Boolean()|| bar",
			"/**/true|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"(!Boolean()|| bar)/**/",
			"(true|| bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if(/**/Boolean()|| bar);",
			"if(/**/false|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if(Boolean()|| bar/**/);",
			"if(false|| bar/**/);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(foo) || bar) {}",
			"if (foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"do {} while (Boolean(foo) || bar)",
			"do {} while (foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"while (Boolean(foo) || bar) {}",
			"while (foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"Boolean(foo) || bat ? bar : baz",
			"foo || bat ? bar : baz",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"for (; Boolean(foo) || bar;) {}",
			"for (; foo || bar;) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo) || bar",
			"!foo || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo && bar) || bat",
			"!(foo && bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo + bar) || bat",
			"!(foo + bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(+foo)  || bar",
			"!+foo  || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo()) || bar",
			"!foo() || bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo() || bar)",
			"!(foo() || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean(foo = bar) || bat",
			"!(foo = bar) || bat",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean((foo, bar()) || bat);",
			"!((foo, bar()) || bat);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!Boolean() || bar;",
			"true || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!(Boolean()) || bar;",
			"true || bar;",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!Boolean() || bar) { foo() }",
			"if (true || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"while (!Boolean() || bar) { foo() }",
			"while (true || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"var foo = Boolean() || bar ? bar() : baz()",
			"var foo = false || bar ? bar() : baz()",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean() || bar) { foo() }",
			"if (false || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"while (Boolean() || bar) { foo() }",
			"while (false || bar) { foo() }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"void(!Boolean() || bar)",
			"void(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"void(! Boolean() || bar)",
			"void(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"typeof(!Boolean() || bar)",
			"typeof(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"(!Boolean() || bar)",
			"(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"void/**/(!Boolean() || bar)",
			"void/**/(true || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!/**/(Boolean(foo) || bar)",
			"!/**/(foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!(Boolean(foo)|| bar)/**/",
			"!(foo|| bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"/**/!Boolean()|| bar",
			"/**/true|| bar",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"(!Boolean()|| bar)/**/",
			"(true|| bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if(/**/Boolean()|| bar);",
			"if(/**/false|| bar);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if(Boolean()|| bar/**/);",
			"if(false|| bar/**/);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"Boolean(Boolean((a, b)))", "Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean((Boolean((a, b))))", "Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(Boolean(a = b))", "Boolean(a = b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(Boolean((a += b)))", "Boolean(a += b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(Boolean((a)))", "Boolean(a)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(Boolean((a, b)))", "new Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean((Boolean((a, b))))", "new Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(Boolean(a = b))", "new Boolean(a = b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(Boolean((a += b)))", "new Boolean(a += b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(Boolean((a)))", "new Boolean(a)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (Boolean((a, b)));", "if (a, b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (Boolean(a = b));", "if (a = b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (Boolean(a === b));", "if (a === b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (Boolean(f(a)));", "if (f(a));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if ((Boolean(f(a))));", "if ((f(a)));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (Boolean(a));", "if (a);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (Boolean((a, b)));", "while (a, b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (Boolean(a = b));", "while (a = b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (Boolean(a === b));", "while (a === b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (Boolean(f(a)));", "while (f(a));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while ((Boolean(f(a))));", "while ((f(a)));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (Boolean(a));", "while (a);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (Boolean((a, b)));", "do {} while (a, b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (Boolean(a = b));", "do {} while (a = b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (Boolean(a === b));", "do {} while (a === b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (Boolean(f(a)));", "do {} while (f(a));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while ((Boolean(f(a))));", "do {} while ((f(a)));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (Boolean(a));", "do {} while (a);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; Boolean((a, b)););", "for (; a, b;);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; Boolean(a = b););", "for (; a = b;);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; Boolean(a === b););", "for (; a === b;);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; Boolean(f(a)););", "for (; f(a););", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; (Boolean(f(a))););", "for (; (f(a)););", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; Boolean(a););", "for (; a;);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean((a, b)) ? c : d", "(a, b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(a -= b) ? c : d", "(a -= b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"(Boolean((a *= b))) ? c : d", "(a *= b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(a ? b : c) ? d : e", "(a ? b : c) ? d : e", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(a && b) ? c : d", "a && b ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(a < b) ? c : d", "a < b ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean((a >= b)) ? c : d", "a >= b ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(!a) ? b : c", "!a ? b : c", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(a.b) ? c : d", "a.b ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(a) ? b : c", "a ? b : c", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean((a, b))", "!(a, b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(a -= b)", "!(a -= b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean((a -= b))", "!(a -= b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!(Boolean(a -= b))", "!(a -= b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(a || b)", "!(a || b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(a && b)", "!(a && b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"var x = !Boolean(a > b)", "var x = !(a > b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(a ** b)", "!(a ** b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"async function f() { !Boolean(await a) }",
			"async function f() { !await a }",
			rule.Given[NoExtraBooleanCastOptions]{},
		},
		{"!Boolean(!a)", "!!a", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean((!a))", "!!a", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(!(a))", "!!(a)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!(Boolean(!a))", "!(!a)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean((-a))", "!-a", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(-(a))", "!-(a)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(a++)", "!a++", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!Boolean(a)", "!a", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"if (Boolean((a, b)) || Boolean((c, d))) {}",
			"if ((a, b) || (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean((a, b)) && Boolean((c, d))) {}",
			"if ((a, b) && (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a /= b) || Boolean(c /= d)) {}",
			"if ((a /= b) || (c /= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a **= b) && Boolean(c **= d)) {}",
			"if ((a **= b) && (c **= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a ? b : c) || Boolean(d ? e : f)) {}",
			"if ((a ? b : c) || (d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a ? b : c) && Boolean(d ? e : f)) {}",
			"if ((a ? b : c) && (d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a || b) || Boolean(c || d)) {}",
			"if (a || b || (c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a || b) && Boolean(c || d)) {}",
			"if ((a || b) && (c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a && b) || Boolean(c && d)) {}",
			"if (a && b || c && d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a && b) && Boolean(c && d)) {}",
			"if (a && b && (c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a != b) || Boolean(c != d)) {}",
			"if (a != b || c != d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(!a) || Boolean(+b)) {}",
			"if (!a || +b) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a ?? b) || c) {}",
			"if ((a ?? b) || c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"if (Boolean?.(foo)) {};", "if (foo) {};", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"if (Boolean?.(a ?? b) || c) {}",
			"if ((a ?? b) || c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"if (!Boolean(a as any)) { }", "if (!(a as any)) { }", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"if ((1, 2, Boolean(3))) {}",
			"if ((1, 2, 3)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (a ?? Boolean(b)) {}",
			"if (a ?? b) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (a ?? Boolean(b || c)) {}",
			"if (a ?? (b || c)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if ((a, b, c ?? (d, e, f ?? Boolean(g)))) {}",
			"if ((a, b, c ?? (d, e, f ?? g))) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean((a, b)) || Boolean((c, d))) {}",
			"if ((a, b) || (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean((a, b)) && Boolean((c, d))) {}",
			"if ((a, b) && (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a /= b) || Boolean(c /= d)) {}",
			"if ((a /= b) || (c /= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a **= b) && Boolean(c **= d)) {}",
			"if ((a **= b) && (c **= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a ? b : c) || Boolean(d ? e : f)) {}",
			"if ((a ? b : c) || (d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a ? b : c) && Boolean(d ? e : f)) {}",
			"if ((a ? b : c) && (d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a || b) || Boolean(c || d)) {}",
			"if (a || b || (c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a || b) && Boolean(c || d)) {}",
			"if ((a || b) && (c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a && b) || Boolean(c && d)) {}",
			"if (a && b || c && d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a && b) && Boolean(c && d)) {}",
			"if (a && b && (c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a != b) || Boolean(c != d)) {}",
			"if (a != b || c != d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(!a) || Boolean(+b)) {}",
			"if (!a || +b) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a ?? b) || c) {}",
			"if ((a ?? b) || c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean?.(a ?? b) || c) {}",
			"if ((a ?? b) || c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (a ? Boolean(b) : c) {}",
			"if (a ? b : c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (a ? b : Boolean(c)) {}",
			"if (a ? b : c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (a ? b : Boolean(c ? d : e)) {}",
			"if (a ? b : c ? d : e) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"for (let i = 0; (console.log(i), Boolean(i < 10)); i++) {}",
			"for (let i = 0; (console.log(i), i < 10); i++) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"const nullishCoalescingOperator = Boolean(bar ?? Boolean(baz));",
			"const nullishCoalescingOperator = Boolean(bar ?? baz);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (a ? Boolean(b = c) : Boolean(d = e));",
			"if (a ? b = c : d = e);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (a ? Boolean((b, c)) : Boolean((d, e)));",
			"if (a ? (b, c) : (d, e));",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"\n\tfunction * generator() {\n\t    if (a ? Boolean(yield y) : x) {\n\t        return a;\n\t    };\n\t}\n\t",
			"\n\tfunction * generator() {\n\t    if (a ? yield y : x) {\n\t        return a;\n\t    };\n\t}\n\t",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFixedSource(test,
				rule_testing.RunGiven(test, NoExtraBooleanCast, extraBooleanCastFile,
					testCase.sourceText, testCase.options), testCase.wantSource)
		})
	}
}

// The repairs that involve a double negation, which the shared harness cannot apply.
//
// `rule_testing.ExpectFixedSource` reads Fixes only, and correctly so: a Suggestion is not something the
// engine applies, and a harness that silently applied one would be asserting a rewrite that never
// happens in production. These 183 entries each carry at least one `!!` finding, whose repair is a
// Suggestion, so they are applied here instead.
//
// Upstream's expected output covers them because its own tester applies suggestions alongside fixes.
// Comparing against it is still the right assertion: it says what the offered repair would produce
// if a human took it, which is exactly what a suggestion promises.
func TestNoExtraBooleanCastSuggestsTheRightRepairs(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		wantSource string
		options    rule.Given[NoExtraBooleanCastOptions]
	}{
		{"if (!!foo) {}", "if (foo) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (!!foo)", "do {} while (foo)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (!!foo) {}", "while (foo) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!foo ? bar : baz", "foo ? bar : baz", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; !!foo;) {}", "for (; foo;) {}", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!foo", "!foo", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(!!foo)", "Boolean(foo)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(!!foo)", "new Boolean(foo)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(!!foo, bar)", "Boolean(foo, bar)", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"function *foo() { yield!!a ? b : c }",
			"function *foo() { yield a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
		},
		{
			"function *foo() { yield!! a ? b : c }",
			"function *foo() { yield a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
		},
		{
			"function *foo() { yield! !a ? b : c }",
			"function *foo() { yield a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
		},
		{
			"function *foo() { yield !!a ? b : c }",
			"function *foo() { yield a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
		},
		{
			"function *foo() { yield(!!a) ? b : c }",
			"function *foo() { yield(a) ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
		},
		{
			"function *foo() { yield/**/!!a ? b : c }",
			"function *foo() { yield/**/a ? b : c }",
			rule.Given[NoExtraBooleanCastOptions]{},
		},
		{"x=!!a ? b : c ", "x=a ? b : c ", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!/**/!!foo", "!/**/foo", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!foo/**/", "!foo/**/", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"if (!!foo || bar) {}",
			"if (foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!foo && bar) {}",
			"if (foo && bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if ((!!foo || bar) && bat) {}",
			"if ((foo || bar) && bat) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (foo && !!bar) {}",
			"if (foo && bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"do {} while (!!foo || bar)",
			"do {} while (foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"while (!!foo || bar) {}",
			"while (foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!!foo && bat ? bar : baz",
			"foo && bat ? bar : baz",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"for (; !!foo || bar;) {}",
			"for (; foo || bar;) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"!!!foo || bar", "!foo || bar", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{
			"Boolean(!!foo || bar)",
			"Boolean(foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"new Boolean(!!foo || bar)",
			"new Boolean(foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield(!!a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield(!! a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield(! !a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield (!!a || d) ? b : c }",
			"function *foo() { yield (a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield/**/(!!a || d) ? b : c }",
			"function *foo() { yield/**/(a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"x=!!a || d ? b : c ",
			"x=a || d ? b : c ",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!/**/(!!foo || bar)",
			"!/**/(foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!(!!foo || bar)/**/",
			"!(foo || bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (a && !!(b ? c : d)){}",
			"if (a && (b ? c : d)){}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield!!a || d ? b : c }",
			"function *foo() { yield a || d ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!foo || bar) {}",
			"if (foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!foo && bar) {}",
			"if (foo && bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if ((!!foo || bar) && bat) {}",
			"if ((foo || bar) && bat) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (foo && !!bar) {}",
			"if (foo && bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"do {} while (!!foo || bar)",
			"do {} while (foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"while (!!foo || bar) {}",
			"while (foo || bar) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!!foo && bat ? bar : baz",
			"foo && bat ? bar : baz",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"for (; !!foo || bar;) {}",
			"for (; foo || bar;) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"!!!foo || bar", "!foo || bar", rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true})},
		{
			"Boolean(!!foo || bar)",
			"Boolean(foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"new Boolean(!!foo || bar)",
			"new Boolean(foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield(!!a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield(!! a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield(! !a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield (!!a || d) ? b : c }",
			"function *foo() { yield (a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield/**/(!!a || d) ? b : c }",
			"function *foo() { yield/**/(a || d) ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"x=!!a || d ? b : c ",
			"x=a || d ? b : c ",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!/**/(!!foo || bar)",
			"!/**/(foo || bar)",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"!(!!foo || bar)/**/",
			"!(foo || bar)/**/",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (a && !!(b ? c : d)){}",
			"if (a && (b ? c : d)){}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"function *foo() { yield!!a || d ? b : c }",
			"function *foo() { yield a || d ? b : c }",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{"Boolean(!!(a, b))", "Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean((!!(a, b)))", "Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(!(!(a, b)))", "Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean((!(!(a, b))))", "Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(!!(a = b))", "Boolean(a = b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean((!!(a = b)))", "Boolean((a = b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(!!(a === b))", "Boolean(a === b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(!!((a !== b)))", "Boolean(a !== b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean(!!a.b)", "Boolean(a.b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"Boolean((!!(a)))", "Boolean((a))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(!!(a, b))", "new Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean((!!(a, b)))", "new Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(!(!(a, b)))", "new Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean((!(!(a, b))))", "new Boolean((a, b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(!!(a = b))", "new Boolean(a = b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean((!!(a = b)))", "new Boolean((a = b))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(!!(a === b))", "new Boolean(a === b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(!!((a !== b)))", "new Boolean(a !== b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean(!!a.b)", "new Boolean(a.b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"new Boolean((!!(a)))", "new Boolean((a))", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (!!(a, b));", "if (a, b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (!(!(a, b)));", "if (a, b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (!!(a = b));", "if (a = b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (!!(a > b));", "if (a > b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (!!f(a));", "if (f(a));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (!!(f(a)));", "if (f(a));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if ((!!f(a)));", "if ((f(a)));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"if (!!a);", "if (a);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (!!(a, b));", "while (a, b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (!(!(a, b)));", "while (a, b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (!!(a = b));", "while (a = b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (!!(a > b));", "while (a > b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (!!f(a));", "while (f(a));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (!!(f(a)));", "while (f(a));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while ((!!f(a)));", "while ((f(a)));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"while (!!a);", "while (a);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (!!(a, b));", "do {} while (a, b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (!(!(a, b)));", "do {} while (a, b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (!!(a = b));", "do {} while (a = b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (!!(a > b));", "do {} while (a > b);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (!!f(a));", "do {} while (f(a));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (!!(f(a)));", "do {} while (f(a));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while ((!!f(a)));", "do {} while ((f(a)));", rule.Given[NoExtraBooleanCastOptions]{}},
		{"do {} while (!!a);", "do {} while (a);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; !!(a, b););", "for (; a, b;);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; !(!(a, b)););", "for (; a, b;);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; !!(a = b););", "for (; a = b;);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; !!(a > b););", "for (; a > b;);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; !!f(a););", "for (; f(a););", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; !!(f(a)););", "for (; f(a););", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; (!!f(a)););", "for (; (f(a)););", rule.Given[NoExtraBooleanCastOptions]{}},
		{"for (; !!a;);", "for (; a;);", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!(a, b) ? c : d", "(a, b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"(!!(a, b)) ? c : d", "(a, b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!(a = b) ? c : d", "(a = b) ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!(a ? b : c) ? d : e", "(a ? b : c) ? d : e", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!(a || b) ? c : d", "a || b ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!(a === b) ? c : d", "a === b ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!((a !== b)) ? c : d", "a !== b ? c : d", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!+a ? b : c", "+a ? b : c", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!+(a) ? b : c", "+(a) ? b : c", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!f(a) ? b : c", "f(a) ? b : c", rule.Given[NoExtraBooleanCastOptions]{}},
		{"(!!f(a)) ? b : c", "(f(a)) ? b : c", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!a ? b : c", "a ? b : c", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!(a, b)", "!(a, b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!(a = b)", "!(a = b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!(!(a += b))", "!(a += b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!(!!(a += b))", "!(a += b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!(a || b)", "!(a || b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!(a && b)", "!(a && b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!(a != b)", "!(a != b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!(a === b)", "!(a === b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!(a - b)", "!(a - b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!(a ** b)", "!(a ** b)", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"async function f() { !!!(await a) }",
			"async function f() { !await a }",
			rule.Given[NoExtraBooleanCastOptions]{},
		},
		{"!!!!a", "!!a", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!(!(!a))", "!!a", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!+a", "!+a", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!(!+a)", "!+a", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!(!!+a)", "!(+a)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!f(a)", "!f(a)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!(f(a))", "!f(a)", rule.Given[NoExtraBooleanCastOptions]{}},
		{"!!!a", "!a", rule.Given[NoExtraBooleanCastOptions]{}},
		{
			"if (!!(a, b) || !!(c, d)) {}",
			"if ((a, b) || (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if ((!!((a, b))) || (!!((c, d)))) {}",
			"if ((a, b) || (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a, b) && !!(c, d)) {}",
			"if ((a, b) && (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if ((!!((a, b))) && (!!((c, d)))) {}",
			"if ((a, b) && (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a = b) || !!(c = d)) {}",
			"if ((a = b) || (c = d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a >>= b) && !!(c >>= d)) {}",
			"if ((a >>= b) && (c >>= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a ? b : c) || !!(d ? e : f)) {}",
			"if ((a ? b : c) || (d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a ? b : c) && !!(d ? e : f)) {}",
			"if ((a ? b : c) && (d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a || b) || !!(c || d)) {}",
			"if (a || b || (c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a || b) && !!(c || d)) {}",
			"if ((a || b) && (c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a && b) || !!(c && d)) {}",
			"if (a && b || c && d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a && b) && !!(c && d)) {}",
			"if (a && b && (c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a !== b) || !!(c !== d)) {}",
			"if (a !== b || c !== d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a === b) && !!(c === d)) {}",
			"if (a === b && c === d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a > b) || !!(c < d)) {}",
			"if (a > b || c < d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!f(a) && !!b.c) {}",
			"if (f(a) && b.c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a) || !!b) {}",
			"if (a || b) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!a && Boolean(b)) {}",
			"if (a && b) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if ((!!a) || (Boolean(b))) {}",
			"if ((a) || (b)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a, b) || !!(c, d)) {}",
			"if ((a, b) || (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if ((!!((a, b))) || (!!((c, d)))) {}",
			"if ((a, b) || (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a, b) && !!(c, d)) {}",
			"if ((a, b) && (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if ((!!((a, b))) && (!!((c, d)))) {}",
			"if ((a, b) && (c, d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a = b) || !!(c = d)) {}",
			"if ((a = b) || (c = d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a >>= b) && !!(c >>= d)) {}",
			"if ((a >>= b) && (c >>= d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a ? b : c) || !!(d ? e : f)) {}",
			"if ((a ? b : c) || (d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a ? b : c) && !!(d ? e : f)) {}",
			"if ((a ? b : c) && (d ? e : f)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a || b) || !!(c || d)) {}",
			"if (a || b || (c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a || b) && !!(c || d)) {}",
			"if ((a || b) && (c || d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a && b) || !!(c && d)) {}",
			"if (a && b || c && d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a && b) && !!(c && d)) {}",
			"if (a && b && (c && d)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a !== b) || !!(c !== d)) {}",
			"if (a !== b || c !== d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a === b) && !!(c === d)) {}",
			"if (a === b && c === d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!(a > b) || !!(c < d)) {}",
			"if (a > b || c < d) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!f(a) && !!b.c) {}",
			"if (f(a) && b.c) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (Boolean(a) || !!b) {}",
			"if (a || b) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if (!!a && Boolean(b)) {}",
			"if (a && b) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"if ((!!a) || (Boolean(b))) {}",
			"if ((a) || (b)) {}",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"const ternary = Boolean(bar ? !!baz : bat);",
			"const ternary = Boolean(bar ? baz : bat);",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
		{
			"const commaOperator = Boolean((bar, baz, !!bat));",
			"const commaOperator = Boolean((bar, baz, bat));",
			rule.Configured(NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}),
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunGiven(test, NoExtraBooleanCast, extraBooleanCastFile,
				testCase.sourceText, testCase.options)
			if got := applyEveryRepair(testCase.sourceText, result); got != testCase.wantSource {
				test.Fatalf("applying the repairs gave %q, wanted %q", got, testCase.wantSource)
			}
		})
	}
}

// applyEveryRepair rewrites source with every repair a run proposed, back to front.
//
// Both kinds, because upstream's expected-output vector was produced by a tester that applies
// suggestions alongside fixes, so comparing against it with fixes only would fail on the 177 entries
// whose only repair is the `!!` removal.
//
// Back to front so no surviving edit is measured against text that moved under it, which is the same
// ordering `internal/fix` uses. Overlapping repairs are skipped rather than applied: a nested cast
// such as `Boolean(Boolean(a))` reports twice with the outer repair spanning the inner one, and
// upstream's fixer lands one pass too.
func applyEveryRepair(sourceText string, result rule_testing.Result) string {
	type edit struct {
		start int
		end   int
		text  string
	}

	edits := []edit{}
	for _, diagnostic := range result.Diagnostics {
		for _, repair := range diagnostic.Fixes {
			edits = append(edits, edit{repair.Range.Pos(), repair.Range.End(), repair.Text})
		}
		for _, suggestion := range diagnostic.Suggestions {
			for _, repair := range suggestion.Fixes {
				edits = append(edits, edit{repair.Range.Pos(), repair.Range.End(), repair.Text})
			}
		}
	}
	sort.Slice(edits, func(first int, second int) bool {
		if edits[first].start != edits[second].start {
			return edits[first].start < edits[second].start
		}
		return edits[first].end > edits[second].end
	})

	// Keep the first of any overlapping group, which after the sort above is the outermost.
	kept := []edit{}
	lastEnd := -1
	for _, candidate := range edits {
		if candidate.start < lastEnd {
			continue
		}
		kept = append(kept, candidate)
		lastEnd = candidate.end
	}

	rewritten := sourceText
	for index := len(kept) - 1; index >= 0; index-- {
		rewritten = rewritten[:kept[index].start] + kept[index].text + rewritten[kept[index].end:]
	}
	return rewritten
}

// The two findings carry different repair kinds, and collapsing them is the defect this pins.
//
// `Boolean(x)` unwrapping is a Fix the engine applies unattended. `!!x` removal is a Suggestion a
// human chooses. Upstream declares exactly this with `conditional_fix_or_conditional_suggestion`,
// which is the only occurrence of that declaration in its 946 rules, so there is no second rule to
// copy the shape from and nothing else in this package would notice if the two were swapped.
func TestNoExtraBooleanCastSplitsFixFromSuggestion(test *testing.T) {
	test.Parallel()

	booleanCall := rule_testing.Run(test, NoExtraBooleanCast, extraBooleanCastFile, "if (Boolean(foo)) {}")
	if len(booleanCall.Diagnostics) != 1 {
		test.Fatalf("wanted one finding for the Boolean call, got %d", len(booleanCall.Diagnostics))
	}
	if len(booleanCall.Diagnostics[0].Fixes) != 1 {
		test.Fatalf("the Boolean call should carry exactly one Fix, got %d",
			len(booleanCall.Diagnostics[0].Fixes))
	}
	if len(booleanCall.Diagnostics[0].Suggestions) != 0 {
		test.Fatalf("the Boolean call should carry no Suggestion, got %d",
			len(booleanCall.Diagnostics[0].Suggestions))
	}

	doubleNegation := rule_testing.Run(test, NoExtraBooleanCast, extraBooleanCastFile, "if (!!foo) {}")
	if len(doubleNegation.Diagnostics) != 1 {
		test.Fatalf("wanted one finding for the double negation, got %d",
			len(doubleNegation.Diagnostics))
	}
	if len(doubleNegation.Diagnostics[0].Fixes) != 0 {
		test.Fatalf("the double negation should carry no Fix, got %d",
			len(doubleNegation.Diagnostics[0].Fixes))
	}
	if len(doubleNegation.Diagnostics[0].Suggestions) != 1 {
		test.Fatalf("the double negation should carry exactly one Suggestion, got %d",
			len(doubleNegation.Diagnostics[0].Suggestions))
	}
}

// The findings upstream emits with no repair at all, which the fix vector cannot show.
//
// Upstream's `expect_fix` vector is an allowlist rather than a census, so a case missing from it
// proves nothing on its own. These two are read from the rule body instead: more than one argument,
// or a spread, hits `fixer.noop()`, because there is no single expression `Boolean(...foo)` reduces
// to without knowing what `foo` holds.
func TestNoExtraBooleanCastDeclinesToRepairMultipleArguments(test *testing.T) {
	test.Parallel()

	cases := []string{"!Boolean(...foo);", "!Boolean(foo, bar());"}

	for _, sourceText := range cases {
		test.Run(sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoExtraBooleanCast, extraBooleanCastFile, sourceText)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("wanted one finding, got %d", len(result.Diagnostics))
			}
			if len(result.Diagnostics[0].Fixes) != 0 ||
				len(result.Diagnostics[0].Suggestions) != 0 {
				test.Fatalf("wanted a finding with no repair, got %d fixes and %d suggestions",
					len(result.Diagnostics[0].Fixes), len(result.Diagnostics[0].Suggestions))
			}
		})
	}
}
