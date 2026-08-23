package core

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// iteratorFile is where the fixtures pretend to live.
const iteratorFile = "/repository/source/Iterator.ts"

// The corpus is oxc's, copied rather than rewritten.
//
// Every case below is verbatim from `oxc/crates/oxc_linter/src/rules/eslint/no_iterator.rs`:
// 4 pass, 5 fail, and the snapshot records 5 diagnostics from those 5 inputs, so one finding per
// input is right here rather than assumed. Copied because a fixture a porter invents encodes the
// same belief as the port, and the case that catches a bug is the one nobody would think to write.
func TestNoIteratorFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"a dotted access", "var a = test.__iterator__;"},
		{"an assignment to the prototype", "Foo.prototype.__iterator__ = function() {};"},
		{"a string-literal subscript", "var a = test['__iterator__'];"},
		{"a template-literal subscript", "var a = test[`__iterator__`];"},
		{"an assignment through a template subscript", "test[`__iterator__`] = function () {};"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t,
				ruletest.Run(t, NoIterator, iteratorFile, testCase.sourceText), "noIterator")
		})
	}
}

// The clean cases are the whole discrimination, and each fails a different way.
//
// The first reads a *variable* named `__iterator__`, so the property accessed is whatever that
// variable holds and is not this one. The second is a declaration rather than an access. The third
// is a different key that merely starts the same. The fourth is the sharpest: its text does contain
// `__iterator__`, followed by a newline, so any rule matching on source text reports it.
func TestNoIteratorStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"a computed access through a variable", "var a = test[__iterator__];"},
		{"a declaration binding the name", "var __iterator__ = null;"},
		{"a template naming a different key", "foo[`__iterator`] = null;"},
		{"a template whose text merely contains it", "foo[`__iterator__\n`] = null;"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.Run(t, NoIterator, iteratorFile, testCase.sourceText))
		})
	}
}

// A case written because somebody read our code rather than upstream's.
//
// The imported corpus is a floor and not a ceiling: it exercises no substituting template, and a
// port reading a template's cooked text without checking for substitutions would report this while
// upstream does not. `staticPropertyName` declines it by kind, and this pins that.
func TestNoIteratorDeclinesASubstitutingTemplate(t *testing.T) {
	ruletest.ExpectClean(t, ruletest.Run(t, NoIterator, iteratorFile,
		"declare const part: string;\nexport const a = test[`__iterator${part}__`];\n"))
}
