package mediaquery

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/printing"
)

// renderTree writes a tree as one line per node: type, value, sourceIndex, before and after, indented by
// depth, so a test can state the whole tree the library builds.
func renderTree(node *estree.Node, depth int, builder *strings.Builder) {
	builder.WriteString(strings.Repeat("  ", depth))
	nodeType := node.Type()
	if nodeType == "" {
		nodeType = "<undefined>"
	}
	builder.WriteString(nodeType)
	builder.WriteString(" " + quote(node.String("value")))
	sourceIndex, _ := node.Get("sourceIndex").(int)
	builder.WriteString(" @" + itoa(sourceIndex))
	builder.WriteString(" before=" + quote(node.String("before")) + " after=" + quote(node.String("after")))
	builder.WriteString("\n")
	for _, child := range node.List("nodes") {
		renderTree(child, depth+1, builder)
	}
}

func quote(text string) string { return "\"" + strings.ReplaceAll(text, "\n", "\\n") + "\"" }

func itoa(number int) string {
	if number == 0 {
		return "0"
	}
	digits := ""
	for number > 0 {
		digits = string(rune('0'+number%10)) + digits
		number /= 10
	}
	return digits
}

// TestParseTrees runs without node: trees taken from the library's own output for these params.
func TestParseTrees(t *testing.T) {
	cases := []struct {
		params string
		want   string
	}{
		{
			params: "screen and (min-width: 768px)",
			want: `media-query-list "screen and (min-width: 768px)" @0 before="" after=""
  media-query "screen and (min-width: 768px)" @0 before="" after=""
    media-type "screen" @0 before="" after=" "
    keyword "and" @7 before=" " after=" "
    media-feature-expression "(min-width: 768px)" @11 before=" " after=""
      media-feature "min-width" @12 before="" after=""
      colon ":" @21 before="" after=" "
      value "768px" @23 before=" " after=""
`,
		},
		{
			params: " url(a) print , (b)",
			want: `media-query-list "url(a) print , (b)" @1 before=" " after=""
  url "url(a)" @1 before=" " after=" "
  media-query "print" @8 before=" " after=" "
    media-type "print" @8 before=" " after=""
  media-query "(b)" @16 before=" " after=""
    media-feature-expression "(b)" @16 before=" " after=""
      media-feature "b" @17 before="" after=""
`,
		},
		{
			// Four words the post-pass cannot decide: every type stays undefined, for addMissingType.
			params: "a b c d",
			want: `media-query-list "a b c d" @0 before="" after=""
  media-query "a b c d" @0 before="" after=""
    <undefined> "a" @0 before="" after=" "
    <undefined> "b" @2 before=" " after=" "
    <undefined> "c" @4 before=" " after=" "
    <undefined> "d" @6 before=" " after=""
`,
		},
		{
			// sourceIndex counts bytes: the e-acute is two.
			params: "\xc3\xa9 and (x)",
			want: `media-query-list "<e> and (x)" @0 before="" after=""
  media-query "<e> and (x)" @0 before="" after=""
    media-type "<e>" @0 before="" after=" "
    keyword "and" @3 before=" " after=" "
    media-feature-expression "(x)" @7 before=" " after=""
      media-feature "x" @8 before="" after=""
`,
		},
	}
	for _, each := range cases {
		tree, err := Parse(each.params)
		if err != nil {
			t.Errorf("%q: %v", each.params, err)
			continue
		}
		var builder strings.Builder
		renderTree(tree, 0, &builder)
		want := strings.ReplaceAll(each.want, "<e>", "\xc3\xa9")
		if builder.String() != want {
			t.Errorf("%q:\ngot\n%s\nwant\n%s", each.params, builder.String(), want)
		}
	}
}

// TestParseRefusals checks the two ways the library does not return: a TypeError from an unbalanced "}"
// in a feature, and an endless loop on an unclosed url( (which the oracle cannot run).
func TestParseRefusals(t *testing.T) {
	for _, params := range []string{"(a} b", "screen and (color}", "url(", "  url (a(b)"} {
		tree, err := Parse(params)
		if err == nil {
			t.Errorf("%q: parsed to %v, want a refusal", params, tree.Type())
			continue
		}
		if !printing.IsSyntax(err) {
			t.Errorf("%q: %v is not marked as a syntax error", params, err)
		}
	}
}
