package markdown

import (
	"fmt"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
)

// The proseWrap oracle. Every corpus prints with proseWrap "preserve", where a line break returns before
// whitespace.js reaches its CJK and Korean rules, and format_options.Options does not carry the option. So
// these fixtures hold the port, with proseWrap "always" and "never", to what the fork's bundles printed
// with the same setting.

// proseWrapOracle answers as the fork formatted markdown with a proseWrap of its own: its bundle's
// prettier.format on the text at fixture.md with parser "markdown", the proseWrap and the options' printWidth,
// tabWidth, useTabs and singleQuote. Its answers are recorded (see oracle_testing).
type proseWrapOracle struct {
	golden *oracle_testing.Golden
}

func newProseWrapOracle(test *testing.T) *proseWrapOracle {
	test.Helper()
	return &proseWrapOracle{golden: oracle_testing.Open(test, test.Name())}
}

// format is the recorded answer for text, keyed by everything the fork was passed: the text, the file path,
// the parser, the options and the proseWrap.
func (oracle *proseWrapOracle) format(text string, options format_options.Options, proseWrap string) (string, error) {
	return oracle.golden.Answer(
		oracle_testing.Key("proseWrap", text, "fixture.md", "markdown", fmt.Sprintf("%+v", options), proseWrap),
	)
}

// proseWrapFixtures are prose the always and never branches print differently from preserve: long
// lines to wrap, breaks to join, and the CJK, Korean and punctuation rules between them.
var proseWrapFixtures = []string{
	"a b c d e f g h i j k l m n o p q r s t u v w x y z a b c d e f g h i j k l m n o p q r s t u v w x y z",
	"a\nb\nc", "a  \nb", "# a\nb", "a\n=\nb", "- a\n  b\n- c", "> a\n> b", "[a\nb](c)", "[[a\nb]]",
	"| a\nb |", "`a\nb`", "*a\nb*", "a\n- b", "a\n> b", "a\n# b", "a\n1. b", "a\n+ b", "a\n10) b",
	"a\n-\nb", "a\n    ===", "[a\nb]\n\n[a b]: c", "[x][a\nb]\n\n[a b]: c", "![a\nb]\n\n[a b]: c",
	"中文\n中文", "中文\nabc", "abc\n中文", "한국어\n한국어", "한국어\n中文", "中文\n한국어", "中文。\n中文",
	"中文\n。中文", "a\n한국어", "中文\n：中文", "中文\n(abc)", "(abc)\n中文", "中文 abc 中文\nabc 中文",
	"中文abc中文\nabc中文", "中文 abc 中文 abc\n中文", "これはひどい……\nなんと", "ア〜\nエの中から",
	":::\n句子句子句子\n:::", "汉字\n汉字\n汉字", "中文 中文\n中文", "中文\u3000中文\n中文", "a\u3000b\nc",
	"[中文\n中文](a)", "中文" + strings.Repeat("中文", 40), strings.Repeat("abc 中文 ", 20),
	"*a*\nb", "a\n*b*", "**中文**\nabc", "`a`\n中文", "a[^1]\nb\n\n[^1]: c\nd", "[^1]: a\n  b",
}

// unsettledProseWrapFixtures are the inputs whose fork output the fork itself rewrites, so the port, which prints as
// the fork does, rewrites it on a second pass too. Their second pass is held to the fork's own, recorded beside the
// first in <test>SecondPass.json, so a change to it in either direction fails, settling included. One whose
// recorded second pass settles everywhere fails too, so the list only shrinks.
var unsettledProseWrapFixtures = map[string]bool{
	// Under proseWrap always, the blank quoted line after the ignored list prints twice, so each pass adds one.
	"> <!-- prettier-ignore -->\n> - a\n>   - b\n>\n> c": true,
}

func TestProseWrapFixturesMatchOracle(test *testing.T) {
	test.Parallel()
	oracle := newProseWrapOracle(test)
	forkAgain := &proseWrapOracle{golden: oracle_testing.Open(test, test.Name()+"SecondPass")}
	unsettled := map[string]bool{}
	for _, proseWrap := range []string{"always", "never", "preserve"} {
		for variantIndex, options := range formatVariants {
			for _, input := range append(append([]string{}, proseWrapFixtures...), formatFixtures...) {
				if dependsOnEmbed(input) {
					continue
				}
				expected, formatError := oracle.format(input, options, proseWrap)
				if formatError != nil {
					test.Fatalf("the oracle failed on %q: %v", input, formatError)
				}
				actual, formatWithProseWrapError := formatWithProseWrap(input, options, proseWrap, nil)
				if formatWithProseWrapError != nil {
					test.Errorf("%s, variant %d, %q: %v", proseWrap, variantIndex, input, formatWithProseWrapError)
					continue
				}
				if actual != expected {
					test.Errorf(
						"%s, variant %d, %q: %s",
						proseWrap,
						variantIndex,
						input,
						differential.FirstDifference(expected, actual),
					)
					continue
				}
				// Formatted output formats to itself, or every run rewrites what the last one wrote.
				again, againError := formatWithProseWrap(expected, options, proseWrap, nil)
				if unsettledProseWrapFixtures[input] {
					want, wantError := forkAgain.format(expected, options, proseWrap)
					unsettled[input] = unsettled[input] || wantError != nil || want != expected
					if (againError != nil) != (wantError != nil) || again != want {
						test.Errorf("%s, variant %d, %q: formatted again, not as the fork formats it again (%v, the "+
							"fork %v): %s", proseWrap, variantIndex, input, againError, wantError,
							differential.FirstDifference(want, again))
					}
				} else if againError != nil || again != expected {
					test.Errorf("%s, variant %d, %q: formatted again (%v): %s", proseWrap, variantIndex, input,
						againError, differential.FirstDifference(expected, again))
				}
			}
		}
	}
	// Only a clean run judges the list: an input whose first pass differed never reached its second.
	for input := range unsettledProseWrapFixtures {
		if !test.Failed() && !unsettled[input] {
			test.Errorf("the fork formats %q to itself on a second pass, so take it out of unsettledProseWrapFixtures",
				input)
		}
	}
}
