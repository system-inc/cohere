package markdown

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/format_options"
	"github.com/system-inc/cohere/internal/format/oracle_testing"
)

// The proseWrap oracle. Every corpus prints with proseWrap "preserve", where a line break returns before
// whitespace.js reaches its CJK and Korean rules, and format_options.Options does not carry the option. So
// these fixtures run the same bundles in their own runtime with proseWrap "always" and "never", against
// the port with the same setting.

// proseWrapOracle formats markdown with the embedded bundles and a proseWrap of its own. Its answers are
// recorded (see oracle_testing), so the bundles run only when they are recorded again.
type proseWrapOracle struct {
	golden *oracle_testing.Golden
}

func newProseWrapOracle(test *testing.T) *proseWrapOracle {
	test.Helper()
	return &proseWrapOracle{golden: oracle_testing.Open(test, test.Name())}
}

// proseWrapScript formats __input's source as the markdown at fixture.md, with its options.
const proseWrapScript = `(() => {
	const input = JSON.parse(__input);
	return prettier.format(input.source, {
		filepath: "fixture.md", parser: "markdown", plugins: prettierPlugins, proseWrap: input.proseWrap,
		printWidth: input.printWidth, tabWidth: input.tabWidth, useTabs: input.useTabs, singleQuote: input.singleQuote,
	});
})()`

// format is the recorded answer for text, keyed by everything the script passes the bundles: the text, the
// file path, the parser, the options and the proseWrap.
func (oracle *proseWrapOracle) format(text string, options format_options.Options, proseWrap string) (string, error) {
	key := oracle_testing.Key("proseWrap", text, "fixture.md", "markdown", fmt.Sprintf("%+v", options), proseWrap)
	input, marshalError := json.Marshal(struct {
		Source      string `json:"source"`
		ProseWrap   string `json:"proseWrap"`
		PrintWidth  int    `json:"printWidth"`
		TabWidth    int    `json:"tabWidth"`
		UseTabs     bool   `json:"useTabs"`
		SingleQuote bool   `json:"singleQuote"`
	}{text, proseWrap, options.PrintWidth, options.TabWidth, options.UseTabs, options.SingleQuote})
	if marshalError != nil {
		return "", fmt.Errorf("encoding the oracle's input: %w", marshalError)
	}
	return oracle.golden.Evaluate(key, proseWrapScript, string(input))
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

func TestProseWrapFixturesMatchOracle(test *testing.T) {
	test.Parallel()
	oracle := newProseWrapOracle(test)
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
				}
			}
		}
	}
}
