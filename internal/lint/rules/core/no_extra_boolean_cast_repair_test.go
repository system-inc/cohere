package core

import (
	"sort"
	"testing"

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
		options    any
	}{
		{"if (Boolean(foo)) {}", "if (foo) {}", nil},
		{"do {} while (Boolean(foo))", "do {} while (foo)", nil},
		{"while (Boolean(foo)) {}", "while (foo) {}", nil},
		{"Boolean(foo) ? bar : baz", "foo ? bar : baz", nil},
		{"for (; Boolean(foo);) {}", "for (; foo;) {}", nil},
		{"!Boolean(foo)", "!foo", nil},
		{"!Boolean(foo && bar)", "!(foo && bar)", nil},
		{"!Boolean(foo + bar)", "!(foo + bar)", nil},
		{"!Boolean(+foo)", "!+foo", nil},
		{"!Boolean(foo())", "!foo()", nil},
		{"!Boolean(foo = bar)", "!(foo = bar)", nil},
		{"!Boolean((foo, bar()));", "!(foo, bar());", nil},
		{"!Boolean();", "true;", nil},
		{"!(Boolean());", "true;", nil},
		{"if (!Boolean()) { foo() }", "if (true) { foo() }", nil},
		{"while (!Boolean()) { foo() }", "while (true) { foo() }", nil},
		{"var foo = Boolean() ? bar() : baz()", "var foo = false ? bar() : baz()", nil},
		{"if (Boolean()) { foo() }", "if (false) { foo() }", nil},
		{"while (Boolean()) { foo() }", "while (false) { foo() }", nil},
		{"Boolean(Boolean(foo))", "Boolean(foo)", nil},
		{"void!Boolean()", "void true", nil},
		{"void! Boolean()", "void true", nil},
		{"typeof!Boolean()", "typeof true", nil},
		{"(!Boolean())", "(true)", nil},
		{"+!Boolean()", "+true", nil},
		{"void !Boolean()", "void true", nil},
		{"void(!Boolean())", "void(true)", nil},
		{"void/**/!Boolean()", "void/**/true", nil},
		{"!/**/Boolean(foo)", "!/**/foo", nil},
		{"!Boolean(foo)/**/", "!foo/**/", nil},
		{"/**/!Boolean()", "/**/true", nil},
		{"!Boolean()/**/", "true/**/", nil},
		{"if(/**/Boolean());", "if(/**/false);", nil},
		{"if(Boolean()/**/);", "if(false/**/);", nil},
		{
			"if (Boolean(foo) || bar) {}",
			"if (foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"do {} while (Boolean(foo) || bar)",
			"do {} while (foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"while (Boolean(foo) || bar) {}",
			"while (foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"Boolean(foo) || bat ? bar : baz",
			"foo || bat ? bar : baz",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"for (; Boolean(foo) || bar;) {}",
			"for (; foo || bar;) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!Boolean(foo) || bar", "!foo || bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"!Boolean(foo && bar) || bat",
			"!(foo && bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"!Boolean(foo + bar) || bat",
			"!(foo + bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!Boolean(+foo)  || bar", "!+foo  || bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!Boolean(foo()) || bar", "!foo() || bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!Boolean(foo() || bar)", "!(foo() || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"!Boolean(foo = bar) || bat",
			"!(foo = bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"!Boolean((foo, bar()) || bat);",
			"!((foo, bar()) || bat);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!Boolean() || bar;", "true || bar;", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!(Boolean()) || bar;", "true || bar;", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if (!Boolean() || bar) { foo() }",
			"if (true || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"while (!Boolean() || bar) { foo() }",
			"while (true || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"var foo = Boolean() || bar ? bar() : baz()",
			"var foo = false || bar ? bar() : baz()",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean() || bar) { foo() }",
			"if (false || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"while (Boolean() || bar) { foo() }",
			"while (false || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"void(!Boolean() || bar)", "void(true || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"void(! Boolean() || bar)", "void(true || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"typeof(!Boolean() || bar)",
			"typeof(true || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"(!Boolean() || bar)", "(true || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"void/**/(!Boolean() || bar)",
			"void/**/(true || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"!/**/(Boolean(foo) || bar)",
			"!/**/(foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!(Boolean(foo)|| bar)/**/", "!(foo|| bar)/**/", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"/**/!Boolean()|| bar", "/**/true|| bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"(!Boolean()|| bar)/**/", "(true|| bar)/**/", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if(/**/Boolean()|| bar);",
			"if(/**/false|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if(Boolean()|| bar/**/);",
			"if(false|| bar/**/);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(foo) || bar) {}",
			"if (foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"do {} while (Boolean(foo) || bar)",
			"do {} while (foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"while (Boolean(foo) || bar) {}",
			"while (foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"Boolean(foo) || bat ? bar : baz",
			"foo || bat ? bar : baz",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"for (; Boolean(foo) || bar;) {}",
			"for (; foo || bar;) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!Boolean(foo) || bar", "!foo || bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"!Boolean(foo && bar) || bat",
			"!(foo && bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"!Boolean(foo + bar) || bat",
			"!(foo + bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!Boolean(+foo)  || bar", "!+foo  || bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!Boolean(foo()) || bar", "!foo() || bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!Boolean(foo() || bar)", "!(foo() || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"!Boolean(foo = bar) || bat",
			"!(foo = bar) || bat",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"!Boolean((foo, bar()) || bat);",
			"!((foo, bar()) || bat);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!Boolean() || bar;", "true || bar;", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!(Boolean()) || bar;", "true || bar;", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if (!Boolean() || bar) { foo() }",
			"if (true || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"while (!Boolean() || bar) { foo() }",
			"while (true || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"var foo = Boolean() || bar ? bar() : baz()",
			"var foo = false || bar ? bar() : baz()",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean() || bar) { foo() }",
			"if (false || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"while (Boolean() || bar) { foo() }",
			"while (false || bar) { foo() }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"void(!Boolean() || bar)", "void(true || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"void(! Boolean() || bar)", "void(true || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"typeof(!Boolean() || bar)",
			"typeof(true || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"(!Boolean() || bar)", "(true || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"void/**/(!Boolean() || bar)",
			"void/**/(true || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"!/**/(Boolean(foo) || bar)",
			"!/**/(foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!(Boolean(foo)|| bar)/**/", "!(foo|| bar)/**/", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"/**/!Boolean()|| bar", "/**/true|| bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"(!Boolean()|| bar)/**/", "(true|| bar)/**/", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if(/**/Boolean()|| bar);",
			"if(/**/false|| bar);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if(Boolean()|| bar/**/);",
			"if(false|| bar/**/);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"Boolean(Boolean((a, b)))", "Boolean((a, b))", nil},
		{"Boolean((Boolean((a, b))))", "Boolean((a, b))", nil},
		{"Boolean(Boolean(a = b))", "Boolean(a = b)", nil},
		{"Boolean(Boolean((a += b)))", "Boolean(a += b)", nil},
		{"Boolean(Boolean((a)))", "Boolean(a)", nil},
		{"new Boolean(Boolean((a, b)))", "new Boolean((a, b))", nil},
		{"new Boolean((Boolean((a, b))))", "new Boolean((a, b))", nil},
		{"new Boolean(Boolean(a = b))", "new Boolean(a = b)", nil},
		{"new Boolean(Boolean((a += b)))", "new Boolean(a += b)", nil},
		{"new Boolean(Boolean((a)))", "new Boolean(a)", nil},
		{"if (Boolean((a, b)));", "if (a, b);", nil},
		{"if (Boolean(a = b));", "if (a = b);", nil},
		{"if (Boolean(a === b));", "if (a === b);", nil},
		{"if (Boolean(f(a)));", "if (f(a));", nil},
		{"if ((Boolean(f(a))));", "if ((f(a)));", nil},
		{"if (Boolean(a));", "if (a);", nil},
		{"while (Boolean((a, b)));", "while (a, b);", nil},
		{"while (Boolean(a = b));", "while (a = b);", nil},
		{"while (Boolean(a === b));", "while (a === b);", nil},
		{"while (Boolean(f(a)));", "while (f(a));", nil},
		{"while ((Boolean(f(a))));", "while ((f(a)));", nil},
		{"while (Boolean(a));", "while (a);", nil},
		{"do {} while (Boolean((a, b)));", "do {} while (a, b);", nil},
		{"do {} while (Boolean(a = b));", "do {} while (a = b);", nil},
		{"do {} while (Boolean(a === b));", "do {} while (a === b);", nil},
		{"do {} while (Boolean(f(a)));", "do {} while (f(a));", nil},
		{"do {} while ((Boolean(f(a))));", "do {} while ((f(a)));", nil},
		{"do {} while (Boolean(a));", "do {} while (a);", nil},
		{"for (; Boolean((a, b)););", "for (; a, b;);", nil},
		{"for (; Boolean(a = b););", "for (; a = b;);", nil},
		{"for (; Boolean(a === b););", "for (; a === b;);", nil},
		{"for (; Boolean(f(a)););", "for (; f(a););", nil},
		{"for (; (Boolean(f(a))););", "for (; (f(a)););", nil},
		{"for (; Boolean(a););", "for (; a;);", nil},
		{"Boolean((a, b)) ? c : d", "(a, b) ? c : d", nil},
		{"Boolean(a -= b) ? c : d", "(a -= b) ? c : d", nil},
		{"(Boolean((a *= b))) ? c : d", "(a *= b) ? c : d", nil},
		{"Boolean(a ? b : c) ? d : e", "(a ? b : c) ? d : e", nil},
		{"Boolean(a && b) ? c : d", "a && b ? c : d", nil},
		{"Boolean(a < b) ? c : d", "a < b ? c : d", nil},
		{"Boolean((a >= b)) ? c : d", "a >= b ? c : d", nil},
		{"Boolean(!a) ? b : c", "!a ? b : c", nil},
		{"Boolean(a.b) ? c : d", "a.b ? c : d", nil},
		{"Boolean(a) ? b : c", "a ? b : c", nil},
		{"!Boolean((a, b))", "!(a, b)", nil},
		{"!Boolean(a -= b)", "!(a -= b)", nil},
		{"!Boolean((a -= b))", "!(a -= b)", nil},
		{"!(Boolean(a -= b))", "!(a -= b)", nil},
		{"!Boolean(a || b)", "!(a || b)", nil},
		{"!Boolean(a && b)", "!(a && b)", nil},
		{"var x = !Boolean(a > b)", "var x = !(a > b)", nil},
		{"!Boolean(a ** b)", "!(a ** b)", nil},
		{"async function f() { !Boolean(await a) }", "async function f() { !await a }", nil},
		{"!Boolean(!a)", "!!a", nil},
		{"!Boolean((!a))", "!!a", nil},
		{"!Boolean(!(a))", "!!(a)", nil},
		{"!(Boolean(!a))", "!(!a)", nil},
		{"!Boolean((-a))", "!-a", nil},
		{"!Boolean(-(a))", "!-(a)", nil},
		{"!Boolean(a++)", "!a++", nil},
		{"!Boolean(a)", "!a", nil},
		{
			"if (Boolean((a, b)) || Boolean((c, d))) {}",
			"if ((a, b) || (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean((a, b)) && Boolean((c, d))) {}",
			"if ((a, b) && (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a /= b) || Boolean(c /= d)) {}",
			"if ((a /= b) || (c /= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a **= b) && Boolean(c **= d)) {}",
			"if ((a **= b) && (c **= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a ? b : c) || Boolean(d ? e : f)) {}",
			"if ((a ? b : c) || (d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a ? b : c) && Boolean(d ? e : f)) {}",
			"if ((a ? b : c) && (d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a || b) || Boolean(c || d)) {}",
			"if (a || b || (c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a || b) && Boolean(c || d)) {}",
			"if ((a || b) && (c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a && b) || Boolean(c && d)) {}",
			"if (a && b || c && d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a && b) && Boolean(c && d)) {}",
			"if (a && b && (c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a != b) || Boolean(c != d)) {}",
			"if (a != b || c != d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(!a) || Boolean(+b)) {}",
			"if (!a || +b) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a ?? b) || c) {}",
			"if ((a ?? b) || c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"if (Boolean?.(foo)) {};", "if (foo) {};", nil},
		{
			"if (Boolean?.(a ?? b) || c) {}",
			"if ((a ?? b) || c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"if (!Boolean(a as any)) { }", "if (!(a as any)) { }", nil},
		{
			"if ((1, 2, Boolean(3))) {}",
			"if ((1, 2, 3)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"if (a ?? Boolean(b)) {}", "if (a ?? b) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if (a ?? Boolean(b || c)) {}",
			"if (a ?? (b || c)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if ((a, b, c ?? (d, e, f ?? Boolean(g)))) {}",
			"if ((a, b, c ?? (d, e, f ?? g))) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean((a, b)) || Boolean((c, d))) {}",
			"if ((a, b) || (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean((a, b)) && Boolean((c, d))) {}",
			"if ((a, b) && (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a /= b) || Boolean(c /= d)) {}",
			"if ((a /= b) || (c /= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a **= b) && Boolean(c **= d)) {}",
			"if ((a **= b) && (c **= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a ? b : c) || Boolean(d ? e : f)) {}",
			"if ((a ? b : c) || (d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a ? b : c) && Boolean(d ? e : f)) {}",
			"if ((a ? b : c) && (d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a || b) || Boolean(c || d)) {}",
			"if (a || b || (c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a || b) && Boolean(c || d)) {}",
			"if ((a || b) && (c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a && b) || Boolean(c && d)) {}",
			"if (a && b || c && d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a && b) && Boolean(c && d)) {}",
			"if (a && b && (c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a != b) || Boolean(c != d)) {}",
			"if (a != b || c != d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(!a) || Boolean(+b)) {}",
			"if (!a || +b) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean(a ?? b) || c) {}",
			"if ((a ?? b) || c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (Boolean?.(a ?? b) || c) {}",
			"if ((a ?? b) || c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (a ? Boolean(b) : c) {}",
			"if (a ? b : c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (a ? b : Boolean(c)) {}",
			"if (a ? b : c) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (a ? b : Boolean(c ? d : e)) {}",
			"if (a ? b : c ? d : e) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"for (let i = 0; (console.log(i), Boolean(i < 10)); i++) {}",
			"for (let i = 0; (console.log(i), i < 10); i++) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"const nullishCoalescingOperator = Boolean(bar ?? Boolean(baz));",
			"const nullishCoalescingOperator = Boolean(bar ?? baz);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (a ? Boolean(b = c) : Boolean(d = e));",
			"if (a ? b = c : d = e);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (a ? Boolean((b, c)) : Boolean((d, e)));",
			"if (a ? (b, c) : (d, e));",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"\n\tfunction * generator() {\n\t    if (a ? Boolean(yield y) : x) {\n\t        return a;\n\t    };\n\t}\n\t",
			"\n\tfunction * generator() {\n\t    if (a ? yield y : x) {\n\t        return a;\n\t    };\n\t}\n\t",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectFixedSource(test,
				rule_testing.RunWithOptions(test, NoExtraBooleanCast, extraBooleanCastFile,
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
		options    any
	}{
		{"if (!!foo) {}", "if (foo) {}", nil},
		{"do {} while (!!foo)", "do {} while (foo)", nil},
		{"while (!!foo) {}", "while (foo) {}", nil},
		{"!!foo ? bar : baz", "foo ? bar : baz", nil},
		{"for (; !!foo;) {}", "for (; foo;) {}", nil},
		{"!!!foo", "!foo", nil},
		{"Boolean(!!foo)", "Boolean(foo)", nil},
		{"new Boolean(!!foo)", "new Boolean(foo)", nil},
		{"Boolean(!!foo, bar)", "Boolean(foo, bar)", nil},
		{"function *foo() { yield!!a ? b : c }", "function *foo() { yield a ? b : c }", nil},
		{"function *foo() { yield!! a ? b : c }", "function *foo() { yield a ? b : c }", nil},
		{"function *foo() { yield! !a ? b : c }", "function *foo() { yield a ? b : c }", nil},
		{"function *foo() { yield !!a ? b : c }", "function *foo() { yield a ? b : c }", nil},
		{"function *foo() { yield(!!a) ? b : c }", "function *foo() { yield(a) ? b : c }", nil},
		{"function *foo() { yield/**/!!a ? b : c }", "function *foo() { yield/**/a ? b : c }", nil},
		{"x=!!a ? b : c ", "x=a ? b : c ", nil},
		{"!/**/!!foo", "!/**/foo", nil},
		{"!!!foo/**/", "!foo/**/", nil},
		{"if (!!foo || bar) {}", "if (foo || bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (!!foo && bar) {}", "if (foo && bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if ((!!foo || bar) && bat) {}",
			"if ((foo || bar) && bat) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"if (foo && !!bar) {}", "if (foo && bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"do {} while (!!foo || bar)",
			"do {} while (foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"while (!!foo || bar) {}",
			"while (foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"!!foo && bat ? bar : baz",
			"foo && bat ? bar : baz",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"for (; !!foo || bar;) {}",
			"for (; foo || bar;) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!!!foo || bar", "!foo || bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"Boolean(!!foo || bar)", "Boolean(foo || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"new Boolean(!!foo || bar)",
			"new Boolean(foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield(!!a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield(!! a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield(! !a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield (!!a || d) ? b : c }",
			"function *foo() { yield (a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield/**/(!!a || d) ? b : c }",
			"function *foo() { yield/**/(a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"x=!!a || d ? b : c ", "x=a || d ? b : c ", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!/**/(!!foo || bar)", "!/**/(foo || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!(!!foo || bar)/**/", "!(foo || bar)/**/", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if (a && !!(b ? c : d)){}",
			"if (a && (b ? c : d)){}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield!!a || d ? b : c }",
			"function *foo() { yield a || d ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"if (!!foo || bar) {}", "if (foo || bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (!!foo && bar) {}", "if (foo && bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if ((!!foo || bar) && bat) {}",
			"if ((foo || bar) && bat) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"if (foo && !!bar) {}", "if (foo && bar) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"do {} while (!!foo || bar)",
			"do {} while (foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"while (!!foo || bar) {}",
			"while (foo || bar) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"!!foo && bat ? bar : baz",
			"foo && bat ? bar : baz",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"for (; !!foo || bar;) {}",
			"for (; foo || bar;) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"!!!foo || bar", "!foo || bar", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"Boolean(!!foo || bar)", "Boolean(foo || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"new Boolean(!!foo || bar)",
			"new Boolean(foo || bar)",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield(!!a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield(!! a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield(! !a || d) ? b : c }",
			"function *foo() { yield(a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield (!!a || d) ? b : c }",
			"function *foo() { yield (a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield/**/(!!a || d) ? b : c }",
			"function *foo() { yield/**/(a || d) ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"x=!!a || d ? b : c ", "x=a || d ? b : c ", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!/**/(!!foo || bar)", "!/**/(foo || bar)", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"!(!!foo || bar)/**/", "!(foo || bar)/**/", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if (a && !!(b ? c : d)){}",
			"if (a && (b ? c : d)){}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"function *foo() { yield!!a || d ? b : c }",
			"function *foo() { yield a || d ? b : c }",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"Boolean(!!(a, b))", "Boolean((a, b))", nil},
		{"Boolean((!!(a, b)))", "Boolean((a, b))", nil},
		{"Boolean(!(!(a, b)))", "Boolean((a, b))", nil},
		{"Boolean((!(!(a, b))))", "Boolean((a, b))", nil},
		{"Boolean(!!(a = b))", "Boolean(a = b)", nil},
		{"Boolean((!!(a = b)))", "Boolean((a = b))", nil},
		{"Boolean(!!(a === b))", "Boolean(a === b)", nil},
		{"Boolean(!!((a !== b)))", "Boolean(a !== b)", nil},
		{"Boolean(!!a.b)", "Boolean(a.b)", nil},
		{"Boolean((!!(a)))", "Boolean((a))", nil},
		{"new Boolean(!!(a, b))", "new Boolean((a, b))", nil},
		{"new Boolean((!!(a, b)))", "new Boolean((a, b))", nil},
		{"new Boolean(!(!(a, b)))", "new Boolean((a, b))", nil},
		{"new Boolean((!(!(a, b))))", "new Boolean((a, b))", nil},
		{"new Boolean(!!(a = b))", "new Boolean(a = b)", nil},
		{"new Boolean((!!(a = b)))", "new Boolean((a = b))", nil},
		{"new Boolean(!!(a === b))", "new Boolean(a === b)", nil},
		{"new Boolean(!!((a !== b)))", "new Boolean(a !== b)", nil},
		{"new Boolean(!!a.b)", "new Boolean(a.b)", nil},
		{"new Boolean((!!(a)))", "new Boolean((a))", nil},
		{"if (!!(a, b));", "if (a, b);", nil},
		{"if (!(!(a, b)));", "if (a, b);", nil},
		{"if (!!(a = b));", "if (a = b);", nil},
		{"if (!!(a > b));", "if (a > b);", nil},
		{"if (!!f(a));", "if (f(a));", nil},
		{"if (!!(f(a)));", "if (f(a));", nil},
		{"if ((!!f(a)));", "if ((f(a)));", nil},
		{"if (!!a);", "if (a);", nil},
		{"while (!!(a, b));", "while (a, b);", nil},
		{"while (!(!(a, b)));", "while (a, b);", nil},
		{"while (!!(a = b));", "while (a = b);", nil},
		{"while (!!(a > b));", "while (a > b);", nil},
		{"while (!!f(a));", "while (f(a));", nil},
		{"while (!!(f(a)));", "while (f(a));", nil},
		{"while ((!!f(a)));", "while ((f(a)));", nil},
		{"while (!!a);", "while (a);", nil},
		{"do {} while (!!(a, b));", "do {} while (a, b);", nil},
		{"do {} while (!(!(a, b)));", "do {} while (a, b);", nil},
		{"do {} while (!!(a = b));", "do {} while (a = b);", nil},
		{"do {} while (!!(a > b));", "do {} while (a > b);", nil},
		{"do {} while (!!f(a));", "do {} while (f(a));", nil},
		{"do {} while (!!(f(a)));", "do {} while (f(a));", nil},
		{"do {} while ((!!f(a)));", "do {} while ((f(a)));", nil},
		{"do {} while (!!a);", "do {} while (a);", nil},
		{"for (; !!(a, b););", "for (; a, b;);", nil},
		{"for (; !(!(a, b)););", "for (; a, b;);", nil},
		{"for (; !!(a = b););", "for (; a = b;);", nil},
		{"for (; !!(a > b););", "for (; a > b;);", nil},
		{"for (; !!f(a););", "for (; f(a););", nil},
		{"for (; !!(f(a)););", "for (; f(a););", nil},
		{"for (; (!!f(a)););", "for (; (f(a)););", nil},
		{"for (; !!a;);", "for (; a;);", nil},
		{"!!(a, b) ? c : d", "(a, b) ? c : d", nil},
		{"(!!(a, b)) ? c : d", "(a, b) ? c : d", nil},
		{"!!(a = b) ? c : d", "(a = b) ? c : d", nil},
		{"!!(a ? b : c) ? d : e", "(a ? b : c) ? d : e", nil},
		{"!!(a || b) ? c : d", "a || b ? c : d", nil},
		{"!!(a === b) ? c : d", "a === b ? c : d", nil},
		{"!!((a !== b)) ? c : d", "a !== b ? c : d", nil},
		{"!!+a ? b : c", "+a ? b : c", nil},
		{"!!+(a) ? b : c", "+(a) ? b : c", nil},
		{"!!f(a) ? b : c", "f(a) ? b : c", nil},
		{"(!!f(a)) ? b : c", "(f(a)) ? b : c", nil},
		{"!!a ? b : c", "a ? b : c", nil},
		{"!!!(a, b)", "!(a, b)", nil},
		{"!!!(a = b)", "!(a = b)", nil},
		{"!!(!(a += b))", "!(a += b)", nil},
		{"!(!!(a += b))", "!(a += b)", nil},
		{"!!!(a || b)", "!(a || b)", nil},
		{"!!!(a && b)", "!(a && b)", nil},
		{"!!!(a != b)", "!(a != b)", nil},
		{"!!!(a === b)", "!(a === b)", nil},
		{"!!!(a - b)", "!(a - b)", nil},
		{"!!!(a ** b)", "!(a ** b)", nil},
		{"async function f() { !!!(await a) }", "async function f() { !await a }", nil},
		{"!!!!a", "!!a", nil},
		{"!!(!(!a))", "!!a", nil},
		{"!!!+a", "!+a", nil},
		{"!!(!+a)", "!+a", nil},
		{"!(!!+a)", "!(+a)", nil},
		{"!!!f(a)", "!f(a)", nil},
		{"!!!(f(a))", "!f(a)", nil},
		{"!!!a", "!a", nil},
		{
			"if (!!(a, b) || !!(c, d)) {}",
			"if ((a, b) || (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if ((!!((a, b))) || (!!((c, d)))) {}",
			"if ((a, b) || (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a, b) && !!(c, d)) {}",
			"if ((a, b) && (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if ((!!((a, b))) && (!!((c, d)))) {}",
			"if ((a, b) && (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a = b) || !!(c = d)) {}",
			"if ((a = b) || (c = d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a >>= b) && !!(c >>= d)) {}",
			"if ((a >>= b) && (c >>= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a ? b : c) || !!(d ? e : f)) {}",
			"if ((a ? b : c) || (d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a ? b : c) && !!(d ? e : f)) {}",
			"if ((a ? b : c) && (d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a || b) || !!(c || d)) {}",
			"if (a || b || (c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a || b) && !!(c || d)) {}",
			"if ((a || b) && (c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a && b) || !!(c && d)) {}",
			"if (a && b || c && d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a && b) && !!(c && d)) {}",
			"if (a && b && (c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a !== b) || !!(c !== d)) {}",
			"if (a !== b || c !== d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a === b) && !!(c === d)) {}",
			"if (a === b && c === d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a > b) || !!(c < d)) {}",
			"if (a > b || c < d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"if (!!f(a) && !!b.c) {}", "if (f(a) && b.c) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (Boolean(a) || !!b) {}", "if (a || b) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (!!a && Boolean(b)) {}", "if (a && b) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if ((!!a) || (Boolean(b))) {}",
			"if ((a) || (b)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a, b) || !!(c, d)) {}",
			"if ((a, b) || (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if ((!!((a, b))) || (!!((c, d)))) {}",
			"if ((a, b) || (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a, b) && !!(c, d)) {}",
			"if ((a, b) && (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if ((!!((a, b))) && (!!((c, d)))) {}",
			"if ((a, b) && (c, d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a = b) || !!(c = d)) {}",
			"if ((a = b) || (c = d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a >>= b) && !!(c >>= d)) {}",
			"if ((a >>= b) && (c >>= d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a ? b : c) || !!(d ? e : f)) {}",
			"if ((a ? b : c) || (d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a ? b : c) && !!(d ? e : f)) {}",
			"if ((a ? b : c) && (d ? e : f)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a || b) || !!(c || d)) {}",
			"if (a || b || (c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a || b) && !!(c || d)) {}",
			"if ((a || b) && (c || d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a && b) || !!(c && d)) {}",
			"if (a && b || c && d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a && b) && !!(c && d)) {}",
			"if (a && b && (c && d)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a !== b) || !!(c !== d)) {}",
			"if (a !== b || c !== d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a === b) && !!(c === d)) {}",
			"if (a === b && c === d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"if (!!(a > b) || !!(c < d)) {}",
			"if (a > b || c < d) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{"if (!!f(a) && !!b.c) {}", "if (f(a) && b.c) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (Boolean(a) || !!b) {}", "if (a || b) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{"if (!!a && Boolean(b)) {}", "if (a && b) {}", NoExtraBooleanCastOptions{EnforceForInnerExpressions: true}},
		{
			"if ((!!a) || (Boolean(b))) {}",
			"if ((a) || (b)) {}",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"const ternary = Boolean(bar ? !!baz : bat);",
			"const ternary = Boolean(bar ? baz : bat);",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
		{
			"const commaOperator = Boolean((bar, baz, !!bat));",
			"const commaOperator = Boolean((bar, baz, bat));",
			NoExtraBooleanCastOptions{EnforceForInnerExpressions: true},
		},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunWithOptions(test, NoExtraBooleanCast, extraBooleanCastFile,
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
