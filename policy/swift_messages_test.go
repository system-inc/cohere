package policy

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// TestSwiftMessagesTypeTheValuesAndTheOptions: a Swift message becomes a function whose parameters are its
// text's values and phrases in the order the sentence reads them, its terms in their Swift words, its
// string phrase in place, and its object phrase an enum whose cases carry their own values.
func TestSwiftMessagesTypeTheValuesAndTheOptions(test *testing.T) {
	test.Parallel()
	catalog, loadMessagesError := LoadMessages(fstest.MapFS{"consistency-no-thing.json": {Data: []byte(`{
  "rules": { "TypeScript": "base/consistency-no-thing", "Swift": "cohere-swift/consistency-no-thing" },
  "phrases": {
    "reasoning": "A thing hides what it is.",
    "exitCount": { "one": "", "several": ", the first of {{count}}" }
  },
  "messages": {
    "thing": {
      "text": "This is [[subject]] named {{name}}<<exitCount>>. <<reasoning>>",
      "terms": { "subject": { "TypeScript": "a {{kind}}", "Swift": "an enum" } }
    },
    "otherThing": { "text": "Another is {{name}}. <<reasoning>>" }
  }
}`)}})
	if loadMessagesError != nil {
		test.Fatal(loadMessagesError)
	}
	source, swiftMessagesSourceError := swiftMessagesSource(catalog, "digest")
	if swiftMessagesSourceError != nil {
		test.Fatal(swiftMessagesSourceError)
	}
	for _, want := range []string{
		`static let sourceDigest = "digest"`,
		`"ConsistencyNoThing.otherThing",` + "\n" + `        "ConsistencyNoThing.thing",`,
		"    /* cohere-swift/consistency-no-thing */\n    enum ConsistencyNoThing {",
		`static func otherThing(name: String) -> Message {
            Message(
                id: "otherThing",
                text:
                    #"Another is \#(name). A thing hides what it is."#,
            )`,
		"        enum ExitCount {\n            case one\n            case several(count: String)\n        }",
		`static func thing(name: String, exitCount: ExitCount) -> Message {`,
		"case .one:\n                        #\"\"#",
		"case .several(let count):\n                        #\", the first of \\#(count)\"#",
		"return Message(\n                id: \"thing\",\n                text:\n" +
			`                    #"This is an enum named \#(name)\#(exitCountText). A thing hides what it is."#,`,
	} {
		if !strings.Contains(string(source), want) {
			test.Errorf("the source does not hold %q:\n%s", want, source)
		}
	}
	if strings.Contains(string(source), "{{kind}}") || strings.Contains(string(source), "kind") {
		test.Errorf("the TypeScript term's value reached Swift:\n%s", source)
	}
}

// TestSwiftMessagesRefuseAValueWithTwoPlaces: a value in the text and in an option of the same message
// would be two parameters for one name, so generation stops and names it.
func TestSwiftMessagesRefuseAValueWithTwoPlaces(test *testing.T) {
	test.Parallel()
	catalog, loadMessagesError := LoadMessages(fstest.MapFS{"consistency-no-thing.json": {Data: []byte(`{
  "rules": { "Swift": "cohere-swift/consistency-no-thing" },
  "phrases": { "exitCount": { "one": "", "several": " of {{count}}" } },
  "messages": { "thing": { "text": "{{count}} things<<exitCount>>." } }
}`)}})
	if loadMessagesError != nil {
		test.Fatal(loadMessagesError)
	}
	if _, swiftMessagesSourceError := swiftMessagesSource(
		catalog,
		"digest",
	); swiftMessagesSourceError == nil || !strings.Contains(swiftMessagesSourceError.Error(), `"count"`) {
		test.Errorf("a value with two places was generated (%v)", swiftMessagesSourceError)
	}
}

// TestSwiftLiteralsOutrunTheText: a `"#` in the text stays text, a line break is the raw string's escape,
// and a value interpolates through the same delimiter.
func TestSwiftLiteralsOutrunTheText(test *testing.T) {
	test.Parallel()
	literal, swiftInterpolatedLiteralError := swiftInterpolatedLiteral("a \"# b\n{{name}}")
	if swiftInterpolatedLiteralError != nil {
		test.Fatal(swiftInterpolatedLiteralError)
	}
	if want := `##"a "# b\##n\##(name)"##`; literal != want {
		test.Errorf("got %s, expected %s", literal, want)
	}
}

// TestOneEditReachesBothEngines: one edited entry shows in the Go rule's rendered message and in the
// Swift source generated from the same files. The Swift test theFindingIsTheCatalogsMessage closes the
// chain from that source to the finding a user sees.
func TestOneEditReachesBothEngines(test *testing.T) {
	test.Parallel()
	edited := fstest.MapFS{}
	walkDirectoryError := fs.WalkDir(MessageFiles(), ".", func(name string, entry fs.DirEntry, walkError error) error {
		if walkError != nil || entry.IsDir() {
			return walkError
		}
		data, readFileError := fs.ReadFile(MessageFiles(), name)
		edited[name] = &fstest.MapFile{Data: data}
		return readFileError
	})
	if walkDirectoryError != nil {
		test.Fatal(walkDirectoryError)
	}
	const original = "which names no declared failure."
	const changed = "which names no failure anyone declared."
	file := edited["consistency-no-bare-throw.json"]
	if file == nil || !strings.Contains(string(file.Data), original) {
		test.Fatalf("bare-throw's shared clause %q is not in its file, so the edit would not apply", original)
	}
	file.Data = []byte(strings.Replace(string(file.Data), original, changed, 1))
	catalog, loadMessagesError := LoadMessages(edited)
	if loadMessagesError != nil {
		test.Fatal(loadMessagesError)
	}

	goText := catalog.Render(
		MessageHandle{Rule: "base/consistency-no-bare-throw", Id: "bareThrow"},
		map[string]string{"constructor": "Error"},
	)
	if !strings.Contains(goText, changed) {
		test.Errorf("the Go rule renders %q", goText)
	}

	source, swiftMessagesSourceError := swiftMessagesSource(catalog, "digest")
	if swiftMessagesSourceError != nil {
		test.Fatal(swiftMessagesSourceError)
	}
	function := string(source)[strings.Index(string(source), "static func bareThrow("):]
	if !strings.Contains(function[:strings.Index(function, "\n        }")], changed) {
		test.Errorf("the Swift function does not carry the edit:\n%s", source)
	}
}

// TestALongSignatureTakesOneParameterToALine: past the house format's 120 columns, the signature breaks
// as the format breaks it, so the formatter leaves the generated file as written.
func TestALongSignatureTakesOneParameterToALine(test *testing.T) {
	test.Parallel()
	catalog, loadMessagesError := LoadMessages(fstest.MapFS{"consistency-no-thing.json": {Data: []byte(`{
  "rules": { "Swift": "cohere-swift/consistency-no-thing" },
  "messages": {
    "thing": { "text": "{{target}} lacks {{features}}; add {{settings}} because {{reasons}}, {{explanation}}." },
    "short": { "text": "{{target}} is short." }
  }
}`)}})
	if loadMessagesError != nil {
		test.Fatal(loadMessagesError)
	}
	source, swiftMessagesSourceError := swiftMessagesSource(catalog, "digest")
	if swiftMessagesSourceError != nil {
		test.Fatal(swiftMessagesSourceError)
	}
	for _, want := range []string{
		"        static func thing(\n            target: String,\n            features: String,\n            settings: String,\n            reasons: String,\n            explanation: String,\n        ) -> Message {",
		"        static func short(target: String) -> Message {",
	} {
		if !strings.Contains(string(source), want) {
			test.Errorf("the source does not hold %q:\n%s", want, source)
		}
	}
}

// TestATwinWithNoSwiftMessageGetsNoType: a twin file whose messages are all TypeScript's names the Swift
// rule, and generates nothing for it rather than an empty enum.
func TestATwinWithNoSwiftMessageGetsNoType(test *testing.T) {
	test.Parallel()
	catalog, loadMessagesError := LoadMessages(fstest.MapFS{"consistency-no-thing.json": {Data: []byte(`{
  "rules": { "TypeScript": "nexus/consistency-no-thing", "Swift": "cohere-swift/consistency-no-thing" },
  "messages": { "thing": { "text": "A thing.", "languages": ["TypeScript"] } }
}`)}})
	if loadMessagesError != nil {
		test.Fatal(loadMessagesError)
	}
	source, swiftMessagesSourceError := swiftMessagesSource(catalog, "digest")
	if swiftMessagesSourceError != nil {
		test.Fatal(swiftMessagesSourceError)
	}
	if strings.Contains(string(source), "ConsistencyNoThing") {
		test.Errorf("a Swift rule with no Swift message got a type:\n%s", source)
	}
}
