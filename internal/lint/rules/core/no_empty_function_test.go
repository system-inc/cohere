package core

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// emptyFunctionFile is where the fixtures pretend to live.
const emptyFunctionFile = "/repository/source/EmptyFunction.ts"

// decodedEmptyFunctionOptions routes a fixture's options through the rule's own decoder.
//
// An empty string means the rule is configured as bare "error", which is what the live config
// does and what hands the rule no options.
func decodedEmptyFunctionOptions(test *testing.T, raw string) rule.Given[NoEmptyFunctionOptions] {
	test.Helper()
	if raw == "" {
		return rule.Given[NoEmptyFunctionOptions]{}
	}
	options, decodeNoEmptyFunctionOptionsError := DecodeNoEmptyFunctionOptions(json.RawMessage(raw))
	if decodeNoEmptyFunctionOptionsError != nil {
		test.Fatalf("the decoder refused %s: %v", raw, decodeNoEmptyFunctionOptionsError)
	}
	return rule.Configured(options)
}

// The corpus is ESLint's own, at `tests/lib/rules/no-empty-function.js`, extracted by loading
// that file with a stub rule tester rather than retyped.
//
// It is generated upstream rather than written by hand, and the shape is worth knowing before
// reading the counts. Each seed shape expands into four clean patterns (a body with a statement,
// a block comment, a line comment, and one per option that allows it) and fourteen reporting
// ones (the bare case plus one for every unrelated allow value, proving it has no effect).
// So 132 reporting seeds become 1740 cases, and that repetition is the point: it is the full
// cross product of every function kind against every allow value, which no hand-written fixture
// set would ever contain.
//
// Upstream splits these across two rule testers, one with its default parser and one with the
// TypeScript parser. Both are imported here as one set, because our parser reads TypeScript
// natively and the split is a fact about espree rather than about the rule.
func TestNoEmptyFunctionStaysSilent(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText string
		options    string
	}{
		{"var foo = () => 0;", ""},
		{"function foo() { bar(); }", ""},
		{"function foo() { /* empty */ }", ""},
		{"function foo() {\n    // empty\n}", ""},
		{"function foo() {} // allow: functions", "{\"allow\": [\"functions\"]}"},
		{"var foo = function() { bar(); };", ""},
		{"var foo = function() { /* empty */ };", ""},
		{"var foo = function() {\n    // empty\n};", ""},
		{"var foo = function() {}; // allow: functions", "{\"allow\": [\"functions\"]}"},
		{"var obj = {foo: function() { bar(); }};", ""},
		{"var obj = {foo: function() { /* empty */ }};", ""},
		{"var obj = {foo: function() {\n    // empty\n}};", ""},
		{"var obj = {foo: function() {}}; // allow: functions", "{\"allow\": [\"functions\"]}"},
		{"var foo = () => { bar(); };", ""},
		{"var foo = () => { /* empty */ };", ""},
		{"var foo = () => {\n    // empty\n};", ""},
		{"var foo = () => {}; // allow: arrowFunctions", "{\"allow\": [\"arrowFunctions\"]}"},
		{"function* foo() { bar(); }", ""},
		{"function* foo() { /* empty */ }", ""},
		{"function* foo() {\n    // empty\n}", ""},
		{"function* foo() {} // allow: generatorFunctions", "{\"allow\": [\"generatorFunctions\"]}"},
		{"var foo = function*() { bar(); };", ""},
		{"var foo = function*() { /* empty */ };", ""},
		{"var foo = function*() {\n    // empty\n};", ""},
		{"var foo = function*() {}; // allow: generatorFunctions", "{\"allow\": [\"generatorFunctions\"]}"},
		{"var obj = {foo: function*() { bar(); }};", ""},
		{"var obj = {foo: function*() { /* empty */ }};", ""},
		{"var obj = {foo: function*() {\n    // empty\n}};", ""},
		{"var obj = {foo: function*() {}}; // allow: generatorFunctions", "{\"allow\": [\"generatorFunctions\"]}"},
		{"var obj = {foo() { bar(); }};", ""},
		{"var obj = {foo() { /* empty */ }};", ""},
		{"var obj = {foo() {\n    // empty\n}};", ""},
		{"var obj = {foo() {}}; // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class A {foo() { bar(); }}", ""},
		{"class A {foo() { /* empty */ }}", ""},
		{"class A {foo() {\n    // empty\n}}", ""},
		{"class A {foo() {}} // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class A {static foo() { bar(); }}", ""},
		{"class A {static foo() { /* empty */ }}", ""},
		{"class A {static foo() {\n    // empty\n}}", ""},
		{"class A {static foo() {}} // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"var A = class {foo() { bar(); }};", ""},
		{"var A = class {foo() { /* empty */ }};", ""},
		{"var A = class {foo() {\n    // empty\n}};", ""},
		{"var A = class {foo() {}}; // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"var A = class {static foo() { bar(); }};", ""},
		{"var A = class {static foo() { /* empty */ }};", ""},
		{"var A = class {static foo() {\n    // empty\n}};", ""},
		{"var A = class {static foo() {}}; // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"var obj = {*foo() { bar(); }};", ""},
		{"var obj = {*foo() { /* empty */ }};", ""},
		{"var obj = {*foo() {\n    // empty\n}};", ""},
		{"var obj = {*foo() {}}; // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"class A {*foo() { bar(); }}", ""},
		{"class A {*foo() { /* empty */ }}", ""},
		{"class A {*foo() {\n    // empty\n}}", ""},
		{"class A {*foo() {}} // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"class A {static *foo() { bar(); }}", ""},
		{"class A {static *foo() { /* empty */ }}", ""},
		{"class A {static *foo() {\n    // empty\n}}", ""},
		{"class A {static *foo() {}} // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"var A = class {*foo() { bar(); }};", ""},
		{"var A = class {*foo() { /* empty */ }};", ""},
		{"var A = class {*foo() {\n    // empty\n}};", ""},
		{"var A = class {*foo() {}}; // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"var A = class {static *foo() { bar(); }};", ""},
		{"var A = class {static *foo() { /* empty */ }};", ""},
		{"var A = class {static *foo() {\n    // empty\n}};", ""},
		{"var A = class {static *foo() {}}; // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"var obj = {get foo() { bar(); }};", ""},
		{"var obj = {get foo() { /* empty */ }};", ""},
		{"var obj = {get foo() {\n    // empty\n}};", ""},
		{"var obj = {get foo() {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"class A {get foo() { bar(); }}", ""},
		{"class A {get foo() { /* empty */ }}", ""},
		{"class A {get foo() {\n    // empty\n}}", ""},
		{"class A {get foo() {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"class A {static get foo() { bar(); }}", ""},
		{"class A {static get foo() { /* empty */ }}", ""},
		{"class A {static get foo() {\n    // empty\n}}", ""},
		{"class A {static get foo() {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"var A = class {get foo() { bar(); }};", ""},
		{"var A = class {get foo() { /* empty */ }};", ""},
		{"var A = class {get foo() {\n    // empty\n}};", ""},
		{"var A = class {get foo() {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"var A = class {static get foo() { bar(); }};", ""},
		{"var A = class {static get foo() { /* empty */ }};", ""},
		{"var A = class {static get foo() {\n    // empty\n}};", ""},
		{"var A = class {static get foo() {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"var obj = {set foo(value) { bar(); }};", ""},
		{"var obj = {set foo(value) { /* empty */ }};", ""},
		{"var obj = {set foo(value) {\n    // empty\n}};", ""},
		{"var obj = {set foo(value) {}}; // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"class A {set foo(value) { bar(); }}", ""},
		{"class A {set foo(value) { /* empty */ }}", ""},
		{"class A {set foo(value) {\n    // empty\n}}", ""},
		{"class A {set foo(value) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"class A {static set foo(value) { bar(); }}", ""},
		{"class A {static set foo(value) { /* empty */ }}", ""},
		{"class A {static set foo(value) {\n    // empty\n}}", ""},
		{"class A {static set foo(value) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"var A = class {set foo(value) { bar(); }};", ""},
		{"var A = class {set foo(value) { /* empty */ }};", ""},
		{"var A = class {set foo(value) {\n    // empty\n}};", ""},
		{"var A = class {set foo(value) {}}; // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"var A = class {static set foo(value) { bar(); }};", ""},
		{"var A = class {static set foo(value) { /* empty */ }};", ""},
		{"var A = class {static set foo(value) {\n    // empty\n}};", ""},
		{"var A = class {static set foo(value) {}}; // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"class A {constructor() { bar(); }}", ""},
		{"class A {constructor() { /* empty */ }}", ""},
		{"class A {constructor() {\n    // empty\n}}", ""},
		{"class A {constructor() {}} // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{"var A = class {constructor() { bar(); }};", ""},
		{"var A = class {constructor() { /* empty */ }};", ""},
		{"var A = class {constructor() {\n    // empty\n}};", ""},
		{"var A = class {constructor() {}}; // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{"const foo = { async method() { bar(); } }", ""},
		{"const foo = { async method() { /* empty */ } }", ""},
		{"const foo = { async method() {\n    // empty\n} }", ""},
		{"const foo = { async method() {} } // allow: asyncMethods", "{\"allow\": [\"asyncMethods\"]}"},
		{"async function a(){ bar(); }", ""},
		{"async function a(){ /* empty */ }", ""},
		{"async function a(){\n    // empty\n}", ""},
		{"async function a(){} // allow: asyncFunctions", "{\"allow\": [\"asyncFunctions\"]}"},
		{"const foo = async function () { bar(); }", ""},
		{"const foo = async function () { /* empty */ }", ""},
		{"const foo = async function () {\n    // empty\n}", ""},
		{"const foo = async function () {} // allow: asyncFunctions", "{\"allow\": [\"asyncFunctions\"]}"},
		{"class Foo { async bar() { bar(); } }", ""},
		{"class Foo { async bar() { /* empty */ } }", ""},
		{"class Foo { async bar() {\n    // empty\n} }", ""},
		{"class Foo { async bar() {} } // allow: asyncMethods", "{\"allow\": [\"asyncMethods\"]}"},
		{"const foo = async () => { bar(); };", ""},
		{"const foo = async () => { /* empty */ };", ""},
		{"const foo = async () => {\n    // empty\n};", ""},
		{"const foo = async () => {}; // allow: arrowFunctions", "{\"allow\": [\"arrowFunctions\"]}"},
		{"class A { constructor(public param: string) {} }", ""},
		{"class A { constructor(private param: string) {} }", ""},
		{"class A { constructor(protected param: string) {} }", ""},
		{"class A { constructor(readonly param: string) {} }", ""},
		{"const foo = function(param: string) { bar(); };", ""},
		{"const foo = function(param: string) { /* empty */ };", ""},
		{"const foo = function(param: string) {\n    // empty\n};", ""},
		{"const foo = function(param: string) {}; // allow: functions", "{\"allow\": [\"functions\"]}"},
		{"const obj = {foo: function(param: string) { bar(); }};", ""},
		{"const obj = {foo: function(param: string) { /* empty */ }};", ""},
		{"const obj = {foo: function(param: string) {\n    // empty\n}};", ""},
		{"const obj = {foo: function(param: string) {}}; // allow: functions", "{\"allow\": [\"functions\"]}"},
		{"const foo = (param: string) => { bar(); };", ""},
		{"const foo = (param: string) => { /* empty */ };", ""},
		{"const foo = (param: string) => {\n    // empty\n};", ""},
		{"const foo = (param: string) => {}; // allow: arrowFunctions", "{\"allow\": [\"arrowFunctions\"]}"},
		{"function* foo(param: string) { bar(); }", ""},
		{"function* foo(param: string) { /* empty */ }", ""},
		{"function* foo(param: string) {\n    // empty\n}", ""},
		{"function* foo(param: string) {} // allow: generatorFunctions", "{\"allow\": [\"generatorFunctions\"]}"},
		{"const foo = function*(param: string) { bar(); };", ""},
		{"const foo = function*(param: string) { /* empty */ };", ""},
		{"const foo = function*(param: string) {\n    // empty\n};", ""},
		{
			"const foo = function*(param: string) {}; // allow: generatorFunctions",
			"{\"allow\": [\"generatorFunctions\"]}",
		},
		{"const obj = {foo: function*(param: string) { bar(); }};", ""},
		{"const obj = {foo: function*(param: string) { /* empty */ }};", ""},
		{"const obj = {foo: function*(param: string) {\n    // empty\n}};", ""},
		{
			"const obj = {foo: function*(param: string) {}}; // allow: generatorFunctions",
			"{\"allow\": [\"generatorFunctions\"]}",
		},
		{"const obj = {foo(param: string) { bar(); }};", ""},
		{"const obj = {foo(param: string) { /* empty */ }};", ""},
		{"const obj = {foo(param: string) {\n    // empty\n}};", ""},
		{"const obj = {foo(param: string) {}}; // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class A { foo(param: string) { bar(); } }", ""},
		{"class A { foo(param: string) { /* empty */ } }", ""},
		{"class A { foo(param: string) {\n    // empty\n} }", ""},
		{"class A { foo(param: string) {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class A { private foo() { bar(); } }", ""},
		{"class A { private foo() { /* empty */ } }", ""},
		{"class A { private foo() {\n    // empty\n} }", ""},
		{"class A { private foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class A { protected foo() { bar(); } }", ""},
		{"class A { protected foo() { /* empty */ } }", ""},
		{"class A { protected foo() {\n    // empty\n} }", ""},
		{"class A { protected foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class A { static foo(param: string) { bar(); } }", ""},
		{"class A { static foo(param: string) { /* empty */ } }", ""},
		{"class A { static foo(param: string) {\n    // empty\n} }", ""},
		{"class A { static foo(param: string) {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class A { private static foo() { bar(); } }", ""},
		{"class A { private static foo() { /* empty */ } }", ""},
		{"class A { private static foo() {\n    // empty\n} }", ""},
		{"class A { private static foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class A { protected static foo() { bar(); } }", ""},
		{"class A { protected static foo() { /* empty */ } }", ""},
		{"class A { protected static foo() {\n    // empty\n} }", ""},
		{"class A { protected static foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"const A = class {foo(param: string) { bar(); }};", ""},
		{"const A = class {foo(param: string) { /* empty */ }};", ""},
		{"const A = class {foo(param: string) {\n    // empty\n}};", ""},
		{"const A = class {foo(param: string) {}}; // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"const A = class {static foo(param: string) { bar(); }};", ""},
		{"const A = class {static foo(param: string) { /* empty */ }};", ""},
		{"const A = class {static foo(param: string) {\n    // empty\n}};", ""},
		{"const A = class {static foo(param: string) {}}; // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"const A = class {private static foo() { bar(); }};", ""},
		{"const A = class {private static foo() { /* empty */ }};", ""},
		{"const A = class {private static foo() {\n    // empty\n}};", ""},
		{"const A = class {private static foo() {}}; // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"const A = class {protected static foo() { bar(); }};", ""},
		{"const A = class {protected static foo() { /* empty */ }};", ""},
		{"const A = class {protected static foo() {\n    // empty\n}};", ""},
		{"const A = class {protected static foo() {}}; // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class B { @decorator() foo() { bar(); } }", ""},
		{"class B { @decorator() foo() { /* empty */ } }", ""},
		{"class B { @decorator() foo() {\n    // empty\n} }", ""},
		{"class B { @decorator() foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class B { @decorator() foo() {} } // allow: decoratedFunctions", "{\"allow\": [\"decoratedFunctions\"]}"},
		{"const B = class { @decorator() foo() { bar(); } }", ""},
		{"const B = class { @decorator() foo() { /* empty */ } }", ""},
		{"const B = class { @decorator() foo() {\n    // empty\n} }", ""},
		{"const B = class { @decorator() foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{
			"const B = class { @decorator() foo() {} } // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class B extends C { override foo() { bar(); } }", ""},
		{"class B extends C { override foo() { /* empty */ } }", ""},
		{"class B extends C { override foo() {\n    // empty\n} }", ""},
		{"class B extends C { override foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class B extends C { override foo() {} } // allow: overrideMethods", "{\"allow\": [\"overrideMethods\"]}"},
		{"class B extends C { @decorator() override foo() { bar(); } }", ""},
		{"class B extends C { @decorator() override foo() { /* empty */ } }", ""},
		{"class B extends C { @decorator() override foo() {\n    // empty\n} }", ""},
		{"class B extends C { @decorator() override foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{
			"class B extends C { @decorator() override foo() {} } // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{
			"class B extends C { @decorator() override foo() {} } // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"const obj = {*foo(param: string) { bar(); }};", ""},
		{"const obj = {*foo(param: string) { /* empty */ }};", ""},
		{"const obj = {*foo(param: string) {\n    // empty\n}};", ""},
		{"const obj = {*foo(param: string) {}}; // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"class A { *foo(param: string) { bar(); } }", ""},
		{"class A { *foo(param: string) { /* empty */ } }", ""},
		{"class A { *foo(param: string) {\n    // empty\n} }", ""},
		{"class A { *foo(param: string) {} } // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"class A {static *foo(param: string) { bar(); }}", ""},
		{"class A {static *foo(param: string) { /* empty */ }}", ""},
		{"class A {static *foo(param: string) {\n    // empty\n}}", ""},
		{"class A {static *foo(param: string) {}} // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"class A {private static *foo() { bar(); }}", ""},
		{"class A {private static *foo() { /* empty */ }}", ""},
		{"class A {private static *foo() {\n    // empty\n}}", ""},
		{"class A {private static *foo() {}} // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"class A {protected static *foo() { bar(); }}", ""},
		{"class A {protected static *foo() { /* empty */ }}", ""},
		{"class A {protected static *foo() {\n    // empty\n}}", ""},
		{"class A {protected static *foo() {}} // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"const A = class {*foo(param: string) { bar(); }};", ""},
		{"const A = class {*foo(param: string) { /* empty */ }};", ""},
		{"const A = class {*foo(param: string) {\n    // empty\n}};", ""},
		{"const A = class {*foo(param: string) {}}; // allow: generatorMethods", "{\"allow\": [\"generatorMethods\"]}"},
		{"const A = class {static *foo(param: string) { bar(); }};", ""},
		{"const A = class {static *foo(param: string) { /* empty */ }};", ""},
		{"const A = class {static *foo(param: string) {\n    // empty\n}};", ""},
		{
			"const A = class {static *foo(param: string) {}}; // allow: generatorMethods",
			"{\"allow\": [\"generatorMethods\"]}",
		},
		{"const obj = {get foo(): string { bar(); }};", ""},
		{"const obj = {get foo(): string { /* empty */ }};", ""},
		{"const obj = {get foo(): string {\n    // empty\n}};", ""},
		{"const obj = {get foo(): string {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"class A {get foo(): string { bar(); }}", ""},
		{"class A {get foo(): string { /* empty */ }}", ""},
		{"class A {get foo(): string {\n    // empty\n}}", ""},
		{"class A {get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"class A {static get foo(): string { bar(); }}", ""},
		{"class A {static get foo(): string { /* empty */ }}", ""},
		{"class A {static get foo(): string {\n    // empty\n}}", ""},
		{"class A {static get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"const A = class {get foo(): string { bar(); }};", ""},
		{"const A = class {get foo(): string { /* empty */ }};", ""},
		{"const A = class {get foo(): string {\n    // empty\n}};", ""},
		{"const A = class {get foo(): string {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"const A = class {static get foo(): string { bar(); }};", ""},
		{"const A = class {static get foo(): string { /* empty */ }};", ""},
		{"const A = class {static get foo(): string {\n    // empty\n}};", ""},
		{"const A = class {static get foo(): string {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"class A {@decorator() get foo(): string { bar(); }}", ""},
		{"class A {@decorator() get foo(): string { /* empty */ }}", ""},
		{"class A {@decorator() get foo(): string {\n    // empty\n}}", ""},
		{"class A {@decorator() get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{
			"class A {@decorator() get foo(): string {}} // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class A {@decorator() static get foo(): string { bar(); }}", ""},
		{"class A {@decorator() static get foo(): string { /* empty */ }}", ""},
		{"class A {@decorator() static get foo(): string {\n    // empty\n}}", ""},
		{"class A {@decorator() static get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{
			"class A {@decorator() static get foo(): string {}} // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"const A = class {@decorator() get foo(): string { bar(); }};", ""},
		{"const A = class {@decorator() get foo(): string { /* empty */ }};", ""},
		{"const A = class {@decorator() get foo(): string {\n    // empty\n}};", ""},
		{"const A = class {@decorator() get foo(): string {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{
			"const A = class {@decorator() get foo(): string {}}; // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"const A = class {@decorator() static get foo(): string { bar(); }};", ""},
		{"const A = class {@decorator() static get foo(): string { /* empty */ }};", ""},
		{"const A = class {@decorator() static get foo(): string {\n    // empty\n}};", ""},
		{"const A = class {@decorator() static get foo(): string {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{
			"const A = class {@decorator() static get foo(): string {}}; // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class A extends B {override get foo(): string { bar(); }}", ""},
		{"class A extends B {override get foo(): string { /* empty */ }}", ""},
		{"class A extends B {override get foo(): string {\n    // empty\n}}", ""},
		{"class A extends B {override get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{
			"class A extends B {override get foo(): string {}} // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"class A extends B {static override get foo(): string { bar(); }}", ""},
		{"class A extends B {static override get foo(): string { /* empty */ }}", ""},
		{"class A extends B {static override get foo(): string {\n    // empty\n}}", ""},
		{"class A extends B {static override get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{
			"class A extends B {static override get foo(): string {}} // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"const A = class extends B {override get foo(): string { bar(); }};", ""},
		{"const A = class extends B {override get foo(): string { /* empty */ }};", ""},
		{"const A = class extends B {override get foo(): string {\n    // empty\n}};", ""},
		{"const A = class extends B {override get foo(): string {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{
			"const A = class extends B {override get foo(): string {}}; // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"const A = class extends B {static override get foo(): string { bar(); }};", ""},
		{"const A = class extends B {static override get foo(): string { /* empty */ }};", ""},
		{"const A = class extends B {static override get foo(): string {\n    // empty\n}};", ""},
		{
			"const A = class extends B {static override get foo(): string {}}; // allow: getters",
			"{\"allow\": [\"getters\"]}",
		},
		{
			"const A = class extends B {static override get foo(): string {}}; // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"const obj = {set foo(value: string) { bar(); }};", ""},
		{"const obj = {set foo(value: string) { /* empty */ }};", ""},
		{"const obj = {set foo(value: string) {\n    // empty\n}};", ""},
		{"const obj = {set foo(value: string) {}}; // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"class A {set foo(value: string) { bar(); }}", ""},
		{"class A {set foo(value: string) { /* empty */ }}", ""},
		{"class A {set foo(value: string) {\n    // empty\n}}", ""},
		{"class A {set foo(value: string) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"class A {static set foo(value: string) { bar(); }}", ""},
		{"class A {static set foo(value: string) { /* empty */ }}", ""},
		{"class A {static set foo(value: string) {\n    // empty\n}}", ""},
		{"class A {static set foo(value: string) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"const A = class {set foo(value: string) { bar(); }};", ""},
		{"const A = class {set foo(value: string) { /* empty */ }};", ""},
		{"const A = class {set foo(value: string) {\n    // empty\n}};", ""},
		{"const A = class {set foo(value: string) {}}; // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"const A = class {static set foo(value: string) { bar(); }};", ""},
		{"const A = class {static set foo(value: string) { /* empty */ }};", ""},
		{"const A = class {static set foo(value: string) {\n    // empty\n}};", ""},
		{"const A = class {static set foo(value: string) {}}; // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"class A {@decorator() set foo(value: string) { bar(); }}", ""},
		{"class A {@decorator() set foo(value: string) { /* empty */ }}", ""},
		{"class A {@decorator() set foo(value: string) {\n    // empty\n}}", ""},
		{"class A {@decorator() set foo(value: string) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{
			"class A {@decorator() set foo(value: string) {}} // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class A {@decorator() static set foo(value: string) { bar(); }}", ""},
		{"class A {@decorator() static set foo(value: string) { /* empty */ }}", ""},
		{"class A {@decorator() static set foo(value: string) {\n    // empty\n}}", ""},
		{"class A {@decorator() static set foo(value: string) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{
			"class A {@decorator() static set foo(value: string) {}} // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"const A = class {@decorator() set foo(value: string) { bar(); }};", ""},
		{"const A = class {@decorator() set foo(value: string) { /* empty */ }};", ""},
		{"const A = class {@decorator() set foo(value: string) {\n    // empty\n}};", ""},
		{"const A = class {@decorator() set foo(value: string) {}}; // allow: setters", "{\"allow\": [\"setters\"]}"},
		{
			"const A = class {@decorator() set foo(value: string) {}}; // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"const A = class {@decorator() static set foo(value: string) { bar(); }};", ""},
		{"const A = class {@decorator() static set foo(value: string) { /* empty */ }};", ""},
		{"const A = class {@decorator() static set foo(value: string) {\n    // empty\n}};", ""},
		{
			"const A = class {@decorator() static set foo(value: string) {}}; // allow: setters",
			"{\"allow\": [\"setters\"]}",
		},
		{
			"const A = class {@decorator() static set foo(value: string) {}}; // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class A extends B {override set foo(value: string) { bar(); }}", ""},
		{"class A extends B {override set foo(value: string) { /* empty */ }}", ""},
		{"class A extends B {override set foo(value: string) {\n    // empty\n}}", ""},
		{"class A extends B {override set foo(value: string) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{
			"class A extends B {override set foo(value: string) {}} // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"class A extends B {static override set foo(value: string) { bar(); }}", ""},
		{"class A extends B {static override set foo(value: string) { /* empty */ }}", ""},
		{"class A extends B {static override set foo(value: string) {\n    // empty\n}}", ""},
		{
			"class A extends B {static override set foo(value: string) {}} // allow: setters",
			"{\"allow\": [\"setters\"]}",
		},
		{
			"class A extends B {static override set foo(value: string) {}} // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"const A = class extends B {override set foo(value: string) { bar(); }};", ""},
		{"const A = class extends B {override set foo(value: string) { /* empty */ }};", ""},
		{"const A = class extends B {override set foo(value: string) {\n    // empty\n}};", ""},
		{
			"const A = class extends B {override set foo(value: string) {}}; // allow: setters",
			"{\"allow\": [\"setters\"]}",
		},
		{
			"const A = class extends B {override set foo(value: string) {}}; // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"const A = class extends B {static override set foo(value: string) { bar(); }};", ""},
		{"const A = class extends B {static override set foo(value: string) { /* empty */ }};", ""},
		{"const A = class extends B {static override set foo(value: string) {\n    // empty\n}};", ""},
		{
			"const A = class extends B {static override set foo(value: string) {}}; // allow: setters",
			"{\"allow\": [\"setters\"]}",
		},
		{
			"const A = class extends B {static override set foo(value: string) {}}; // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"class A { constructor(param: string) { bar(); } }", ""},
		{"class A { constructor(param: string) { /* empty */ } }", ""},
		{"class A { constructor(param: string) {\n    // empty\n} }", ""},
		{"class A { constructor(param: string) {} } // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{"class B { private constructor() { bar(); } }", ""},
		{"class B { private constructor() { /* empty */ } }", ""},
		{"class B { private constructor() {\n    // empty\n} }", ""},
		{"class B { private constructor() {} } // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{
			"class B { private constructor() {} } // allow: privateConstructors",
			"{\"allow\": [\"privateConstructors\"]}",
		},
		{"class B { protected constructor() { bar(); } }", ""},
		{"class B { protected constructor() { /* empty */ } }", ""},
		{"class B { protected constructor() {\n    // empty\n} }", ""},
		{"class B { protected constructor() {} } // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{
			"class B { protected constructor() {} } // allow: protectedConstructors",
			"{\"allow\": [\"protectedConstructors\"]}",
		},
		{"const A = class {constructor(param: string) { bar(); }};", ""},
		{"const A = class {constructor(param: string) { /* empty */ }};", ""},
		{"const A = class {constructor(param: string) {\n    // empty\n}};", ""},
		{"const A = class {constructor(param: string) {}}; // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{"const B = class { private constructor() { bar(); } }", ""},
		{"const B = class { private constructor() { /* empty */ } }", ""},
		{"const B = class { private constructor() {\n    // empty\n} }", ""},
		{"const B = class { private constructor() {} } // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{
			"const B = class { private constructor() {} } // allow: privateConstructors",
			"{\"allow\": [\"privateConstructors\"]}",
		},
		{"const B = class { protected constructor() { bar(); } }", ""},
		{"const B = class { protected constructor() { /* empty */ } }", ""},
		{"const B = class { protected constructor() {\n    // empty\n} }", ""},
		{"const B = class { protected constructor() {} } // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{
			"const B = class { protected constructor() {} } // allow: protectedConstructors",
			"{\"allow\": [\"protectedConstructors\"]}",
		},
		{"const foo = { async method(param: string) { bar(); } }", ""},
		{"const foo = { async method(param: string) { /* empty */ } }", ""},
		{"const foo = { async method(param: string) {\n    // empty\n} }", ""},
		{"const foo = { async method(param: string) {} } // allow: asyncMethods", "{\"allow\": [\"asyncMethods\"]}"},
		{"async function a(param: string){ bar(); }", ""},
		{"async function a(param: string){ /* empty */ }", ""},
		{"async function a(param: string){\n    // empty\n}", ""},
		{"async function a(param: string){} // allow: asyncFunctions", "{\"allow\": [\"asyncFunctions\"]}"},
		{"const foo = async function(param: string) { bar(); }", ""},
		{"const foo = async function(param: string) { /* empty */ }", ""},
		{"const foo = async function(param: string) {\n    // empty\n}", ""},
		{"const foo = async function(param: string) {} // allow: asyncFunctions", "{\"allow\": [\"asyncFunctions\"]}"},
		{"class A { async foo(param: string) { bar(); } }", ""},
		{"class A { async foo(param: string) { /* empty */ } }", ""},
		{"class A { async foo(param: string) {\n    // empty\n} }", ""},
		{"class A { async foo(param: string) {} } // allow: asyncMethods", "{\"allow\": [\"asyncMethods\"]}"},
		{"class A { @decorator() async foo(param: string) { bar(); } }", ""},
		{"class A { @decorator() async foo(param: string) { /* empty */ } }", ""},
		{"class A { @decorator() async foo(param: string) {\n    // empty\n} }", ""},
		{
			"class A { @decorator() async foo(param: string) {} } // allow: asyncMethods",
			"{\"allow\": [\"asyncMethods\"]}",
		},
		{
			"class A { @decorator() async foo(param: string) {} } // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class A extends B { override async foo(param: string) { bar(); } }", ""},
		{"class A extends B { override async foo(param: string) { /* empty */ } }", ""},
		{"class A extends B { override async foo(param: string) {\n    // empty\n} }", ""},
		{
			"class A extends B { override async foo(param: string) {} } // allow: asyncMethods",
			"{\"allow\": [\"asyncMethods\"]}",
		},
		{
			"class A extends B { override async foo(param: string) {} } // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"const foo = async (): Promise<void> => { bar(); };", ""},
		{"const foo = async (): Promise<void> => { /* empty */ };", ""},
		{"const foo = async (): Promise<void> => {\n    // empty\n};", ""},
		{"const foo = async (): Promise<void> => {}; // allow: arrowFunctions", "{\"allow\": [\"arrowFunctions\"]}"},
		{"class A { private constructor() { bar(); } }", ""},
		{"class A { private constructor() { /* empty */ } }", ""},
		{"class A { private constructor() {\n    // empty\n} }", ""},
		{
			"class A { private constructor() {} } // allow: privateConstructors",
			"{\"allow\": [\"privateConstructors\"]}",
		},
		{"class A { private constructor() {} } // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{"const A = class { private constructor() { bar(); } };", ""},
		{"const A = class { private constructor() { /* empty */ } };", ""},
		{"const A = class { private constructor() {\n    // empty\n} };", ""},
		{
			"const A = class { private constructor() {} }; // allow: privateConstructors",
			"{\"allow\": [\"privateConstructors\"]}",
		},
		{"const A = class { private constructor() {} }; // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{"class A { protected constructor() { bar(); } }", ""},
		{"class A { protected constructor() { /* empty */ } }", ""},
		{"class A { protected constructor() {\n    // empty\n} }", ""},
		{
			"class A { protected constructor() {} } // allow: protectedConstructors",
			"{\"allow\": [\"protectedConstructors\"]}",
		},
		{"class A { protected constructor() {} } // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{"const A = class { protected constructor() { bar(); } };", ""},
		{"const A = class { protected constructor() { /* empty */ } };", ""},
		{"const A = class { protected constructor() {\n    // empty\n} };", ""},
		{
			"const A = class { protected constructor() {} }; // allow: protectedConstructors",
			"{\"allow\": [\"protectedConstructors\"]}",
		},
		{"const A = class { protected constructor() {} }; // allow: constructors", "{\"allow\": [\"constructors\"]}"},
		{"class A { @decorator() foo() { bar(); } }", ""},
		{"class A { @decorator() foo() { /* empty */ } }", ""},
		{"class A { @decorator() foo() {\n    // empty\n} }", ""},
		{"class A { @decorator() foo() {} } // allow: decoratedFunctions", "{\"allow\": [\"decoratedFunctions\"]}"},
		{"class A { @decorator() foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"const A = class { @decorator() foo() { bar(); } }", ""},
		{"const A = class { @decorator() foo() { /* empty */ } }", ""},
		{"const A = class { @decorator() foo() {\n    // empty\n} }", ""},
		{
			"const A = class { @decorator() foo() {} } // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"const A = class { @decorator() foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{"class B {@decorator() get foo(): string { bar(); }}", ""},
		{"class B {@decorator() get foo(): string { /* empty */ }}", ""},
		{"class B {@decorator() get foo(): string {\n    // empty\n}}", ""},
		{
			"class B {@decorator() get foo(): string {}} // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class B {@decorator() get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"class B {@decorator() static get foo(): string { bar(); }}", ""},
		{"class B {@decorator() static get foo(): string { /* empty */ }}", ""},
		{"class B {@decorator() static get foo(): string {\n    // empty\n}}", ""},
		{
			"class B {@decorator() static get foo(): string {}} // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class B {@decorator() static get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"const B = class {@decorator() get foo(): string { bar(); }};", ""},
		{"const B = class {@decorator() get foo(): string { /* empty */ }};", ""},
		{"const B = class {@decorator() get foo(): string {\n    // empty\n}};", ""},
		{
			"const B = class {@decorator() get foo(): string {}}; // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"const B = class {@decorator() get foo(): string {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"const B = class {@decorator() static get foo(): string { bar(); }};", ""},
		{"const B = class {@decorator() static get foo(): string { /* empty */ }};", ""},
		{"const B = class {@decorator() static get foo(): string {\n    // empty\n}};", ""},
		{
			"const B = class {@decorator() static get foo(): string {}}; // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"const B = class {@decorator() static get foo(): string {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"class B {@decorator() set foo(value: string) { bar(); }}", ""},
		{"class B {@decorator() set foo(value: string) { /* empty */ }}", ""},
		{"class B {@decorator() set foo(value: string) {\n    // empty\n}}", ""},
		{
			"class B {@decorator() set foo(value: string) {}} // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class B {@decorator() set foo(value: string) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"class B {@decorator() static set foo(value: string) { bar(); }}", ""},
		{"class B {@decorator() static set foo(value: string) { /* empty */ }}", ""},
		{"class B {@decorator() static set foo(value: string) {\n    // empty\n}}", ""},
		{
			"class B {@decorator() static set foo(value: string) {}} // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class B {@decorator() static set foo(value: string) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"const B = class {@decorator() set foo(value: string) { bar(); }};", ""},
		{"const B = class {@decorator() set foo(value: string) { /* empty */ }};", ""},
		{"const B = class {@decorator() set foo(value: string) {\n    // empty\n}};", ""},
		{
			"const B = class {@decorator() set foo(value: string) {}}; // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"const B = class {@decorator() set foo(value: string) {}}; // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"const B = class {@decorator() static set foo(value: string) { bar(); }};", ""},
		{"const B = class {@decorator() static set foo(value: string) { /* empty */ }};", ""},
		{"const B = class {@decorator() static set foo(value: string) {\n    // empty\n}};", ""},
		{
			"const B = class {@decorator() static set foo(value: string) {}}; // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{
			"const B = class {@decorator() static set foo(value: string) {}}; // allow: setters",
			"{\"allow\": [\"setters\"]}",
		},
		{"class B { @decorator() async foo(param: string) { bar(); } }", ""},
		{"class B { @decorator() async foo(param: string) { /* empty */ } }", ""},
		{"class B { @decorator() async foo(param: string) {\n    // empty\n} }", ""},
		{
			"class B { @decorator() async foo(param: string) {} } // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{
			"class B { @decorator() async foo(param: string) {} } // allow: asyncMethods",
			"{\"allow\": [\"asyncMethods\"]}",
		},
		{"class A extends B { @decorator() override foo() { bar(); } }", ""},
		{"class A extends B { @decorator() override foo() { /* empty */ } }", ""},
		{"class A extends B { @decorator() override foo() {\n    // empty\n} }", ""},
		{
			"class A extends B { @decorator() override foo() {} } // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class A extends B { @decorator() override foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{
			"class A extends B { @decorator() override foo() {} } // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"class B extends C {override get foo(): string { bar(); }}", ""},
		{"class B extends C {override get foo(): string { /* empty */ }}", ""},
		{"class B extends C {override get foo(): string {\n    // empty\n}}", ""},
		{
			"class B extends C {override get foo(): string {}} // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"class B extends C {override get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"class B extends C {static override get foo(): string { bar(); }}", ""},
		{"class B extends C {static override get foo(): string { /* empty */ }}", ""},
		{"class B extends C {static override get foo(): string {\n    // empty\n}}", ""},
		{
			"class B extends C {static override get foo(): string {}} // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"class B extends C {static override get foo(): string {}} // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"const B = class extends C {override get foo(): string { bar(); }};", ""},
		{"const B = class extends C {override get foo(): string { /* empty */ }};", ""},
		{"const B = class extends C {override get foo(): string {\n    // empty\n}};", ""},
		{
			"const B = class extends C {override get foo(): string {}}; // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"const B = class extends C {override get foo(): string {}}; // allow: getters", "{\"allow\": [\"getters\"]}"},
		{"const B = class extends C {static override get foo(): string { bar(); }};", ""},
		{"const B = class extends C {static override get foo(): string { /* empty */ }};", ""},
		{"const B = class extends C {static override get foo(): string {\n    // empty\n}};", ""},
		{
			"const B = class extends C {static override get foo(): string {}}; // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{
			"const B = class extends C {static override get foo(): string {}}; // allow: getters",
			"{\"allow\": [\"getters\"]}",
		},
		{"class B extends C {override set foo(value: string) { bar(); }}", ""},
		{"class B extends C {override set foo(value: string) { /* empty */ }}", ""},
		{"class B extends C {override set foo(value: string) {\n    // empty\n}}", ""},
		{
			"class B extends C {override set foo(value: string) {}} // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"class B extends C {override set foo(value: string) {}} // allow: setters", "{\"allow\": [\"setters\"]}"},
		{"class B extends C {static override set foo(value: string) { bar(); }}", ""},
		{"class B extends C {static override set foo(value: string) { /* empty */ }}", ""},
		{"class B extends C {static override set foo(value: string) {\n    // empty\n}}", ""},
		{
			"class B extends C {static override set foo(value: string) {}} // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{
			"class B extends C {static override set foo(value: string) {}} // allow: setters",
			"{\"allow\": [\"setters\"]}",
		},
		{"const B = class extends C {override set foo(value: string) { bar(); }};", ""},
		{"const B = class extends C {override set foo(value: string) { /* empty */ }};", ""},
		{"const B = class extends C {override set foo(value: string) {\n    // empty\n}};", ""},
		{
			"const B = class extends C {override set foo(value: string) {}}; // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{
			"const B = class extends C {override set foo(value: string) {}}; // allow: setters",
			"{\"allow\": [\"setters\"]}",
		},
		{"const B = class extends C {static override set foo(value: string) { bar(); }};", ""},
		{"const B = class extends C {static override set foo(value: string) { /* empty */ }};", ""},
		{"const B = class extends C {static override set foo(value: string) {\n    // empty\n}};", ""},
		{
			"const B = class extends C {static override set foo(value: string) {}}; // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{
			"const B = class extends C {static override set foo(value: string) {}}; // allow: setters",
			"{\"allow\": [\"setters\"]}",
		},
		{"class B extends C { override async foo(param: string) { bar(); } }", ""},
		{"class B extends C { override async foo(param: string) { /* empty */ } }", ""},
		{"class B extends C { override async foo(param: string) {\n    // empty\n} }", ""},
		{
			"class B extends C { override async foo(param: string) {} } // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{
			"class B extends C { override async foo(param: string) {} } // allow: asyncMethods",
			"{\"allow\": [\"asyncMethods\"]}",
		},
		{"class C extends D { @decorator() override foo() { bar(); } }", ""},
		{"class C extends D { @decorator() override foo() { /* empty */ } }", ""},
		{"class C extends D { @decorator() override foo() {\n    // empty\n} }", ""},
		{
			"class C extends D { @decorator() override foo() {} } // allow: overrideMethods",
			"{\"allow\": [\"overrideMethods\"]}",
		},
		{"class C extends D { @decorator() override foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
		{
			"class C extends D { @decorator() override foo() {} } // allow: decoratedFunctions",
			"{\"allow\": [\"decoratedFunctions\"]}",
		},
		{"class A extends B { override foo() { bar(); } }", ""},
		{"class A extends B { override foo() { /* empty */ } }", ""},
		{"class A extends B { override foo() {\n    // empty\n} }", ""},
		{"class A extends B { override foo() {} } // allow: overrideMethods", "{\"allow\": [\"overrideMethods\"]}"},
		{"class A extends B { override foo() {} } // allow: methods", "{\"allow\": [\"methods\"]}"},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText+"|"+testCase.options, func(test *testing.T) {
			test.Parallel()
			rule_testing.ExpectClean(test, rule_testing.RunGiven(test, NoEmptyFunction,
				emptyFunctionFile, testCase.sourceText, decodedEmptyFunctionOptions(test, testCase.options)))
		})
	}
}

// noEmptyFunctionFiresCase is one reporting row of upstream's corpus: the source, the options it
// runs under, the name upstream's message interpolates, and the source after its suggestion.
type noEmptyFunctionFiresCase struct {
	sourceText string
	options    string
	wantName   string
	wantOutput string
}

// Every reporting case, with the rendered message text and the suggestion output upstream
// asserts for each.
//
// The message interpolates a function name that has twenty distinct forms in this corpus, so a
// message-id assertion cannot see most of what the rule decides. The text in the rows is
// upstream's own `data` rendered through its own template.
func TestNoEmptyFunctionFires(test *testing.T) {
	test.Parallel()

	// The rows live in the files beside this one, a slice per kind of function, joined here in
	// upstream's order so every subtest keeps its name and its place.
	cases := slices.Concat(
		noEmptyFunctionFiresJavaScriptFunctionsAndMethodsCases,
		noEmptyFunctionFiresJavaScriptAccessorsConstructorsAndAsyncCases,
		noEmptyFunctionFiresTypeScriptFunctionsAndMethodsCases,
		noEmptyFunctionFiresTypeScriptGeneratorMethodsCases,
		noEmptyFunctionFiresTypeScriptGettersCases,
		noEmptyFunctionFiresTypeScriptSettersCases,
		noEmptyFunctionFiresTypeScriptConstructorsAndAsyncCases,
		noEmptyFunctionFiresTypeScriptVisibilityAndDecoratorsCases,
		noEmptyFunctionFiresTypeScriptOverridesCases,
	)

	for _, testCase := range cases {
		test.Run(testCase.sourceText+"|"+testCase.options, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.RunGiven(test, NoEmptyFunction, emptyFunctionFile,
				testCase.sourceText, decodedEmptyFunctionOptions(test, testCase.options))
			rule_testing.ExpectFindings(test, result, "unexpected")

			// Equality on the rendered text, not a prefix: the interpolated name is most of what
			// this rule decides and a weaker predicate would not be a guard.
			wantText := "Unexpected empty " + testCase.wantName + "."
			if got := result.Diagnostics[0].Message.Description; got != wantText {
				test.Errorf("rendered:\n  %q\nwant:\n  %q", got, wantText)
			}

			// The harness applies a fix but not a suggestion, so the applier is hand-rolled. The
			// expectation is transformed the way `RunWithOptions` did not transform the input:
			// `Run` does not trim, so upstream's output is compared as written.
			suggestions := result.Diagnostics[0].Suggestions
			if len(suggestions) != 1 {
				test.Fatalf("offered %d suggestions, want 1", len(suggestions))
			}
			if suggestions[0].Message.Id != "suggestComment" {
				test.Errorf("suggestion is %q, want %q", suggestions[0].Message.Id, "suggestComment")
			}
			if got := applyEmptyFunctionSuggestion(
				test,
				result.SourceFile.Text(),
				suggestions[0],
			); got != testCase.wantOutput {
				test.Errorf("applying the suggestion produced:\n  %q\nwant:\n  %q", got, testCase.wantOutput)
			}
		})
	}
}

// applyEmptyFunctionSuggestion replays a suggestion's fixes into the source, back to front.
//
// Same ordering the fix engine and `rule_testing.ExpectFixedSource` use, and for the same reason.
// `rule_testing` has no suggestion support at all, so this lives here.
func applyEmptyFunctionSuggestion(test *testing.T, source string, suggestion rule.Suggestion) string {
	test.Helper()
	if len(suggestion.Fixes) == 0 {
		test.Fatalf("suggestion %q carries no fixes", suggestion.Message.Id)
	}
	ordered := make([]rule.Fix, len(suggestion.Fixes))
	copy(ordered, suggestion.Fixes)
	sort.Slice(ordered, func(first, second int) bool {
		return ordered[first].Range.Pos() > ordered[second].Range.Pos()
	})
	fixed := source
	for _, fix := range ordered {
		if fix.Range.Pos() < 0 || fix.Range.End() > len(fixed) || fix.Range.Pos() > fix.Range.End() {
			test.Fatalf("fix range [%d,%d) is outside the source, which is a defect in the rule",
				fix.Range.Pos(), fix.Range.End())
		}
		fixed = fixed[:fix.Range.Pos()] + fix.Text + fixed[fix.Range.End():]
	}
	return fixed
}

// The span upstream reports, measured against the installed eslint at 10.8.1.
//
// It is the body block rather than the function: `function a() {}` reports columns 14 to 16,
// which is the `{}`. Upstream reports the function node with an explicit `loc` of `node.body.loc`,
// so a port reporting the function itself would pass every message-id fixture while pointing at
// the wrong place on all 1740 of them.
//
// The multi-line row is the one that pins the end: a body spanning a newline reports through to
// the closing brace on the next line rather than stopping at the first.
func TestNoEmptyFunctionSpansTheBody(test *testing.T) {
	test.Parallel()

	cases := []struct {
		sourceText   string
		wantReported string
	}{
		{"function a() {}", "{}"},
		{"var a = function() {};", "{}"},
		{"var a = () => {};", "{}"},
		{"class A { constructor() {} }", "{}"},
		{"class A { get foo() {} }", "{}"},
		{"var obj = { foo() {} };", "{}"},
		{"function a() {\n}", "{\n}"},
	}

	for _, testCase := range cases {
		test.Run(testCase.sourceText, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, NoEmptyFunction, emptyFunctionFile,
				testCase.sourceText)
			if len(result.Diagnostics) != 1 {
				test.Fatalf("want one finding, got %d", len(result.Diagnostics))
			}
			source := result.SourceFile.Text()
			diagnostic := result.Diagnostics[0]
			if got := source[diagnostic.Range.Pos():diagnostic.Range.End()]; got != testCase.wantReported {
				test.Errorf("the finding points at %q, want %q", got, testCase.wantReported)
			}
		})
	}
}

// The decoder is the line with no upstream counterpart. `allow` defaults to an empty list, so
// the zero value happens to be right, and the fallback is written out anyway so a later default
// change cannot silently switch every kind off.
func TestDecodeNoEmptyFunctionOptions(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty input allows nothing", "", nil},
		{"an empty object allows nothing", "{}", nil},
		{"an explicit empty list", `{"allow": []}`, nil},
		{"one kind", `{"allow": ["functions"]}`, []string{"functions"}},
		{"several kinds", `{"allow": ["getters", "setters"]}`, []string{"getters", "setters"}},
	}

	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			decoded, decodeNoEmptyFunctionOptionsError := DecodeNoEmptyFunctionOptions(json.RawMessage(testCase.raw))
			if decodeNoEmptyFunctionOptionsError != nil {
				test.Fatalf("the decoder refused %q: %v", testCase.raw, decodeNoEmptyFunctionOptionsError)
			}
			if strings.Join(decoded.Allow, ",") != strings.Join(testCase.want, ",") {
				test.Errorf("allow decodes to %v, want %v", decoded.Allow, testCase.want)
			}
		})
	}
}

// A value outside upstream's enum is refused rather than ignored.
//
// Upstream gets that refusal from its schema before the rule runs; there is no schema layer here.
// The kebab spellings are the interesting rows: `private-constructors` and
// `protected-constructors` are what the typescript-eslint extension accepts, and core rejects
// both. Measured: eslint 10.8.1 refuses to load a config naming either. This port is the core
// rule, so it refuses them too, and the extension is where the kebab spellings belong.
func TestDecodeNoEmptyFunctionOptionsRefusesAnUnknownKind(test *testing.T) {
	test.Parallel()

	for _, raw := range []string{
		`{"allow": ["nonsense"]}`,
		`{"allow": ["private-constructors"]}`,
		`{"allow": ["protected-constructors"]}`,
		`{"allow": ["Functions"]}`,
	} {
		test.Run(raw, func(test *testing.T) {
			test.Parallel()
			if _, decodeNoEmptyFunctionOptionsError := DecodeNoEmptyFunctionOptions(
				json.RawMessage(raw),
			); decodeNoEmptyFunctionOptionsError == nil {
				test.Errorf("the decoder accepted %s, which is outside upstream's enum", raw)
			}
		})
	}
}

// A rule configured as bare "error" reaches Run unconfigured rather than with an options struct.
func TestNoEmptyFunctionWithNilOptionsAllowsNothing(test *testing.T) {
	test.Parallel()

	rule_testing.ExpectFindings(test, rule_testing.Run(test, NoEmptyFunction,
		emptyFunctionFile, "function a() {}"), "unexpected")
	rule_testing.ExpectClean(test, rule_testing.Run(test, NoEmptyFunction,
		emptyFunctionFile, "function a() { /* empty */ }"))
}

// A constructor carrying `override`, which upstream's corpus never writes.
//
// A mutation widening the accessor-and-method gate to accept every kind survived all 2313 imported
// cases, because none of them puts a decorator or an `override` on anything but a getter, a setter
// or a method. This one does, and it separates them: upstream's regular expression over the kind
// string matches `getters`, `setters`, `methods`, `generatorMethods` and `asyncMethods`, and
// `constructors` is not in that set, so `overrideMethods` does not exempt a constructor.
//
// Measured against the installed eslint at 10.8.1 with the TypeScript parser: this reports, and the
// same class with `get foo()` in place of the constructor is clean.
func TestNoEmptyFunctionOverrideDoesNotExemptAConstructor(test *testing.T) {
	test.Parallel()

	allowOverride := decodedEmptyFunctionOptions(test, `{"allow": ["overrideMethods"]}`)

	rule_testing.ExpectFindings(test, rule_testing.RunGiven(test, NoEmptyFunction, emptyFunctionFile,
		"class A extends B { override constructor() {} }", allowOverride), "unexpected")

	// The control, and the reason the row above is about the gate rather than about `override`
	// itself: the same modifier on a getter does exempt.
	rule_testing.ExpectClean(test, rule_testing.RunGiven(test, NoEmptyFunction, emptyFunctionFile,
		"class A extends B { override get foo() {} }", allowOverride))
}
