package selector

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"unicode/utf16"
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

// selectorInputs is an inputs file: each selector's name, its text, and malformed, which says the library
// refuses it, so a recording that refuses a valid fixture, or accepts a malformed one, fails the test
// rather than quietly agreeing. The random inputs carry no malformed; their test holds them to nothing but
// agreement.
type selectorInputs struct {
	About  string `json:"about"`
	Inputs []struct {
		Name      string `json:"name"`
		Text      string `json:"text"`
		Malformed bool   `json:"malformed"`
	} `json:"inputs"`
}

// selectorAnswer is the library's answer for one selector: its tree, raw JSON until it is compared, or the
// message it refused with.
type selectorAnswer struct {
	Name  string          `json:"name"`
	Text  string          `json:"text"`
	Ast   json.RawMessage `json:"ast"`
	Error *string         `json:"error"`
}

// selectorAnswers is an answers file for an inputs file.
type selectorAnswers struct {
	Source   string           `json:"source"`
	Recorded string           `json:"recorded"`
	Answers  []selectorAnswer `json:"answers"`
}

// vendoredAnswers is vendored_answers.json.gz: for each vendored stylesheet, by its path there and its
// digest, the selectors Prettier's parse hands parseSelector with the library's answer for each (a
// selector an earlier file already gave is left out), or the reason Prettier's parse refuses the file.
type vendoredAnswers struct {
	Source   string `json:"source"`
	Recorded string `json:"recorded"`
	Answers  []struct {
		Name      string           `json:"name"`
		Sha256    string           `json:"sha256"`
		CssError  *string          `json:"cssError"`
		Selectors []selectorAnswer `json:"selectors"`
	} `json:"answers"`
}

// readSelectorComparison reads an inputs file and its answers and returns them paired, input by input,
// with the answers' source. An input with no answer, or one recorded for another text, fails the test.
func readSelectorComparison(test *testing.T, inputsName string, answersName string) (
	selectorInputs,
	[]selectorAnswer,
	string,
) {
	test.Helper()
	inputs, readInputsError := recorded.ReadJson[selectorInputs](filepath.Join(libraryDirectory, inputsName))
	if readInputsError != nil {
		test.Fatal(readInputsError)
	}
	if len(inputs.Inputs) == 0 {
		test.Fatalf("%s holds no inputs", inputsName)
	}
	answersPath := filepath.Join(libraryDirectory, answersName)
	answers, readAnswersError := recorded.ReadJson[selectorAnswers](answersPath)
	if readAnswersError != nil {
		test.Fatal(readAnswersError)
	}
	inputEntries := make([]recorded.Entry, len(inputs.Inputs))
	for index, input := range inputs.Inputs {
		inputEntries[index] = recorded.Entry{Name: input.Name, Input: input.Text}
	}
	answerEntries := make([]recorded.Entry, len(answers.Answers))
	for index, answer := range answers.Answers {
		answerEntries[index] = recorded.Entry{Name: answer.Name, Input: answer.Text}
	}
	indexes, matchError := recorded.Match(inputEntries, answerEntries)
	if matchError != nil {
		test.Fatalf("%s: %v: record them again with %s", answersPath, matchError, recorder)
	}
	paired := make([]selectorAnswer, len(indexes))
	for position, index := range indexes {
		paired[position] = answers.Answers[index]
	}
	return inputs, paired, answers.Source + ", recorded " + answers.Recorded
}

// compareSelector parses text and compares it with the library's answer: the same tree, property for
// property with sourceIndex in bytes, or the same refusal. It says whether the library refused, and any
// difference.
func compareSelector(text string, want selectorAnswer) (bool, string) {
	got, dumpSelectorError := dumpSelector(text)
	switch {
	case want.Error != nil && dumpSelectorError != nil:
		if dumpSelectorError.Error() != *want.Error {
			return true, fmt.Sprintf(
				"both refuse, with different messages:\n  library %s\n  Go      %s",
				*want.Error,
				dumpSelectorError.Error(),
			)
		}
		return true, ""
	case want.Error != nil:
		return true, fmt.Sprintf("the library refuses it (%s) and Go parsed it", *want.Error)
	case dumpSelectorError != nil:
		return false, fmt.Sprintf("the library parses it and Go refused: %v", dumpSelectorError)
	}
	wantTree, treeError := json_value.Parse(want.Ast)
	if treeError != nil {
		return false, fmt.Sprintf("the library's tree does not read as JSON: %v", treeError)
	}
	return false, firstSelectorDifference(oracleInBytes(wantTree, text), got, "root")
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
// sorted, "<undefined>" against absent; offsets are UTF-16 there, and oracleInBytes converts them.
//
// A stylesheet Prettier itself refuses gives no selectors to compare. The vendored ones that do are named
// in prettierRefusedFixtures, and a vendored stylesheet refused but not named there, or named there and
// parsed, fails the test. A corpus selector the library cannot parse is counted, and the test fails if that
// count passes a tenth of them: a recording that rejects everything would otherwise read as agreement.
//
// To add or change an input, or after the vendored fixtures change, record the answers again with
// record-postcss-selector-parser.mjs, attached to #yah3d69, its usage in its header.
func TestParseAgreesWithPostcssSelectorParser(test *testing.T) {
	test.Parallel()
	inputs, answers, source := readSelectorComparison(test, "parse_inputs.json", "parse_answers.json.gz")

	identical, refusedByBoth, different := 0, 0, 0
	fail := func(format string, arguments ...any) {
		different++
		if different <= 20 {
			test.Errorf(format, arguments...)
		}
	}
	tally := func(name string, text string, want selectorAnswer) bool {
		refused, difference := compareSelector(text, want)
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
		if input.Malformed != (want.Error != nil) {
			fail("%s: the input says malformed=%v and the library says error %v, tree %s", input.Name,
				input.Malformed, want.Error, want.Ast)
			continue
		}
		tally(input.Name, input.Text, want)
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
	vendoredPath := filepath.Join(libraryDirectory, "vendored_answers.json.gz")
	vendored, readVendoredError := recorded.ReadJson[vendoredAnswers](vendoredPath)
	if readVendoredError != nil {
		test.Fatal(readVendoredError)
	}
	answerEntries := make([]recorded.Entry, len(vendored.Answers))
	for index, answer := range vendored.Answers {
		answerEntries[index] = recorded.Entry{Name: answer.Name, Input: answer.Sha256}
	}
	if _, matchError := recorded.Match(vendoredEntries, answerEntries); matchError != nil {
		test.Fatalf("%s: %v: record them again with %s", vendoredPath, matchError, recorder)
	}

	corpusSelectors, corpusRefused := 0, 0
	refusedFiles := map[string]string{}
	for _, file := range vendored.Answers {
		if file.CssError != nil {
			refusedFiles[file.Name] = *file.CssError
			continue
		}
		for _, want := range file.Selectors {
			corpusSelectors++
			if tally(file.Name, want.Text, want) {
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
		len(vendored.Answers),
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

// TestParseAgreesOnRandomSelectors holds the parser to the library on random selectors, most of them
// malformed in some way: the same tree or the same error on each. They are the old live test's, from its
// generator at its fixed seed, rand.NewPCG(2, 3), the first 2,000 of the 20,000 it ran (random_inputs.json.gz
// says how they were made; the generator is attached to #yah3d69 as generate-random-selectors.go). A
// selector Go refuses with ErrLoopsForever was left out, since the library never returns on it.
//
// The answers are recorded in random_answers.json.gz, as TestParseAgreesWithPostcssSelectorParser's are.
func TestParseAgreesOnRandomSelectors(test *testing.T) {
	test.Parallel()
	inputs, answers, source := readSelectorComparison(test, "random_inputs.json.gz", "random_answers.json.gz")
	parsed, refused, different := 0, 0, 0
	messages := map[string]int{}
	for position, input := range inputs.Inputs {
		libraryRefused, difference := compareSelector(input.Text, answers[position])
		if difference != "" {
			different++
			if different <= 20 {
				test.Errorf("%q: %s", input.Text, difference)
			}
			continue
		}
		if libraryRefused {
			refused++
			messages[*answers[position].Error]++
		} else {
			parsed++
		}
	}
	test.Logf(
		"%d random selectors against %s: %d parsed alike, %d refused alike, %d different",
		len(inputs.Inputs),
		source,
		parsed,
		refused,
		different,
	)
	for message, times := range messages {
		test.Logf("  %5d refused with %q", times, message)
	}
	if parsed < len(inputs.Inputs)/10 {
		test.Errorf("only %d of %d random selectors parsed; the comparison is mostly of refusals", parsed,
			len(inputs.Inputs))
	}
}

// dumpSelector parses one selector and renders it the way the oracle script does, as the JSON value the
// script prints. sourceIndex stays in bytes; oracleInBytes converts the library's side.
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

// oracleInBytes converts every sourceIndex in the library's dump from a UTF-16 index into text to the
// byte offset Parse promises: the start of the character the index falls in (the middle of a surrogate
// pair included, which splitWord can produce), and past the end by the same distance. It is written
// apart from Parse's conversion so that a bug there cannot hide here.
func oracleInBytes(value json_value.Value, text string) json_value.Value {
	switch typed := value.(type) {
	case json_value.Object:
		for key, each := range typed {
			if number, isNumber := each.(json_value.Number); key == "sourceIndex" && isNumber {
				if index, parseError := json.Number(number).Float64(); parseError == nil {
					typed[key] = json_value.Number(strconv.Itoa(utf16IndexToByte(text, int(index))))
					continue
				}
			}
			typed[key] = oracleInBytes(each, text)
		}
	case json_value.Array:
		for index, each := range typed {
			typed[index] = oracleInBytes(each, text)
		}
	}
	return value
}

func utf16IndexToByte(text string, index int) int {
	units := 0
	for position, character := range text {
		width := len(utf16.Encode([]rune{character}))
		if index < units+width {
			return position
		}
		units += width
	}
	return len(text) + index - units
}

// firstSelectorDifference names the first path where two JSON values differ, or "".
func firstSelectorDifference(want json_value.Value, got json_value.Value, path string) string {
	switch typed := want.(type) {
	case json_value.Object:
		other, isMap := got.(json_value.Object)
		if !isMap {
			return fmt.Sprintf("%s: want an object, got %v", path, got)
		}
		keys := map[string]bool{}
		for key := range typed {
			keys[key] = true
		}
		for key := range other {
			keys[key] = true
		}
		sorted := make([]string, 0, len(keys))
		for key := range keys {
			sorted = append(sorted, key)
		}
		sort.Strings(sorted)
		for _, key := range sorted {
			wantValue, wantPresent := typed[key]
			gotValue, gotPresent := other[key]
			if wantPresent != gotPresent {
				return fmt.Sprintf(
					"%s.%s: present in the library %v, in Go %v (%v)",
					path,
					key,
					wantPresent,
					gotPresent,
					typed["type"],
				)
			}
			if difference := firstSelectorDifference(wantValue, gotValue, path+"."+key); difference != "" {
				return difference
			}
		}
		return ""
	case json_value.Array:
		other, isList := got.(json_value.Array)
		if !isList || len(other) != len(typed) {
			return fmt.Sprintf("%s: want %d elements, got %v", path, len(typed), got)
		}
		for index := range typed {
			if difference := firstSelectorDifference(
				typed[index],
				other[index],
				fmt.Sprintf("%s[%d]", path, index),
			); difference != "" {
				return difference
			}
		}
		return ""
	}
	if jsonLeafText(want) != jsonLeafText(got) {
		return fmt.Sprintf("%s: want %s, got %s", path, jsonLeafText(want), jsonLeafText(got))
	}
	return ""
}

// jsonLeafText is a JSON value as Go's untyped decode would hold it, its type and its value, so two
// leaves are equal exactly when they decode to the same thing: a number is read as a float64, so 1
// and 1.0 are one number, as they are in JavaScript.
func jsonLeafText(value json_value.Value) string {
	switch typed := value.(type) {
	case json_value.Number:
		number, parseError := json.Number(typed).Float64()
		if parseError != nil {
			return fmt.Sprintf("a number that does not read as a float64, %q", string(typed))
		}
		return fmt.Sprintf("float64 %v", number)
	case json_value.String:
		return fmt.Sprintf("string %q", string(typed))
	case json_value.Boolean:
		return fmt.Sprintf("bool %v", bool(typed))
	case json_value.Null:
		return "null"
	}
	return fmt.Sprintf("%T %v", value, value)
}
