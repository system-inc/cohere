package line_wrap

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
)

// wrapCases each hold a source with long lines and the exact output the tool must produce under options.
var wrapCases = []struct {
	name    string
	options Options
	source  string
	want    string
}{
	{
		name: "a table row breaks into one element per line",
		source: `package p

var rows = []struct {
	source  string
	options string
}{
	{"const foo = function*(param: string, other: number) {}; // allow: generatorFunctions", "{\"allow\": [\"generatorFunctions\"]}"},
	{"short", ""},
}
`,
		want: `package p

var rows = []struct {
	source  string
	options string
}{
	{
		"const foo = function*(param: string, other: number) {}; // allow: generatorFunctions",
		"{\"allow\": [\"generatorFunctions\"]}",
	},
	{"short", ""},
}
`,
	},
	{
		name: "a call breaks one argument per line",
		source: `package p

func f() {
	report(context, "the description of what went wrong here, in a sentence", firstArgumentName, secondArgumentName, third)
}
`,
		want: `package p

func f() {
	report(
		context,
		"the description of what went wrong here, in a sentence",
		firstArgumentName,
		secondArgumentName,
		third,
	)
}
`,
	},
	{
		name: "a signature breaks one parameter per line and keeps a grouped pair together",
		source: `package p

func (utility Utility) readingOf(root, literal string, resolved ResolvedUtilityValue, theme *Theme, negative bool) (Reading, bool) {
	return Reading{}, false
}
`,
		want: `package p

func (utility Utility) readingOf(
	root, literal string,
	resolved ResolvedUtilityValue,
	theme *Theme,
	negative bool,
) (Reading, bool) {
	return Reading{}, false
}
`,
	},
	{
		name: "a condition breaks after each operator of the chain",
		source: `package p

func f() {
	if object != nil && object.Package() != nil && strings.HasSuffix(object.Package().Path(), "/lint/rule") && ready && enabled {
		return
	}
}
`,
		want: `package p

func f() {
	if object != nil &&
		object.Package() != nil &&
		strings.HasSuffix(object.Package().Path(), "/lint/rule") &&
		ready &&
		enabled {
		return
	}
}
`,
	},
	{
		name: "a call around a long string breaks so the string sits alone on its line, whole",
		source: `package p

func f() {
	t.Errorf("the quick brown fox jumps over the lazy dog, then keeps running across the field until the sun goes down at %s", value)
}
`,
		want: `package p

func f() {
	t.Errorf(
		"the quick brown fox jumps over the lazy dog, then keeps running across the field until the sun goes down at %s",
		value,
	)
}
`,
	},
	{
		name: "a table row around a long string breaks so the string sits alone on its line, whole",
		source: `package p

var rows = []struct {
	source string
	ids    []string
}{
	{"if (!!foo) { the quick brown fox jumps over the lazy dog and keeps running until it reaches the edge of the field }", []string{"id"}},
}
`,
		want: `package p

var rows = []struct {
	source string
	ids    []string
}{
	{
		"if (!!foo) { the quick brown fox jumps over the lazy dog and keeps running until it reaches the edge of the field }",
		[]string{"id"},
	},
}
`,
	},
	{
		name: "a long string after code no shape can break is left whole",
		source: `package p

const message = "the quick brown fox jumps over the lazy dog, then keeps running across the field until the sun goes down at %s"
`,
		want: `package p

const message = "the quick brown fox jumps over the lazy dog, then keeps running across the field until the sun goes down at %s"
`,
	},
	{
		name: "a raw string's body is never touched while the row around its opening line breaks",
		source: "package p\n\nvar rows = []struct{ name, source string }{\n" +
			"\t{\"a name long enough to push the opening of the raw string past the limit\", " +
			"`first line of the body, which runs on far enough to push this opening line past the limit\n" +
			"second line of the body, long on purpose so that it runs well past the limit of one hundred and twenty columns\n`},\n}\n",
		want: "package p\n\nvar rows = []struct{ name, source string }{\n\t{\n" +
			"\t\t\"a name long enough to push the opening of the raw string past the limit\",\n" +
			"\t\t`first line of the body, which runs on far enough to push this opening line past the limit\n" +
			"second line of the body, long on purpose so that it runs well past the limit of one hundred and twenty columns\n`,\n\t},\n}\n",
	},
	{
		name: "a comment reflows from the long line, carrying words into the next line of the paragraph",
		source: `package p

// Reading answers the question the engine asks of a candidate class, using the theme the repository declares for its own
// classes when there is one. The rest of the paragraph stays.
//
// A second paragraph is untouched.
func Reading() {}
`,
		want: `package p

// Reading answers the question the engine asks of a candidate class, using the theme the repository declares for its
// own classes when there is one. The rest of the paragraph stays.
//
// A second paragraph is untouched.
func Reading() {}
`,
	},
	{
		name: "a long struct tag is left whole",
		source: `package p

type Settings struct {
	Name string "the tag of this field is a string literal with spaces in it that runs well past the limit of the line"
}
`,
		want: `package p

type Settings struct {
	Name string "the tag of this field is a string literal with spaces in it that runs well past the limit of the line"
}
`,
	},
	{
		name: "a doc-comment code block and a list item are never reflowed",
		source: `package p

// Run is invoked like this:
//
//	go run ./internal/lint/rules/react/tools/vendor_fixtures -sha commit -out internal/react_conformance/testdata fixtures
//
// It has two modes:
//   - one that writes the fixtures to the directory given and reports how many were written, skipped, or refused
func Run() {}
`,
		want: `package p

// Run is invoked like this:
//
//	go run ./internal/lint/rules/react/tools/vendor_fixtures -sha commit -out internal/react_conformance/testdata fixtures
//
// It has two modes:
//   - one that writes the fixtures to the directory given and reports how many were written, skipped, or refused
func Run() {}
`,
	},
	{
		name:    "with keyed strings exempt, a row breaks so its keyed long string starts its line",
		options: Options{ExemptKeyedStrings: true},
		source: `package p

var rows = []row{
	{name: "first", source: "the quick brown fox jumps over the lazy dog, then keeps running across the field until the sun goes down at %s"},
}
`,
		want: `package p

var rows = []row{
	{
		name:   "first",
		source: "the quick brown fox jumps over the lazy dog, then keeps running across the field until the sun goes down at %s",
	},
}
`,
	},
}

func TestWrap(test *testing.T) {
	test.Parallel()
	for _, testCase := range wrapCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result, wrapError := Wrap([]byte(testCase.source), testCase.options)
			if wrapError != nil {
				test.Fatalf("Wrap: %v", wrapError)
			}
			if result.Rejected != nil {
				test.Fatalf("the equivalence proof rejected the output: %v", result.Rejected)
			}
			if string(result.Output) != testCase.want {
				test.Errorf("output differs.\ngot:\n%s\nwant:\n%s", result.Output, testCase.want)
			}
		})
	}
}

// TestWrapIsIdempotent runs every case's output through the tool again: the second run changes nothing.
func TestWrapIsIdempotent(test *testing.T) {
	test.Parallel()
	for _, testCase := range wrapCases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			first, wrapError := Wrap([]byte(testCase.source), testCase.options)
			if wrapError != nil {
				test.Fatalf("first run: %v", wrapError)
			}
			second, wrapError := Wrap(first.Output, testCase.options)
			if wrapError != nil {
				test.Fatalf("second run: %v", wrapError)
			}
			if second.Passes != 0 || string(second.Output) != string(first.Output) {
				test.Errorf("the second run changed the file in %d passes:\n%s", second.Passes, second.Output)
			}
		})
	}
}

func TestEquivalentAcceptsLineBreaksAndReflow(test *testing.T) {
	test.Parallel()
	before := "package p\n\n// one two three four\nvar v = f(a, b...)\n"
	after := "package p\n\n// one two\n// three four\nvar v = f(\n\ta,\n\tb...,\n)\n"
	if equivalentError := Equivalent([]byte(before), []byte(after)); equivalentError != nil {
		test.Errorf("a pure rewrap was rejected: %v", equivalentError)
	}
}

// TestEquivalentRejectsBrokenTransforms feeds the proof outputs a buggy transform could produce. Each must fail.
func TestEquivalentRejectsBrokenTransforms(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name   string
		before string
		after  string
	}{
		{
			name:   "a string split into a concatenation of the same value",
			before: "package p\n\nvar s = \"the quick brown fox\"\n",
			after:  "package p\n\nvar s = \"the quick \" +\n\t\"brown fox\"\n",
		},
		{
			name:   "a string literal respelled with the same value",
			before: "package p\n\nvar s = \"A\"\n",
			after:  "package p\n\nvar s = \"\\x41\"\n",
		},
		{
			name:   "an operator changed while breaking a chain",
			before: "package p\n\nvar ok = a && b && c\n",
			after:  "package p\n\nvar ok = a &&\n\tb ||\n\tc\n",
		},
		{
			name:   "a dropped trailing argument",
			before: "package p\n\nvar v = f(a, b, c)\n",
			after:  "package p\n\nvar v = f(\n\ta,\n\tb,\n)\n",
		},
		{
			name:   "a variadic spread lost",
			before: "package p\n\nvar v = f(a, b...)\n",
			after:  "package p\n\nvar v = f(\n\ta,\n\tb,\n)\n",
		},
		{
			name:   "a reflow that reorders comment words",
			before: "package p\n\n// one two three four\nfunc F() {}\n",
			after:  "package p\n\n// one two four\n// three\nfunc F() {}\n",
		},
		{
			name:   "a reflow that drops a comment word",
			before: "package p\n\n// one two three four\nfunc F() {}\n",
			after:  "package p\n\n// one two\n// three\nfunc F() {}\n",
		},
	} {
		if equivalentError := Equivalent([]byte(testCase.before), []byte(testCase.after)); equivalentError == nil {
			test.Errorf("%s: the proof accepted it", testCase.name)
		} else if strings.Contains(equivalentError.Error(), "parse") {
			test.Errorf("%s: the case does not parse, so it proves nothing: %v", testCase.name, equivalentError)
		}
	}
}

// TestClassify pins the definition of a violation, line by line, with keyed strings exempt and without.
// wantKeyedExempt is the category with Options.ExemptKeyedStrings set, when it differs from want.
func TestClassify(test *testing.T) {
	test.Parallel()
	long := strings.Repeat("word ", 26)
	url := "https://example.com/" + strings.Repeat("segment/", 8)
	lines := []struct {
		text            string
		want            Category
		wantKeyedExempt Category
	}{
		{text: "package p"},
		{text: ""},
		{text: "import alias \"example.com/" + strings.Repeat("a", 120) + "\"", want: CategoryImport},
		{text: "", want: ""},
		{text: "//go:generate go run ./tools/generate -out " + strings.Repeat("x", 100), want: CategoryDirective},
		{text: "var rows = []string{"},
		{text: "\t\"" + long + "\",", want: CategoryStringAlone},
		{text: "\t\"" + long + "\" +", want: CategoryActionable},
		{text: "\t\t\"tail\","},
		{text: "\t\"" + long + "\", // a comment after it", want: CategoryActionable},
		{text: "\tf(\"" + long + "\"),", want: CategoryActionable},
		{text: "\t\"" + long + "\"[0:3],", want: CategoryActionable},
		{text: "\t`" + long + "", want: CategoryStringAlone},
		{text: long + long, want: CategoryRawStringBody},
		{text: long + "`,", want: CategoryRawStringBody},
		{text: "}"},
		{text: "var m = map[string]string{"},
		{text: "\t\"key\": \"" + long + "\",", want: CategoryActionable, wantKeyedExempt: CategoryKeyedString},
		{text: "}"},
		{text: "var s = T{"},
		{text: "\tsource: \"" + long + "\",", want: CategoryActionable, wantKeyedExempt: CategoryKeyedString},
		{text: "\tsource: `" + long + "`,", want: CategoryActionable, wantKeyedExempt: CategoryKeyedString},
		{text: "\tsource: \"" + long + "\", // a comment after it", want: CategoryActionable},
		{text: "\tsource: \"" + long + "\" + suffix,", want: CategoryActionable},
		{text: "\tsource: f(\"" + long + "\"),", want: CategoryActionable},
		{text: "\tfirst: 1, source: \"" + long + "\",", want: CategoryActionable},
		{text: "}"},
		{text: "var ("},
		{text: "\tname = \"" + long + "\"", want: CategoryActionable, wantKeyedExempt: CategoryKeyedString},
		{text: "\ttyped string = \"" + long + "\"", want: CategoryActionable},
		{text: ")"},
		{text: "const message = \"" + long + "\"", want: CategoryActionable},
		{text: "func f() {"},
		{text: "\tname := \"" + long + "\"", want: CategoryActionable, wantKeyedExempt: CategoryKeyedString},
		{text: "\tname = \"" + long + "\"", want: CategoryActionable, wantKeyedExempt: CategoryKeyedString},
		{text: "\tname += \"" + long + "\"", want: CategoryActionable},
		{text: "\tfirst, second := \"" + long + "\", \"x\"", want: CategoryActionable},
		{text: "\tobject.field = \"" + long + "\"", want: CategoryActionable},
		{text: "}"},
		{text: "// see the upstream source at " + strings.Repeat("u", 60) + " " + url, want: CategoryUnbrokenToken},
		{text: "// " + long + long, want: CategoryActionable},
		{text: "var x int"},
	}
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(line.text + "\n")
	}
	parsed, parseSourceError := parseSource([]byte(builder.String()), Options{})
	if parseSourceError != nil {
		test.Fatalf("parse: %v\n%s", parseSourceError, builder.String())
	}
	keyedExempt, parseSourceError := parseSource([]byte(builder.String()), Options{ExemptKeyedStrings: true})
	if parseSourceError != nil {
		test.Fatalf("parse: %v", parseSourceError)
	}
	for index, line := range lines {
		if width(line.text) <= consistency_no_long_line.Limit {
			if line.want != "" {
				test.Errorf("line %d is not long, the case is wrong: %s", index+1, line.text)
			}
			continue
		}
		if got := parsed.classify(index + 1); got != line.want {
			test.Errorf("line %d: %s, want %s: %s", index+1, got, line.want, line.text)
		}
		want := line.want
		if line.wantKeyedExempt != "" {
			want = line.wantKeyedExempt
		}
		if got := keyedExempt.classify(index + 1); got != want {
			test.Errorf("line %d with keyed strings exempt: %s, want %s: %s", index+1, got, want, line.text)
		}
	}
}

// TestReportedOnlyWrapsOnlyTheRulesLines pins Options.ReportedOnly: a long line consistency_no_long_line leaves alone
// is CategoryNotReported and never wrapped, though without the option it is actionable, while a line the rule reports
// keeps its own category and wraps. The rule leaves alone a comment that overruns only on one token too long for a
// comment line of its own, which deep indentation makes of a token of thirty letters, while this package calls such a
// token unbroken only from forty letters or with a scheme, and would move it to a line of its own, as long as before.
func TestReportedOnlyWrapsOnlyTheRulesLines(test *testing.T) {
	test.Parallel()
	const depth = 22
	var builder strings.Builder
	builder.WriteString("package p\n\nfunc report() {\n")
	for level := 1; level < depth; level++ {
		builder.WriteString(strings.Repeat("\t", level) + "{\n")
	}
	token := strings.Repeat("abcdefghij", 3)
	builder.WriteString(strings.Repeat("\t", depth) + "// see " + token + "\n")
	for level := depth - 1; level >= 1; level-- {
		builder.WriteString(strings.Repeat("\t", level) + "}\n")
	}
	builder.WriteString("\tsend(context, \"" + strings.Repeat("word ", 24) + "\", first)\n}\n")
	source := builder.String()
	commentLine := depth + 3
	callLine := 2*depth + 3
	plain, parseSourceError := parseSource([]byte(source), Options{})
	if parseSourceError != nil {
		test.Fatalf("parse: %v\n%s", parseSourceError, source)
	}
	reportedOnly, parseSourceError := parseSource([]byte(source), Options{ReportedOnly: true})
	if parseSourceError != nil {
		test.Fatalf("parse: %v", parseSourceError)
	}
	for _, line := range []struct {
		number         int
		want           Category
		wantInReported Category
	}{
		{number: commentLine, want: CategoryActionable, wantInReported: CategoryNotReported},
		{number: callLine, want: CategoryActionable, wantInReported: CategoryActionable},
	} {
		if width(plain.lineText(line.number)) <= consistency_no_long_line.Limit {
			test.Fatalf("line %d is not long, the case is wrong: %s", line.number, plain.lineText(line.number))
		}
		if got := plain.classify(line.number); got != line.want {
			test.Errorf("line %d: %s, want %s", line.number, got, line.want)
		}
		if got := reportedOnly.classify(line.number); got != line.wantInReported {
			test.Errorf("line %d with ReportedOnly: %s, want %s", line.number, got, line.wantInReported)
		}
	}
	for _, testCase := range []struct {
		options     Options
		commentKept bool
	}{
		{options: Options{}, commentKept: false},
		{options: Options{ReportedOnly: true}, commentKept: true},
	} {
		result, wrapError := Wrap([]byte(source), testCase.options)
		if wrapError != nil || result.Rejected != nil {
			test.Fatalf("Wrap with %+v: %v, %v", testCase.options, wrapError, result.Rejected)
		}
		output := string(result.Output)
		if strings.Contains(output, "// see "+token+"\n") != testCase.commentKept ||
			!strings.Contains(output, "\tsend(\n") {
			test.Errorf("with %+v, want the comment kept %v and the call wrapped, got:\n%s", testCase.options,
				testCase.commentKept, output)
		}
	}
}

// TestCuredOnlyLeavesWhatItCannotCure pins Options.CuredOnly: a return of a long string and a name, which a break after
// the + would leave long, stays byte for byte as it was, while a call beside it is still wrapped. Without the option
// the return is broken and left long.
func TestCuredOnlyLeavesWhatItCannotCure(test *testing.T) {
	test.Parallel()
	uncured := "\treturn \"" + strings.Repeat("word ", 24) + "\" + name\n"
	source := "package p\n\nfunc signature(name string) string {\n" + uncured + "}\n\n" +
		"func report() {\n\tsend(context, \"" + strings.Repeat("word ", 20) + "\", first, second, third)\n}\n"
	for _, testCase := range []struct {
		options Options
		kept    bool
	}{
		{options: Options{}, kept: false},
		{options: Options{CuredOnly: true}, kept: true},
	} {
		result, wrapError := Wrap([]byte(source), testCase.options)
		if wrapError != nil || result.Rejected != nil {
			test.Fatalf("Wrap with %+v: %v, %v", testCase.options, wrapError, result.Rejected)
		}
		output := string(result.Output)
		if strings.Contains(output, uncured) != testCase.kept || !strings.Contains(output, "\tsend(\n") {
			test.Errorf("with %+v, want the return kept whole %v and the call wrapped, got:\n%s", testCase.options,
				testCase.kept, output)
		}
		if testCase.kept && (result.After[CategoryNotCured] != 1 || result.After[CategoryActionable] != 0) {
			test.Errorf("want the return counted as not cured and nothing left actionable, got %v", result.After)
		}
	}
}

// TestKeepCommentsLeavesEveryComment pins Options.KeepComments: a long prose comment stays byte for byte and counts as
// CategoryComment, a call whose break would leave a block comment ending its line, where code followed it before,
// stays whole, and a call with a trailing marker comment is still wrapped, its comment's text kept after the closing
// parenthesis. Without the option the comment is reflowed and the first call broken.
func TestKeepCommentsLeavesEveryComment(test *testing.T) {
	test.Parallel()
	prose := "// " + strings.Repeat("word ", 26) + "end\n"
	moved := "\tsend( /* the context */ context, \"" + strings.Repeat("word ", 20) + "\", first)\n"
	source := "package p\n\n" + prose + "func report() {\n" + moved + "\tsend(context, \"" +
		strings.Repeat("word ", 20) + "\", first, second) // Serial: a marker line reads this\n}\n"
	for _, testCase := range []struct {
		options Options
		kept    bool
	}{
		{options: Options{}, kept: false},
		{options: Options{KeepComments: true}, kept: true},
	} {
		result, wrapError := Wrap([]byte(source), testCase.options)
		if wrapError != nil || result.Rejected != nil {
			test.Fatalf("Wrap with %+v: %v, %v", testCase.options, wrapError, result.Rejected)
		}
		output := string(result.Output)
		if strings.Contains(output, prose) != testCase.kept || strings.Contains(output, moved) != testCase.kept {
			test.Errorf("with %+v, want the comment and the first call kept whole %v, got:\n%s", testCase.options,
				testCase.kept, output)
		}
		if !strings.Contains(output, "\t) // Serial: a marker line reads this\n") {
			test.Errorf("with %+v, want the second call wrapped and its marker kept after it, got:\n%s",
				testCase.options, output)
		}
		if testCase.kept && (result.After[CategoryComment] != 1 || result.After[CategoryActionable] != 0) {
			test.Errorf("want the comment counted as a comment and nothing left actionable, got %v", result.After)
		}
	}
}
