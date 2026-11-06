package textrules

import (
	"strings"
	"testing"
	"testing/fstest"
)

// The embedded corpus loads, and every engine has cases in it, so no adapter can be green over nothing.
func TestTheEmbeddedCorpusLoads(test *testing.T) {
	test.Parallel()

	corpus, loadError := Load()
	if loadError != nil {
		test.Fatalf("the embedded corpus does not load: %v", loadError)
	}
	if len(corpus.Families) == 0 {
		test.Fatal("the corpus has no families")
	}
	for _, engine := range Engines {
		if len(corpus.CasesFor(engine)) == 0 {
			test.Errorf("the corpus has no %s cases", engine)
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
			test.Errorf("family %s has %d controls, and a family has exactly one", family.Name, controls)
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
		"testdata/words/w01/case.json": {Data: []byte(`{"why": "the control", "control": true, ` +
			`"expect": [{"covers": "WORD", "occurrence": 1}], "options": {}, "path": null, ` +
			`"languages": ["typescript", "go"]}`)},
		"testdata/words/w01/case.ts":   {Data: []byte("// WORD here\n")},
		"testdata/words/w01/case.go":   {Data: []byte("package w01\n\n// WORD here\n")},
		"testdata/words/w02/case.json": {Data: []byte(`{"why": "clean", "expect": [], "languages": ["typescript"]}`)},
		"testdata/words/w02/case.ts":   {Data: []byte("// word here\n")},
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

func TestTheValidCorpusLoads(test *testing.T) {
	test.Parallel()

	corpus, loadFromError := LoadFrom(validCorpus())
	if loadFromError != nil {
		test.Fatalf("the valid corpus does not load: %v", loadFromError)
	}
	if got := len(corpus.CasesFor(EngineTypeScript)); got != 2 {
		test.Errorf("%d TypeScript cases, want 2", got)
	}
	if got := len(corpus.CasesFor(EngineSwift)); got != 0 {
		test.Errorf("%d Swift cases, want 0", got)
	}
}

// Each malformed input is refused, and the refusal names what is wrong.
func TestTheLoaderRefusesAMalformedCorpus(test *testing.T) {
	test.Parallel()

	valid := validCorpus()
	cases := []struct {
		name     string
		files    fstest.MapFS
		mustName string
	}{
		{
			"an unknown key in a case",
			with(
				valid,
				"testdata/words/w02/case.json",
				`{"why": "clean", "expect": [], "languages": ["typescript"], "expected": []}`,
			),
			`unknown field "expected"`,
		},
		{
			"an unknown key in a span",
			with(valid, "testdata/words/w01/case.json", `{"why": "c", "control": true, "expect": [{"covers": "WORD", `+
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
			with(valid, "testdata/words/w02/case.go", "package w02\n"),
			"case.go is not a source the case lists",
		},
		{
			"a language with no source",
			with(valid, "testdata/words/w01/case.go", ""),
			"case.go can't be read",
		},
		{
			"a language where the family has no rule",
			with(
				valid,
				"testdata/words/w02/case.json",
				`{"why": "clean", "expect": [], "languages": ["typescript", "swift"]}`,
			),
			"where the family has no rule",
		},
		{
			"an expectation that is not in a source",
			with(valid, "testdata/words/w01/case.ts", "// word here\n"),
			`"WORD" occurs 0 times`,
		},
		{
			"a span of two forms",
			with(valid, "testdata/words/w01/case.json", `{"why": "c", "control": true, "expect": [{"covers": "WORD", `+
				`"occurrence": 1, "fileStart": true}], "languages": ["typescript", "go"]}`),
			"writes 2",
		},
		{
			"a case with no expect",
			with(valid, "testdata/words/w02/case.json", `{"why": "clean", "languages": ["typescript"]}`),
			"has no expect",
		},
		{
			"a family directory families.json does not name",
			with(valid, "testdata/other/o01/case.json", `{"why": "x", "expect": [], "languages": ["typescript"]}`),
			"does not name",
		},
		{
			"a path that leaves the project",
			with(
				valid,
				"testdata/words/w02/case.json",
				`{"why": "clean", "expect": [], "path": "../utils", "languages": ["typescript"]}`,
			),
			"not a clean project-relative file name",
		},
		{
			"an occurrence per language that leaves a language out",
			with(valid, "testdata/words/w01/case.json", `{"why": "c", "control": true, "expect": [{"covers": "WORD", `+
				`"occurrence": {"typescript": 1}}], "languages": ["typescript", "go"]}`),
			"gives occurrences for 1 languages, and the case lists 2",
		},
		{
			"an occurrence per language naming no language",
			with(valid, "testdata/words/w01/case.json", `{"why": "c", "control": true, "expect": [{"covers": "WORD", `+
				`"occurrence": {"typescript": 1, "rust": 1}}], "languages": ["typescript", "go"]}`),
			`gives rust occurrence 1`,
		},
		{
			"a root that is not absolute",
			with(
				valid,
				"testdata/words/w02/case.json",
				`{"why": "clean", "expect": [], "root": "checkouts/utils", "languages": ["typescript"]}`,
			),
			"not a clean absolute directory",
		},
		{
			"an unknown key beside missing",
			with(valid, "families.json", `{"words": {"typescript": "no-words", "go": "go/no-words", `+
				`"swift": {"missing": true, "task": "t"}}}`),
			`unknown field "task"`,
		},
		{
			"an engine both none and missing",
			with(valid, "families.json", `{"words": {"typescript": "no-words", "go": "go/no-words", `+
				`"swift": {"missing": true, "none": "r"}}}`),
			"never both",
		},
		{
			"a missing engine with no family-level row",
			with(valid, "families.json", `{"words": {"typescript": "no-words", "go": "go/no-words", `+
				`"swift": {"missing": true}}}`),
			"has no family-level row",
		},
		{
			"a family-level row where the engine names a rule",
			with(valid, "ledger.json", `[{"engine": "typescript", "family": "words", "task": "t", "reason": "r"}]`),
			"the row is stale",
		},
		{
			"a family-level row where the language has no such idea",
			with(valid, "ledger.json", `[{"engine": "swift", "family": "words", "task": "t", "reason": "r"}]`),
			"no such idea",
		},
		{
			"a family-level row with a kind",
			withMissingSwift(`[{"engine": "swift", "family": "words", "kind": "verdict", "task": "t", "reason": "r"}]`),
			"family-level row with a kind",
		},
		{
			"a case-level row for a missing engine",
			withMissingSwift(`[{"engine": "swift", "family": "words", "task": "t", "reason": "r"}, ` +
				`{"engine": "swift", "family": "words", "case": "w01", "kind": "span", "task": "t", "reason": "r"}]`),
			"case-level row where swift has no rule",
		},
		{
			"two family-level rows for one engine",
			withMissingSwift(`[{"engine": "swift", "family": "words", "task": "t", "reason": "r"}, ` +
				`{"engine": "swift", "family": "words", "task": "u", "reason": "r"}]`),
			"second family-level row",
		},
		{
			"a family-level row naming a family that does not exist",
			with(valid, "ledger.json", `[{"engine": "swift", "family": "letters", "task": "t", "reason": "r"}]`),
			"names a family that does not exist",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			_, loadFromError := LoadFrom(testCase.files)
			if loadFromError == nil {
				test.Fatal("loaded, and should have been refused")
			}
			if !strings.Contains(loadFromError.Error(), testCase.mustName) {
				test.Errorf("the refusal does not say %q: %v", testCase.mustName, loadFromError)
			}
		})
	}
}

// withMissingSwift is the valid corpus with Swift missing from its family, a Swift form on the control, and
// this ledger.
func withMissingSwift(ledger string) fstest.MapFS {
	files := with(validCorpus(), "families.json", `{"words": {"typescript": "no-words", "go": "go/no-words", `+
		`"swift": {"missing": true}}}`)
	files = with(files, "testdata/words/w01/case.json", `{"why": "the control", "control": true, `+
		`"expect": [{"covers": "WORD", "occurrence": 1}], "languages": ["typescript", "go", "swift"]}`)
	files = with(files, "testdata/words/w01/case.swift", "// WORD here\n")
	return with(files, "ledger.json", ledger)
}

// A missing engine with its family-level row loads, and its cases there are not compared, though they are
// written.
func TestAMissingEngineWithItsRowLoadsAndRunsNothing(test *testing.T) {
	test.Parallel()

	corpus, loadFromError := LoadFrom(
		withMissingSwift(`[{"engine": "swift", "family": "words", "task": "t", "reason": "r"}]`),
	)
	if loadFromError != nil {
		test.Fatalf("a missing engine with its row was refused: %v", loadFromError)
	}
	if got := len(corpus.CasesFor(EngineSwift)); got != 0 {
		test.Errorf("%d Swift cases compared where Swift has no rule, want 0", got)
	}
	if got := len(corpus.CasesFor(EngineTypeScript)); got != 2 {
		test.Errorf("%d TypeScript cases, want 2", got)
	}
	if !corpus.Families[0].Cases[0].Has(EngineSwift) {
		test.Error("the control's Swift form was not read")
	}
}

// An occurrence given per language names each language's own occurrence of the same text.
func TestAnOccurrencePerLanguageResolvesInEach(test *testing.T) {
	test.Parallel()

	files := with(validCorpus(), "testdata/words/w01/case.json", `{"why": "c", "control": true, "expect": `+
		`[{"covers": "WORD", "occurrence": {"typescript": 1, "go": 2}}], "languages": ["typescript", "go"]}`)
	files = with(files, "testdata/words/w01/case.go", "package w01\n\n// WORD and WORD here\n")
	corpus, loadFromError := LoadFrom(files)
	if loadFromError != nil {
		test.Fatal(loadFromError)
	}
	control := corpus.Families[0].Cases[0]
	for engine, want := range map[Engine]Range{EngineTypeScript: {Start: 3, End: 7}, EngineGo: {Start: 25, End: 29}} {
		got, resolveError := Resolve(control.Expect[0], control.Sources[engine], engine)
		if resolveError != nil || got != want {
			test.Errorf("%s resolved to %+v (%v), want %+v", engine, got, resolveError, want)
		}
	}
}

// A case's path and root name its file in each language, and default to the project's `case`.
func TestACaseNamesItsFileByPathAndRoot(test *testing.T) {
	test.Parallel()

	files := with(validCorpus(), "testdata/words/w02/case.json", `{"why": "clean", "expect": [], `+
		`"path": "utils/lf02.generated", "root": "/checkouts/utils/probe", "languages": ["typescript"]}`)
	corpus, loadFromError := LoadFrom(files)
	if loadFromError != nil {
		test.Fatal(loadFromError)
	}
	named, plain := corpus.Families[0].Cases[1], corpus.Families[0].Cases[0]
	if got := named.AbsoluteFileName(EngineTypeScript); got != "/checkouts/utils/probe/utils/lf02.generated.ts" {
		test.Errorf("named %s", got)
	}
	if got := plain.AbsoluteFileName(EngineGo); got != "/project/case.go" {
		test.Errorf("named %s", got)
	}
}

// A control may carry a span row, since its count and start lines are still checked.
func TestASpanRowOnAControlLoads(test *testing.T) {
	test.Parallel()

	files := with(validCorpus(), "ledger.json", `[{"engine": "typescript", "family": "words", "case": "w01", `+
		`"kind": "span", "task": "t", "reason": "r"}]`)
	if _, loadFromError := LoadFrom(files); loadFromError != nil {
		test.Fatalf("a span row on a control was refused: %v", loadFromError)
	}
}

// covers is a span's covers field, for a test that writes one.
func covers(text string, occurrence int) Span {
	return Span{Covers: &text, Occurrence: occurrence}
}

// Each span form resolves to the bytes it names, and lines end as each language ends them.
func TestResolveReadsEachSpanForm(test *testing.T) {
	test.Parallel()

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
		{"a line separator in TypeScript", Span{LinesFrom: 2}, "a\u2028b", EngineTypeScript, Range{Start: 4, End: 5}},
		{
			"a carriage return and line feed as one",
			Span{LinesFrom: 3},
			"a\r\nb\r\nc",
			EngineTypeScript,
			Range{Start: 6, End: 7},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			got, resolveError := Resolve(testCase.span, testCase.source, testCase.engine)
			if resolveError != nil {
				test.Fatalf("does not resolve: %v", resolveError)
			}
			if got != testCase.want {
				test.Errorf("resolved to %+v, want %+v", got, testCase.want)
			}
		})
	}

	// A lone carriage return ends no line in Go, and a line separator ends none in Swift.
	for _, unresolved := range []struct {
		source string
		engine Engine
	}{{"a\rb", EngineGo}, {"a\u2028b", EngineSwift}} {
		if _, resolveError := Resolve(Span{LinesFrom: 2}, unresolved.source, unresolved.engine); resolveError == nil {
			test.Errorf("%q has a second line in %s, and should not", unresolved.source, unresolved.engine)
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
func TestCompareNamesTheKindOfFailure(test *testing.T) {
	test.Parallel()

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
		// A span over the whole file holds a control to its count alone: where the finding sits is placement.
		{
			"a long file reported on its first line",
			probeCase(true, source, Span{LinesFrom: 2}),
			[]Range{{Start: 0, End: 0}},
			KindSpan,
			true,
		},
		{
			"a long file reported twice",
			probeCase(true, source, Span{LinesFrom: 2}),
			[]Range{{Start: 0}, {Start: 12, End: 24}},
			KindVerdict,
			false,
		},
		{
			"a folder reported off the file's start",
			probeCase(true, source, Span{FileStart: true}),
			[]Range{{Start: 12, End: 12}},
			KindSpan,
			true,
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := Compare(testCase.testCase, EngineTypeScript, testCase.found)
			if result.Failure != testCase.want {
				test.Errorf("failure %q, want %q: %s", result.Failure, testCase.want, result.Detail)
			}
			if result.ControlHeld != testCase.controlHeld {
				test.Errorf("control held %v, want %v: %s", result.ControlHeld, testCase.controlHeld, result.Detail)
			}
		})
	}
}

// Judge holds a result to the ledger: a failure needs a row of its kind, a pass must have none, and a
// control's count and start lines are checked whatever the ledger says.
func TestJudgeHoldsAResultToTheLedger(test *testing.T) {
	test.Parallel()

	source := "// TODO\n"
	plain := probeCase(false, source, covers("TODO", 1))
	control := probeCase(true, source, covers("TODO", 1))
	verdictRow := LedgerRow{
		Engine: EngineTypeScript,
		Family: "words",
		Case:   "probe",
		Kind:   KindVerdict,
		Task:   "t",
		Reason: "r",
	}
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
		{
			"a control covering text on the wrong line",
			probeCase(true, "// one\n// TODO\n", covers("TODO", 1)),
			[]LedgerRow{spanRow},
			[]Range{{Start: 0, End: 6}},
			"no ledger row can excuse",
		},
		{
			"a long-file control at the wrong line, with its span row",
			probeCase(true, source, Span{LinesFrom: 1}),
			[]LedgerRow{spanRow},
			[]Range{{Start: 3, End: 3}},
			"",
		},
		{
			"a long-file control at the wrong line, with no row",
			probeCase(true, source, Span{LinesFrom: 1}),
			nil,
			[]Range{{Start: 3, End: 3}},
			"no ledger row",
		},
		{
			"a long-file control with the wrong count",
			probeCase(true, source, Span{LinesFrom: 1}),
			[]LedgerRow{spanRow},
			nil,
			"no ledger row can excuse",
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			corpus := &Corpus{Ledger: testCase.ledger}
			judgeError := corpus.Judge(EngineTypeScript, testCase.testCase,
				Compare(testCase.testCase, EngineTypeScript, testCase.found))
			switch {
			case testCase.mustName == "" && judgeError != nil:
				test.Errorf("refused: %v", judgeError)
			case testCase.mustName != "" && judgeError == nil:
				test.Errorf("accepted, and should have said %q", testCase.mustName)
			case testCase.mustName != "" && !strings.Contains(judgeError.Error(), testCase.mustName):
				test.Errorf("the refusal does not say %q: %v", testCase.mustName, judgeError)
			}
		})
	}
}
