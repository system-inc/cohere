package line_wrap

import (
	"go/scanner"
	"go/token"
	"slices"
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
		name: "a marker line written straight after a paragraph is never joined into it, nor reflowed",
		source: `package p

func TestRuns(test *testing.T) {
	// The runner holds the machine's only lock for the length of the whole suite, so no other test may take it while this one
	// runs.
	// Serial: the runner takes the machine-wide lock, which no two tests in this process may hold at once, so it runs alone
	// Wall: about four seconds
	run(test)
}

func ExampleSort() {
	Sort(values)
	// The order the sort leaves its input in is not fixed for equal elements, so the output is compared as a set of its lines.
	// Unordered output:
	// first
}

// Parse reads a configuration file and returns what it holds, the way the loader expects it to be laid out for a single run.
// Deprecated: use Load, which reads every file the configuration names and reports the first one it cannot read
func Parse() {}
`,
		want: `package p

func TestRuns(test *testing.T) {
	// The runner holds the machine's only lock for the length of the whole suite, so no other test may take it while
	// this one runs.
	// Serial: the runner takes the machine-wide lock, which no two tests in this process may hold at once, so it runs alone
	// Wall: about four seconds
	run(test)
}

func ExampleSort() {
	Sort(values)
	// The order the sort leaves its input in is not fixed for equal elements, so the output is compared as a set of its
	// lines.
	// Unordered output:
	// first
}

// Parse reads a configuration file and returns what it holds, the way the loader expects it to be laid out for a single
// run.
// Deprecated: use Load, which reads every file the configuration names and reports the first one it cannot read
func Parse() {}
`,
	},
	{
		name: "a paragraph written straight above a rule's own marker stops before it, though the marker is two words or more",
		source: `package p

func TestHoldsTheLock(test *testing.T) {
	// The runner holds the machine's only lock for the length of the whole suite, so no other test may take it while this one runs.
	// Not parallel: the runner takes the machine-wide lock
	run(test)
}

func TestReadsTheFixture(test *testing.T) {
	test.Parallel()
	// The fixture names the home directory of the account the suite was first written under, so the parser is handed a real one.
	// Not a machine path: the parser reads it as text and never opens it
	read(fixture)
}
`,
		want: `package p

func TestHoldsTheLock(test *testing.T) {
	// The runner holds the machine's only lock for the length of the whole suite, so no other test may take it while
	// this one runs.
	// Not parallel: the runner takes the machine-wide lock
	run(test)
}

func TestReadsTheFixture(test *testing.T) {
	test.Parallel()
	// The fixture names the home directory of the account the suite was first written under, so the parser is handed a
	// real one.
	// Not a machine path: the parser reads it as text and never opens it
	read(fixture)
}
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

// fixPhaseOptions are the options the engine's fix phase wraps with.
var fixPhaseOptions = Options{ReportedOnly: true, CuredOnly: true, KeepComments: true}

// TestAChainNeverBreaksBetweenJoinedLiterals pins the fourth guard: a + joining two string literals, which
// consistency_no_useless_concatenation reports on one line and leaves alone across lines, is never broken, so a wrap
// never hides that finding. A chain holding such a join breaks at its other operators with the two literals kept
// together, and a line only that break could cure stays as it was.
func TestAChainNeverBreaksBetweenJoinedLiterals(test *testing.T) {
	test.Parallel()
	joined := `", and welcome to the depot's " + "own courier service in "`
	wrappable := "\treturn \"good morning, \" + name + " + joined + " + city + \", have a nice day\"\n"
	onlyThere := "\treturn prefix + \"" + strings.Repeat("word ", 12) + "\" + \"" + strings.Repeat("more ", 12) + "\"\n"
	source := "package p\n\nfunc greeting(name, city string) string {\n" + wrappable + "}\n\n" +
		"func signature(prefix string) string {\n" + onlyThere + "}\n"
	for _, options := range []Options{{}, fixPhaseOptions} {
		result, wrapError := Wrap([]byte(source), options)
		if wrapError != nil || result.Rejected != nil {
			test.Fatalf("Wrap with %+v: %v, %v", options, wrapError, result.Rejected)
		}
		output := string(result.Output)
		if !strings.Contains(output, "\t\t"+joined+" +\n") ||
			!strings.Contains(output, "\treturn \"good morning, \" +\n") {
			test.Errorf("with %+v, want the chain broken with its joined literals on one line, got:\n%s", options,
				output)
		}
		if options.CuredOnly && !strings.Contains(output, onlyThere) {
			test.Errorf("want the line only a break between literals could cure kept whole, got:\n%s", output)
		}
		if strings.Contains(output, "\" +\n\t\t\"") {
			test.Errorf("with %+v, a break between two literals:\n%s", options, output)
		}
	}
}

// codeTokens is source's tokens as go/scanner reads them, comments left out, with the two shapes a wrap is allowed to
// change taken out of both sides: a semicolon the scanner inserted at a line's end, and a comma just before a closing
// bracket. Each token is its kind and its literal, so a changed name, operator or literal reads as a different token.
func codeTokens(test *testing.T, source []byte) []string {
	test.Helper()
	fileSet := token.NewFileSet()
	var tokenScanner scanner.Scanner
	tokenScanner.Init(fileSet.AddFile("", fileSet.Base(), len(source)), source,
		func(position token.Position, message string) { test.Fatalf("scanning at %s: %s", position, message) }, 0)
	var tokens []string
	for {
		_, kind, literal := tokenScanner.Scan()
		if kind == token.EOF {
			return tokens
		}
		if kind == token.SEMICOLON && literal == "\n" {
			continue
		}
		if (kind == token.RPAREN || kind == token.RBRACK || kind == token.RBRACE) && len(tokens) > 0 &&
			tokens[len(tokens)-1] == token.COMMA.String() {
			tokens = tokens[:len(tokens)-1]
		}
		if literal == "" {
			tokens = append(tokens, kind.String())
			continue
		}
		tokens = append(tokens, kind.String()+" "+literal)
	}
}

// TestTheWrapKeepsEveryTokenButTwoShapes pins the proof of record Loom's lander took over 1,632 wrapped files: the
// wrapped output's go/scanner token stream is the input's, comments aside, once a newline-inserted semicolon and a
// trailing comma before a closing bracket are taken out. It holds for every case here, with the command line's
// options and with the fix phase's, beside Equivalent's proof by syntax tree. The comparison is shown to see a change:
// a renamed argument and a dropped one each read as different streams, while the two shapes alone do not.
func TestTheWrapKeepsEveryTokenButTwoShapes(test *testing.T) {
	test.Parallel()
	const callSource = "package p\n\nvar v = f(first, second)\n"
	for _, check := range []struct {
		name  string
		after string
		same  bool
	}{
		{name: "the two shapes alone", after: "package p\n\nvar v = f(\n\tfirst,\n\tsecond,\n)\n", same: true},
		{name: "a renamed argument", after: "package p\n\nvar v = f(\n\tfirst,\n\tsecnod,\n)\n"},
		{name: "a dropped argument", after: "package p\n\nvar v = f(\n\tfirst,\n)\n"},
	} {
		same := slices.Equal(codeTokens(test, []byte(callSource)), codeTokens(test, []byte(check.after)))
		if same != check.same {
			test.Errorf("%s: the streams read the same %t, want %t", check.name, same, check.same)
		}
	}
	wrappedCount := 0
	for _, testCase := range wrapCases {
		for _, options := range []Options{testCase.options, fixPhaseOptions} {
			result, wrapError := Wrap([]byte(testCase.source), options)
			if wrapError != nil {
				test.Fatalf("%s with %+v: %v", testCase.name, options, wrapError)
			}
			if string(result.Output) != testCase.source {
				wrappedCount++
			}
			if !slices.Equal(codeTokens(test, []byte(testCase.source)), codeTokens(test, result.Output)) {
				test.Errorf("%s with %+v: the wrap changed a token:\n%s", testCase.name, options, result.Output)
			}
		}
	}
	if wrappedCount < len(wrapCases) {
		test.Errorf("only %d of the %d wraps changed their file, so the proof shows little", wrappedCount,
			2*len(wrapCases))
	}
}
