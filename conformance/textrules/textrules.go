// Package textrules is the text rules' conformance corpus (#4b337jk): one set of cases with one expected
// verdict each, which every engine's own test suite reads and must meet. Some rules exist once per language,
// as twins, and the twins disagreed. The verdicts are decided once, in the cases, and never by what an
// engine happens to do.
//
// The corpus is data beside this file:
//
//	families.json        family -> each engine's rule, by its registered name
//	ledger.json          the cases an engine fails today, each naming the task that fixes it
//	<family>/<case>/
//	  case.json          the expectation, options, path, why, and which languages have a form
//	  case.ts            one source per language the case lists, and none it does not
//	  case.go
//	  case.swift
//
// Go reads it through go:embed, so neither the TypeScript-rule adapter nor the Go-engine adapter looks for a
// path. Swift reads the same files by walking up from its test's #filePath and keeps its own copy of the
// ledger's checks, since only Swift can run Swift.
//
// # The ledger, and why green here means something
//
// A failing case with no row fails, a row whose case now passes fails as stale, and a row naming a case that
// does not exist is refused at load. So each engine's rows are its work list, and a row's removal, which its
// fix must make in the same commit, is the proof the fix landed. A row is one of two kinds: `verdict`, where
// the engine reports what should be clean, is silent where it should report, or reports a different number of
// findings; and `span`, where the findings are the right ones but one covers the wrong bytes or has no end.
//
// A case marked as its family's control can carry a span row and never a verdict row. Its count of findings,
// and the line each one starts on, are checked in every engine and can never be ledgered, because a control
// whose silence was ledgered would hide an adapter that measures nothing.
package textrules

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// The corpus files: the two at the top, and every family's case directories whole, so a stray file in a
// case is seen and refused rather than left out of the embed.
//
//go:embed families.json ledger.json */*
var embeddedCorpus embed.FS

// Engine is one of cohere's three engines, which is also the language it judges. Each language has exactly
// one engine, so a case's languages and a ledger row's engine are spelled alike.
type Engine string

const (
	// EngineTypeScript is the TypeScript engine, whose rules run in cohere's own Go linter.
	EngineTypeScript Engine = "typescript"
	// EngineGo is cohere-go, the Go engine.
	EngineGo Engine = "go"
	// EngineSwift is cohere-swift, the Swift engine.
	EngineSwift Engine = "swift"
)

// Engines is every engine, in the order the corpus lists them.
var Engines = []Engine{EngineTypeScript, EngineGo, EngineSwift}

// extensionOf is the file extension of each engine's case source.
var extensionOf = map[Engine]string{
	EngineTypeScript: "ts",
	EngineGo:         "go",
	EngineSwift:      "swift",
}

// SourceName is the name of a case's source file for an engine: `case.ts`, `case.go`, `case.swift`.
func (engine Engine) SourceName() string {
	return "case." + extensionOf[engine]
}

// Kind is a ledger row's kind, and the kind of failure the comparator finds.
type Kind string

const (
	// KindVerdict is a failure of whether, or how often, the rule reports: findings on a clean case, silence
	// where it should report, or a different number of findings than expected.
	KindVerdict Kind = "verdict"
	// KindSpan is the right findings with at least one at the wrong bytes: a wrong start, a wrong end, or no
	// end at all.
	KindSpan Kind = "span"
)

// EngineRule is one engine's entry for a family in families.json: the rule's registered name, written as a
// string, or the reason the language has no such idea, written `{"none": "<reason>"}`.
type EngineRule struct {
	// Rule is the rule's registered name in its engine, or empty when None says why there is none.
	Rule string
	// None is why the language has no form of the family, or empty when Rule names the rule.
	None string
}

// UnmarshalJSON reads a rule name or `{"none": "<reason>"}`, and refuses anything else, an empty name or
// reason included.
func (entry *EngineRule) UnmarshalJSON(raw []byte) error {
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		if strings.TrimSpace(name) == "" {
			return errors.New("the rule name is empty")
		}
		entry.Rule = name
		return nil
	}
	var none struct {
		None *string `json:"none"`
	}
	if err := decodeStrictly(raw, &none); err != nil {
		return fmt.Errorf(`an engine's entry is a rule name or {"none": "<reason>"}: %w`, err)
	}
	if none.None == nil || strings.TrimSpace(*none.None) == "" {
		return errors.New(`{"none": ...} needs the reason the language has no such rule`)
	}
	entry.None = *none.None
	return nil
}

// Span is one expected finding, by what it covers rather than by coordinates, so one expectation is exact
// in every language: Go's form of a case has a `package` line the others do not. Exactly one of the three
// forms is written.
type Span struct {
	// Covers is the text the finding covers, and Occurrence which occurrence of it in the source, from 1.
	Covers     *string `json:"covers,omitempty"`
	Occurrence int     `json:"occurrence,omitempty"`

	// FileStart is a point at the start of the file, for a rule about the file's path.
	FileStart bool `json:"fileStart,omitempty"`

	// LinesFrom runs from the start of this 1-based line to the end of the file, for a rule about the file's
	// length. A line is the language's own: each engine's terminators are a fact of its grammar.
	LinesFrom int `json:"linesFrom,omitempty"`
}

// String names the span as a case writes it.
func (span Span) String() string {
	switch {
	case span.Covers != nil:
		return fmt.Sprintf("%q (occurrence %d)", *span.Covers, span.Occurrence)
	case span.FileStart:
		return "the file's start"
	default:
		return fmt.Sprintf("lines from %d", span.LinesFrom)
	}
}

// check refuses a span that is not exactly one of the three forms.
func (span Span) check() error {
	forms := 0
	if span.Covers != nil {
		forms++
		if *span.Covers == "" {
			return errors.New("covers is empty")
		}
		if span.Occurrence < 1 {
			return fmt.Errorf("covers %q needs an occurrence of 1 or more", *span.Covers)
		}
	} else if span.Occurrence != 0 {
		return errors.New("occurrence is written without covers")
	}
	if span.FileStart {
		forms++
	}
	if span.LinesFrom != 0 {
		forms++
		if span.LinesFrom < 1 {
			return fmt.Errorf("linesFrom is %d, and lines count from 1", span.LinesFrom)
		}
	}
	if forms != 1 {
		return fmt.Errorf("a span is exactly one of covers, fileStart and linesFrom, and this one writes %d", forms)
	}
	return nil
}

// Case is one case, with its sources.
type Case struct {
	Family string
	Name   string
	Why    string

	// Control marks the family's positive control: a case every engine with the family's rule must report,
	// whose count and start lines can never be ledgered.
	Control bool

	// Expect is every finding the rule must report, in source order. Empty means clean, which binds as hard
	// as a reported case does.
	Expect []Span

	// Options is the case's options in one neutral shape, which each adapter translates into its engine's
	// own form over the options the house sets give the rule.
	Options map[string]json.RawMessage

	// Path is the project-relative directory the case's source sits in, for a rule about paths, or empty for
	// the project's root. A directory rather than a file name, since a case has one source per language.
	Path string

	Languages []Engine
	Sources   map[Engine]string
}

// Id names the case as `<family>/<case>`.
func (testCase Case) Id() string {
	return testCase.Family + "/" + testCase.Name
}

// Has reports whether the case has a form in an engine's language.
func (testCase Case) Has(engine Engine) bool {
	_, has := testCase.Sources[engine]
	return has
}

// FileName is the case source's project-relative file name for an engine: `case.ts`, or `utils/case.ts`
// under a Path.
func (testCase Case) FileName(engine Engine) string {
	return path.Join(testCase.Path, engine.SourceName())
}

// Family is one family of twins, with its cases sorted by name.
type Family struct {
	Name  string
	Rules map[Engine]EngineRule
	Cases []Case
}

// LedgerRow is one case an engine fails today.
type LedgerRow struct {
	Engine Engine `json:"engine"`
	Family string `json:"family"`
	Case   string `json:"case"`
	Kind   Kind   `json:"kind"`
	// Task is the task whose fix makes the case pass, and must delete this row.
	Task string `json:"task"`
	// Reason is what the engine does instead, in one line.
	Reason string `json:"reason"`
}

// Corpus is the loaded corpus.
type Corpus struct {
	// Families are sorted by name.
	Families []Family
	Ledger   []LedgerRow
}

// CasesFor is every case with a form in an engine's language, across every family, in order.
func (corpus *Corpus) CasesFor(engine Engine) []Case {
	cases := []Case{}
	for _, family := range corpus.Families {
		for _, testCase := range family.Cases {
			if testCase.Has(engine) {
				cases = append(cases, testCase)
			}
		}
	}
	return cases
}

// RuleFor is an engine's entry for a family, and false for a family the corpus does not have.
func (corpus *Corpus) RuleFor(family string, engine Engine) (EngineRule, bool) {
	for _, candidate := range corpus.Families {
		if candidate.Name == family {
			entry, has := candidate.Rules[engine]
			return entry, has
		}
	}
	return EngineRule{}, false
}

// RowFor is the ledger's row for a case in an engine, if there is one.
func (corpus *Corpus) RowFor(engine Engine, testCase Case) (LedgerRow, bool) {
	for _, row := range corpus.Ledger {
		if row.Engine == engine && row.Family == testCase.Family && row.Case == testCase.Name {
			return row, true
		}
	}
	return LedgerRow{}, false
}

// Judge says whether an engine's result on a case is acceptable, given the ledger: nil for a pass with no
// row, or for a failure whose row is of the failure's kind; an error otherwise. A control whose count or
// start lines are wrong is an error whatever the ledger says.
func (corpus *Corpus) Judge(engine Engine, testCase Case, result Result) error {
	if testCase.Control && !result.ControlHeld {
		return fmt.Errorf("%s is its family's control, and %s does not report it as a control must, which no "+
			"ledger row can excuse: %s", testCase.Id(), engine, result.Detail)
	}
	row, ledgered := corpus.RowFor(engine, testCase)
	switch {
	case result.Passed() && ledgered:
		return fmt.Errorf("%s now passes in %s, so its %s row (task %s) is stale: delete it", testCase.Id(),
			engine, row.Kind, row.Task)
	case result.Passed():
		return nil
	case !ledgered:
		return fmt.Errorf("%s fails in %s with a %s failure and no ledger row: %s", testCase.Id(), engine,
			result.Failure, result.Detail)
	case row.Kind != result.Failure:
		return fmt.Errorf("%s fails in %s with a %s failure, and its ledger row (task %s) says %s: %s",
			testCase.Id(), engine, result.Failure, row.Task, row.Kind, result.Detail)
	}
	return nil
}

// Load reads the embedded corpus, refusing a malformed one.
func Load() (*Corpus, error) {
	return LoadFrom(embeddedCorpus)
}

// familiesFileName and ledgerFileName are the corpus's two files at its top.
const (
	familiesFileName = "families.json"
	ledgerFileName   = "ledger.json"
	caseFileName     = "case.json"
)

// caseFile is case.json as written.
type caseFile struct {
	Why       string                     `json:"why"`
	Control   bool                       `json:"control"`
	Expect    []Span                     `json:"expect"`
	Options   map[string]json.RawMessage `json:"options"`
	Path      *string                    `json:"path"`
	Languages []Engine                   `json:"languages"`
}

// LoadFrom reads a corpus from a file tree laid out as the embedded one is, and refuses: an unknown key in
// any file; a family where an engine is missing; a family directory families.json does not name, or a named
// family with no cases; a case whose sources differ from the languages it lists; an expectation that is not
// in a source; a ledger row naming a case that does not exist, or a language the case has no form in; a
// verdict row on a control; and a second row for one case in one engine.
func LoadFrom(files fs.FS) (*Corpus, error) {
	familyRules, err := readFamilies(files)
	if err != nil {
		return nil, err
	}

	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, err
	}
	corpus := &Corpus{}
	onDisk := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			// families.json and ledger.json, and in a tree read from disk, this package's Go files.
			continue
		}
		rules, named := familyRules[entry.Name()]
		if !named {
			return nil, fmt.Errorf("%s is a family directory that %s does not name", entry.Name(), familiesFileName)
		}
		onDisk[entry.Name()] = true
		family := Family{Name: entry.Name(), Rules: rules}
		family.Cases, err = readCases(files, family)
		if err != nil {
			return nil, err
		}
		corpus.Families = append(corpus.Families, family)
	}
	for name := range familyRules {
		if !onDisk[name] {
			return nil, fmt.Errorf("%s names the family %s, which has no directory of cases", familiesFileName, name)
		}
	}
	sort.Slice(corpus.Families, func(left int, right int) bool {
		return corpus.Families[left].Name < corpus.Families[right].Name
	})

	corpus.Ledger, err = readLedger(files, corpus)
	if err != nil {
		return nil, err
	}
	return corpus, nil
}

// readFamilies reads families.json, refusing a family where an engine is missing or unknown.
func readFamilies(files fs.FS) (map[string]map[Engine]EngineRule, error) {
	contents, err := fs.ReadFile(files, familiesFileName)
	if err != nil {
		return nil, err
	}
	families := map[string]map[Engine]EngineRule{}
	if err := decodeStrictly(contents, &families); err != nil {
		return nil, fmt.Errorf("%s: %w", familiesFileName, err)
	}
	if len(families) == 0 {
		return nil, fmt.Errorf("%s names no family, so the corpus would measure nothing", familiesFileName)
	}
	for name, rules := range families {
		for engine := range rules {
			if _, known := extensionOf[engine]; !known {
				return nil, fmt.Errorf("%s: family %s names %q, which is not an engine (the engines are %v)",
					familiesFileName, name, engine, Engines)
			}
		}
		for _, engine := range Engines {
			if _, written := rules[engine]; !written {
				return nil, fmt.Errorf("%s: family %s has no entry for %s. Name its rule, or write "+
					`{"none": "<reason>"} where the language has no such idea`, familiesFileName, name, engine)
			}
		}
	}
	return families, nil
}

// readCases reads every case of one family, sorted by name.
func readCases(files fs.FS, family Family) ([]Case, error) {
	entries, err := fs.ReadDir(files, family.Name)
	if err != nil {
		return nil, err
	}
	cases := []Case{}
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("%s/%s is a file, and a family holds only case directories", family.Name,
				entry.Name())
		}
		testCase, err := readCase(files, family, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", family.Name, entry.Name(), err)
		}
		cases = append(cases, testCase)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("family %s has no cases, so it would measure nothing", family.Name)
	}
	sort.Slice(cases, func(left int, right int) bool { return cases[left].Name < cases[right].Name })
	return cases, nil
}

// readCase reads one case directory: its case.json and the one source per language it lists.
func readCase(files fs.FS, family Family, name string) (Case, error) {
	directory := path.Join(family.Name, name)
	contents, err := fs.ReadFile(files, path.Join(directory, caseFileName))
	if err != nil {
		return Case{}, err
	}
	var written caseFile
	if err := decodeStrictly(contents, &written); err != nil {
		return Case{}, fmt.Errorf("%s: %w", caseFileName, err)
	}
	if strings.TrimSpace(written.Why) == "" {
		return Case{}, fmt.Errorf("%s has no why", caseFileName)
	}
	if written.Expect == nil {
		return Case{}, fmt.Errorf(`%s has no expect; write "expect": [] for a clean case`, caseFileName)
	}
	for index, span := range written.Expect {
		if err := span.check(); err != nil {
			return Case{}, fmt.Errorf("%s: expect %d: %w", caseFileName, index, err)
		}
	}
	if written.Control && len(written.Expect) == 0 {
		return Case{}, fmt.Errorf("%s is a control that expects nothing, and a control must be reported", caseFileName)
	}
	testCase := Case{
		Family:  family.Name,
		Name:    name,
		Why:     written.Why,
		Control: written.Control,
		Expect:  written.Expect,
		Options: written.Options,
		Sources: map[Engine]string{},
	}
	if written.Path != nil {
		cleaned := path.Clean(*written.Path)
		if *written.Path == "" || cleaned != *written.Path || path.IsAbs(cleaned) || cleaned == "." ||
			cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return Case{}, fmt.Errorf("%s: path %q is not a clean project-relative directory", caseFileName,
				*written.Path)
		}
		testCase.Path = cleaned
	}

	if len(written.Languages) == 0 {
		return Case{}, fmt.Errorf("%s lists no languages", caseFileName)
	}
	for _, language := range written.Languages {
		if _, known := extensionOf[language]; !known {
			return Case{}, fmt.Errorf("%s lists %q, which is not a language (the languages are %v)", caseFileName,
				language, Engines)
		}
		if testCase.Has(language) {
			return Case{}, fmt.Errorf("%s lists %s twice", caseFileName, language)
		}
		if entry := family.Rules[language]; entry.None != "" {
			return Case{}, fmt.Errorf("%s lists %s, where the family has no rule: %s", caseFileName, language,
				entry.None)
		}
		source, err := fs.ReadFile(files, path.Join(directory, language.SourceName()))
		if err != nil {
			return Case{}, fmt.Errorf("%s lists %s, and %s can't be read: %w", caseFileName, language,
				language.SourceName(), err)
		}
		testCase.Sources[language] = string(source)
		testCase.Languages = append(testCase.Languages, language)
	}

	// Every file is case.json or a source the case lists, so a language's form is never inferred from a file
	// that happens to be there.
	entries, err := fs.ReadDir(files, directory)
	if err != nil {
		return Case{}, err
	}
	for _, entry := range entries {
		if entry.Name() == caseFileName {
			continue
		}
		listed := false
		for _, language := range testCase.Languages {
			if entry.Name() == language.SourceName() {
				listed = true
			}
		}
		if !listed {
			return Case{}, fmt.Errorf("%s is not a source the case lists in its languages", entry.Name())
		}
	}

	// Each expectation has to be in each source, so a case can't expect a finding over text it does not hold.
	for _, language := range testCase.Languages {
		for index, span := range testCase.Expect {
			if _, err := Resolve(span, testCase.Sources[language], language); err != nil {
				return Case{}, fmt.Errorf("expect %d in %s: %w", index, language.SourceName(), err)
			}
		}
	}
	return testCase, nil
}

// readLedger reads ledger.json and checks each row against the cases it names.
func readLedger(files fs.FS, corpus *Corpus) ([]LedgerRow, error) {
	contents, err := fs.ReadFile(files, ledgerFileName)
	if err != nil {
		return nil, err
	}
	rows := []LedgerRow{}
	if err := decodeStrictly(contents, &rows); err != nil {
		return nil, fmt.Errorf("%s: %w", ledgerFileName, err)
	}
	seen := map[string]bool{}
	for index, row := range rows {
		label := fmt.Sprintf("%s row %d (%s %s/%s)", ledgerFileName, index, row.Engine, row.Family, row.Case)
		if _, known := extensionOf[row.Engine]; !known {
			return nil, fmt.Errorf("%s: %q is not an engine (the engines are %v)", label, row.Engine, Engines)
		}
		if row.Kind != KindVerdict && row.Kind != KindSpan {
			return nil, fmt.Errorf("%s: kind is %q, and a row is %q or %q", label, row.Kind, KindVerdict, KindSpan)
		}
		if strings.TrimSpace(row.Task) == "" || strings.TrimSpace(row.Reason) == "" {
			return nil, fmt.Errorf("%s needs both the task that fixes it and the reason it fails", label)
		}
		var named *Case
		for familyIndex := range corpus.Families {
			if corpus.Families[familyIndex].Name != row.Family {
				continue
			}
			for caseIndex := range corpus.Families[familyIndex].Cases {
				if corpus.Families[familyIndex].Cases[caseIndex].Name == row.Case {
					named = &corpus.Families[familyIndex].Cases[caseIndex]
				}
			}
		}
		if named == nil {
			return nil, fmt.Errorf("%s names a case that does not exist", label)
		}
		if !named.Has(row.Engine) {
			return nil, fmt.Errorf("%s names a case with no %s form", label, row.Engine)
		}
		if named.Control && row.Kind != KindSpan {
			return nil, fmt.Errorf("%s is a %s row on a control. A control's count and start lines can never be "+
				"ledgered, since that would hide a broken adapter; only a span row may sit on one", label, row.Kind)
		}
		key := string(row.Engine) + "\x00" + row.Family + "\x00" + row.Case
		if seen[key] {
			return nil, fmt.Errorf("%s is the second row for that case in that engine", label)
		}
		seen[key] = true
	}
	return rows, nil
}

// decodeStrictly decodes one JSON value, refusing an unknown key and anything after the value.
func decodeStrictly(contents []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("there is more after the JSON value")
	}
	return nil
}

// Range is one finding as a byte range of the case's source: 0-based UTF-8 byte offsets, End exclusive.
type Range struct {
	Start int
	End   int

	// NoEnd marks a finding whose engine gave only a start. It fails any span, since a span checks the end
	// as well as the start.
	NoEnd bool
}

// Result is what the comparator found.
type Result struct {
	// Failure is the kind of failure, or empty when the findings meet the expectation.
	Failure Kind

	// ControlHeld is whether the findings are as many as expected and each starts on the line its expected
	// span starts on: the check a control must always pass.
	ControlHeld bool

	// Detail says what differed, or what matched, in a line a reader can check.
	Detail string
}

// Passed reports whether the findings meet the expectation exactly.
func (result Result) Passed() bool {
	return result.Failure == ""
}

// Resolve is a span's byte range in a source, read with an engine's line terminators, or why it has none.
func Resolve(span Span, source string, engine Engine) (Range, error) {
	if err := span.check(); err != nil {
		return Range{}, err
	}
	switch {
	case span.Covers != nil:
		from := 0
		for occurrence := 1; ; occurrence++ {
			index := strings.Index(source[from:], *span.Covers)
			if index < 0 {
				return Range{}, fmt.Errorf("%q occurs %d times, so it has no occurrence %d", *span.Covers,
					occurrence-1, span.Occurrence)
			}
			start := from + index
			if occurrence == span.Occurrence {
				return Range{Start: start, End: start + len(*span.Covers)}, nil
			}
			from = start + 1
		}
	case span.FileStart:
		return Range{Start: 0, End: 0}, nil
	default:
		starts := LineStarts(source, engine)
		if span.LinesFrom > len(starts) {
			return Range{}, fmt.Errorf("the source has %d lines, so it has no line %d", len(starts), span.LinesFrom)
		}
		return Range{Start: starts[span.LinesFrom-1], End: len(source)}, nil
	}
}

// LineStarts is the byte offset each line of a source starts at, as an engine's language ends a line:
// ECMAScript's `\r\n`, `\n`, `\r`, U+2028 and U+2029 for TypeScript; `\r\n`, `\n` and a lone `\r` for Swift;
// `\n` for Go.
func LineStarts(source string, engine Engine) []int {
	starts := []int{0}
	for index := 0; index < len(source); index++ {
		switch {
		case source[index] == '\n':
			starts = append(starts, index+1)
		case source[index] == '\r' && engine != EngineGo:
			if index+1 < len(source) && source[index+1] == '\n' {
				index++
			}
			starts = append(starts, index+1)
		case engine == EngineTypeScript && strings.HasPrefix(source[index:], " "),
			engine == EngineTypeScript && strings.HasPrefix(source[index:], " "):
			index += len(" ") - 1
			starts = append(starts, index+1)
		}
	}
	return starts
}

// lineOf is the 1-based line an offset sits on.
func lineOf(starts []int, offset int) int {
	return sort.Search(len(starts), func(index int) bool { return starts[index] > offset })
}

// Compare holds an engine's findings on a case, as byte ranges of the case's source in that engine's
// language, to the case's expectation. Findings are compared in source order, whatever order they came in.
func Compare(testCase Case, engine Engine, found []Range) Result {
	source, has := testCase.Sources[engine]
	if !has {
		return Result{Failure: KindVerdict, Detail: fmt.Sprintf("%s has no %s form", testCase.Id(), engine)}
	}
	starts := LineStarts(source, engine)

	expected := make([]Range, 0, len(testCase.Expect))
	for index, span := range testCase.Expect {
		resolved, err := Resolve(span, source, engine)
		if err != nil {
			return Result{Failure: KindVerdict, Detail: fmt.Sprintf("expect %d does not resolve: %v", index, err)}
		}
		expected = append(expected, resolved)
	}
	sort.SliceStable(expected, func(left int, right int) bool { return expected[left].Start < expected[right].Start })
	sorted := append([]Range(nil), found...)
	sort.SliceStable(sorted, func(left int, right int) bool {
		if sorted[left].Start != sorted[right].Start {
			return sorted[left].Start < sorted[right].Start
		}
		return sorted[left].End < sorted[right].End
	})

	detail := fmt.Sprintf("expected %s, found %s", describeRanges(expected, source, starts),
		describeRanges(sorted, source, starts))
	if len(sorted) != len(expected) {
		return Result{Failure: KindVerdict, Detail: detail}
	}

	result := Result{ControlHeld: true, Detail: detail}
	for index := range sorted {
		if lineOf(starts, sorted[index].Start) != lineOf(starts, expected[index].Start) {
			result.ControlHeld = false
		}
		if sorted[index].NoEnd || sorted[index].Start != expected[index].Start ||
			sorted[index].End != expected[index].End {
			result.Failure = KindSpan
		}
	}
	return result
}

// describeRanges renders ranges with the line each starts on and the text each covers.
func describeRanges(ranges []Range, source string, starts []int) string {
	if len(ranges) == 0 {
		return "nothing"
	}
	described := make([]string, 0, len(ranges))
	for _, each := range ranges {
		if each.NoEnd {
			described = append(described, fmt.Sprintf("line %d from byte %d with no end", lineOf(starts, each.Start),
				each.Start))
			continue
		}
		if each.Start < 0 || each.End > len(source) || each.Start > each.End {
			described = append(described, fmt.Sprintf("[%d,%d), outside the source", each.Start, each.End))
			continue
		}
		described = append(described, fmt.Sprintf("line %d [%d,%d) %q", lineOf(starts, each.Start), each.Start,
			each.End, source[each.Start:each.End]))
	}
	return strings.Join(described, ", ")
}
