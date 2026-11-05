package suppression

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// knownRules are the rules every case's run knows, beside this package's own two.
var knownRules = []string{
	"cohere-go/printf",
	"cohere-go/correctness-no-machine-path",
	"cohere-go/consistency-no-long-line",
}

type expectedDirectiveFinding struct {
	line      int
	column    int
	messageId string

	// mentions is text the message must hold, such as the dead rule's name.
	mentions string
}

// wantedOutcome names findings as "<rule leaf>@<line>" and rules by their leaf, so a table row reads as the
// fixture does.
type wantedOutcome struct {
	kept              []string
	withheld          map[string]int
	directiveFindings []expectedDirectiveFinding
}

// applyCase is one fixture run twice: as written, and with its directive altered, so every case proves the
// directive is what made the difference.
type applyCase struct {
	name          string
	findings      []string
	source        []string
	wanted        wantedOutcome
	changed       []string
	changedWanted wantedOutcome
}

var applyCases = []applyCase{
	{
		name:     "own line covers the next line, not the line after",
		findings: []string{"printf@4", "printf@5"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignore printf the format is built at run time on purpose",
			"var first = 1",
			"var second = 2",
		},
		wanted: wantedOutcome{kept: []string{"printf@5"}, withheld: map[string]int{"printf": 1}},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore correctness-no-machine-path the format is built at run time on purpose",
			"var first = 1",
			"var second = 2",
		},
		changedWanted: wantedOutcome{
			kept:              []string{"printf@4", "printf@5"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdUnused, "correctness-no-machine-path"}},
		},
	},
	{
		name:     "trailing covers its own line",
		findings: []string{"printf@3", "printf@4"},
		source: []string{
			"package fixture",
			"",
			"var first = risky() //cohere:ignore printf the format is built at run time on purpose",
			"var second = risky()",
		},
		wanted: wantedOutcome{kept: []string{"printf@4"}, withheld: map[string]int{"printf": 1}},
		changed: []string{
			"package fixture",
			"",
			"var first = risky()",
			"var second = risky()",
		},
		changedWanted: wantedOutcome{kept: []string{"printf@3", "printf@4"}},
	},
	{
		name:     "trailing after a raw string that spans lines covers its own line",
		findings: []string{"printf@4", "printf@5"},
		source: []string{
			"package fixture",
			"",
			"var first = `one",
			"two` //cohere:ignore printf the literal is the format",
			"var second = risky()",
		},
		wanted: wantedOutcome{kept: []string{"printf@5"}, withheld: map[string]int{"printf": 1}},
		changed: []string{
			"package fixture",
			"",
			"var first = `one",
			"two`",
			"var second = risky()",
		},
		changedWanted: wantedOutcome{kept: []string{"printf@4", "printf@5"}},
	},
	{
		name:     "above a blank line covers nothing and is unused",
		findings: []string{"printf@5"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignore printf the format is built at run time on purpose",
			"",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			kept:              []string{"printf@5"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdUnused, "cohere-go/printf"}},
		},
		changed: []string{
			"package fixture",
			"",
			"",
			"//cohere:ignore printf the format is built at run time on purpose",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "file-ignore covers the whole file wherever it sits",
		findings: []string{"consistency-no-long-line@2", "consistency-no-long-line@4"},
		source: []string{
			"package fixture",
			`var first = "long"`,
			"",
			`var second = "long"`,
			"",
			"//cohere:file-ignore consistency-no-long-line one fixture case per line, by design",
		},
		wanted: wantedOutcome{withheld: map[string]int{"consistency-no-long-line": 2}},
		changed: []string{
			"package fixture",
			`var first = "long"`,
			"",
			`var second = "long"`,
			"",
			"//cohere:ignore consistency-no-long-line one fixture case per line, by design",
		},
		changedWanted: wantedOutcome{
			kept:              []string{"consistency-no-long-line@2", "consistency-no-long-line@4"},
			directiveFindings: []expectedDirectiveFinding{{6, 1, MessageIdUnused, "consistency-no-long-line"}},
		},
	},
	{
		name:     "two rules with one dead report the dead one only",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignore printf,correctness-no-machine-path the format is built at run time",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			withheld:          map[string]int{"printf": 1},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdUnused, "correctness-no-machine-path"}},
		},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "the full name resolves",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignore cohere-go/printf the format is built at run time",
			"var first = risky()",
		},
		wanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore cohere-go/printfs the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{
			kept:              []string{"printf@4"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdUnknownRule, "cohere-go/printfs"}},
		},
	},
	{
		name:     "the short name resolves, and only as the part after cohere-go/",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		wanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore go/printf the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{
			kept:              []string{"printf@4"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdUnknownRule, "go/printf"}},
		},
	},
	{
		name:     "an unknown rule beside a known one is reported at the comment's column",
		findings: []string{"printf@3"},
		source: []string{
			"package fixture",
			"",
			"var first = risky() //cohere:ignore printf,no-such-rule the format is built at run time",
		},
		wanted: wantedOutcome{
			withheld:          map[string]int{"printf": 1},
			directiveFindings: []expectedDirectiveFinding{{3, 21, MessageIdUnknownRule, "no-such-rule"}},
		},
		changed: []string{
			"package fixture",
			"",
			"var first = risky() //cohere:ignore no-such-rule the format is built at run time",
		},
		changedWanted: wantedOutcome{
			kept:              []string{"printf@3"},
			directiveFindings: []expectedDirectiveFinding{{3, 21, MessageIdUnknownRule, "no-such-rule"}},
		},
	},
	{
		name:     "no reason suppresses nothing",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignore printf",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			kept:              []string{"printf@4"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdMissingReason, ""}},
		},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "punctuation is not a reason",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignore printf --",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			kept:              []string{"printf@4"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdMissingReason, ""}},
		},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore printf -- built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "no rule is malformed",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignore",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			kept:              []string{"printf@4"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdMalformed, "//cohere:ignore <rule>"}},
		},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "a space after a comma in the rule list is malformed",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignore printf, correctness-no-machine-path the format is built at run time",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			kept:              []string{"printf@4"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdMalformed, ""}},
		},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore printf,correctness-no-machine-path the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{
			withheld:          map[string]int{"printf": 1},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdUnused, "correctness-no-machine-path"}},
		},
	},
	{
		name:     "a space after the slashes is a malformed ignore",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"// cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			kept:              []string{"printf@4"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdMalformed, "no space after the slashes"}},
		},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "a space after the slashes is a malformed file-ignore",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"// cohere:file-ignore printf every format here is built at run time",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			kept:              []string{"printf@4"},
			directiveFindings: []expectedDirectiveFinding{{3, 1, MessageIdMalformed, ""}},
		},
		changed: []string{
			"package fixture",
			"",
			"//cohere:file-ignore printf every format here is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "a longer word is not the keyword",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"//cohere:ignored printf the format is built at run time",
			"var first = risky()",
		},
		wanted: wantedOutcome{kept: []string{"printf@4"}},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "a block comment is never a directive",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"",
			"/*cohere:ignore printf the format is built at run time*/",
			"var first = risky()",
		},
		wanted: wantedOutcome{kept: []string{"printf@4"}},
		changed: []string{
			"package fixture",
			"",
			"//cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "a string literal is never a directive",
		findings: []string{"printf@3", "printf@4"},
		source: []string{
			"package fixture",
			"",
			`var first = risky("//cohere:ignore printf the format is built at run time")`,
			"var second = risky()",
		},
		wanted: wantedOutcome{kept: []string{"printf@3", "printf@4"}},
		changed: []string{
			"package fixture",
			"",
			`var first = risky("") //cohere:ignore printf the format is built at run time`,
			"var second = risky()",
		},
		changedWanted: wantedOutcome{kept: []string{"printf@4"}, withheld: map[string]int{"printf": 1}},
	},
	{
		name:     "a //line directive does not move the lines a directive covers",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"//line other.go:100",
			"//cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		wanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
		changed: []string{
			"package fixture",
			"//line other.go:100",
			"// the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{kept: []string{"printf@4"}},
	},
	{
		name:     "the two engine rules are never withheld",
		findings: []string{"consistency-no-unused-ignore@3", "printf@3"},
		source: []string{
			"package fixture",
			"//cohere:ignore consistency-no-unused-ignore,consistency-require-ignore-reason the next one is a fixture",
			"//cohere:ignore printf",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			kept: []string{"consistency-no-unused-ignore@3", "printf@3"},
			directiveFindings: []expectedDirectiveFinding{
				{2, 1, MessageIdUnused, "cohere-go/consistency-no-unused-ignore"},
				{2, 1, MessageIdUnused, "cohere-go/consistency-require-ignore-reason"},
				{3, 1, MessageIdMissingReason, ""},
			},
		},
		changed: []string{
			"package fixture",
			"//cohere:ignore consistency-no-unused-ignore,printf the next one is a fixture",
			"//cohere:ignore printf",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{
			kept:     []string{"consistency-no-unused-ignore@3"},
			withheld: map[string]int{"printf": 1},
			directiveFindings: []expectedDirectiveFinding{
				{2, 1, MessageIdUnused, "cohere-go/consistency-no-unused-ignore"},
				{3, 1, MessageIdMissingReason, ""},
			},
		},
	},
	{
		name: "withheld findings are counted by rule",
		findings: []string{
			"consistency-no-long-line@3",
			"printf@4",
			"consistency-no-long-line@5",
			"printf@6",
		},
		source: []string{
			"package fixture",
			"//cohere:file-ignore consistency-no-long-line one fixture case per line, by design",
			`var first = "long"`,
			"var second = risky() //cohere:ignore printf the format is built at run time",
			`var third = "long"`,
			"var fourth = risky()",
		},
		wanted: wantedOutcome{
			kept:     []string{"printf@6"},
			withheld: map[string]int{"consistency-no-long-line": 2, "printf": 1},
		},
		changed: []string{
			"package fixture",
			"// one fixture case per line",
			`var first = "long"`,
			"var second = risky() //cohere:ignore printf the format is built at run time",
			`var third = "long"`,
			"var fourth = risky()",
		},
		changedWanted: wantedOutcome{
			kept:     []string{"consistency-no-long-line@3", "consistency-no-long-line@5", "printf@6"},
			withheld: map[string]int{"printf": 1},
		},
	},
	{
		name:     "a line directive is credited before a file-ignore",
		findings: []string{"printf@4"},
		source: []string{
			"package fixture",
			"//cohere:file-ignore printf every format here is built at run time",
			"//cohere:ignore printf the format is built at run time",
			"var first = risky()",
		},
		wanted: wantedOutcome{
			withheld:          map[string]int{"printf": 1},
			directiveFindings: []expectedDirectiveFinding{{2, 1, MessageIdUnused, "cohere-go/printf"}},
		},
		changed: []string{
			"package fixture",
			"//cohere:file-ignore printf every format here is built at run time",
			"// the format is built at run time",
			"var first = risky()",
		},
		changedWanted: wantedOutcome{withheld: map[string]int{"printf": 1}},
	},
}

func TestApply(test *testing.T) {
	test.Parallel()
	for _, testCase := range applyCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			checkOutcome(test, "as written", testCase.source, testCase.findings, testCase.wanted)
			checkOutcome(test, "changed", testCase.changed, testCase.findings, testCase.changedWanted)
		})
	}
}

// TestApplyCoversOnlyItsOwnFile keeps a directive from reaching the same line of another file, and keeps a
// finding in a file nobody indexed.
func TestApplyCoversOnlyItsOwnFile(test *testing.T) {
	test.Parallel()
	source := "package fixture\n//cohere:ignore printf the format is built at run time\nvar first = risky()\n"
	indexes := []*Index{Build("a.go", []byte(source)), Build("b.go", []byte("package fixture\n"))}
	findings := []Finding{
		{Rule: "cohere-go/printf", File: "a.go", Line: 3},
		{Rule: "cohere-go/printf", File: "b.go", Line: 3},
		{Rule: "cohere-go/printf", File: "c.go", Line: 3},
	}

	outcome := Apply(knownRules, knownRules, indexes, findings)

	if !slices.Equal(outcome.Kept, findings[1:]) {
		test.Fatalf("kept %v, want the findings in b.go and c.go", outcome.Kept)
	}
	if len(outcome.DirectiveFindings) != 0 {
		test.Fatalf("directive findings %v, want none", outcome.DirectiveFindings)
	}
}

// TestApplyLeavesTheIndexUnchanged runs one set of indexes twice. An Apply that recorded what it withheld on
// the Index would call the directive used on the second run before any finding reached it.
func TestApplyLeavesTheIndexUnchanged(test *testing.T) {
	test.Parallel()
	source := "package fixture\n//cohere:ignore printf the format is built at run time\nvar first = risky()\n"
	indexes := []*Index{Build("a.go", []byte(source))}

	Apply(knownRules, knownRules, indexes, []Finding{{Rule: "cohere-go/printf", File: "a.go", Line: 3}})
	outcome := Apply(knownRules, knownRules, indexes, nil)

	if len(outcome.DirectiveFindings) != 1 || outcome.DirectiveFindings[0].MessageId != MessageIdUnused {
		test.Fatalf("directive findings %v, want the one unused directive", outcome.DirectiveFindings)
	}
}

// TestApplyLeavesARuleThatDidNotRunAlone runs one directive naming two rules, one with a finding to withhold and
// one without, twice: once with both rules run, where the second is reported unused, and once with the second
// turned off by the settings, where it is neither used nor unused and nothing is reported.
func TestApplyLeavesARuleThatDidNotRunAlone(test *testing.T) {
	test.Parallel()
	source := "package fixture\n//cohere:ignore printf,correctness-no-machine-path built at run time\n" +
		"var first = risky()\n"
	findings := []Finding{{Rule: "cohere-go/printf", File: "a.go", Line: 3}}

	bothRan := Apply(knownRules, knownRules, []*Index{Build("a.go", []byte(source))}, findings)
	if len(bothRan.DirectiveFindings) != 1 || bothRan.DirectiveFindings[0].MessageId != MessageIdUnused ||
		!strings.Contains(bothRan.DirectiveFindings[0].Message, "correctness-no-machine-path") {
		test.Fatalf("with both rules run, directive findings %s, want correctness-no-machine-path unused",
			renderDirectiveFindings(bothRan.DirectiveFindings))
	}

	onlyPrintf := Apply(knownRules, []string{"cohere-go/printf"}, []*Index{Build("a.go", []byte(source))}, findings)
	if len(onlyPrintf.DirectiveFindings) != 0 {
		test.Fatalf("with correctness-no-machine-path off, directive findings %s, want none",
			renderDirectiveFindings(onlyPrintf.DirectiveFindings))
	}
	if len(onlyPrintf.Kept) != 0 || onlyPrintf.WithheldByRule["cohere-go/printf"] != 1 {
		test.Fatalf("with correctness-no-machine-path off, kept %v and withheld %v, want printf withheld",
			onlyPrintf.Kept, onlyPrintf.WithheldByRule)
	}
}

// TestApplyStillReportsAnUnknownRuleAndAMissingReason keeps the rules that ran from excusing what is wrong with a
// directive itself: a name no rule has, and a directive with no reason, are findings whatever ran.
func TestApplyStillReportsAnUnknownRuleAndAMissingReason(test *testing.T) {
	test.Parallel()
	source := "package fixture\n//cohere:ignore no-such-rule built at run time\n//cohere:ignore printf\nvar first = 1\n"
	outcome := Apply(knownRules, nil, []*Index{Build("a.go", []byte(source))}, nil)

	var messageIds []string
	for _, directiveFinding := range outcome.DirectiveFindings {
		messageIds = append(messageIds, directiveFinding.MessageId)
	}
	if !slices.Equal(messageIds, []string{MessageIdUnknownRule, MessageIdMissingReason}) {
		test.Fatalf("with no rule run, directive findings %s, want an unknown rule then a missing reason",
			renderDirectiveFindings(outcome.DirectiveFindings))
	}
}

// TestBuildConcurrently builds and applies from many goroutines at once, as the engine does per package.
func TestBuildConcurrently(test *testing.T) {
	test.Parallel()
	source := []byte("package fixture\n//cohere:ignore printf the format is built at run time\nvar first = risky()\n")
	outcomes := make([]Outcome, 32)
	var group sync.WaitGroup
	for slot := range outcomes {
		group.Go(func() {
			file := fmt.Sprintf("file%d.go", slot)
			findings := []Finding{{Rule: "cohere-go/printf", File: file, Line: 3}}
			outcomes[slot] = Apply(knownRules, knownRules, []*Index{Build(file, source)}, findings)
		})
	}
	group.Wait()

	for slot, outcome := range outcomes {
		if len(outcome.Kept) != 0 || outcome.WithheldByRule["cohere-go/printf"] != 1 {
			test.Fatalf("goroutine %d kept %v and withheld %v, want the one finding withheld", slot, outcome.Kept,
				outcome.WithheldByRule)
		}
	}
}

func checkOutcome(test *testing.T, label string, source []string, findingSpecs []string, wanted wantedOutcome) {
	test.Helper()
	index := Build("fixture.go", []byte(strings.Join(source, "\n")+"\n"))
	outcome := Apply(knownRules, knownRules, []*Index{index}, parseFindings(test, "fixture.go", findingSpecs))

	var kept []string
	for _, finding := range outcome.Kept {
		kept = append(kept, fmt.Sprintf("%s@%d", strings.TrimPrefix(finding.Rule, ruleNamespace), finding.Line))
	}
	if !slices.Equal(kept, wanted.kept) {
		test.Errorf("%s: kept %v, want %v", label, kept, wanted.kept)
	}

	withheld := map[string]int{}
	for ruleName, count := range wanted.withheld {
		withheld[ruleNamespace+ruleName] = count
	}
	if !maps.Equal(outcome.WithheldByRule, withheld) {
		test.Errorf("%s: withheld %v, want %v", label, outcome.WithheldByRule, withheld)
	}

	if len(outcome.DirectiveFindings) != len(wanted.directiveFindings) {
		test.Fatalf("%s: directive findings %s, want %v", label, renderDirectiveFindings(outcome.DirectiveFindings),
			wanted.directiveFindings)
	}
	for position, expected := range wanted.directiveFindings {
		actual := outcome.DirectiveFindings[position]
		matches := actual.File == "fixture.go" && actual.Line == expected.line && actual.Column == expected.column &&
			actual.MessageId == expected.messageId && actual.Rule == ruleOfMessageId[expected.messageId] &&
			strings.Contains(actual.Message, expected.mentions) && actual.Message != "" &&
			!strings.Contains(actual.Message, "{{")
		if !matches {
			test.Errorf("%s: directive finding %d is %s, want %+v", label, position,
				renderDirectiveFindings([]DirectiveFinding{actual}), expected)
		}
	}
}

var ruleOfMessageId = map[string]string{
	MessageIdMissingReason: RuleRequireIgnoreReason,
	MessageIdMalformed:     RuleRequireIgnoreReason,
	MessageIdUnused:        RuleNoUnusedIgnore,
	MessageIdUnknownRule:   RuleNoUnusedIgnore,
}

func parseFindings(test *testing.T, file string, specs []string) []Finding {
	test.Helper()
	var findings []Finding
	for _, spec := range specs {
		ruleName, lineText, found := strings.Cut(spec, "@")
		line, parseError := strconv.Atoi(lineText)
		if !found || parseError != nil {
			test.Fatalf("finding %q is not <rule>@<line>", spec)
		}
		findings = append(findings, Finding{Rule: ruleNamespace + ruleName, File: file, Line: line})
	}
	return findings
}

func renderDirectiveFindings(directiveFindings []DirectiveFinding) string {
	var rendered []string
	for _, directiveFinding := range directiveFindings {
		rendered = append(rendered, fmt.Sprintf("%s:%d:%d %s/%s %q", directiveFinding.File, directiveFinding.Line,
			directiveFinding.Column, directiveFinding.Rule, directiveFinding.MessageId, directiveFinding.Message))
	}
	return "[" + strings.Join(rendered, ", ") + "]"
}
