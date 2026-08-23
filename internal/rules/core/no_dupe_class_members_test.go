package core

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// dupeClassMembersFile is where the fixtures pretend to live.
const dupeClassMembersFile = "/repository/source/Members.ts"

// The corpus is oxc's, copied rather than rewritten.
//
// One Tester block, 36 pass and 38 fail, with the snapshot recording 40 diagnostics from 38 inputs:
// two inputs declare the same member three times and report twice, so a fixture asserting one
// finding per input is wrong on those and right everywhere else.
func TestNoDupeClassMembersFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"two methods", "class A { foo() {} foo() {} }"},
		{"inside a class expression", "!class A { foo() {} foo() {} };"},
		{"two string keys", "class A { 'foo'() {} 'foo'() {} }"},
		// Numeric keys compare by value, so two spellings of ten are one member.
		{"ten and one-e-one", "class A { 10() {} 1e1() {} }"},
		{"two computed string keys", "class A { ['foo']() {} ['foo']() {} }"},
		{"a computed key against a plain one", "class A { static ['foo']() {} static foo() {} }"},
		{"two setters of one name", "class A { set 'foo'(value) {} set ['foo'](val) {} }"},
		{"the empty string twice", "class A { ''() {} ['']() {} }"},
		{"two template keys", "class A { [`foo`]() {} [`foo`]() {} }"},
		{"two static getters", "class A { static get [`foo`]() {} static get ['foo']() {} }"},
		// A template key and a plain identifier are the same member.
		{"an identifier against a template", "class A { foo() {} [`foo`]() {} }"},
		{"a getter against a method", "class A { get [`foo`]() {} 'foo'() {} }"},
		{"two static string keys", "class A { static 'foo'() {} static [`foo`]() {} }"},
		{"constructor as a computed key", "class A { ['constructor']() {} ['constructor']() {} }"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t,
				ruletest.Run(t, NoDupeClassMembers, dupeClassMembersFile, testCase.sourceText),
				"noDupeClassMembers")
		})
	}
}

// The clean cases carry the whole discrimination, and four of them are about key identity.
//
// `1.0` and `'1.0'` are a number and a string, so their texts agree and they are different members.
// `0x1` and a template spelling it are the same, for the same reason in reverse. `null` is a
// keyword rather than the empty string. And a computed key naming a variable is not knowable before
// the class runs, so it collides with nothing.
//
// A rule comparing source text gets two of these right by accident and two wrong; one comparing
// cooked text gets the other pair wrong. The type has to travel with the value.
func TestNoDupeClassMembersStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"different names", "class A { foo() {} bar() {} }"},
		{"static and instance are different members", "class A { static foo() {} foo() {} }"},
		{"a getter and a setter are one member", "class A { get foo() {} set foo(value) {} }"},
		{"static method with instance accessors",
			"class A { static foo() {} get foo() {} set foo(value) {} }"},
		{"two classes with the same member", "class A { foo() { } } class B { foo() { } }"},
		{"a computed key naming a variable", "class A { [foo]() {} foo() {} }"},
		{"distinct string keys", "class A { 'foo'() {} 'bar'() {} baz() {} }"},
		{"distinct generator keys", "class A { *'foo'() {} *'bar'() {} *baz() {} }"},
		{"distinct getter keys", "class A { get 'foo'() {} get 'bar'() {} get baz() {} }"},
		{"distinct numeric keys", "class A { 1() {} 2() {} }"},
		{"distinct computed string keys", "class A { ['foo']() {} ['bar']() {} }"},
		{"distinct template keys", "class A { [`foo`]() {} [`bar`]() {} }"},
		{"distinct computed numbers", "class A { [12]() {} [123]() {} }"},
		{"a number against its string spelling", "class A { [1.0]() {} ['1.0']() {} }"},
		{"hexadecimal against its text", "class A { [0x1]() {} [`0x1`]() {} }"},
		{"null against the empty string", "class A { [null]() {} ['']() {} }"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t,
				ruletest.Run(t, NoDupeClassMembers, dupeClassMembersFile, testCase.sourceText))
		})
	}
}

// Cases written because somebody read our code rather than upstream's.
//
// A private member is a distinct namespace, so `#foo` and `foo` coexist, and two `#foo` collide.
// Upstream tracks that with an `is_private` flag on the element and its corpus never exercises it,
// which is exactly the shape a port drops silently: the flag looks like defensive copying until a
// file declares both.
func TestNoDupeClassMembersSeparatesPrivateNames(t *testing.T) {
	ruletest.ExpectClean(t, ruletest.Run(t, NoDupeClassMembers, dupeClassMembersFile,
		"class A { #foo() {} foo() {} }"))

	ruletest.ExpectFindings(t, ruletest.Run(t, NoDupeClassMembers, dupeClassMembersFile,
		"class A { #foo() {} #foo() {} }"), "noDupeClassMembers")
}
