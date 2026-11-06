// Package textrules is the text rules' conformance corpus (#4b337jk): one set of cases with one expected
// verdict each, which every engine's own test suite reads and must meet. Some rules exist once per language,
// as twins, and the twins disagreed. The verdicts are decided once, in the cases, and never by what an
// engine happens to do.
//
// The corpus is data beside this file:
//
//	families.json        family -> each engine's rule, by its registered name
//	ledger.json          the cases an engine fails today, each naming the task that fixes it
//	testdata/<family>/<case>/
//	  case.json          the expectation, options, path, why, and which languages have a form
//	  case.ts            one source per language the case lists, and none it does not
//	  case.go
//	  case.swift
//
// The cases sit under testdata because their sources carry the very violations the rules judge. No tool
// builds a `case.go` there as a package, and cohere-go skips any directory named testdata, so none of their
// findings join cohere's own. The two index files stay beside this one, since they describe the corpus rather
// than being cases in it.
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
// A case marked as its family's control proves the adapter and the rule are live in an engine, so part of its
// check can never be ledgered, because a control whose silence was ledgered would hide an adapter that
// measures nothing. Its count of findings is always checked. Where a span covers text, the line each finding
// starts on is checked too, since the text is the defect's identity. Where a span is the file's start or its
// lines from one on, the defect is the whole file, and where the finding sits is placement, a span question.
// Past that, a control can carry a span row and never a verdict row.
//
// A row with no case is a family-level row: the engine has no rule for the family yet, where the verdict says
// it should, and families.json names it `{"missing": true}`. Its cases are written in that language anyway and
// none of them runs. The row goes stale the moment families.json names a rule there, which the loader refuses,
// so deleting the row is what lets the cases run.
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
//go:embed families.json ledger.json testdata/*/*
var embeddedCorpus embed.FS

// extensionOf is the file extension of each engine's case source.
var extensionOf = map[Engine]string{
	EngineTypeScript: "ts",
	EngineGo:         "go",
	EngineSwift:      "swift",
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

// Family is one family of twins, with its cases sorted by name.
type Family struct {
	Name  string
	Rules map[Engine]EngineRule
	Cases []Case
}

// LedgerRow is one case an engine fails today, or with no Case and no Kind, one family it has no rule for yet.
type LedgerRow struct {
	Engine Engine `json:"engine"`
	Family string `json:"family"`
	Case   string `json:"case,omitempty"`
	Kind   Kind   `json:"kind,omitempty"`
	// Task is the task whose fix makes the case pass, and must delete this row.
	Task string `json:"task"`
	// Reason is what the engine does instead, in one line.
	Reason string `json:"reason"`
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
	// casesDirectory holds a directory per family, and in each a directory per case.
	casesDirectory = "testdata"
	// defaultPath and defaultRoot are a case's file name, without its extension, and the project it sits in,
	// where the case names neither.
	defaultPath = "case"
	defaultRoot = "/project"
)

// caseFile is case.json as written.
type caseFile struct {
	Why       string                     `json:"why"`
	Control   bool                       `json:"control"`
	Expect    []Span                     `json:"expect"`
	Options   map[string]json.RawMessage `json:"options"`
	Path      *string                    `json:"path"`
	Root      *string                    `json:"root"`
	Languages []Engine                   `json:"languages"`
}

// LoadFrom reads a corpus from a file tree laid out as the embedded one is, and refuses: an unknown key in
// any file; a family where an engine has no entry; a family directory families.json does not name, or a named
// family with no cases; a case whose sources differ from the languages it lists; an expectation that is not
// in a source; a ledger row naming a case that does not exist, or a language the case has no form in; a
// verdict row on a control; a second row for one case in one engine; a `missing` engine with no family-level
// row, a family-level row for any other entry, and a case-level row for a `missing` engine.
func LoadFrom(files fs.FS) (*Corpus, error) {
	familyRules, readFamiliesError := readFamilies(files)
	if readFamiliesError != nil {
		return nil, readFamiliesError
	}

	caseFiles, subError := fs.Sub(files, casesDirectory)
	if subError != nil {
		return nil, subError
	}
	entries, readDirectoryError := fs.ReadDir(caseFiles, ".")
	if readDirectoryError != nil {
		return nil, readDirectoryError
	}
	corpus := &Corpus{}
	onDisk := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("%s/%s is a file, and %s holds only family directories", casesDirectory,
				entry.Name(), casesDirectory)
		}
		rules, named := familyRules[entry.Name()]
		if !named {
			return nil, fmt.Errorf("%s/%s is a family directory that %s does not name", casesDirectory, entry.Name(),
				familiesFileName)
		}
		onDisk[entry.Name()] = true
		family := Family{Name: entry.Name(), Rules: rules}
		var readCasesError error
		family.Cases, readCasesError = readCases(caseFiles, family)
		if readCasesError != nil {
			return nil, readCasesError
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

	var readLedgerError error
	corpus.Ledger, readLedgerError = readLedger(files, corpus)
	if readLedgerError != nil {
		return nil, readLedgerError
	}
	return corpus, nil
}

// readFamilies reads families.json, refusing a family where an engine is missing or unknown.
func readFamilies(files fs.FS) (map[string]map[Engine]EngineRule, error) {
	contents, readFileError := fs.ReadFile(files, familiesFileName)
	if readFileError != nil {
		return nil, readFileError
	}
	families := map[string]map[Engine]EngineRule{}
	if decodeStrictlyError := decodeStrictly(contents, &families); decodeStrictlyError != nil {
		return nil, fmt.Errorf("%s: %w", familiesFileName, decodeStrictlyError)
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
	entries, readDirectoryError := fs.ReadDir(files, family.Name)
	if readDirectoryError != nil {
		return nil, readDirectoryError
	}
	cases := []Case{}
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("%s/%s is a file, and a family holds only case directories", family.Name,
				entry.Name())
		}
		testCase, readCaseError := readCase(files, family, entry.Name())
		if readCaseError != nil {
			return nil, fmt.Errorf("%s/%s: %w", family.Name, entry.Name(), readCaseError)
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
	contents, readFileError := fs.ReadFile(files, path.Join(directory, caseFileName))
	if readFileError != nil {
		return Case{}, readFileError
	}
	var written caseFile
	if decodeStrictlyError := decodeStrictly(contents, &written); decodeStrictlyError != nil {
		return Case{}, fmt.Errorf("%s: %w", caseFileName, decodeStrictlyError)
	}
	if strings.TrimSpace(written.Why) == "" {
		return Case{}, fmt.Errorf("%s has no why", caseFileName)
	}
	if written.Expect == nil {
		return Case{}, fmt.Errorf(`%s has no expect; write "expect": [] for a clean case`, caseFileName)
	}
	for index, span := range written.Expect {
		if checkError := span.check(); checkError != nil {
			return Case{}, fmt.Errorf("%s: expect %d: %w", caseFileName, index, checkError)
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
		Path:    defaultPath,
		Root:    defaultRoot,
		Sources: map[Engine]string{},
	}
	if written.Path != nil {
		cleaned := path.Clean(*written.Path)
		if *written.Path == "" || cleaned != *written.Path || path.IsAbs(cleaned) || cleaned == "." ||
			cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.Base(cleaned) == ".." {
			return Case{}, fmt.Errorf("%s: path %q is not a clean project-relative file name without its "+
				"extension", caseFileName, *written.Path)
		}
		testCase.Path = cleaned
	}
	if written.Root != nil {
		if !path.IsAbs(*written.Root) || path.Clean(*written.Root) != *written.Root || *written.Root == "/" {
			return Case{}, fmt.Errorf("%s: root %q is not a clean absolute directory", caseFileName, *written.Root)
		}
		testCase.Root = *written.Root
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
		source, readFileError := fs.ReadFile(files, path.Join(directory, language.SourceName()))
		if readFileError != nil {
			return Case{}, fmt.Errorf("%s lists %s, and %s can't be read: %w", caseFileName, language,
				language.SourceName(), readFileError)
		}
		testCase.Sources[language] = string(source)
		testCase.Languages = append(testCase.Languages, language)
	}

	// Every file is case.json or a source the case lists, so a language's form is never inferred from a file
	// that happens to be there.
	entries, readDirectoryError := fs.ReadDir(files, directory)
	if readDirectoryError != nil {
		return Case{}, readDirectoryError
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

	// A per-language occurrence names exactly the languages the case lists, so none is guessed or left over.
	for index, span := range testCase.Expect {
		if span.OccurrenceIn != nil && len(span.OccurrenceIn) != len(testCase.Languages) {
			return Case{}, fmt.Errorf("expect %d gives occurrences for %d languages, and the case lists %d", index,
				len(span.OccurrenceIn), len(testCase.Languages))
		}
	}

	// Each expectation has to be in each source it applies to, so a case can't expect a finding over text it
	// does not hold, and each has to apply somewhere.
	for index, span := range testCase.Expect {
		applies := false
		for _, language := range testCase.Languages {
			applies = applies || span.AppliesTo(language)
		}
		if !applies {
			return Case{}, fmt.Errorf("expect %d applies to none of the case's languages", index)
		}
	}
	for _, language := range testCase.Languages {
		for index, span := range testCase.Expect {
			if !span.AppliesTo(language) {
				continue
			}
			if _, resolveError := Resolve(span, testCase.Sources[language], language); resolveError != nil {
				return Case{}, fmt.Errorf("expect %d in %s: %w", index, language.SourceName(), resolveError)
			}
		}
	}
	return testCase, nil
}

// readLedger reads ledger.json and checks each row against the cases it names.
func readLedger(files fs.FS, corpus *Corpus) ([]LedgerRow, error) {
	contents, readFileError := fs.ReadFile(files, ledgerFileName)
	if readFileError != nil {
		return nil, readFileError
	}
	rows := []LedgerRow{}
	if decodeStrictlyError := decodeStrictly(contents, &rows); decodeStrictlyError != nil {
		return nil, fmt.Errorf("%s: %w", ledgerFileName, decodeStrictlyError)
	}
	seen := map[string]bool{}
	for index, row := range rows {
		label := fmt.Sprintf("%s row %d (%s %s/%s)", ledgerFileName, index, row.Engine, row.Family, row.Case)
		if _, known := extensionOf[row.Engine]; !known {
			return nil, fmt.Errorf("%s: %q is not an engine (the engines are %v)", label, row.Engine, Engines)
		}
		if strings.TrimSpace(row.Task) == "" || strings.TrimSpace(row.Reason) == "" {
			return nil, fmt.Errorf("%s needs both the task that fixes it and the reason it fails", label)
		}
		entry, familyFound := corpus.RuleFor(row.Family, row.Engine)
		if !familyFound {
			return nil, fmt.Errorf("%s names a family that does not exist", label)
		}
		if row.Case == "" {
			if checkFamilyRowError := checkFamilyRow(label, row, entry); checkFamilyRowError != nil {
				return nil, checkFamilyRowError
			}
			key := string(row.Engine) + "\x00" + row.Family
			if seen[key] {
				return nil, fmt.Errorf("%s is the second family-level row for that family in that engine", label)
			}
			seen[key] = true
			continue
		}
		if entry.Missing {
			return nil, fmt.Errorf("%s is a case-level row where %s has no rule for the family, so none of its "+
				"cases runs; the family-level row covers them", label, row.Engine)
		}
		if row.Kind != KindVerdict && row.Kind != KindSpan {
			return nil, fmt.Errorf("%s: kind is %q, and a row is %q or %q", label, row.Kind, KindVerdict, KindSpan)
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
	for _, family := range corpus.Families {
		for _, engine := range Engines {
			if family.Rules[engine].Missing && !seen[string(engine)+"\x00"+family.Name] {
				return nil, fmt.Errorf("%s names %s as missing in family %s, and %s has no family-level row naming "+
					"the task that adds the rule", familiesFileName, engine, family.Name, ledgerFileName)
			}
		}
	}
	return rows, nil
}

// checkFamilyRow refuses a family-level row, one with no case, unless families.json names its engine as
// missing there, and refuses a kind on one, since a family-level row judges no case.
func checkFamilyRow(label string, row LedgerRow, entry EngineRule) error {
	if row.Kind != "" {
		return fmt.Errorf("%s is a family-level row with a kind, and a family-level row judges no case, so it "+
			"has none", label)
	}
	switch {
	case entry.Runs():
		return fmt.Errorf("%s is a family-level row, and %s names %s there now, so the row is stale: "+
			"delete it and let the cases run", label, familiesFileName, entry)
	case entry.None != "":
		return fmt.Errorf("%s is a family-level row where the language has no such idea (%s), so there is no "+
			"rule to add", label, entry.None)
	}
	return nil
}

// decodeStrictly decodes one JSON value into the Target it points at, refusing an unknown key and anything
// after the value. Its callers decode several shapes, a rule entry, a span, the families, a case and the ledger,
// so Target is a type parameter, and each caller still names the shape it reads.
func decodeStrictly[Target any](contents []byte, into *Target) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if decodeError := decoder.Decode(into); decodeError != nil {
		return decodeError
	}
	if decodeError := decoder.Decode(&struct{}{}); !errors.Is(decodeError, io.EOF) {
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

// Resolve is a span's byte range in a source, read with an engine's line terminators, or why it has none.
func Resolve(span Span, source string, engine Engine) (Range, error) {
	if checkError := span.check(); checkError != nil {
		return Range{}, checkError
	}
	switch {
	case span.Covers != nil:
		wanted := span.occurrenceFor(engine)
		if wanted == 0 {
			return Range{}, fmt.Errorf("%q names no occurrence in %s", *span.Covers, engine)
		}
		from := 0
		for occurrence := 1; ; occurrence++ {
			index := strings.Index(source[from:], *span.Covers)
			if index < 0 {
				return Range{}, fmt.Errorf("%q occurs %d times, so it has no occurrence %d", *span.Covers,
					occurrence-1, wanted)
			}
			start := from + index
			if occurrence == wanted {
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
		case engine == EngineTypeScript && strings.HasPrefix(source[index:], "\u2028"),
			engine == EngineTypeScript && strings.HasPrefix(source[index:], "\u2029"):
			index += len("\u2028") - 1
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

	// coversText is each expected range's span kind, aligned with it through the sort: a span that covers text
	// holds a control to its line, and one over the whole file does not.
	expected := make([]Range, 0, len(testCase.Expect))
	coversText := make([]bool, 0, len(testCase.Expect))
	for index, span := range testCase.Expect {
		if !span.AppliesTo(engine) {
			continue
		}
		resolved, resolveError := Resolve(span, source, engine)
		if resolveError != nil {
			return Result{
				Failure: KindVerdict,
				Detail:  fmt.Sprintf("expect %d does not resolve: %v", index, resolveError),
			}
		}
		expected = append(expected, resolved)
		coversText = append(coversText, span.Covers != nil)
	}
	order := make([]int, len(expected))
	for index := range order {
		order[index] = index
	}
	sort.SliceStable(
		order,
		func(left int, right int) bool { return expected[order[left]].Start < expected[order[right]].Start },
	)
	sortedExpected := make([]Range, 0, len(expected))
	sortedCoversText := make([]bool, 0, len(expected))
	for _, index := range order {
		sortedExpected = append(sortedExpected, expected[index])
		sortedCoversText = append(sortedCoversText, coversText[index])
	}
	expected, coversText = sortedExpected, sortedCoversText
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
		if coversText[index] && lineOf(starts, sorted[index].Start) != lineOf(starts, expected[index].Start) {
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
