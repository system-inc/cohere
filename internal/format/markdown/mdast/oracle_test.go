package mdast

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/system-inc/cohere/internal/format/oracle_testing"
)

// The tree oracle: the fork's markdown bundle's own parser, the exact code the format oracle ran. Every node of
// the Go tree must carry the same type, the same fields with the same values (null distinct from absent), and
// the same positions, offsets converted from UTF-16 to bytes.
//
// The trees are recorded (see oracle_testing) as the JSON text upstream's parser gave:
// prettierPlugins.markdown.parsers.markdown.parse on the text, serialized by JSON.stringify with every `data`
// key (hast hints for HTML output) dropped.

type treeOracle struct {
	golden *oracle_testing.Golden
}

// newTreeOracle answers from the test's golden.
func newTreeOracle(test *testing.T) *treeOracle {
	test.Helper()
	return &treeOracle{golden: oracle_testing.Open(test, test.Name())}
}

// treeValue is one value of a tree serialized as JSON, upstream's or the port's: an object, a list, a
// string, a number, a boolean or null. Sealed by isTreeValue, so a comparison's type switch sees every kind.
type treeValue interface{ isTreeValue() }

type (
	treeObject  map[string]treeValue
	treeList    []treeValue
	treeString  string
	treeNumber  float64
	treeBoolean bool
	treeNull    struct{}
)

func (treeObject) isTreeValue()  {}
func (treeList) isTreeValue()    {}
func (treeString) isTreeValue()  {}
func (treeNumber) isTreeValue()  {}
func (treeBoolean) isTreeValue() {}
func (treeNull) isTreeValue()    {}

// decodeTree reads serialized JSON into a treeValue, as json.Unmarshal into an interface would: numbers as
// float64, strings with invalid UTF-8 replaced, and the last of two equal keys kept.
func decodeTree(serialized []byte) (treeValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(serialized))
	tree, decodeError := decodeTreeValue(decoder)
	if decodeError != nil {
		return nil, decodeError
	}
	if decoder.More() {
		return nil, fmt.Errorf("the tree's JSON holds more than one value")
	}
	return tree, nil
}

// decodeTreeValue reads the next JSON value from decoder.
func decodeTreeValue(decoder *json.Decoder) (treeValue, error) {
	token, tokenError := decoder.Token()
	if tokenError != nil {
		return nil, tokenError
	}
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			object := treeObject{}
			for decoder.More() {
				keyToken, keyError := decoder.Token()
				if keyError != nil {
					return nil, keyError
				}
				key, isKey := keyToken.(string)
				if !isKey {
					return nil, fmt.Errorf("an object key is a %T, not a string", keyToken)
				}
				value, valueError := decodeTreeValue(decoder)
				if valueError != nil {
					return nil, valueError
				}
				object[key] = value
			}
			if _, closeError := decoder.Token(); closeError != nil {
				return nil, closeError
			}
			return object, nil
		case '[':
			list := treeList{}
			for decoder.More() {
				value, valueError := decodeTreeValue(decoder)
				if valueError != nil {
					return nil, valueError
				}
				list = append(list, value)
			}
			if _, closeError := decoder.Token(); closeError != nil {
				return nil, closeError
			}
			return list, nil
		}
		return nil, fmt.Errorf("a value opens with %v", typed)
	case string:
		return treeString(typed), nil
	case float64:
		return treeNumber(typed), nil
	case bool:
		return treeBoolean(typed), nil
	case nil:
		return treeNull{}, nil
	}
	return nil, fmt.Errorf("a JSON token of type %T", token)
}

// parse returns upstream's tree, with `data` (hast hints for HTML output) dropped.
func (oracle *treeOracle) parse(text string) (treeValue, error) {
	serialized, serializeError := oracle.golden.Answer(oracle_testing.Key("tree", text))
	if serializeError != nil {
		return nil, serializeError
	}
	return decodeTree([]byte(serialized))
}

// convertOffsets rewrites every position offset in upstream's tree from UTF-16 units to bytes.
//
// Mutates: tree its offsets are rewritten in place, so the comparison reads the same tree in bytes.
func convertOffsets(tree treeValue, offsets []int) {
	switch typed := tree.(type) {
	case treeObject:
		for key, value := range typed {
			if key == "offset" {
				if number, isNumber := value.(treeNumber); isNumber {
					typed[key] = treeNumber(offsets[int(number)])
				}
				continue
			}
			convertOffsets(value, offsets)
		}
	case treeList:
		for _, value := range typed {
			convertOffsets(value, offsets)
		}
	}
}

// describe renders a Go node with exactly the keys upstream's node of that type has. A null is a nil
// treeValue, which json.Marshal writes as null and describeThroughJson reads back as a treeNull.
func describe(node *Node) treeObject {
	description := treeObject{"type": treeString(node.NodeType)}
	if node.Position != nil {
		description["position"] = treeObject{
			"start": describePoint(node.Position.Start),
			"end":   describePoint(node.Position.End),
		}
	}
	if node.IsParent {
		children := make(treeList, len(node.Children))
		for index, child := range node.Children {
			children[index] = describe(child)
		}
		description["children"] = children
	}
	if node.IsLiteral {
		if node.ValueNull {
			description["value"] = nil
		} else {
			description["value"] = treeString(node.Value)
		}
	}
	optional := func(key string, value *string) {
		if value == nil {
			description[key] = nil
		} else {
			description[key] = treeString(*value)
		}
	}
	switch node.NodeType {
	case "heading":
		description["depth"] = treeNumber(node.Depth)
	case "code":
		optional("lang", node.Lang)
		optional("meta", node.Meta)
	case "math":
		optional("meta", node.Meta)
	case "definition":
		description["identifier"] = treeString(node.Identifier)
		optional("label", node.Label)
		optional("title", node.Title)
		description["url"] = treeString(node.Url)
	case "link":
		optional("title", node.Title)
		description["url"] = treeString(node.Url)
	case "image":
		optional("title", node.Title)
		description["url"] = treeString(node.Url)
		optional("alt", node.Alt)
	case "linkReference":
		description["identifier"] = treeString(node.Identifier)
		optional("label", node.Label)
		description["referenceType"] = treeString(node.ReferenceType)
	case "imageReference":
		optional("alt", node.Alt)
		description["identifier"] = treeString(node.Identifier)
		optional("label", node.Label)
		description["referenceType"] = treeString(node.ReferenceType)
	case "list":
		description["ordered"] = treeBoolean(node.Ordered)
		if node.Start == nil {
			description["start"] = nil
		} else {
			description["start"] = treeNumber(*node.Start)
		}
		description["spread"] = treeBoolean(node.Spread)
	case "listItem":
		description["spread"] = treeBoolean(node.Spread)
		if node.Checked == nil {
			description["checked"] = nil
		} else {
			description["checked"] = treeBoolean(*node.Checked)
		}
	case "footnoteReference", "footnoteDefinition":
		description["identifier"] = treeString(node.Identifier)
		optional("label", node.Label)
	case "table":
		align := make(treeList, len(node.Align))
		for index, value := range node.Align {
			if value != "" {
				align[index] = treeString(value)
			}
		}
		description["align"] = align
	case "frontMatter":
		frontMatter := node.FrontMatter
		description["language"] = treeString(frontMatter.Language)
		optional("explicitLanguage", frontMatter.ExplicitLanguage)
		description["value"] = treeString(frontMatter.Value)
		description["startDelimiter"] = treeString(frontMatter.StartDelimiter)
		description["endDelimiter"] = treeString(frontMatter.EndDelimiter)
		description["raw"] = treeString(frontMatter.Raw)
	}
	return description
}

func describePoint(point Point) treeObject {
	return treeObject{
		"line":   treeNumber(point.Line),
		"column": treeNumber(point.Column),
		"offset": treeNumber(point.Offset),
	}
}

// normalizeUpstream drops the fields of upstream's front matter node that are parser bookkeeping (its
// own start and end, which the position restates).
//
// Mutates: tree the fields are deleted in place, from the tree compareTree has just decoded for itself.
func normalizeUpstream(tree treeValue) {
	switch typed := tree.(type) {
	case treeObject:
		if typed["type"] == treeString("frontMatter") {
			delete(typed, "start")
			delete(typed, "end")
		}
		for _, value := range typed {
			normalizeUpstream(value)
		}
	case treeList:
		for _, value := range typed {
			normalizeUpstream(value)
		}
	}
}

// compareTree reports the first difference between upstream's tree and the Go tree for a text.
func compareTree(oracle *treeOracle, text string) string {
	expected, parseError := oracle.parse(text)
	if parseError != nil {
		return "upstream failed: " + parseError.Error()
	}
	convertOffsets(expected, byteOffsets(text, nil))
	normalizeUpstream(expected)

	root, parseMarkdownError := ParseMarkdown(text, nil)
	if parseMarkdownError != nil {
		return "the port failed: " + parseMarkdownError.Error()
	}
	actual, describeError := describeThroughJson(root)
	if describeError != nil {
		return "describing the port's tree: " + describeError.Error()
	}

	return firstTreeDifference("root", expected, actual)
}

// describeThroughJson is describe round-tripped through JSON, so the port's strings are read back the way
// upstream's are, invalid UTF-8 replaced on both sides.
func describeThroughJson(root *Node) (treeValue, error) {
	encoded, encodeError := json.Marshal(describe(root))
	if encodeError != nil {
		return nil, fmt.Errorf("encoding the tree: %w", encodeError)
	}
	return decodeTree(encoded)
}

func firstTreeDifference(path string, expected treeValue, actual treeValue) string {
	switch want := expected.(type) {
	case treeObject:
		got, isMap := actual.(treeObject)
		if !isMap {
			return fmt.Sprintf("%s: want an object, got %v", path, actual)
		}
		keys := map[string]bool{}
		for key := range want {
			keys[key] = true
		}
		for key := range got {
			keys[key] = true
		}
		sorted := make([]string, 0, len(keys))
		for key := range keys {
			sorted = append(sorted, key)
		}
		sort.Strings(sorted)
		// Type first, so a wrong node reads as a wrong node, not as a field mismatch.
		sort.SliceStable(
			sorted,
			func(left, right int) bool { return sorted[left] == "type" && sorted[right] != "type" },
		)
		for _, key := range sorted {
			wantValue, wantPresent := want[key]
			gotValue, gotPresent := got[key]
			if wantPresent != gotPresent {
				return fmt.Sprintf(
					"%s.%s: present upstream %v, in the port %v (want %v, got %v)",
					path,
					key,
					wantPresent,
					gotPresent,
					wantValue,
					gotValue,
				)
			}
			if difference := firstTreeDifference(path+"."+key, wantValue, gotValue); difference != "" {
				return difference
			}
		}
		return ""
	case treeList:
		got, isSlice := actual.(treeList)
		if !isSlice {
			return fmt.Sprintf("%s: want a list, got %v", path, actual)
		}
		for index := 0; index < len(want) || index < len(got); index++ {
			if index >= len(want) || index >= len(got) {
				return fmt.Sprintf("%s: want %d items, got %d", path, len(want), len(got))
			}
			if difference := firstTreeDifference(
				fmt.Sprintf("%s[%d]", path, index),
				want[index],
				got[index],
			); difference != "" {
				return difference
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(expected, actual) {
			return fmt.Sprintf("%s: want %#v, got %#v", path, expected, actual)
		}
		return ""
	}
}

var treeFixtures = []string{
	"",
	"a",
	"a\nb\n\nc",
	"# x",
	"---\ntitle: x\n---\n\nbody",
	"---\nt: é😀\n---\n\nbody é😀 x",
	"+++\na = 1\n+++\n",
	"é😀 a\n\nb",
	"***",
	"a  \nb\\\nc",
}

func TestTreesMatchUpstream(test *testing.T) {
	test.Parallel()
	oracle := newTreeOracle(test)
	for _, text := range treeFixtures {
		if difference := compareTree(oracle, text); difference != "" {
			test.Errorf("%q: %s", text, difference)
		}
	}
}

// TestTreeOracleCanFail: the comparison must see a wrong node and a wrong offset.
func TestTreeOracleCanFail(test *testing.T) {
	test.Parallel()
	oracle := newTreeOracle(test)
	expected, parseError := oracle.parse("*a*")
	if parseError != nil {
		test.Fatal(parseError)
	}
	root, parseMarkdownError := ParseMarkdown("**a**", nil)
	if parseMarkdownError != nil {
		test.Fatal(parseMarkdownError)
	}
	actual, describeError := describeThroughJson(root)
	if describeError != nil {
		test.Fatal(describeError)
	}
	if firstTreeDifference("root", expected, actual) == "" {
		test.Fatal("no difference found between the trees of *a* and **a**")
	}
}

// documentsDirectory is the markdown package's documents (see its documents_test.go): whole files built around
// tables, nested lists and embedded code, the constructs the corpus harness reported most.
var documentsDirectory = filepath.Join("..", "testdata", "documents")

// TestDocumentTreesMatchUpstream compares the tree of every document with upstream's recorded tree. It replaces
// TestCorpusTreesMatchUpstream, which walked whatever directory a variable named with the live oracle; these
// documents are in the repository, so every default run checks them.
func TestDocumentTreesMatchUpstream(test *testing.T) {
	test.Parallel()
	paths, globError := filepath.Glob(filepath.Join(documentsDirectory, "*.md"))
	if globError != nil {
		test.Fatal(globError)
	}
	if len(paths) < 6 {
		test.Fatalf("%s holds %d documents, fewer than the six it was written with", documentsDirectory, len(paths))
	}
	oracle := newTreeOracle(test)
	for _, path := range paths {
		contents, readError := os.ReadFile(path)
		if readError != nil {
			test.Fatal(readError)
		}
		if difference := compareTree(oracle, string(contents)); difference != "" {
			test.Errorf("%s: %s", filepath.Base(path), difference)
		}
	}
}
