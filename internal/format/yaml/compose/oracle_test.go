package compose

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/system-inc/cohere/internal/format/yaml/cst"
	"github.com/system-inc/cohere/internal/format/yaml/upstream_testing"
)

// The composer oracle: the fork's own eemeli/yaml, 2.9.0, parsing each input with
// `new Parser(lineCounter.addNewLine).parse(text)` and composing the tokens with
// `new Composer({ keepSourceTokens: true, uniqueKeys: false, lineCounter, merge: true })
// .compose(tokens, true, text.length)`, exactly as yaml-unist-parser does. Every document is serialized
// whole, node by node at every depth: the node's kind and class, range, srcToken, anchor, tag, flow,
// scalar type, format, minFractionDigits, source and value, comments, spaceBefore, items, and a pair's
// key and value; the document's contents, range, comments, directives, and every error and warning with
// its name, code, message and position. The Go port composes the same text and serializes to the same
// JSON, field for field in the same order, and the two strings must be identical.
//
// A srcToken is compared by identity: both sides number every token and collection item of the CST in
// the same depth-first order, and a node's srcToken is written as that number with its type and offset.
// A number is written as its IEEE 754 bits, so -0, the infinities and every last bit compare exactly.
//
// Upstream's strings are recorded (see upstream_testing), so no node runs here: each answers file under
// testdata/eemeli_yaml holds, per input, the text it was recorded for and the string upstream wrote.

// eemeliYamlDirectory holds the inputs and upstream's recorded answers for them.
var eemeliYamlDirectory = filepath.Join("testdata", "eemeli_yaml")

// suiteDirectory holds the yaml-test-suite's cases, shared with the other YAML packages.
var suiteDirectory = filepath.Join("..", "testdata", "yaml_test_suite")

// composeAnswer is one recorded answer: the input's name, its text or, for a vendored input, the text's
// digest, and the string upstream's serializer wrote for it, or "throw " and the error when composing threw.
type composeAnswer struct {
	Name   string `json:"name"`
	Text   string `json:"text"`
	Sha256 string `json:"sha256"`
	Output string `json:"output"`
}

// recordedOutputs reads an answers file and pairs it with the inputs, returning upstream's string for each
// input, in the inputs' order, and the file's source and date.
func recordedOutputs(test *testing.T, answersFile string, inputs []upstream_testing.Input) ([]string, string) {
	test.Helper()
	path := filepath.Join(eemeliYamlDirectory, answersFile)
	answers := upstream_testing.ReadAnswers[composeAnswer](test, path)
	byName := upstream_testing.Match(test, path, inputs, answers.Answers, func(answer composeAnswer) (string, string) {
		return answer.Name, upstream_testing.RecordedFor(answer.Text, answer.Sha256)
	})
	outputs := make([]string, len(inputs))
	for index, input := range inputs {
		outputs[index] = byName[input.Name].Output
	}
	return outputs, answers.Source + ", recorded " + answers.Recorded
}

// composeText parses and composes text as yaml-unist-parser does, returning the tokens too.
func composeText(text string) ([]*cst.Token, []*Document) {
	units := utf16.Encode([]rune(text))
	lineCounter := cst.NewLineCounter()
	tokens := slices.Collect(cst.NewParser(lineCounter.AddNewLine).Parse(units, false))
	documents := slices.Collect(NewComposer(UnistParserOptions()).Compose(tokens, true, len(units)))
	return tokens, documents
}

// portOutput composes like the oracle and serializes like it.
func portOutput(text string) (output string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			output = fmt.Sprintf("throw %v", recovered)
		}
	}()
	tokens, documents := composeText(text)
	writer := &jsonWriter{
		tokenIdentifiers: map[*cst.Token]int{},
		itemIdentifiers:  map[*cst.CollectionItem]int{},
	}
	for _, token := range tokens {
		writer.number(token)
	}
	writer.builder.WriteByte('[')
	for index, document := range documents {
		if index > 0 {
			writer.builder.WriteByte(',')
		}
		writer.document(document)
	}
	writer.builder.WriteByte(']')
	return writer.builder.String()
}

// jsonWriter numbers tokens and collection items in one sequence, as the oracle's one Map does: each one's
// number is the count of both maps before it.
type jsonWriter struct {
	builder          strings.Builder
	tokenIdentifiers map[*cst.Token]int
	itemIdentifiers  map[*cst.CollectionItem]int
}

// nextIdentifier is the oracle's `ids.size`.
func (writer *jsonWriter) nextIdentifier() int {
	return len(writer.tokenIdentifiers) + len(writer.itemIdentifiers)
}

// number numbers a token and its descendants in the oracle's order: start, value, items (each item,
// then its start, key, sep and value), props, end.
func (writer *jsonWriter) number(token *cst.Token) {
	if token == nil {
		return
	}
	writer.tokenIdentifiers[token] = writer.nextIdentifier()
	for _, each := range token.Start {
		writer.number(each)
	}
	writer.number(token.FlowStart)
	writer.number(token.Value)
	for _, item := range token.Items {
		writer.itemIdentifiers[item] = writer.nextIdentifier()
		for _, each := range item.Start {
			writer.number(each)
		}
		writer.number(item.Key)
		for _, each := range item.Sep {
			writer.number(each)
		}
		writer.number(item.Value)
	}
	for _, each := range token.Properties {
		writer.number(each)
	}
	for _, each := range token.End {
		writer.number(each)
	}
}

func (writer *jsonWriter) raw(text string) { writer.builder.WriteString(text) }

// key writes a field's name, with the comma every field but an object's first is preceded by.
//
// Mutates: first the object's own flag, cleared at its first key so every later key writes its comma
func (writer *jsonWriter) key(first *bool, name string) {
	if !*first {
		writer.builder.WriteByte(',')
	}
	*first = false
	writer.builder.WriteString(`"` + name + `":`)
}

func (writer *jsonWriter) text(text string) { writeJsonString(&writer.builder, stringToUnits(text)) }

func (writer *jsonWriter) ints(values []int) {
	if values == nil {
		writer.raw("null")
		return
	}
	writer.builder.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			writer.builder.WriteByte(',')
		}
		writer.raw(strconv.Itoa(value))
	}
	writer.builder.WriteByte(']')
}

func numberBits(value float64) string {
	if math.IsNaN(value) {
		return `"NaN"`
	}
	return `"` + strconv.FormatUint(math.Float64bits(value), 16) + `"`
}

func (writer *jsonWriter) value(value ScalarValue) {
	switch typed := value.(type) {
	case nil:
		writer.raw("null")
	case ScalarString:
		writer.text(string(typed))
	case ScalarBoolean:
		writer.raw(strconv.FormatBool(bool(typed)))
	case ScalarNumber:
		writer.raw(`{"number":` + numberBits(float64(typed)) + `}`)
	case *MergeKey:
		writer.raw(`{"symbol":"<<"}`)
	case Date:
		writer.raw(`{"date":` + numberBits(float64(typed)) + `}`)
	case Binary:
		writer.raw(fmt.Sprintf(`{"bytes":"%x"}`, []byte(typed)))
	default:
		writer.raw(fmt.Sprintf(`{"other":"%T"}`, typed))
	}
}

func (writer *jsonWriter) sourceToken(token *cst.Token) {
	id, found := writer.tokenIdentifiers[token]
	if !found {
		writer.text(fmt.Sprintf("foreign %s@%d", token.Type, token.Offset))
		return
	}
	writer.text(fmt.Sprintf("#%d %s@%d", id, token.Type, token.Offset))
}

func (writer *jsonWriter) node(node *Node) {
	if node == nil {
		writer.raw("null")
		return
	}
	first := true
	writer.builder.WriteByte('{')
	writer.key(&first, "kind")
	writer.text(string(node.Kind))
	writer.key(&first, "class")
	writer.text(node.Class)
	if node.Range != nil {
		writer.key(&first, "range")
		writer.ints(node.Range)
	}
	if node.SourceToken != nil {
		writer.key(&first, "srcToken")
		writer.sourceToken(node.SourceToken)
	} else if node.SourceItem != nil {
		writer.key(&first, "srcToken")
		if id, found := writer.itemIdentifiers[node.SourceItem]; found {
			writer.text(fmt.Sprintf("#%d item", id))
		} else {
			writer.text("foreign item")
		}
	}
	if node.HasAnchor() {
		writer.key(&first, "anchor")
		writer.text(node.Anchor)
	}
	if node.Tag != "" {
		writer.key(&first, "tag")
		writer.text(node.Tag)
	}
	if node.Flow {
		writer.key(&first, "flow")
		writer.raw("true")
	}
	if node.Type != "" {
		writer.key(&first, "type")
		writer.text(node.Type)
	}
	if node.Format != "" {
		writer.key(&first, "format")
		writer.text(node.Format)
	}
	if node.MinFractionDigits != 0 {
		writer.key(&first, "minFractionDigits")
		writer.raw(strconv.Itoa(node.MinFractionDigits))
	}
	if node.HasSource() {
		writer.key(&first, "source")
		writer.text(node.Source)
	}
	if node.Kind == KindScalar {
		writer.key(&first, "value")
		writer.value(node.ScalarValue)
	}
	if node.Comment != "" {
		writer.key(&first, "comment")
		writer.text(node.Comment)
	}
	if node.CommentBefore != "" {
		writer.key(&first, "commentBefore")
		writer.text(node.CommentBefore)
	}
	if node.SpaceBefore {
		writer.key(&first, "spaceBefore")
		writer.raw("true")
	}
	if node.Kind == KindMap || node.Kind == KindSequence {
		writer.key(&first, "items")
		writer.builder.WriteByte('[')
		for index, item := range node.Items {
			if index > 0 {
				writer.builder.WriteByte(',')
			}
			writer.node(item)
		}
		writer.builder.WriteByte(']')
	}
	if node.Kind == KindPair {
		writer.key(&first, "key")
		writer.node(node.Key)
		writer.key(&first, "value")
		writer.node(node.Value)
	}
	writer.builder.WriteByte('}')
}

func (writer *jsonWriter) nullableText(text string) {
	if text == "" {
		writer.raw("null")
		return
	}
	writer.text(text)
}

func (writer *jsonWriter) errors(errors []*YamlError) {
	writer.builder.WriteByte('[')
	for index, yamlError := range errors {
		if index > 0 {
			writer.builder.WriteByte(',')
		}
		first := true
		writer.builder.WriteByte('{')
		writer.key(&first, "name")
		writer.text(yamlError.Name)
		writer.key(&first, "code")
		writer.text(yamlError.Code)
		writer.key(&first, "message")
		writer.text(yamlError.Message)
		writer.key(&first, "pos")
		writer.ints(yamlError.Position[:])
		writer.builder.WriteByte('}')
	}
	writer.builder.WriteByte(']')
}

func (writer *jsonWriter) document(document *Document) {
	first := true
	writer.builder.WriteByte('{')
	writer.key(&first, "contents")
	writer.node(document.Contents)
	writer.key(&first, "range")
	writer.ints(document.Range)
	writer.key(&first, "comment")
	writer.nullableText(document.Comment)
	writer.key(&first, "commentBefore")
	writer.nullableText(document.CommentBefore)
	writer.key(&first, "directives")
	directives := document.Directives
	inner := true
	writer.builder.WriteByte('{')
	writer.key(&inner, "docStart")
	if directives.DocStart {
		writer.raw("true")
	} else {
		writer.raw("null")
	}
	writer.key(&inner, "docEnd")
	writer.raw(strconv.FormatBool(directives.DocEnd))
	writer.key(&inner, "yaml")
	writer.raw(`{"explicit":` + strconv.FormatBool(directives.Yaml.Explicit) + `,"version":`)
	writer.text(directives.Yaml.Version)
	writer.raw("}")
	writer.key(&inner, "tags")
	handles := make([]string, 0, len(directives.Tags))
	for handle := range directives.Tags {
		handles = append(handles, handle)
	}
	slices.SortFunc(handles, func(left string, right string) int {
		return slices.Compare(stringToUnits(left), stringToUnits(right))
	})
	writer.builder.WriteByte('[')
	for index, handle := range handles {
		if index > 0 {
			writer.builder.WriteByte(',')
		}
		writer.builder.WriteByte('[')
		writer.text(handle)
		writer.builder.WriteByte(',')
		writer.text(directives.Tags[handle])
		writer.builder.WriteByte(']')
	}
	writer.builder.WriteByte(']')
	writer.builder.WriteByte('}')
	writer.key(&first, "errors")
	writer.errors(document.Errors)
	writer.key(&first, "warnings")
	writer.errors(document.Warnings)
	writer.builder.WriteByte('}')
}

// compareOutputs reports each input whose port output differs from upstream's recorded one, up to limit
// of them.
func compareOutputs(
	test *testing.T,
	label string,
	answersFile string,
	inputs []upstream_testing.Input,
	limit int,
) (failures int) {
	test.Helper()
	expected, source := recordedOutputs(test, answersFile, inputs)
	threw, withErrors, withWarnings := 0, 0, 0
	for index, input := range inputs {
		actual := portOutput(input.Text)
		if actual != expected[index] {
			failures++
			if failures <= limit {
				test.Errorf("%s %s %q: %s", label, input.Name, truncateInput(input.Text),
					firstDifference(expected[index], actual))
			}
		}
		switch {
		case strings.HasPrefix(expected[index], "throw"):
			threw++
		case strings.Contains(expected[index], `"name":"YAMLParseError"`):
			withErrors++
		}
		if strings.Contains(expected[index], `"name":"YAMLWarning"`) {
			withWarnings++
		}
	}
	test.Logf("%s: of %d inputs, upstream (%s) threw on %d, reported errors on %d and warnings on %d",
		label, len(inputs), source, threw, withErrors, withWarnings)
	return failures
}

func firstDifference(expected string, actual string) string {
	index := 0
	for index < len(expected) && index < len(actual) && expected[index] == actual[index] {
		index++
	}
	from := max(index-160, 0)
	return fmt.Sprintf("differs at byte %d\n  want …%s\n  got  …%s", index,
		expected[from:min(index+120, len(expected))], actual[from:min(index+120, len(actual))])
}

func truncateInput(input string) string {
	if len(input) > 80 {
		return input[:80] + "…"
	}
	return input
}

// TestDocumentsMatchUpstream runs every fixture through both sides: the hand-written fixtures of the
// deleted fixtures_test.go, grouped there by the compose or resolve function they mostly drive.
func TestDocumentsMatchUpstream(test *testing.T) {
	test.Parallel()
	inputs := upstream_testing.ReadInputs(test, filepath.Join(eemeliYamlDirectory, "fixture_inputs.json"))
	failures := compareOutputs(test, "fixture", "fixture_answers.json.gz", inputs, 60)
	test.Logf("%d of %d fixtures match upstream", len(inputs)-failures, len(inputs))
}

// TestGeneratedDocumentsMatchUpstream compares inputs drawn from YAML's indicators, properties, scalar
// styles, spaces, tabs and line breaks, where the hand-written fixtures cannot reach every combination:
// one fixed seed's first 500, the slice that keeps the answers under 50 KB.
func TestGeneratedDocumentsMatchUpstream(test *testing.T) {
	test.Parallel()
	inputs := upstream_testing.ReadInputs(test, filepath.Join(eemeliYamlDirectory, "generated_inputs.json"))
	failures := compareOutputs(test, "generated", "generated_answers.json.gz", inputs, 40)
	test.Logf("%d of %d generated inputs match upstream", len(inputs)-failures, len(inputs))
}

// TestOracleCanFail proves the comparison sees a difference: documents composed from one input compared
// against upstream's for another must differ.
func TestOracleCanFail(test *testing.T) {
	test.Parallel()
	inputs := upstream_testing.ReadInputs(test, filepath.Join(eemeliYamlDirectory, "fixture_inputs.json"))
	outputs, _ := recordedOutputs(test, "fixture_answers.json.gz", inputs)
	byText := map[string]string{}
	for index, input := range inputs {
		byText[input.Text] = outputs[index]
	}
	for _, each := range []struct{ mine, theirs, what string }{
		{"a:  b", "a: b", "a: b and a:  b"},
		{"a: 1.0", "a: 1", "the values of a: 1 and a: 1.0"},
		{"[a ]", "[a]", "[a] and [a ]"},
	} {
		expected, recorded := byText[each.theirs]
		if !recorded {
			test.Fatalf("fixture_inputs.json no longer holds %q", each.theirs)
		}
		if portOutput(each.mine) == expected {
			test.Errorf("the comparison found no difference between %s", each.what)
		}
	}
}

// TestYamlTestSuiteMatchesUpstream compares every case of the yaml-test-suite, part by part.
func TestYamlTestSuiteMatchesUpstream(test *testing.T) {
	test.Parallel()
	compared, failures := 0, 0
	for part := 1; part <= upstream_testing.SuitePartCount; part++ {
		inputs := upstream_testing.SuiteInputs(upstream_testing.ReadSuitePart(test, suiteDirectory, part), false)
		failures += compareOutputs(
			test,
			fmt.Sprintf("suite part %d", part),
			fmt.Sprintf("suite_answers_%d.json.gz", part),
			inputs,
			40,
		)
		compared += len(inputs)
	}
	test.Logf("%d of %d yaml-test-suite cases match upstream", compared-failures, compared)
}

// corpusDirectory holds the corpus, Prettier's own format fixtures, shared with the other YAML packages.
var corpusDirectory = filepath.Join("..", "testdata", "prettier_tests")

// TestCorpusMatchesUpstream compares every YAML file of the corpus: Prettier's own YAML format fixtures
// but its spec examples, which the yaml-test-suite holds, half by half.
func TestCorpusMatchesUpstream(test *testing.T) {
	test.Parallel()
	compared, failures := 0, 0
	for half := 1; half <= upstream_testing.CorpusHalfCount; half++ {
		inputs := upstream_testing.ReadCorpusHalf(test, corpusDirectory, half)
		failures += compareOutputs(
			test,
			fmt.Sprintf("corpus half %d", half),
			fmt.Sprintf("corpus_answers_%d.json.gz", half),
			inputs,
			40,
		)
		compared += len(inputs)
	}
	test.Logf("%d of %d files match upstream's documents", compared-failures, compared)
}
