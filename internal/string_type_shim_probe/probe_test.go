// Package string_type_shim_probe measures whether a string-keyed index signature is reachable.
//
// It exists for one commit: the addition of `stringType` to `ExtraFields.Checker` in
// `TypeScript-shim/checker/extra-shim.json`. Run it before and after that change.
//
//	before   numberIndex TRUE, stringIndex false, recordIndex false
//	after    numberIndex TRUE, stringIndex TRUE,  recordIndex TRUE
//
// The number row is the control and it already works. It is what makes the diff readable as "the
// mechanism was fine, one field was unexported to us" rather than "a field was added and things
// changed": if the number row ever moves, the change did something other than what it claims.
package string_type_shim_probe

import (
	"strconv"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	type_checking "github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

var probe = rule.Rule{
	Name:             "string-type-shim-probe",
	NeedsTypeChecker: true,
	ReadsProgram:     true,
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindAsExpression: func(node *ast.Node) {
				subject := ctx.TypeChecker.GetTypeAtLocation(node.AsAsExpression().Expression)
				ctx.ReportNode(node, rule.Message{
					Id: "probe",
					Description: "type=" + ctx.TypeChecker.TypeToString(subject) +
						" numberIndex=" + strconv.FormatBool(hasIndex(ctx, subject, numberKey(ctx))) +
						" stringIndex=" + strconv.FormatBool(hasIndex(ctx, subject, stringKey(ctx))),
				})
			},
		}
	},
}

func numberKey(ctx rule.Context) *checker.Type {
	return checker.Checker_numberType(ctx.TypeChecker)
}

// stringKey is the field this commit exposes. Before the change there is no accessor for it and this
// probe does not compile, which is itself the measurement: the gap is an absent accessor rather than
// an absent capability.
func stringKey(ctx rule.Context) *checker.Type {
	return checker.Checker_stringType(ctx.TypeChecker)
}

func hasIndex(ctx rule.Context, subject *checker.Type, key *checker.Type) bool {
	if subject == nil || key == nil {
		return false
	}
	for _, part := range type_checking.UnionTypeParts(subject) {
		if part == nil {
			continue
		}
		if checker.Checker_getIndexTypeOfType(ctx.TypeChecker, part, key) != nil {
			return true
		}
	}
	return false
}

func TestIndexSignaturesResolveForBothKeyTypes(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		wantNumber bool
		wantString bool
	}{
		{
			name:       "a number-keyed index signature, which is the CONTROL and already worked",
			source:     "declare const a: { [k: number]: string };\nconst x = a as {};\n",
			wantNumber: true,
		},
		{
			name:       "a string-keyed index signature, which is what this commit makes reachable",
			source:     "declare const a: { [k: string]: unknown };\nconst x = a as {};\n",
			wantString: true,
		},
		{
			name:       "Record<string, unknown>, the shape the corpus actually writes",
			source:     "declare const a: Record<string, unknown>;\nconst x = a as {};\n",
			wantString: true,
		},
		{
			name:   "no index signature at all, so neither key resolves",
			source: "declare const a: { b: string };\nconst x = a as {};\n",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.RunTyped(t, probe, "/repository/source/Index.ts", testCase.source)
			if len(result.Diagnostics) != 1 {
				t.Fatalf("the probe reported %d times, so it measured nothing",
					len(result.Diagnostics))
			}
			description := result.Diagnostics[0].Message.Description
			wantNumber := " numberIndex=" + strconv.FormatBool(testCase.wantNumber)
			wantString := " stringIndex=" + strconv.FormatBool(testCase.wantString)
			if !contains(description, wantNumber) || !contains(description, wantString) {
				t.Errorf("expected%s%s, got %q", wantNumber, wantString, description)
			}
		})
	}
}

func contains(haystack string, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for index := 0; index+len(needle) <= len(haystack); index++ {
			if haystack[index:index+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
