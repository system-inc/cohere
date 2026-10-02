package markdown

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/system-inc/cohere/internal/format/differential"
	"github.com/system-inc/cohere/internal/format/prettier"
)

// The proseWrap oracle. Every corpus prints with proseWrap "preserve", where a line break returns before
// whitespace.js reaches its CJK and Korean rules, and prettier.Options does not carry the option. So
// these fixtures run the same bundles in their own runtime with proseWrap "always" and "never", against
// the port with the same setting.

// proseWrapOracle formats markdown with the embedded bundles and a proseWrap of its own.
type proseWrapOracle struct{ runtime *goja.Runtime }

func newProseWrapOracle(t *testing.T) *proseWrapOracle {
	t.Helper()
	bundles, err := prettier.Bundles()
	if err != nil {
		t.Fatal(err)
	}
	runtime := goja.New()
	for _, name := range prettier.BundleFiles {
		if _, err := runtime.RunString(string(bundles.Files[name])); err != nil {
			t.Fatalf("evaluating %s: %v", name, err)
		}
	}
	return &proseWrapOracle{runtime: runtime}
}

func (oracle *proseWrapOracle) format(text string, options prettier.Options, proseWrap string) (string, error) {
	oracle.runtime.Set("__source", text)
	oracle.runtime.Set("__proseWrap", proseWrap)
	oracle.runtime.Set("__printWidth", options.PrintWidth)
	oracle.runtime.Set("__tabWidth", options.TabWidth)
	oracle.runtime.Set("__useTabs", options.UseTabs)
	oracle.runtime.Set("__singleQuote", options.SingleQuote)
	value, err := oracle.runtime.RunString(`prettier.format(__source, {
		filepath: "fixture.md", parser: "markdown", plugins: prettierPlugins, proseWrap: __proseWrap,
		printWidth: __printWidth, tabWidth: __tabWidth, useTabs: __useTabs, singleQuote: __singleQuote,
	})`)
	if err != nil {
		return "", err
	}
	// prettier.format returns a promise; each RunString drains goja's job queue, so pump until settled.
	promise, isPromise := value.Export().(*goja.Promise)
	if !isPromise {
		return value.String(), nil
	}
	for pump := 0; pump < 100000 && promise.State() == goja.PromiseStatePending; pump++ {
		if _, err := oracle.runtime.RunString("0"); err != nil {
			return "", err
		}
	}
	if promise.State() != goja.PromiseStateFulfilled {
		return "", fmt.Errorf("prettier.format did not fulfil: %v", promise.Result())
	}
	return promise.Result().String(), nil
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

func TestProseWrapFixturesMatchOracle(t *testing.T) {
	oracle := newProseWrapOracle(t)
	for _, proseWrap := range []string{"always", "never", "preserve"} {
		for variantIndex, options := range formatVariants {
			for _, input := range append(append([]string{}, proseWrapFixtures...), formatFixtures...) {
				if dependsOnEmbed(input) {
					continue
				}
				expected, err := oracle.format(input, options, proseWrap)
				if err != nil {
					t.Fatalf("the oracle failed on %q: %v", input, err)
				}
				actual, err := formatWithProseWrap(input, options, proseWrap, nil)
				if err != nil {
					t.Errorf("%s, variant %d, %q: %v", proseWrap, variantIndex, input, err)
					continue
				}
				if actual != expected {
					t.Errorf("%s, variant %d, %q: %s", proseWrap, variantIndex, input, differential.FirstDifference(expected, actual))
				}
			}
		}
	}
}
