package selector

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/json_value"
	"github.com/system-inc/cohere/internal/testing/recorded"
)

// libraryDirectory holds the comparisons' inputs and postcss-selector-parser's recorded answers.
var libraryDirectory = filepath.Join("testdata", "postcss_selector_parser")

// vendoredCssDirectory is Prettier's own css fixtures, vendored for the css package's tests.
var vendoredCssDirectory = filepath.Join("..", "testdata", "prettier", "css")

// recorder names the script that records every answer here, for the message that asks for it again.
const recorder = "record-postcss-selector-parser.mjs, attached to #yah3d69"

// selectorInput is one selector to compare: its name, its text, and malformed, which says the library
// refuses it, so a recording that refuses a valid fixture, or accepts a malformed one, fails the test
// rather than quietly agreeing. The random inputs carry no malformed; their test holds them to nothing but
// agreement.
type selectorInput struct {
	Name      string `json:"name"`
	Text      string `json:"text"`
	Malformed bool   `json:"malformed"`
}

// selectorInputs is an inputs file, parse_inputs.json.
type selectorInputs struct {
	About  string          `json:"about"`
	Inputs []selectorInput `json:"inputs"`
}

// selectorAnswer is the library's answer for one input, with what it was recorded for, the input's name
// and text. Ast is the recorded.Fingerprint of the tree, as canonicalJson writes it with every sourceIndex
// in bytes, and Error the fingerprint of the message it refused with. The recorder printed sourceIndex in
// UTF-16; it was converted once, when the answers were written as fingerprints, to the byte offset Parse
// promises (the start of the character the index falls in, the middle of a surrogate pair included, which
// splitWord can produce, and past the end by the same distance), by code written apart from Parse's
// conversion so that a bug there cannot hide here. The tests only compare the two with Go's, so the
// fingerprint is all of them they need: the answers can never be recorded again, so a mismatch is a
// regression of ours, and the failure prints Go's side in full.
type selectorAnswer struct {
	recorded.RecordedFor
	Ast   string `json:"ast"`
	Error string `json:"error"`
}

// vendoredFile is one answer of vendored_answers_<n>.json: for a vendored stylesheet, by its path there
// and its digest, the selectors Prettier's parse hands parseSelector with the library's answer for each, as
// a selectorAnswer keeps it (a selector an earlier file already gave is left out), or the reason Prettier's
// parse refuses the file, which no Go code here answers and which is kept whole. A selector's text is the
// only copy of it, so it stays whole too.
type vendoredFile struct {
	recorded.RecordedFor
	CssError  string `json:"cssError"`
	Selectors []struct {
		Text  string `json:"text"`
		Ast   string `json:"ast"`
		Error string `json:"error"`
	} `json:"selectors"`
}

// readSelectorAnswers reads the answers files at paths, in order, as one set, the parts of a split set or a
// single file, and returns its answers paired with entries, entry by entry, and the answers' source. An
// entry with no answer, or one recorded for another text or digest, fails the test, and so do parts that
// disagree on where they came from.
func readSelectorAnswers[Answer interface{ Entry() recorded.Entry }](
	test *testing.T,
	entries []recorded.Entry,
	paths ...string,
) ([]Answer, string) {
	test.Helper()
	var answers []Answer
	source := ""
	for _, path := range paths {
		file, readAnswersError := recorded.ReadJson[recorded.Answers[Answer]](path)
		if readAnswersError != nil {
			test.Fatal(readAnswersError)
		}
		fileSource := file.Source + ", recorded " + file.Recorded
		if source != "" && fileSource != source {
			test.Fatalf("%s says its answers are %s, and %s says %s", path, fileSource, paths[0], source)
		}
		source = fileSource
		answers = append(answers, file.Answers...)
	}
	answerEntries := make([]recorded.Entry, len(answers))
	for index, answer := range answers {
		answerEntries[index] = answer.Entry()
	}
	indexes, matchError := recorded.Match(entries, answerEntries)
	if matchError != nil {
		test.Fatalf("%v: %v: record them again with %s", paths, matchError, recorder)
	}
	paired := make([]Answer, len(indexes))
	for position, index := range indexes {
		paired[position] = answers[index]
	}
	return paired, source
}

// selectorEntries are inputs as recorded.Match pairs them, by name and text.
func selectorEntries(inputs []selectorInput) []recorded.Entry {
	entries := make([]recorded.Entry, len(inputs))
	for index, input := range inputs {
		entries[index] = recorded.Entry{Name: input.Name, Input: input.Text}
	}
	return entries
}

// treeText is Go's tree as canonicalJson writes it, the text its fingerprint is taken of, or why it does not
// encode, which no recorded fingerprint matches.
func treeText(tree json_value.Value) string {
	text, canonicalError := recorded.CanonicalJson(tree)
	if canonicalError != nil {
		return fmt.Sprintf("a tree that does not encode as JSON: %v", canonicalError)
	}
	return text
}

// compareSelector parses text and compares it with the library's answer, the fingerprints of its tree,
// wantAst, or of its refusal, wantError: Go's tree, as canonicalJson writes it, must have the tree's, so the
// same tree, property for property with sourceIndex in bytes, or Go's message the refusal's, so the same
// refusal. It says whether the library refused, Go's message when Go refused, and any difference, which
// prints Go's side in full beside the library's fingerprint, since the library's can never be recorded again
// and Go's is what to fix.
func compareSelector(text string, wantAst string, wantError string) (bool, string, string) {
	got, dumpSelectorError := dumpSelector(text)
	switch {
	case wantError != "" && dumpSelectorError != nil:
		if gotFingerprint := recorded.Fingerprint(dumpSelectorError.Error()); gotFingerprint != wantError {
			return true, dumpSelectorError.Error(), fmt.Sprintf(
				"both refuse, differently: the library's message is %s, and Go's is %s, %s",
				wantError,
				gotFingerprint,
				dumpSelectorError.Error(),
			)
		}
		return true, dumpSelectorError.Error(), ""
	case wantError != "":
		return true, "", fmt.Sprintf(
			"the library refuses it (its message is %s) and Go parsed it to %s",
			wantError,
			treeText(got),
		)
	case dumpSelectorError != nil:
		return false, dumpSelectorError.Error(), fmt.Sprintf(
			"the library parses it (its tree is %s) and Go refused: %v",
			wantAst,
			dumpSelectorError,
		)
	}
	gotText := treeText(got)
	if gotFingerprint := recorded.Fingerprint(gotText); gotFingerprint != wantAst {
		return false, "", fmt.Sprintf("the library's tree is %s, and Go's is %s, %s", wantAst, gotFingerprint, gotText)
	}
	return false, "", ""
}

// describeSelectorAnswer is a recorded answer as a failure message shows it.
func describeSelectorAnswer(answer selectorAnswer) string {
	if answer.Error != "" {
		return "it refuses it with the message " + answer.Error
	}
	return "it parses it to the tree " + answer.Ast
}

// prettierRefusedFixtures are the vendored fixtures Prettier's own parse refuses, front matter stripped as
// parser-postcss.js strips it, so they hold no selectors to compare: the path under testdata/prettier/css,
// and why. @system_adamic_integration's guard on the same fixtures names the same three (#cr45gcp).
var prettierRefusedFixtures = map[string]string{
	"_errors_/less-syntax.css": "an error fixture by design: a Less mixin call, which the css parser refuses",
	"_errors_/scss-syntax.css": "an error fixture by design: Sass interpolation, which the css parser refuses",
	"range/issue2267.css":      "a range fixture: its <<<PRETTIER_RANGE_END>>> marker is stripped by Prettier's test harness, never by Prettier",
}

// TestParseAgreesWithPostcssSelectorParser is the parser's acceptance test: for every selector Prettier
// would parse in its own css fixtures, vendored in ../testdata/prettier/css, and every inline input in
// parse_inputs.json, the Go tree must equal postcss-selector-parser 2.2.3's, property for property, and a
// refusal must be a refusal on both sides with the same message.
//
// The library's answers are recorded, so the test runs no node and reads nothing outside this repository.
// They are postcss-selector-parser 2.2.3's, the version the Prettier fork pins, called the way Prettier's
// parse-selector.js calls it. A vendored stylesheet's selectors are found as Prettier finds them, front
// matter replaced by the fork's parseFrontMatter and the stylesheet parsed with postcss 8.5.16: a rule's raw
// selector, and the params of custom-selector, extend, nest and at-root. Each answer for a vendored file
// carries its SHA-256, so a changed file fails until it is recorded again. Every own key but parent dumps,
// sorted, "<undefined>" against absent. The answers keep each tree's and each message's
// recorded.Fingerprint (see selectorAnswer), parse_answers.json for the inline inputs and
// vendored_answers_<n>.json, its parts, for the vendored files.
//
// A stylesheet Prettier itself refuses gives no selectors to compare. The vendored ones that do are named
// in prettierRefusedFixtures, and a vendored stylesheet refused but not named there, or named there and
// parsed, fails the test. A corpus selector the library cannot parse is counted, and the test fails if that
// count passes a tenth of them: a recording that rejects everything would otherwise read as agreement.
//
// To add or change an input, or after the vendored fixtures change, record the answers again with
// record-postcss-selector-parser.mjs, attached to #yah3d69, its usage in its header, writing them as the
// answers files hold them.
func TestParseAgreesWithPostcssSelectorParser(test *testing.T) {
	test.Parallel()
	inputs, readInputsError := recorded.ReadJson[selectorInputs](filepath.Join(libraryDirectory, "parse_inputs.json"))
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	if len(inputs.Inputs) == 0 {
		test.Fatal("parse_inputs.json holds no inputs")
	}
	answers, source := readSelectorAnswers[selectorAnswer](
		test,
		selectorEntries(inputs.Inputs),
		filepath.Join(libraryDirectory, "parse_answers.json"),
	)

	identical, refusedByBoth, different := 0, 0, 0
	fail := func(format string, arguments ...any) {
		different++
		if different <= 20 {
			test.Errorf(format, arguments...)
		}
	}
	tally := func(name string, text string, wantAst string, wantError string) bool {
		refused, _, difference := compareSelector(text, wantAst, wantError)
		if difference != "" {
			fail("%s %q: %s", name, text, difference)
			return refused
		}
		if refused {
			refusedByBoth++
		} else {
			identical++
		}
		return refused
	}
	for position, input := range inputs.Inputs {
		want := answers[position]
		if input.Malformed != (want.Error != "") {
			fail("%s: the input says malformed=%v and the library says %s", input.Name, input.Malformed,
				describeSelectorAnswer(want))
			continue
		}
		tally(input.Name, input.Text, want.Ast, want.Error)
	}

	var vendoredEntries []recorded.Entry
	walkError := filepath.WalkDir(vendoredCssDirectory, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() || filepath.Ext(path) != ".css" {
			return pathError
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		if !utf8.Valid(contents) {
			test.Fatalf("%s is not UTF-8, which the library's answers cannot be compared with", path)
		}
		relative, relativeError := filepath.Rel(vendoredCssDirectory, path)
		if relativeError != nil {
			return relativeError
		}
		vendoredEntries = append(
			vendoredEntries,
			recorded.Entry{Name: filepath.ToSlash(relative), Input: recorded.Digest(contents)},
		)
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	if len(vendoredEntries) == 0 {
		test.Fatalf("no .css files under %s", vendoredCssDirectory)
	}
	vendoredParts, partsError := recorded.Parts(libraryDirectory, "vendored_answers_")
	if partsError != nil {
		test.Fatal(partsError)
	}
	vendored, _ := readSelectorAnswers[vendoredFile](test, vendoredEntries, vendoredParts...)

	corpusSelectors, corpusRefused := 0, 0
	refusedFiles := map[string]string{}
	for _, file := range vendored {
		if file.CssError != "" {
			refusedFiles[file.Name] = file.CssError
			continue
		}
		for _, want := range file.Selectors {
			corpusSelectors++
			if tally(file.Name, want.Text, want.Ast, want.Error) {
				corpusRefused++
			}
		}
	}
	for name, cssError := range refusedFiles {
		if _, named := prettierRefusedFixtures[name]; !named {
			test.Errorf("%s: Prettier's parse refuses it (%s), and it isn't named in prettierRefusedFixtures", name,
				cssError)
		}
	}
	for name, reason := range prettierRefusedFixtures {
		if _, refused := refusedFiles[name]; !refused {
			test.Errorf(
				"%s is named as refused by Prettier (%s), and Prettier's parse now accepts it: take it off the list",
				name,
				reason,
			)
		}
	}
	test.Logf(
		"%d vendored files (%d refused whole by Prettier's parse) gave %d distinct selectors; with %d inputs, "+
			"against %s: %d identical, %d refused by both with the same message, %d different; %d corpus "+
			"selectors refused",
		len(vendored),
		len(refusedFiles),
		corpusSelectors,
		len(inputs.Inputs),
		source,
		identical,
		refusedByBoth,
		different,
		corpusRefused,
	)
	if corpusSelectors == 0 {
		test.Errorf("the vendored fixtures gave no selectors")
	}
	if corpusRefused > corpusSelectors/10 {
		test.Errorf(
			"the library refused %d of %d corpus selectors; the comparison is thinner than it looks",
			corpusRefused,
			corpusSelectors,
		)
	}
}

// randomSelectorPieces are the fragments randomSelectors strings together: every character the tokenizer
// and parser branch on, plus words, escapes and non-ASCII text.
var randomSelectorPieces = []string{
	"a", "b", "div", "i", "n", "1", "-", "_", ".", "#", ":", "::", "(", ")", "[", "]", "=", "~=", "|=",
	"^=", "$=", "*=", "|", "*", "&", ",", " ", "  ", "\n", "\t", "\f", "\r\n", ">", "+", "~", "\\", "\\:",
	"\\ ", "'", "\"", "'x'", "\"y z\"", "/*", "*/", "/* c */", "/", "@", "@m", ";", "{", "}", "#{", "!", "%",
	"\xc3\xa9", "\xf0\x9f\x98\x80", "\xc2\xa0", ":not(", ":is(", ":nth-child(", " of ", "2n+1",
}

// randomSelectorCount is how many random selectors are compared: the first tenth of the 20,000 the old
// live test ran.
const randomSelectorCount = 2000

// randomSelectorsLeftOut are the places, counted from 0 among the distinct selectors randomSelectors draws,
// of the 33 the old live test left out because Go refuses them with ErrLoopsForever and the library never
// returns on them. That test found them by parsing each; they are named here so the inputs stay where the
// answers were recorded for them whatever the parser does, and the test holds Go to ErrLoopsForever on them.
var randomSelectorsLeftOut = []int{
	183, 214, 248, 313, 362, 514, 554, 641, 659, 736, 837, 880, 990, 1002, 1089, 1128, 1132, 1149, 1160, 1233,
	1261, 1319, 1398, 1427, 1455, 1469, 1585, 1615, 1773, 1947, 1973, 1991, 1995,
}

// randomSelectors are TestParseAgreesOnRandomSelectors' inputs, drawn as the old live test drew them, from
// its generator at its fixed seed, rand.NewPCG(2, 3): 1 to 10 of randomSelectorPieces strung together, each
// selector drawn before left out, and the first randomSelectorCount that remain, named by their place
// among them. math/rand/v2 pins the streams of a Rand over a seeded PCG in its own regression test, so every
// Go release draws the same. It returns the inputs and the selectors left out at randomSelectorsLeftOut.
func randomSelectors() ([]selectorInput, []string) {
	random := rand.New(rand.NewPCG(2, 3))
	leftOutPlaces := map[int]bool{}
	for _, place := range randomSelectorsLeftOut {
		leftOutPlaces[place] = true
	}
	seen := map[string]bool{}
	var inputs []selectorInput
	var leftOut []string
	for len(inputs) < randomSelectorCount {
		var text strings.Builder
		for range 1 + random.IntN(10) {
			text.WriteString(randomSelectorPieces[random.IntN(len(randomSelectorPieces))])
		}
		if seen[text.String()] {
			continue
		}
		place := len(seen)
		seen[text.String()] = true
		if leftOutPlaces[place] {
			leftOut = append(leftOut, text.String())
			continue
		}
		inputs = append(inputs, selectorInput{Name: strconv.Itoa(len(inputs)), Text: text.String()})
	}
	return inputs, leftOut
}

// TestParseAgreesOnRandomSelectors holds the parser to the library on random selectors, most of them
// malformed in some way: the same tree or the same error on each. They are the old live test's, drawn again
// by randomSelectors from its generator at its fixed seed (the generator is attached to #yah3d69 as
// generate-random-selectors.go too). A selector Go refuses with ErrLoopsForever was left out, since the
// library never returns on it, and each left out must still be refused so.
//
// The answers are recorded in random_answers_<n>.json, its parts, as TestParseAgreesWithPostcssSelectorParser's
// are, each with its selector's text, so a draw that moves fails as answers recorded for other texts.
func TestParseAgreesOnRandomSelectors(test *testing.T) {
	test.Parallel()
	inputs, leftOut := randomSelectors()
	if len(leftOut) != len(randomSelectorsLeftOut) {
		test.Fatalf("%d selectors were left out, and randomSelectorsLeftOut names %d", len(leftOut),
			len(randomSelectorsLeftOut))
	}
	for _, text := range leftOut {
		if _, parseError := Parse(text); !errors.Is(parseError, ErrLoopsForever) {
			test.Errorf(
				"%q is left out of the random selectors because Go refused it with ErrLoopsForever, and Go now gives "+
					"%v: the library never returns on it, so it cannot be recorded",
				text,
				parseError,
			)
		}
	}
	parts, partsError := recorded.Parts(libraryDirectory, "random_answers_")
	if partsError != nil {
		test.Fatal(partsError)
	}
	answers, source := readSelectorAnswers[selectorAnswer](test, selectorEntries(inputs), parts...)
	parsed, refused, different := 0, 0, 0
	messages := map[string]int{}
	for position, input := range inputs {
		libraryRefused, message, difference := compareSelector(
			input.Text,
			answers[position].Ast,
			answers[position].Error,
		)
		if difference != "" {
			different++
			if different <= 20 {
				test.Errorf("%s %q: %s", input.Name, input.Text, difference)
			}
			continue
		}
		if libraryRefused {
			refused++
			// Go's message, which has the library's fingerprint.
			messages[message]++
		} else {
			parsed++
		}
	}
	test.Logf(
		"%d random selectors against %s: %d parsed alike, %d refused alike, %d different",
		len(inputs),
		source,
		parsed,
		refused,
		different,
	)
	for message, times := range messages {
		test.Logf("  %5d refused with %q", times, message)
	}
	if parsed < len(inputs)/10 {
		test.Errorf("only %d of %d random selectors parsed; the comparison is mostly of refusals", parsed,
			len(inputs))
	}
}

// dumpSelector parses one selector and renders it the way the oracle script does, as the JSON value the
// script prints. sourceIndex stays in bytes; the library's side was converted when its answers were written
// as fingerprints (see selectorAnswer).
func dumpSelector(text string) (json_value.Value, error) {
	root, parseError := Parse(text)
	if parseError != nil {
		return nil, parseError
	}
	return dumpSelectorValue(root), nil
}

// dumpSelectorValue is one value of the tree as JSON. Every string on the finished tree is well-formed
// UTF-8 (see wellFormedTree), so it reads in JSON as it is. A float64 JSON cannot carry, which
// json.Marshal would refuse, becomes a description no oracle prints, so the comparison still fails on it.
func dumpSelectorValue(value estree.Value) json_value.Value {
	switch typed := value.(type) {
	case nil:
		return json_value.String("<undefined>")
	case *estree.Node:
		if typed.Range != [2]int{} {
			return json_value.String(fmt.Sprintf("a %s with a Range, which the parser never sets", typed.Type()))
		}
		result := json_value.Object{"type": json_value.String(typed.Type())}
		for _, key := range typed.Keys() {
			result[key] = dumpSelectorValue(typed.Get(key))
		}
		return result
	case estree.Nodes:
		if typed == nil {
			return json_value.String("a nil list, which no library field holds")
		}
		result := make(json_value.Array, len(typed))
		for index, child := range typed {
			result[index] = dumpSelectorValue(child)
		}
		return result
	case estree.Record:
		result := json_value.Object{}
		for key, each := range typed {
			result[key] = dumpSelectorValue(each)
		}
		return result
	case estree.Number:
		if math.IsNaN(float64(typed)) {
			return json_value.Null{} // NaN, which JSON.stringify writes as null
		}
		if math.IsInf(float64(typed), 0) {
			return json_value.String(fmt.Sprintf("a float64 of %v, which JSON cannot carry", float64(typed)))
		}
		return json_value.Number(strconv.FormatFloat(float64(typed), 'g', -1, 64))
	case estree.String:
		return json_value.String(typed)
	case estree.Boolean:
		return json_value.Boolean(typed)
	case estree.Integer:
		return json_value.Number(strconv.Itoa(int(typed)))
	default:
		return json_value.String(fmt.Sprintf("an unexpected %T", typed))
	}
}
