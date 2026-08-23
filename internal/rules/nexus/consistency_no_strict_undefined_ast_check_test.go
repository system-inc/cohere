package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

const lintRuleFile = "/repository/libraries/nexus/code-quality/lint/rules/SomeRule.ts"

func TestConsistencyNoStrictUndefinedAstCheckFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"strict not-equal", "if (annotation.typeArguments !== undefined) { }\n"},
		{"strict equal", "if (node.returnType === undefined) { }\n"},
		{"reversed operands", "if (undefined === node.superClass) { }\n"},
		{"deep chain", "if (declaration.node.typeAnnotation !== undefined) { }\n"},
		{"core estree optional", "if (node.init === undefined) { }\n"},
		// The utilities directory reads parser output too.
		{"lint utilities directory", "if (node.typeParameters === undefined) { }\n"},
	}
	for _, testCase := range cases {
		fileName := lintRuleFile
		if testCase.name == "lint utilities directory" {
			fileName = "/repository/libraries/nexus/code-quality/lint/utilities/Helper.ts"
		}
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyNoStrictUndefinedAstCheck, fileName, testCase.sourceText)
			ruletest.ExpectFindings(t, result, "strictUndefinedAstCheck")
		})
	}
}

func TestConsistencyNoStrictUndefinedAstCheckStaysSilent(t *testing.T) {
	// Quiet is the whole design here. A lint rule is full of legitimate strict comparisons against
	// things that are not AST nodes, and a rule that flagged those would be switched off.
	cases := []struct {
		name       string
		fileName   string
		sourceText string
	}{
		{"the loose form", lintRuleFile, "if (annotation.typeArguments != null) { }\n"},
		{"loose equal", lintRuleFile, "if (node.returnType == null) { }\n"},
		{"a map read is not an AST property", lintRuleFile, "if (englishValue === undefined) { }\n"},
		{"an options read is not an AST property", lintRuleFile, "if (options.allow === undefined) { }\n"},
		{"an indexed read has no static name", lintRuleFile, "if (array[index] === undefined) { }\n"},
		{"a call result is not a property", lintRuleFile, "if (map.get(key) === undefined) { }\n"},
		{"a bare identifier names no property", lintRuleFile, "if (value === undefined) { }\n"},
		{"a property not on the optional list", lintRuleFile, "if (node.operator === undefined) { }\n"},
		{"a truthiness check needs no rewrite", lintRuleFile, "if (!node.init) { }\n"},
		// Outside the directories that read parser output, the two-parser concern does not exist.
		{"ordinary source file", "/repository/source/Thing.ts", "if (node.typeArguments !== undefined) { }\n"},
		{"a string is not the identifier", lintRuleFile, "if (node.typeArguments === 'undefined') { }\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ruletest.Run(t, ConsistencyNoStrictUndefinedAstCheck, testCase.fileName, testCase.sourceText)
			ruletest.ExpectClean(t, result)
		})
	}
}

// The message quotes what the author wrote, so the repair is a copy edit rather than a translation.
func TestConsistencyNoStrictUndefinedAstCheckQuotesTheSource(t *testing.T) {
	result := ruletest.Run(t, ConsistencyNoStrictUndefinedAstCheck, lintRuleFile,
		"if (annotation.typeArguments !== undefined) { }\n")
	ruletest.ExpectFindings(t, result, "strictUndefinedAstCheck")

	description := result.Diagnostics[0].Message.Description
	for _, want := range []string{"annotation.typeArguments !== undefined", "annotation.typeArguments != null"} {
		if !strings.Contains(description, want) {
			t.Fatalf("expected the message to contain %q, got: %s", want, description)
		}
	}
}
