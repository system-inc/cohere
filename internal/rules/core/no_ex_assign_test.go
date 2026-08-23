package core

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const exAssignFile = "/repository/source/Thing.ts"

func TestNoExAssignFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"a plain assignment", "export function run() {\n    try { work(); } catch(error) { error = new Error('x'); }\n}\ndeclare function work(): void;\n"},
		// The compound forms write to the same binding and are the ones that read as harmless.
		// `??=` in particular looks like enrichment rather than replacement.
		{"a nullish assignment", "export function run() {\n    try { work(); } catch(error) { error ??= new Error('x'); }\n}\ndeclare function work(): void;\n"},
		{"an or assignment", "export function run() {\n    try { work(); } catch(error) { error ||= new Error('x'); }\n}\ndeclare function work(): void;\n"},
		{"assignment inside a nested block", "export function run(flag: boolean) {\n    try { work(); } catch(error) { if(flag) { error = new Error('x'); } }\n}\ndeclare function work(): void;\n"},
		{"a parenthesized target", "export function run() {\n    try { work(); } catch(error) { (error) = new Error('x'); }\n}\ndeclare function work(): void;\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t, ruletest.Run(t, NoExAssign, exAssignFile, testCase.sourceText),
				"unexpectedExceptionAssignment")
		})
	}
}

func TestNoExAssignStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"reading the exception", "export function run() {\n    try { work(); } catch(error) { report(error); }\n}\ndeclare function work(): void;\ndeclare function report(value: unknown): void;\n"},
		// The intended repair: a second binding rather than an overwrite.
		{"a new binding beside it", "export function run() {\n    try { work(); } catch(error) { const wrapped = String(error); report(wrapped); }\n}\ndeclare function work(): void;\ndeclare function report(value: unknown): void;\n"},
		// The decision boundary is "does this write to the binding that holds the exception".
		// A same-named binding in a different scope does not.
		{"assigning a different name", "export function run() {\n    let other = 0;\n    try { work(); } catch(error) { other = 1; void error; }\n}\ndeclare function work(): void;\n"},
		// A destructuring catch binds new names taken out of the error rather than the error
		// itself, so writing to one of them loses nothing.
		{"a destructured catch parameter", "export function run() {\n    try { work(); } catch({ message }) { message = 'x'; }\n}\ndeclare function work(): void;\n"},
		{"a catch with no parameter", "export function run() {\n    try { work(); } catch { report(1); }\n}\ndeclare function work(): void;\ndeclare function report(value: unknown): void;\n"},
		{"assigning to a property of the error", "export function run() {\n    try { work(); } catch(error) { (error as { code?: number }).code = 1; }\n}\ndeclare function work(): void;\n"},
		{"no try at all", "export function run() {\n    return 1;\n}\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.Run(t, NoExAssign, exAssignFile, testCase.sourceText))
		})
	}
}
