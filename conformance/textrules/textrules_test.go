package textrules

import (
	"strings"
	"testing"
	"testing/fstest"
)

// The embedded corpus loads, and every engine has cases in it, so no adapter can be green over nothing.
func TestTheEmbeddedCorpusLoads(t *testing.T) {
	t.Parallel()

	corpus, err := Load()
	if err != nil {
		t.Fatalf("the embedded corpus does not load: %v", err)
	}
	if len(corpus.Families) == 0 {
		t.Fatal("the corpus has no families")
	}
	for _, engine := range Engines {
		if len(corpus.CasesFor(engine)) == 0 {
			t.Errorf("the corpus has no %s cases", engine)
		}
	}
	for _, family := range corpus.Families {
		controls := 0
		for _, testCase := range family.Cases {
			if testCase.Control {
				controls++
			}
		}
		if controls != 1 {
			t.Errorf("family %s has %d controls, and a family has exactly one", family.Name, controls)
		}
	}
}

// validCorpus is the smallest corpus that loads: one family, a control and a clean case, and an empty
// ledger. Each refusal below breaks one thing in a copy of it.
func validCorpus() fstest.MapFS {
	return fstest.MapFS{
		"families.json": {Data: []byte(`{"words": {"typescript": "no-words", "go": "go/no-words", ` +
			`"swift": {"none": "a probe of the shape"}}}`)},
		"ledger.json": {Data: []byte(`[]`)},
		"words/w01/case.json": {Data: []byte(`{"why": "the control", "control": true, ` +
			`"expect": [{"covers": "WORD", "occurrence": 1}], "options": {}, "path": null, ` +
			`"languages": ["typescript", "go"]}`)},
		"words/w01/case.ts":   {Data: []byte("// WORD here\n")},
		"words/w01/case.go":   {Data: []byte("package w01\n\n// WORD here\n")},
		"words/w02/case.json": {Data: []byte(`{"why": "clean", "expect": [], "languages": ["typescript"]}`)},
		"words/w02/case.ts":   {Data: []byte("// word here\n")},
	}
}

// with is a copy of a corpus with one file replaced, or removed when the contents are empty.
func with(files fstest.MapFS, name string, contents string) fstest.MapFS {
	copied := fstest.MapFS{}
	for existing, file := range files {
		copied[existing] = file
	}
	if contents == "" {
		delete(copied, name)
		return copied
	}
	copied[name] = &fstest.MapFile{Data: []byte(contents)}
	return copied
}

func TestTheValidCorpusLoads(t *testing.T) {
	t.Parallel()

	corpus, err := LoadFrom(validCorpus())
	if err != nil {
		t.Fatalf("the valid corpus does not load: %v", err)
	}
	if got := len(corpus.CasesFor(EngineTypeScript)); got != 2 {
		t.Errorf("%d TypeScript cases, want 2", got)
	}
	if got := len(corpus.CasesFor(EngineSwift)); got != 0 {
		t.Errorf("%d Swift cases, want 0", got)
	}
}

// Each malformed input is refused, and the refusal names what is wrong.
func TestTheLoaderRefusesAMalformedCorpus(t *testing.T) {
	t.Parallel()

	valid := validCorpus()
	cases := []struct {
		name     string
		files    fstest.MapFS
		mustName string
	}{
		{
			"an unknown key in a case",
			with(valid, "words/w02/case.json", `{"why": "clean", "expect": [], "languages": ["typescript"], "expected": []}`),
			`unknown field "expected"`,
		},
		{
			"an unknown key in a span",
			with(valid, "words/w01/case.json", `{"why": "c", "control": true, "expect": [{"covers": "WORD", `+
				`"occurrence": 1, "column": 4}], "languages": ["typescript", "go"]}`),
			`unknown field "column"`,
		},
		{
			"an unknown key in a ledger row",
			with(valid, "ledger.json", `[{"engine": "typescript", "family": "words", "case": "w02", `+
				`"kind": "verdict", "task": "t", "reason": "r", "note": "n"}]`),
			`unknown field "note"`,
		},
		{
			"an unknown key in an engine's entry",
			with(valid, "families.json", `{"words": {"typescript": "no-words", "go": "go/no-words", `+
				`"swift": {"none": "a probe", "why": "x"}}}`),
			`unknown field "why"`,
		},
		{
			"a family where an engine is missing",
			with(valid, "families.json", `{"words": {"typescript": "no-words", "go": "go/no-words"}}`),
			"has no entry for swift",
		},
		{
			"a verdict row on a control",
			with(valid, "ledger.json", `[{"engine": "typescript", "family": "words", "case": "w01", `+
				`"kind": "verdict", "task": "t", "reason": "r"}]`),
			"verdict row on a control",
		},
		{
			"a ledger row naming a case that does not exist",
			with(valid, "ledger.json", `[{"engine": "typescript", "family": "words", "case": "w09", `+
				`"kind": "verdict", "task": "t", "reason": "r"}]`),
			"names a case that does not exist",
		},
		{
			"a ledger row of an unknown kind",
			with(valid, "ledger.json", `[{"engine": "typescript", "family": "words", "case": "w02", `+
				`"kind": "Verdict", "task": "t", "reason": "r"}]`),
			`kind is "Verdict"`,
		},
		{
			"a ledger row for a language the case has no form in",
			with(valid, "ledger.json", `[{"engine": "go", "family": "words", "case": "w02", `+
				`"kind": "verdict", "task": "t", "reason": "r"}]`),
			"no go form",
		},
		{
			"two rows for one case in one engine",
			with(valid, "ledger.json", `[{"engine": "typescript", "family": "words", "case": "w02", `+
				`"kind": "verdict", "task": "t", "reason": "r"}, {"engine": "typescript", "family": "words", `+
				`"case": "w02", "kind": "span", "task": "t", "reason": "r"}]`),
			"second row",
		},
		{
			"a source the case does not list",
			with(valid, "words/w02/case.go", "package w02\n"),
			"case.go is not a source the case lists",
		},
		{
			"a language with no source",
			with(valid, "words/w01/case.go", ""),
			"case.go can't be read",
		},
		{
			"a language where the family has no rule",
			with(valid, "words/w02/case.json", `{"why": "clean", "expect": [], "languages": ["typescript", "swift"]}`),
			"where the family has no rule",
		},
		{
			"an expectation that is not in a source",
			with(valid, "words/w01/case.ts", "// word here\n"),
			`"WORD" occurs 0 times`,
		},
		{
			"a span of two forms",
			with(valid, "words/w01/case.json", `{"why": "c", "control": true, "expect": [{"covers": "WORD", `+
				`"occurrence": 1, "fileStart": true}], "languages": ["typescript", "go"]}`),
			"writes 2",
		},
		{
			"a case with no expect",
			with(valid, "words/w02/case.json", `{"why": "clean", "languages": ["typescript"]}`),
			"has no expect",
		},
		{
			"a family directory families.json does not name",
			with(valid, "other/o01/case.json", `{"why": "x", "expect": [], "languages": ["typescript"]}`),
			"does not name",
		},
		{
			"a path that leaves the project",
			with(valid, "words/w02/case.json", `{"why": "clean", "expect": [], "path": "../utils", "languages": ["typescript"]}`),
			"not a clean project-relative directory",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadFrom(testCase.files)
			if err == nil {
				t.Fatal("loaded, and should have been refused")
			}
			if !strings.Contains(err.Error(), testCase.mustName) {
				t.Errorf("the refusal does not say %q: %v", testCase.mustName, err)
			}
		})
	}
}

// A control may carry a span row, since its count and start lines are still checked.
func TestASpanRowOnAControlLoads(t *testing.T) {
	t.Parallel()

	files := with(validCorpus(), "ledger.json", `[{"engine": "typescript", "family": "words", "case": "w01", `+
		`"kind": "span", "task": "t", "reason": "r"}]`)
	if _, err := LoadFrom(files); err != nil {
		t.Fatalf("a span row on a control was refused: %v", err)
	}
}

// covers is a span's covers field, for a test that writes one.
func covers(text string, occurrence int) Span {
	return Span{Covers: &text, Occurrence: occurrence}
}

// Each span form resolves to the bytes it names, and lines end as each language ends them.
func TestResolveReadsEachSpanForm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		span   Span
		source string
		engine Engine
		want   Range
	}{
		{"the first occurrence", covers("TODO", 1), "// TODO TODO", EngineGo, Range{Start: 3, End: 7}},
		{"the second occurrence", covers("TODO", 2), "// TODO TODO", EngineGo, Range{Start: 8, End: 12}},
		{"overlapping occurrences", covers("aa", 2), "aaa", EngineGo, Range{Start: 1, End: 3}},
		{"bytes, not characters", covers("TODO", 1), "// café TODO", EngineSwift, Range{Start: 9, End: 13}},
		{"the file's start", Span{FileStart: true}, "one\ntwo\n", EngineGo, Range{Start: 0, End: 0}},
		{"lines from the third", Span{LinesFrom: 3}, "a\nb\nc\nd\n", EngineGo, Range{Start: 4, End: 8}},
		{"a lone carriage return in Swift", Span{LinesFrom: 2}, "a\rb\n", EngineSwift, Range{Start: 2, End: 4}},
		{"a line separator in TypeScript", Span{LinesFrom: 2}, "a b", EngineTypeScript, Range{Start: 4, End: 5}},
		{"a carriage return and line feed as one", Span{LinesFrom: 3}, "a\r\nb\r\nc", EngineTypeScript, Range{Start: 6, End: 7}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := Resolve(testCase.span, testCase.source, testCase.engine)
			if err != nil {
				t.Fatalf("does not resolve: %v", err)
			}
			if got != testCase.want {
				t.Errorf("resolved to %+v, want %+v", got, testCase.want)
			}
		})
	}

	// A lone carriage return ends no line in Go, and a line separator ends none in Swift.
	for _, unresolved := range []struct {
		source string
		engine Engine
	}{{"a\rb", EngineGo}, {"a b", EngineSwift}} {
		if _, err := Resolve(Span{LinesFrom: 2}, unresolved.source, unresolved.engine); err == nil {
			t.Errorf("%q has a second line in %s, and should not", unresolved.source, unresolved.engine)
		}
	}
}

// probeCase is a case over one TypeScript source, expecting the given spans.
func probeCase(control bool, source string, expect ...Span) Case {
	return Case{
		Family:    "words",
		Name:      "probe",
		Control:   control,
		Expect:    expect,
		Languages: []Engine{EngineTypeScript},
		Sources:   map[Engine]string{EngineTypeScript: source},
	}
}

// The comparator passes the right findings and names the kind of each wrong one.
func TestCompareNamesTheKindOfFailure(t *testing.T) {
	t.Parallel()

	source := "// one TODO\n// two TODO\n"
	twoWords := probeCase(false, source, covers("TODO", 1), covers("TODO", 2))
	cases := []struct {
		name        string
		testCase    Case
		found       []Range
		want        Kind
		controlHeld bool
	}{
		{"the right findings", twoWords, []Range{{Start: 7, End: 11}, {Start: 19, End: 23}}, "", true},
		{"the right findings, out of order", twoWords, []Range{{Start: 19, End: 23}, {Start: 7, End: 11}}, "", true},
		{"one finding short", twoWords, []Range{{Start: 7, End: 11}}, KindVerdict, false},
		{"a finding on a clean case", probeCase(false, source), []Range{{Start: 7, End: 11}}, KindVerdict, false},
		{"silence where it should report", twoWords, nil, KindVerdict, false},
		{"the whole comment", twoWords, []Range{{Start: 0, End: 11}, {Start: 12, End: 23}}, KindSpan, true},
		{"the wrong end", twoWords, []Range{{Start: 7, End: 10}, {Start: 19, End: 23}}, KindSpan, true},
		{"no end", twoWords, []Range{{Start: 7, NoEnd: true}, {Start: 19, End: 23}}, KindSpan, true},
		{"the wrong line", twoWords, []Range{{Start: 7, End: 11}, {Start: 7, End: 11}}, KindSpan, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := Compare(testCase.testCase, EngineTypeScript, testCase.found)
			if result.Failure != testCase.want {
				t.Errorf("failure %q, want %q: %s", result.Failure, testCase.want, result.Detail)
			}
			if result.ControlHeld != testCase.controlHeld {
				t.Errorf("control held %v, want %v: %s", result.ControlHeld, testCase.controlHeld, result.Detail)
			}
		})
	}
}

// Judge holds a result to the ledger: a failure needs a row of its kind, a pass must have none, and a
// control's count and start lines are checked whatever the ledger says.
func TestJudgeHoldsAResultToTheLedger(t *testing.T) {
	t.Parallel()

	source := "// TODO\n"
	plain := probeCase(false, source, covers("TODO", 1))
	control := probeCase(true, source, covers("TODO", 1))
	verdictRow := LedgerRow{Engine: EngineTypeScript, Family: "words", Case: "probe", Kind: KindVerdict, Task: "t", Reason: "r"}
	spanRow := verdictRow
	spanRow.Kind = KindSpan

	right := []Range{{Start: 3, End: 7}}
	wholeComment := []Range{{Start: 0, End: 7}}
	cases := []struct {
		name     string
		testCase Case
		ledger   []LedgerRow
		found    []Range
		mustName string
	}{
		{"a pass with no row", plain, nil, right, ""},
		{"a pass with a row", plain, []LedgerRow{spanRow}, right, "is stale"},
		{"a failure with no row", plain, nil, nil, "no ledger row"},
		{"a failure with its row", plain, []LedgerRow{verdictRow}, nil, ""},
		{"a failure with a row of the other kind", plain, []LedgerRow{spanRow}, nil, "says span"},
		{"a control's span failure with its row", control, []LedgerRow{spanRow}, wholeComment, ""},
		{"a silent control", control, []LedgerRow{verdictRow}, nil, "no ledger row can excuse"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			corpus := &Corpus{Ledger: testCase.ledger}
			err := corpus.Judge(EngineTypeScript, testCase.testCase,
				Compare(testCase.testCase, EngineTypeScript, testCase.found))
			switch {
			case testCase.mustName == "" && err != nil:
				t.Errorf("refused: %v", err)
			case testCase.mustName != "" && err == nil:
				t.Errorf("accepted, and should have said %q", testCase.mustName)
			case testCase.mustName != "" && !strings.Contains(err.Error(), testCase.mustName):
				t.Errorf("the refusal does not say %q: %v", testCase.mustName, err)
			}
		})
	}
}
