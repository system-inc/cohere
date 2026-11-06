package typescript

import (
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// noFloatingPromisesSuggestion is one expected repair: the id it carries and the source it writes.
type noFloatingPromisesSuggestion struct {
	messageId string
	output    string
}

// applyNoFloatingPromisesSuggestion rewrites source with one suggestion's fixes, latest range
// first.
//
// Back to front matters here rather than being defensive: the parenthesizing repairs carry two
// fixes, an insert before the statement and an insert after the expression, and applying them in
// source order would shift the second one's offset by the length of the first.
func applyNoFloatingPromisesSuggestion(test *testing.T, source string, suggestion rule.Suggestion) string {
	test.Helper()
	fixes := append([]rule.Fix(nil), suggestion.Fixes...)
	sort.SliceStable(fixes, func(left, right int) bool { return fixes[left].Range.Pos() > fixes[right].Range.Pos() })
	edited := source
	for _, fix := range fixes {
		start, end := fix.Range.Pos(), fix.Range.End()
		if start < 0 || end > len(edited) || start > end {
			test.Fatalf("the suggestion proposes an out-of-range edit [%d,%d) over %d bytes", start, end, len(edited))
		}
		edited = edited[:start] + fix.Text + edited[end:]
	}
	return edited
}

func TestNoFloatingPromisesSuggestionsRewriteTheSource(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name        string
		sourceText  string
		options     rule.Given[NoFloatingPromisesOptions]
		suggestions [][]noFloatingPromisesSuggestion
	}{
		{
			"upstream invalid case 0",
			"\nasync function test() {\n  Promise.resolve('value');\n  Promise.resolve('value').then(() => {});\n  Promise.resolve('value').catch();\n  Promise.resolve('value').finally();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  void Promise.resolve('value');\n  Promise.resolve('value').then(() => {});\n  Promise.resolve('value').catch();\n  Promise.resolve('value').finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  await Promise.resolve('value');\n  Promise.resolve('value').then(() => {});\n  Promise.resolve('value').catch();\n  Promise.resolve('value').finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  Promise.resolve('value');\n  void Promise.resolve('value').then(() => {});\n  Promise.resolve('value').catch();\n  Promise.resolve('value').finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  Promise.resolve('value');\n  await Promise.resolve('value').then(() => {});\n  Promise.resolve('value').catch();\n  Promise.resolve('value').finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  Promise.resolve('value');\n  Promise.resolve('value').then(() => {});\n  void Promise.resolve('value').catch();\n  Promise.resolve('value').finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  Promise.resolve('value');\n  Promise.resolve('value').then(() => {});\n  await Promise.resolve('value').catch();\n  Promise.resolve('value').finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  Promise.resolve('value');\n  Promise.resolve('value').then(() => {});\n  Promise.resolve('value').catch();\n  void Promise.resolve('value').finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  Promise.resolve('value');\n  Promise.resolve('value').then(() => {});\n  Promise.resolve('value').catch();\n  await Promise.resolve('value').finally();\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 1",
			"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  void obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}, {
					"floatingFixAwait",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  await obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  void obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}, {
					"floatingFixAwait",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  await obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  void obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}, {
					"floatingFixAwait",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  await obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  void obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}, {
					"floatingFixAwait",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  await obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  void obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}, {
					"floatingFixAwait",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  await obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  void obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}, {
					"floatingFixAwait",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  await obj6?.a.b.c?.();\n\n  callback?.();\n};\n\ndoSomething();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  void callback?.();\n};\n\ndoSomething();\n      ",
				}, {
					"floatingFixAwait",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  await callback?.();\n};\n\ndoSomething();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\nvoid doSomething();\n      ",
				}, {
					"floatingFixAwait",
					"\nconst doSomething = async (\n  obj1: { a?: { b?: { c?: () => Promise<void> } } },\n  obj2: { a?: { b?: { c: () => Promise<void> } } },\n  obj3: { a?: { b: { c?: () => Promise<void> } } },\n  obj4: { a: { b: { c?: () => Promise<void> } } },\n  obj5: { a?: () => { b?: { c?: () => Promise<void> } } },\n  obj6?: { a: { b: { c?: () => Promise<void> } } },\n  callback?: () => Promise<void>,\n): Promise<void> => {\n  obj1.a?.b?.c?.();\n  obj2.a?.b?.c();\n  obj3.a?.b.c?.();\n  obj4.a.b.c?.();\n  obj5.a?.().b?.c?.();\n  obj6?.a.b.c?.();\n\n  callback?.();\n};\n\nawait doSomething();\n      ",
				}},
			},
		},
		{
			"upstream invalid case 2",
			"\ndeclare const myTag: (strings: TemplateStringsArray) => Promise<void>;\nmyTag`abc`;\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ndeclare const myTag: (strings: TemplateStringsArray) => Promise<void>;\nvoid myTag`abc`;\n      ",
				},
				{
					"floatingFixAwait",
					"\ndeclare const myTag: (strings: TemplateStringsArray) => Promise<void>;\nawait myTag`abc`;\n      ",
				},
			}},
		},
		{
			"upstream invalid case 3",
			"\ndeclare const myTag: (strings: TemplateStringsArray) => Promise<void>;\nmyTag`abc`.then(() => {});\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ndeclare const myTag: (strings: TemplateStringsArray) => Promise<void>;\nvoid myTag`abc`.then(() => {});\n      ",
				},
				{
					"floatingFixAwait",
					"\ndeclare const myTag: (strings: TemplateStringsArray) => Promise<void>;\nawait myTag`abc`.then(() => {});\n      ",
				},
			}},
		},
		{
			"upstream invalid case 4",
			"\ndeclare const myTag: (strings: TemplateStringsArray) => Promise<void>;\nmyTag`abc`.finally(() => {});\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ndeclare const myTag: (strings: TemplateStringsArray) => Promise<void>;\nvoid myTag`abc`.finally(() => {});\n      ",
				},
				{
					"floatingFixAwait",
					"\ndeclare const myTag: (strings: TemplateStringsArray) => Promise<void>;\nawait myTag`abc`.finally(() => {});\n      ",
				},
			}},
		},
		{
			"upstream invalid case 5",
			"\nasync function test() {\n  Promise.resolve('value');\n}\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(true)}),
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nasync function test() {\n  void Promise.resolve('value');\n}\n      "},
				{"floatingFixAwait", "\nasync function test() {\n  await Promise.resolve('value');\n}\n      "},
			}},
		},
		{
			"upstream invalid case 6",
			"\nasync function test() {\n  Promise.reject(new Error('message'));\n  Promise.reject(new Error('message')).then(() => {});\n  Promise.reject(new Error('message')).catch();\n  Promise.reject(new Error('message')).finally();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  void Promise.reject(new Error('message'));\n  Promise.reject(new Error('message')).then(() => {});\n  Promise.reject(new Error('message')).catch();\n  Promise.reject(new Error('message')).finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  await Promise.reject(new Error('message'));\n  Promise.reject(new Error('message')).then(() => {});\n  Promise.reject(new Error('message')).catch();\n  Promise.reject(new Error('message')).finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  Promise.reject(new Error('message'));\n  void Promise.reject(new Error('message')).then(() => {});\n  Promise.reject(new Error('message')).catch();\n  Promise.reject(new Error('message')).finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  Promise.reject(new Error('message'));\n  await Promise.reject(new Error('message')).then(() => {});\n  Promise.reject(new Error('message')).catch();\n  Promise.reject(new Error('message')).finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  Promise.reject(new Error('message'));\n  Promise.reject(new Error('message')).then(() => {});\n  void Promise.reject(new Error('message')).catch();\n  Promise.reject(new Error('message')).finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  Promise.reject(new Error('message'));\n  Promise.reject(new Error('message')).then(() => {});\n  await Promise.reject(new Error('message')).catch();\n  Promise.reject(new Error('message')).finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  Promise.reject(new Error('message'));\n  Promise.reject(new Error('message')).then(() => {});\n  Promise.reject(new Error('message')).catch();\n  void Promise.reject(new Error('message')).finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  Promise.reject(new Error('message'));\n  Promise.reject(new Error('message')).then(() => {});\n  Promise.reject(new Error('message')).catch();\n  await Promise.reject(new Error('message')).finally();\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 7",
			"\nasync function test() {\n  (async () => true)();\n  (async () => true)().then(() => {});\n  (async () => true)().catch();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  void (async () => true)();\n  (async () => true)().then(() => {});\n  (async () => true)().catch();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  await (async () => true)();\n  (async () => true)().then(() => {});\n  (async () => true)().catch();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  (async () => true)();\n  void (async () => true)().then(() => {});\n  (async () => true)().catch();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  (async () => true)();\n  await (async () => true)().then(() => {});\n  (async () => true)().catch();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  (async () => true)();\n  (async () => true)().then(() => {});\n  void (async () => true)().catch();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  (async () => true)();\n  (async () => true)().then(() => {});\n  await (async () => true)().catch();\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 8",
			"\nasync function test() {\n  async function returnsPromise() {}\n\n  returnsPromise();\n  returnsPromise().then(() => {});\n  returnsPromise().catch();\n  returnsPromise().finally();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  async function returnsPromise() {}\n\n  void returnsPromise();\n  returnsPromise().then(() => {});\n  returnsPromise().catch();\n  returnsPromise().finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  async function returnsPromise() {}\n\n  await returnsPromise();\n  returnsPromise().then(() => {});\n  returnsPromise().catch();\n  returnsPromise().finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  async function returnsPromise() {}\n\n  returnsPromise();\n  void returnsPromise().then(() => {});\n  returnsPromise().catch();\n  returnsPromise().finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  async function returnsPromise() {}\n\n  returnsPromise();\n  await returnsPromise().then(() => {});\n  returnsPromise().catch();\n  returnsPromise().finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  async function returnsPromise() {}\n\n  returnsPromise();\n  returnsPromise().then(() => {});\n  void returnsPromise().catch();\n  returnsPromise().finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  async function returnsPromise() {}\n\n  returnsPromise();\n  returnsPromise().then(() => {});\n  await returnsPromise().catch();\n  returnsPromise().finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  async function returnsPromise() {}\n\n  returnsPromise();\n  returnsPromise().then(() => {});\n  returnsPromise().catch();\n  void returnsPromise().finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  async function returnsPromise() {}\n\n  returnsPromise();\n  returnsPromise().then(() => {});\n  returnsPromise().catch();\n  await returnsPromise().finally();\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 9",
			"\nasync function test() {\n  Math.random() > 0.5 ? Promise.resolve() : null;\n  Math.random() > 0.5 ? null : Promise.resolve();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  void (Math.random() > 0.5 ? Promise.resolve() : null);\n  Math.random() > 0.5 ? null : Promise.resolve();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  await (Math.random() > 0.5 ? Promise.resolve() : null);\n  Math.random() > 0.5 ? null : Promise.resolve();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  Math.random() > 0.5 ? Promise.resolve() : null;\n  void (Math.random() > 0.5 ? null : Promise.resolve());\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  Math.random() > 0.5 ? Promise.resolve() : null;\n  await (Math.random() > 0.5 ? null : Promise.resolve());\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 10",
			"\nasync function test() {\n  Promise.resolve(), 123;\n  123, Promise.resolve();\n  123, Promise.resolve(), 123;\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  void (Promise.resolve(), 123);\n  123, Promise.resolve();\n  123, Promise.resolve(), 123;\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  await (Promise.resolve(), 123);\n  123, Promise.resolve();\n  123, Promise.resolve(), 123;\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  Promise.resolve(), 123;\n  void (123, Promise.resolve());\n  123, Promise.resolve(), 123;\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  Promise.resolve(), 123;\n  await (123, Promise.resolve());\n  123, Promise.resolve(), 123;\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  Promise.resolve(), 123;\n  123, Promise.resolve();\n  void (123, Promise.resolve(), 123);\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  Promise.resolve(), 123;\n  123, Promise.resolve();\n  await (123, Promise.resolve(), 123);\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 11",
			"\nasync function test() {\n  void Promise.resolve();\n}\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{
				{{"floatingFixAwait", "\nasync function test() {\n  await Promise.resolve();\n}\n      "}},
			},
		},
		{
			"upstream invalid case 12",
			"\nasync function test() {\n  const promise = new Promise((resolve, reject) => resolve('value'));\n  promise;\n}\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\nasync function test() {\n  const promise = new Promise((resolve, reject) => resolve('value'));\n  await promise;\n}\n      ",
			}}},
		},
		{
			"upstream invalid case 13",
			"\nasync function returnsPromise() {\n  return 'value';\n}\nvoid returnsPromise();\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\nasync function returnsPromise() {\n  return 'value';\n}\nawait returnsPromise();\n      ",
			}}},
		},
		{
			"upstream invalid case 14",
			"\nasync function returnsPromise() {\n  return 'value';\n}\nvoid /* ... */ returnsPromise();\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\nasync function returnsPromise() {\n  return 'value';\n}\nawait /* ... */ returnsPromise();\n      ",
			}}},
		},
		{
			"upstream invalid case 15",
			"\nasync function returnsPromise() {\n  return 'value';\n}\n1, returnsPromise();\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\nasync function returnsPromise() {\n  return 'value';\n}\nawait (1, returnsPromise());\n      ",
			}}},
		},
		{
			"upstream invalid case 16",
			"\nasync function returnsPromise() {\n  return 'value';\n}\nbool ? returnsPromise() : null;\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\nasync function returnsPromise() {\n  return 'value';\n}\nawait (bool ? returnsPromise() : null);\n      ",
			}}},
		},
		{
			"upstream invalid case 17",
			"\nasync function test() {\n  const obj = { foo: Promise.resolve() };\n  obj.foo;\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nasync function test() {\n  const obj = { foo: Promise.resolve() };\n  void obj.foo;\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\nasync function test() {\n  const obj = { foo: Promise.resolve() };\n  await obj.foo;\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 18",
			"\nasync function test() {\n  new Promise(resolve => resolve());\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nasync function test() {\n  void new Promise(resolve => resolve());\n}\n      "},
				{
					"floatingFixAwait",
					"\nasync function test() {\n  await new Promise(resolve => resolve());\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 19",
			"\ndeclare const promiseValue: Promise<number>;\n\nasync function test() {\n  promiseValue;\n  promiseValue.then(() => {});\n  promiseValue.catch();\n  promiseValue.finally();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\ndeclare const promiseValue: Promise<number>;\n\nasync function test() {\n  void promiseValue;\n  promiseValue.then(() => {});\n  promiseValue.catch();\n  promiseValue.finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseValue: Promise<number>;\n\nasync function test() {\n  await promiseValue;\n  promiseValue.then(() => {});\n  promiseValue.catch();\n  promiseValue.finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const promiseValue: Promise<number>;\n\nasync function test() {\n  promiseValue;\n  void promiseValue.then(() => {});\n  promiseValue.catch();\n  promiseValue.finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseValue: Promise<number>;\n\nasync function test() {\n  promiseValue;\n  await promiseValue.then(() => {});\n  promiseValue.catch();\n  promiseValue.finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const promiseValue: Promise<number>;\n\nasync function test() {\n  promiseValue;\n  promiseValue.then(() => {});\n  void promiseValue.catch();\n  promiseValue.finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseValue: Promise<number>;\n\nasync function test() {\n  promiseValue;\n  promiseValue.then(() => {});\n  await promiseValue.catch();\n  promiseValue.finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const promiseValue: Promise<number>;\n\nasync function test() {\n  promiseValue;\n  promiseValue.then(() => {});\n  promiseValue.catch();\n  void promiseValue.finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseValue: Promise<number>;\n\nasync function test() {\n  promiseValue;\n  promiseValue.then(() => {});\n  promiseValue.catch();\n  await promiseValue.finally();\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 20",
			"\ndeclare const promiseUnion: Promise<number> | number;\n\nasync function test() {\n  promiseUnion;\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ndeclare const promiseUnion: Promise<number> | number;\n\nasync function test() {\n  void promiseUnion;\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\ndeclare const promiseUnion: Promise<number> | number;\n\nasync function test() {\n  await promiseUnion;\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 21",
			"\ndeclare const promiseIntersection: Promise<number> & number;\n\nasync function test() {\n  promiseIntersection;\n  promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n\nasync function test() {\n  void promiseIntersection;\n  promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n\nasync function test() {\n  await promiseIntersection;\n  promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n\nasync function test() {\n  promiseIntersection;\n  void promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n\nasync function test() {\n  promiseIntersection;\n  await promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n\nasync function test() {\n  promiseIntersection;\n  promiseIntersection.then(() => {});\n  void promiseIntersection.catch();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n\nasync function test() {\n  promiseIntersection;\n  promiseIntersection.then(() => {});\n  await promiseIntersection.catch();\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 22",
			"\nasync function test() {\n  class CanThen extends Promise<number> {}\n  const canThen: CanThen = Foo.resolve(2);\n\n  canThen;\n  canThen.then(() => {});\n  canThen.catch();\n  canThen.finally();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  class CanThen extends Promise<number> {}\n  const canThen: CanThen = Foo.resolve(2);\n\n  void canThen;\n  canThen.then(() => {});\n  canThen.catch();\n  canThen.finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  class CanThen extends Promise<number> {}\n  const canThen: CanThen = Foo.resolve(2);\n\n  await canThen;\n  canThen.then(() => {});\n  canThen.catch();\n  canThen.finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  class CanThen extends Promise<number> {}\n  const canThen: CanThen = Foo.resolve(2);\n\n  canThen;\n  void canThen.then(() => {});\n  canThen.catch();\n  canThen.finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  class CanThen extends Promise<number> {}\n  const canThen: CanThen = Foo.resolve(2);\n\n  canThen;\n  await canThen.then(() => {});\n  canThen.catch();\n  canThen.finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  class CanThen extends Promise<number> {}\n  const canThen: CanThen = Foo.resolve(2);\n\n  canThen;\n  canThen.then(() => {});\n  void canThen.catch();\n  canThen.finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  class CanThen extends Promise<number> {}\n  const canThen: CanThen = Foo.resolve(2);\n\n  canThen;\n  canThen.then(() => {});\n  await canThen.catch();\n  canThen.finally();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  class CanThen extends Promise<number> {}\n  const canThen: CanThen = Foo.resolve(2);\n\n  canThen;\n  canThen.then(() => {});\n  canThen.catch();\n  void canThen.finally();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  class CanThen extends Promise<number> {}\n  const canThen: CanThen = Foo.resolve(2);\n\n  canThen;\n  canThen.then(() => {});\n  canThen.catch();\n  await canThen.finally();\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 23",
			"\nasync function test() {\n  class CatchableThenable {\n    then(callback: () => void, callback: () => void): CatchableThenable {\n      return new CatchableThenable();\n    }\n  }\n  const thenable = new CatchableThenable();\n\n  thenable;\n  thenable.then(() => {});\n}\n      ",
			rule.Configured(NoFloatingPromisesOptions{CheckThenables: type_checking.Reference(true)}),
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  class CatchableThenable {\n    then(callback: () => void, callback: () => void): CatchableThenable {\n      return new CatchableThenable();\n    }\n  }\n  const thenable = new CatchableThenable();\n\n  void thenable;\n  thenable.then(() => {});\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  class CatchableThenable {\n    then(callback: () => void, callback: () => void): CatchableThenable {\n      return new CatchableThenable();\n    }\n  }\n  const thenable = new CatchableThenable();\n\n  await thenable;\n  thenable.then(() => {});\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\nasync function test() {\n  class CatchableThenable {\n    then(callback: () => void, callback: () => void): CatchableThenable {\n      return new CatchableThenable();\n    }\n  }\n  const thenable = new CatchableThenable();\n\n  thenable;\n  void thenable.then(() => {});\n}\n      ",
				}, {
					"floatingFixAwait",
					"\nasync function test() {\n  class CatchableThenable {\n    then(callback: () => void, callback: () => void): CatchableThenable {\n      return new CatchableThenable();\n    }\n  }\n  const thenable = new CatchableThenable();\n\n  thenable;\n  await thenable.then(() => {});\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 24",
			"\n// https://github.com/DefinitelyTyped/DefinitelyTyped/blob/master/types/promise-polyfill/index.d.ts\n// Type definitions for promise-polyfill 6.0\n// Project: https://github.com/taylorhakes/promise-polyfill\n// Definitions by: Steve Jenkins <https://github.com/skysteve>\n//                 Daniel Cassidy <https://github.com/djcsdy>\n// Definitions: https://github.com/DefinitelyTyped/DefinitelyTyped\n\ninterface PromisePolyfillConstructor extends PromiseConstructor {\n  _immediateFn?: (handler: (() => void) | string) => void;\n}\n\ndeclare const PromisePolyfill: PromisePolyfillConstructor;\n\nasync function test() {\n  const promise = new PromisePolyfill(() => {});\n\n  promise;\n  promise.then(() => {});\n  promise.catch();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\n// https://github.com/DefinitelyTyped/DefinitelyTyped/blob/master/types/promise-polyfill/index.d.ts\n// Type definitions for promise-polyfill 6.0\n// Project: https://github.com/taylorhakes/promise-polyfill\n// Definitions by: Steve Jenkins <https://github.com/skysteve>\n//                 Daniel Cassidy <https://github.com/djcsdy>\n// Definitions: https://github.com/DefinitelyTyped/DefinitelyTyped\n\ninterface PromisePolyfillConstructor extends PromiseConstructor {\n  _immediateFn?: (handler: (() => void) | string) => void;\n}\n\ndeclare const PromisePolyfill: PromisePolyfillConstructor;\n\nasync function test() {\n  const promise = new PromisePolyfill(() => {});\n\n  void promise;\n  promise.then(() => {});\n  promise.catch();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\n// https://github.com/DefinitelyTyped/DefinitelyTyped/blob/master/types/promise-polyfill/index.d.ts\n// Type definitions for promise-polyfill 6.0\n// Project: https://github.com/taylorhakes/promise-polyfill\n// Definitions by: Steve Jenkins <https://github.com/skysteve>\n//                 Daniel Cassidy <https://github.com/djcsdy>\n// Definitions: https://github.com/DefinitelyTyped/DefinitelyTyped\n\ninterface PromisePolyfillConstructor extends PromiseConstructor {\n  _immediateFn?: (handler: (() => void) | string) => void;\n}\n\ndeclare const PromisePolyfill: PromisePolyfillConstructor;\n\nasync function test() {\n  const promise = new PromisePolyfill(() => {});\n\n  await promise;\n  promise.then(() => {});\n  promise.catch();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\n// https://github.com/DefinitelyTyped/DefinitelyTyped/blob/master/types/promise-polyfill/index.d.ts\n// Type definitions for promise-polyfill 6.0\n// Project: https://github.com/taylorhakes/promise-polyfill\n// Definitions by: Steve Jenkins <https://github.com/skysteve>\n//                 Daniel Cassidy <https://github.com/djcsdy>\n// Definitions: https://github.com/DefinitelyTyped/DefinitelyTyped\n\ninterface PromisePolyfillConstructor extends PromiseConstructor {\n  _immediateFn?: (handler: (() => void) | string) => void;\n}\n\ndeclare const PromisePolyfill: PromisePolyfillConstructor;\n\nasync function test() {\n  const promise = new PromisePolyfill(() => {});\n\n  promise;\n  void promise.then(() => {});\n  promise.catch();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\n// https://github.com/DefinitelyTyped/DefinitelyTyped/blob/master/types/promise-polyfill/index.d.ts\n// Type definitions for promise-polyfill 6.0\n// Project: https://github.com/taylorhakes/promise-polyfill\n// Definitions by: Steve Jenkins <https://github.com/skysteve>\n//                 Daniel Cassidy <https://github.com/djcsdy>\n// Definitions: https://github.com/DefinitelyTyped/DefinitelyTyped\n\ninterface PromisePolyfillConstructor extends PromiseConstructor {\n  _immediateFn?: (handler: (() => void) | string) => void;\n}\n\ndeclare const PromisePolyfill: PromisePolyfillConstructor;\n\nasync function test() {\n  const promise = new PromisePolyfill(() => {});\n\n  promise;\n  await promise.then(() => {});\n  promise.catch();\n}\n      ",
				}},
				{{
					"floatingFixVoid",
					"\n// https://github.com/DefinitelyTyped/DefinitelyTyped/blob/master/types/promise-polyfill/index.d.ts\n// Type definitions for promise-polyfill 6.0\n// Project: https://github.com/taylorhakes/promise-polyfill\n// Definitions by: Steve Jenkins <https://github.com/skysteve>\n//                 Daniel Cassidy <https://github.com/djcsdy>\n// Definitions: https://github.com/DefinitelyTyped/DefinitelyTyped\n\ninterface PromisePolyfillConstructor extends PromiseConstructor {\n  _immediateFn?: (handler: (() => void) | string) => void;\n}\n\ndeclare const PromisePolyfill: PromisePolyfillConstructor;\n\nasync function test() {\n  const promise = new PromisePolyfill(() => {});\n\n  promise;\n  promise.then(() => {});\n  void promise.catch();\n}\n      ",
				}, {
					"floatingFixAwait",
					"\n// https://github.com/DefinitelyTyped/DefinitelyTyped/blob/master/types/promise-polyfill/index.d.ts\n// Type definitions for promise-polyfill 6.0\n// Project: https://github.com/taylorhakes/promise-polyfill\n// Definitions by: Steve Jenkins <https://github.com/skysteve>\n//                 Daniel Cassidy <https://github.com/djcsdy>\n// Definitions: https://github.com/DefinitelyTyped/DefinitelyTyped\n\ninterface PromisePolyfillConstructor extends PromiseConstructor {\n  _immediateFn?: (handler: (() => void) | string) => void;\n}\n\ndeclare const PromisePolyfill: PromisePolyfillConstructor;\n\nasync function test() {\n  const promise = new PromisePolyfill(() => {});\n\n  promise;\n  promise.then(() => {});\n  await promise.catch();\n}\n      ",
				}},
			},
		},
		{
			"upstream invalid case 25",
			"\n        (async () => {\n          await something();\n        })();\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\n        void (async () => {\n          await something();\n        })();\n      ",
				},
				{
					"floatingFixAwait",
					"\n        await (async () => {\n          await something();\n        })();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 26",
			"\n        (async () => {\n          something();\n        })();\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\n        void (async () => {\n          something();\n        })();\n      "},
				{"floatingFixAwait", "\n        await (async () => {\n          something();\n        })();\n      "},
			}},
		},
		{
			"upstream invalid case 27",
			"(async function foo() {})();",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "void (async function foo() {})();"},
				{"floatingFixAwait", "await (async function foo() {})();"},
			}},
		},
		{
			"upstream invalid case 28",
			"\n        function foo() {\n          (async function bar() {})();\n        }\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\n        function foo() {\n          void (async function bar() {})();\n        }\n      ",
				},
				{
					"floatingFixAwait",
					"\n        function foo() {\n          await (async function bar() {})();\n        }\n      ",
				},
			}},
		},
		{
			"upstream invalid case 29",
			"\n        const foo = () =>\n          new Promise(res => {\n            (async function () {\n              await res(1);\n            })();\n          });\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\n        const foo = () =>\n          new Promise(res => {\n            void (async function () {\n              await res(1);\n            })();\n          });\n      ",
				},
				{
					"floatingFixAwait",
					"\n        const foo = () =>\n          new Promise(res => {\n            await (async function () {\n              await res(1);\n            })();\n          });\n      ",
				},
			}},
		},
		{
			"upstream invalid case 30",
			"\n        (async function () {\n          await res(1);\n        })();\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\n        void (async function () {\n          await res(1);\n        })();\n      ",
				},
				{
					"floatingFixAwait",
					"\n        await (async function () {\n          await res(1);\n        })();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 31",
			"\n        (async function () {\n          Promise.resolve();\n        })();\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreIife: type_checking.Reference(true)}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\n        (async function () {\n          void Promise.resolve();\n        })();\n      ",
				},
				{
					"floatingFixAwait",
					"\n        (async function () {\n          await Promise.resolve();\n        })();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 32",
			"\ndeclare const promiseIntersection: Promise<number> & number;\n(async function () {\n  promiseIntersection;\n  promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n  promiseIntersection.finally();\n})();\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreIife: type_checking.Reference(true)}),
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n(async function () {\n  void promiseIntersection;\n  promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n  promiseIntersection.finally();\n})();\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n(async function () {\n  await promiseIntersection;\n  promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n  promiseIntersection.finally();\n})();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n(async function () {\n  promiseIntersection;\n  void promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n  promiseIntersection.finally();\n})();\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n(async function () {\n  promiseIntersection;\n  await promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n  promiseIntersection.finally();\n})();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n(async function () {\n  promiseIntersection;\n  promiseIntersection.then(() => {});\n  void promiseIntersection.catch();\n  promiseIntersection.finally();\n})();\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n(async function () {\n  promiseIntersection;\n  promiseIntersection.then(() => {});\n  await promiseIntersection.catch();\n  promiseIntersection.finally();\n})();\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n(async function () {\n  promiseIntersection;\n  promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n  void promiseIntersection.finally();\n})();\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const promiseIntersection: Promise<number> & number;\n(async function () {\n  promiseIntersection;\n  promiseIntersection.then(() => {});\n  promiseIntersection.catch();\n  await promiseIntersection.finally();\n})();\n      ",
				}},
			},
		},
		{
			"upstream invalid case 33",
			"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = true;\n\n  void condition || myPromise();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = true;\n\n  void (void condition || myPromise());\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = true;\n\n  await (void condition || myPromise());\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 34",
			"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = true;\n\n  (await condition) && myPromise();\n}\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = true;\n\n  await ((await condition) && myPromise());\n}\n      ",
			}}},
		},
		{
			"upstream invalid case 35",
			"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = true;\n\n  condition && myPromise();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = true;\n\n  void (condition && myPromise());\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = true;\n\n  await (condition && myPromise());\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 36",
			"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = false;\n\n  condition || myPromise();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = false;\n\n  void (condition || myPromise());\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = false;\n\n  await (condition || myPromise());\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 37",
			"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = null;\n\n  condition ?? myPromise();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = null;\n\n  void (condition ?? myPromise());\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = null;\n\n  await (condition ?? myPromise());\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 38",
			"\nasync function foo() {\n  const myPromise = Promise.resolve(true);\n  let condition = true;\n  condition && myPromise;\n}\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\nasync function foo() {\n  const myPromise = Promise.resolve(true);\n  let condition = true;\n  await (condition && myPromise);\n}\n      ",
			}}},
		},
		{
			"upstream invalid case 39",
			"\nasync function foo() {\n  const myPromise = Promise.resolve(true);\n  let condition = false;\n  condition || myPromise;\n}\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\nasync function foo() {\n  const myPromise = Promise.resolve(true);\n  let condition = false;\n  await (condition || myPromise);\n}\n      ",
			}}},
		},
		{
			"upstream invalid case 40",
			"\nasync function foo() {\n  const myPromise = Promise.resolve(true);\n  let condition = null;\n  condition ?? myPromise;\n}\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\nasync function foo() {\n  const myPromise = Promise.resolve(true);\n  let condition = null;\n  await (condition ?? myPromise);\n}\n      ",
			}}},
		},
		{
			"upstream invalid case 41",
			"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = false;\n\n  condition || condition || myPromise();\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = false;\n\n  void (condition || condition || myPromise());\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\nasync function foo() {\n  const myPromise = async () => void 0;\n  const condition = false;\n\n  await (condition || condition || myPromise());\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 42",
			"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nvoid Promise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nawait Promise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nvoid Promise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nawait Promise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nvoid Promise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nawait Promise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nvoid Promise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nawait Promise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nvoid Promise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nawait Promise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nvoid Promise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nawait Promise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nvoid Promise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nawait Promise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixVoid",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nvoid Promise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}, {
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nawait Promise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
			},
		},
		{
			"upstream invalid case 43",
			"\nPromise.reject() || 3;\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nvoid (Promise.reject() || 3);\n      "},
				{"floatingFixAwait", "\nawait (Promise.reject() || 3);\n      "},
			}},
		},
		{
			"upstream invalid case 44",
			"\nvoid Promise.resolve().then(() => {}, undefined);\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{
				{{"floatingFixAwait", "\nawait Promise.resolve().then(() => {}, undefined);\n      "}},
			},
		},
		{
			"upstream invalid case 45",
			"\ndeclare const maybeCallable: string | (() => void);\nPromise.resolve().then(() => {}, maybeCallable);\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{
				"floatingFixAwait",
				"\ndeclare const maybeCallable: string | (() => void);\nawait Promise.resolve().then(() => {}, maybeCallable);\n      ",
			}}},
		},
		{
			"upstream invalid case 46",
			"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nawait Promise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nawait Promise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nawait Promise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nawait Promise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nawait Promise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nawait Promise.resolve().catch(null);\nPromise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nawait Promise.resolve().catch(3);\nPromise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
				{{
					"floatingFixAwait",
					"\ndeclare const maybeCallable: string | (() => void);\ndeclare const definitelyCallable: () => void;\nPromise.resolve().then(() => {}, undefined);\nPromise.resolve().then(() => {}, null);\nPromise.resolve().then(() => {}, 3);\nPromise.resolve().then(() => {}, maybeCallable);\nPromise.resolve().then(() => {}, definitelyCallable);\n\nPromise.resolve().catch(undefined);\nPromise.resolve().catch(null);\nPromise.resolve().catch(3);\nawait Promise.resolve().catch(maybeCallable);\nPromise.resolve().catch(definitelyCallable);\n      ",
				}},
			},
		},
		{
			"upstream invalid case 47",
			"\nPromise.reject() || 3;\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{{{"floatingFixAwait", "\nawait (Promise.reject() || 3);\n      "}}},
		},
		{
			"upstream invalid case 48",
			"\nPromise.reject().finally(() => {});\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nvoid Promise.reject().finally(() => {});\n      "},
				{"floatingFixAwait", "\nawait Promise.reject().finally(() => {});\n      "},
			}},
		},
		{
			"upstream invalid case 49",
			"\nPromise.reject()\n  .finally(() => {})\n  .finally(() => {});\n      ",
			rule.Configured(NoFloatingPromisesOptions{IgnoreVoid: type_checking.Reference(false)}),
			[][]noFloatingPromisesSuggestion{
				{{"floatingFixAwait", "\nawait Promise.reject()\n  .finally(() => {})\n  .finally(() => {});\n      "}},
			},
		},
		{
			"upstream invalid case 50",
			"\nPromise.reject()\n  .finally(() => {})\n  .finally(() => {})\n  .finally(() => {});\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nvoid Promise.reject()\n  .finally(() => {})\n  .finally(() => {})\n  .finally(() => {});\n      ",
				},
				{
					"floatingFixAwait",
					"\nawait Promise.reject()\n  .finally(() => {})\n  .finally(() => {})\n  .finally(() => {});\n      ",
				},
			}},
		},
		{
			"upstream invalid case 51",
			"\nPromise.reject()\n  .then(() => {})\n  .finally(() => {});\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nvoid Promise.reject()\n  .then(() => {})\n  .finally(() => {});\n      "},
				{"floatingFixAwait", "\nawait Promise.reject()\n  .then(() => {})\n  .finally(() => {});\n      "},
			}},
		},
		{
			"upstream invalid case 52",
			"\ndeclare const returnsPromise: () => Promise<void> | null;\nreturnsPromise()?.finally(() => {});\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ndeclare const returnsPromise: () => Promise<void> | null;\nvoid returnsPromise()?.finally(() => {});\n      ",
				},
				{
					"floatingFixAwait",
					"\ndeclare const returnsPromise: () => Promise<void> | null;\nawait returnsPromise()?.finally(() => {});\n      ",
				},
			}},
		},
		{
			"upstream invalid case 53",
			"\nconst promiseIntersection: Promise<number> & number;\npromiseIntersection.finally(() => {});\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nconst promiseIntersection: Promise<number> & number;\nvoid promiseIntersection.finally(() => {});\n      ",
				},
				{
					"floatingFixAwait",
					"\nconst promiseIntersection: Promise<number> & number;\nawait promiseIntersection.finally(() => {});\n      ",
				},
			}},
		},
		{
			"upstream invalid case 54",
			"\nPromise.resolve().finally(() => {}), 123;\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nvoid (Promise.resolve().finally(() => {}), 123);\n      "},
				{"floatingFixAwait", "\nawait (Promise.resolve().finally(() => {}), 123);\n      "},
			}},
		},
		{
			"upstream invalid case 55",
			"\n(async () => true)().finally();\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nvoid (async () => true)().finally();\n      "},
				{"floatingFixAwait", "\nawait (async () => true)().finally();\n      "},
			}},
		},
		{
			"upstream invalid case 56",
			"\nPromise.reject(new Error('message')).finally(() => {});\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nvoid Promise.reject(new Error('message')).finally(() => {});\n      "},
				{"floatingFixAwait", "\nawait Promise.reject(new Error('message')).finally(() => {});\n      "},
			}},
		},
		{
			"upstream invalid case 57",
			"\nfunction _<T, S extends Array<T | Promise<T>>>(\n  maybePromiseArray: S | undefined,\n): void {\n  maybePromiseArray?.[0];\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nfunction _<T, S extends Array<T | Promise<T>>>(\n  maybePromiseArray: S | undefined,\n): void {\n  void maybePromiseArray?.[0];\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\nfunction _<T, S extends Array<T | Promise<T>>>(\n  maybePromiseArray: S | undefined,\n): void {\n  await maybePromiseArray?.[0];\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 74",
			"\ninterface UnsafeThenable<T> {\n  then<TResult1 = T, TResult2 = never>(\n    onfulfilled?:\n      | ((value: T) => TResult1 | UnsafeThenable<TResult1>)\n      | undefined\n      | null,\n    onrejected?:\n      | ((reason: any) => TResult2 | UnsafeThenable<TResult2>)\n      | undefined\n      | null,\n  ): UnsafeThenable<TResult1 | TResult2>;\n}\nlet promise: UnsafeThenable<number> = Promise.resolve(5);\npromise;\n      ",
			rule.Configured(NoFloatingPromisesOptions{AllowForKnownSafePromises: []type_checking.TypeOrValueSpecifier{
				{From: type_checking.TypeOrValueSpecifierFromFile, Name: []string{"SafeThenable"}},
			}, CheckThenables: type_checking.Reference(true)}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ninterface UnsafeThenable<T> {\n  then<TResult1 = T, TResult2 = never>(\n    onfulfilled?:\n      | ((value: T) => TResult1 | UnsafeThenable<TResult1>)\n      | undefined\n      | null,\n    onrejected?:\n      | ((reason: any) => TResult2 | UnsafeThenable<TResult2>)\n      | undefined\n      | null,\n  ): UnsafeThenable<TResult1 | TResult2>;\n}\nlet promise: UnsafeThenable<number> = Promise.resolve(5);\nvoid promise;\n      ",
				},
				{
					"floatingFixAwait",
					"\ninterface UnsafeThenable<T> {\n  then<TResult1 = T, TResult2 = never>(\n    onfulfilled?:\n      | ((value: T) => TResult1 | UnsafeThenable<TResult1>)\n      | undefined\n      | null,\n    onrejected?:\n      | ((reason: any) => TResult2 | UnsafeThenable<TResult2>)\n      | undefined\n      | null,\n  ): UnsafeThenable<TResult1 | TResult2>;\n}\nlet promise: UnsafeThenable<number> = Promise.resolve(5);\nawait promise;\n      ",
				},
			}},
		},
		{
			"upstream invalid case 75",
			"\nclass SafePromise<T> extends Promise<T> {}\nlet promise: SafePromise<number> = Promise.resolve(5);\npromise.catch();\n      ",
			rule.Configured(NoFloatingPromisesOptions{AllowForKnownSafePromises: []type_checking.TypeOrValueSpecifier{
				{From: type_checking.TypeOrValueSpecifierFromFile, Name: []string{"SafePromise"}},
			}}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nclass SafePromise<T> extends Promise<T> {}\nlet promise: SafePromise<number> = Promise.resolve(5);\nvoid promise.catch();\n      ",
				},
				{
					"floatingFixAwait",
					"\nclass SafePromise<T> extends Promise<T> {}\nlet promise: SafePromise<number> = Promise.resolve(5);\nawait promise.catch();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 76",
			"\nclass UnsafePromise<T> extends Promise<T> {}\nlet promise: () => UnsafePromise<number> = async () => 5;\npromise().finally();\n      ",
			rule.Configured(NoFloatingPromisesOptions{AllowForKnownSafePromises: []type_checking.TypeOrValueSpecifier{
				{From: type_checking.TypeOrValueSpecifierFromFile, Name: []string{"SafePromise"}},
			}}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nclass UnsafePromise<T> extends Promise<T> {}\nlet promise: () => UnsafePromise<number> = async () => 5;\nvoid promise().finally();\n      ",
				},
				{
					"floatingFixAwait",
					"\nclass UnsafePromise<T> extends Promise<T> {}\nlet promise: () => UnsafePromise<number> = async () => 5;\nawait promise().finally();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 77",
			"\ntype UnsafePromise = Promise<number> & { hey?: string };\nlet promise: UnsafePromise = Promise.resolve(5);\n0 ? promise.catch() : 2;\n      ",
			rule.Configured(NoFloatingPromisesOptions{AllowForKnownSafePromises: []type_checking.TypeOrValueSpecifier{
				{From: type_checking.TypeOrValueSpecifierFromFile, Name: []string{"SafePromise"}},
			}}),
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\ntype UnsafePromise = Promise<number> & { hey?: string };\nlet promise: UnsafePromise = Promise.resolve(5);\nvoid (0 ? promise.catch() : 2);\n      ",
				}, {
					"floatingFixAwait",
					"\ntype UnsafePromise = Promise<number> & { hey?: string };\nlet promise: UnsafePromise = Promise.resolve(5);\nawait (0 ? promise.catch() : 2);\n      ",
				}},
			},
		},
		{
			"upstream invalid case 78",
			"\ntype UnsafePromise = Promise<number> & { hey?: string };\nlet promise: () => UnsafePromise = async () => 5;\nnull ?? promise().catch();\n      ",
			rule.Configured(NoFloatingPromisesOptions{AllowForKnownSafePromises: []type_checking.TypeOrValueSpecifier{
				{From: type_checking.TypeOrValueSpecifierFromFile, Name: []string{"SafePromise"}},
			}}),
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\ntype UnsafePromise = Promise<number> & { hey?: string };\nlet promise: () => UnsafePromise = async () => 5;\nvoid (null ?? promise().catch());\n      ",
				}, {
					"floatingFixAwait",
					"\ntype UnsafePromise = Promise<number> & { hey?: string };\nlet promise: () => UnsafePromise = async () => 5;\nawait (null ?? promise().catch());\n      ",
				}},
			},
		},
		{
			"upstream invalid case 82",
			"\ntype SafePromise = Promise<number> & { __linterBrands?: string };\ndeclare const myTag: (strings: TemplateStringsArray) => SafePromise;\nmyTag`abc`;\n      ",
			rule.Configured(NoFloatingPromisesOptions{AllowForKnownSafePromises: []type_checking.TypeOrValueSpecifier{
				{From: type_checking.TypeOrValueSpecifierFromFile, Name: []string{"Foo"}},
			}}),
			[][]noFloatingPromisesSuggestion{
				{{
					"floatingFixVoid",
					"\ntype SafePromise = Promise<number> & { __linterBrands?: string };\ndeclare const myTag: (strings: TemplateStringsArray) => SafePromise;\nvoid myTag`abc`;\n      ",
				}, {
					"floatingFixAwait",
					"\ntype SafePromise = Promise<number> & { __linterBrands?: string };\ndeclare const myTag: (strings: TemplateStringsArray) => SafePromise;\nawait myTag`abc`;\n      ",
				}},
			},
		},
		{
			"upstream invalid case 83",
			"\n        declare function unsafe(...args: unknown[]): Promise<void>;\n\n        unsafe('...', () => {});\n      ",
			rule.Configured(NoFloatingPromisesOptions{AllowForKnownSafeCalls: []type_checking.TypeOrValueSpecifier{{
				From: type_checking.TypeOrValueSpecifierFromFile,
				Name: []string{"it"},
				Path: "tests/fixtures/file.ts",
			}}}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\n        declare function unsafe(...args: unknown[]): Promise<void>;\n\n        void unsafe('...', () => {});\n      ",
				},
				{
					"floatingFixAwait",
					"\n        declare function unsafe(...args: unknown[]): Promise<void>;\n\n        await unsafe('...', () => {});\n      ",
				},
			}},
		},
		{
			"upstream invalid case 84",
			"\n        declare function it(...args: unknown[]): Promise<void>;\n\n        it('...', () => {}).then(() => {});\n      ",
			rule.Configured(NoFloatingPromisesOptions{AllowForKnownSafeCalls: []type_checking.TypeOrValueSpecifier{{
				From: type_checking.TypeOrValueSpecifierFromFile,
				Name: []string{"it"},
				Path: "tests/fixtures/file.ts",
			}}}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\n        declare function it(...args: unknown[]): Promise<void>;\n\n        void it('...', () => {}).then(() => {});\n      ",
				},
				{
					"floatingFixAwait",
					"\n        declare function it(...args: unknown[]): Promise<void>;\n\n        await it('...', () => {}).then(() => {});\n      ",
				},
			}},
		},
		{
			"upstream invalid case 85",
			"\n        declare function it(...args: unknown[]): Promise<void>;\n\n        it('...', () => {}).finally(() => {});\n      ",
			rule.Configured(NoFloatingPromisesOptions{AllowForKnownSafeCalls: []type_checking.TypeOrValueSpecifier{{
				From: type_checking.TypeOrValueSpecifierFromFile,
				Name: []string{"it"},
				Path: "tests/fixtures/file.ts",
			}}}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\n        declare function it(...args: unknown[]): Promise<void>;\n\n        void it('...', () => {}).finally(() => {});\n      ",
				},
				{
					"floatingFixAwait",
					"\n        declare function it(...args: unknown[]): Promise<void>;\n\n        await it('...', () => {}).finally(() => {});\n      ",
				},
			}},
		},
		{
			"upstream invalid case 86",
			"\ndeclare const createPromise: () => PromiseLike<number>;\ncreatePromise();\n      ",
			rule.Configured(NoFloatingPromisesOptions{CheckThenables: type_checking.Reference(true)}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ndeclare const createPromise: () => PromiseLike<number>;\nvoid createPromise();\n      ",
				},
				{
					"floatingFixAwait",
					"\ndeclare const createPromise: () => PromiseLike<number>;\nawait createPromise();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 87",
			"\ninterface MyThenable {\n  then(onFulfilled: () => void, onRejected: () => void): MyThenable;\n}\n\ndeclare function createMyThenable(): MyThenable;\n\ncreateMyThenable();\n      ",
			rule.Configured(NoFloatingPromisesOptions{CheckThenables: type_checking.Reference(true)}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ninterface MyThenable {\n  then(onFulfilled: () => void, onRejected: () => void): MyThenable;\n}\n\ndeclare function createMyThenable(): MyThenable;\n\nvoid createMyThenable();\n      ",
				},
				{
					"floatingFixAwait",
					"\ninterface MyThenable {\n  then(onFulfilled: () => void, onRejected: () => void): MyThenable;\n}\n\ndeclare function createMyThenable(): MyThenable;\n\nawait createMyThenable();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 88",
			"\ndeclare const createPromise: () => Promise<number>;\ncreatePromise();\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ndeclare const createPromise: () => Promise<number>;\nvoid createPromise();\n      ",
				},
				{
					"floatingFixAwait",
					"\ndeclare const createPromise: () => Promise<number>;\nawait createPromise();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 89",
			"\nclass MyPromise<T> extends Promise<T> {}\ndeclare const createMyPromise: () => MyPromise<number>;\ncreateMyPromise();\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nclass MyPromise<T> extends Promise<T> {}\ndeclare const createMyPromise: () => MyPromise<number>;\nvoid createMyPromise();\n      ",
				},
				{
					"floatingFixAwait",
					"\nclass MyPromise<T> extends Promise<T> {}\ndeclare const createMyPromise: () => MyPromise<number>;\nawait createMyPromise();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 90",
			"\nclass MyPromise<T> extends Promise<T> {\n  additional: string;\n}\ndeclare const createMyPromise: () => MyPromise<number>;\ncreateMyPromise();\n      ",
			rule.Configured(NoFloatingPromisesOptions{CheckThenables: type_checking.Reference(true)}),
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\nclass MyPromise<T> extends Promise<T> {\n  additional: string;\n}\ndeclare const createMyPromise: () => MyPromise<number>;\nvoid createMyPromise();\n      ",
				},
				{
					"floatingFixAwait",
					"\nclass MyPromise<T> extends Promise<T> {\n  additional: string;\n}\ndeclare const createMyPromise: () => MyPromise<number>;\nawait createMyPromise();\n      ",
				},
			}},
		},
		{
			"upstream invalid case 91",
			"\ndeclare const x: any;\nfunction* generator(): Generator<number, void, Promise<number>> {\n  yield x;\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ndeclare const x: any;\nfunction* generator(): Generator<number, void, Promise<number>> {\n  void (yield x);\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\ndeclare const x: any;\nfunction* generator(): Generator<number, void, Promise<number>> {\n  await (yield x);\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 92",
			"\ndeclare const x: Generator<number, Promise<number>, void>;\nfunction* generator(): Generator<number, void, void> {\n  yield* x;\n}\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{
					"floatingFixVoid",
					"\ndeclare const x: Generator<number, Promise<number>, void>;\nfunction* generator(): Generator<number, void, void> {\n  void (yield* x);\n}\n      ",
				},
				{
					"floatingFixAwait",
					"\ndeclare const x: Generator<number, Promise<number>, void>;\nfunction* generator(): Generator<number, void, void> {\n  await (yield* x);\n}\n      ",
				},
			}},
		},
		{
			"upstream invalid case 93",
			"\nconst value = {};\nvalue as Promise<number>;\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nconst value = {};\nvoid (value as Promise<number>);\n      "},
				{"floatingFixAwait", "\nconst value = {};\nawait (value as Promise<number>);\n      "},
			}},
		},
		{
			"upstream invalid case 94",
			"\n({}) as Promise<number> & number;\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nvoid (({}) as Promise<number> & number);\n      "},
				{"floatingFixAwait", "\nawait (({}) as Promise<number> & number);\n      "},
			}},
		},
		{
			"upstream invalid case 95",
			"\n({}) as Promise<number> & { yolo?: string };\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nvoid (({}) as Promise<number> & { yolo?: string });\n      "},
				{"floatingFixAwait", "\nawait (({}) as Promise<number> & { yolo?: string });\n      "},
			}},
		},
		{
			"upstream invalid case 96",
			"\n<Promise<number>>{};\n      ",
			rule.Given[NoFloatingPromisesOptions]{},
			[][]noFloatingPromisesSuggestion{{
				{"floatingFixVoid", "\nvoid (<Promise<number>>{});\n      "},
				{"floatingFixAwait", "\nawait (<Promise<number>>{});\n      "},
			}},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := runNoFloatingPromises(test, testCase.sourceText, testCase.options)
			if len(result.Diagnostics) != len(testCase.suggestions) {
				test.Fatalf("want %d findings, got %d", len(testCase.suggestions), len(result.Diagnostics))
			}
			// The harness trims the fixture before building the program, so every offset the rule
			// computed is against the trimmed text rather than against the literal above. The expected
			// output is trimmed exactly the same way rather than with a two-sided trim, because a
			// leading space this repair leaves behind is meaningful and a symmetric trim eats it.
			source := strings.TrimSpace(testCase.sourceText)
			for diagnosticIndex, wantSuggestions := range testCase.suggestions {
				gotSuggestions := result.Diagnostics[diagnosticIndex].Suggestions
				if len(gotSuggestions) != len(wantSuggestions) {
					test.Fatalf(
						"finding %d offers %d suggestions, want %d",
						diagnosticIndex,
						len(gotSuggestions),
						len(wantSuggestions),
					)
				}
				for suggestionIndex, want := range wantSuggestions {
					got := gotSuggestions[suggestionIndex]
					if got.Message.Id != want.messageId {
						test.Errorf(
							"finding %d suggestion %d has id %q, want %q",
							diagnosticIndex,
							suggestionIndex,
							got.Message.Id,
							want.messageId,
						)
					}
					applied := applyNoFloatingPromisesSuggestion(test, source, got)
					if applied != strings.TrimSpace(want.output) {
						test.Errorf(
							"finding %d suggestion %d rewrote the source to\n%q\nwant\n%q",
							diagnosticIndex,
							suggestionIndex,
							applied,
							strings.TrimSpace(want.output),
						)
					}
				}
			}
		})
	}
}
