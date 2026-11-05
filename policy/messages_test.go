package policy

import (
	"strings"
	"testing"
	"testing/fstest"
)

// minimalMessages is a twin pair's file every refusal below starts from, changed in one place, so each
// refusal is the one under test rather than some other.
const minimalMessages = `{
  "rules": { "TypeScript": "base/consistency-no-thing", "Swift": "cohere-swift/consistency-no-thing" },
  "messages": {
    "thing": {
      "text": "This is [[subject]], which names {{count}} things.",
      "terms": { "subject": { "TypeScript": "a {{kind}}", "Swift": "an enum" } }
    }
  }
}`

func loadMinimalMessages(text string) (*MessageCatalog, error) {
	return LoadMessages(fstest.MapFS{"consistency-no-thing.json": {Data: []byte(text)}})
}

// TestTheMessageLoaderRefusesWhatWouldReadAsSomethingElse: the minimal file loads, and each one-place
// change that would misread does not.
func TestTheMessageLoaderRefusesWhatWouldReadAsSomethingElse(test *testing.T) {
	test.Parallel()
	if _, loadMinimalMessagesError := loadMinimalMessages(minimalMessages); loadMinimalMessagesError != nil {
		test.Fatalf(
			"the minimal file is refused (%v), so no refusal below would mean anything",
			loadMinimalMessagesError,
		)
	}
	for _, refused := range []struct {
		name string
		old  string
		new  string
	}{
		{"an unknown key", `"messages": {`, `"descriptions": {}, "messages": {`},
		{"an unknown key in a message", `"text": "This`, `"words": "", "text": "This`},
		{"no rules", `"TypeScript": "base/consistency-no-thing", "Swift": "cohere-swift/consistency-no-thing"`, ``},
		{"a language neither engine is", `"Swift": "cohere`, `"Kotlin": "cohere`},
		{"a rule with no namespace", `"base/consistency-no-thing"`, `"consistency-no-thing"`},
		{"a file named for another idea", `"base/consistency-no-thing"`, `"base/consistency-no-other"`},
		{"an id that is not camelCase", `"thing": {`, `"Thing": {`},
		{"an empty text", `"This is [[subject]], which names {{count}} things."`, `" "`},
		{"a term with no words for a language", `, "Swift": "an enum"`, ``},
		{
			"a term in words for a language with no rule",
			`"Swift": "an enum"`,
			`"Swift": "an enum", "Kotlin": "a sealed class"`,
		},
		{"a term no text uses", `"text": "This is [[subject]],`, `"text": "This is a thing,`},
		{"a term the file does not define", `[[subject]]`, `[[object]]`},
		{"a term inside a term", `"Swift": "an enum"`, `"Swift": "an [[subject]]"`},
		{"an open with no close", `{{count}}`, `{{count`},
		{"a close with no open", `{{count}}`, `count}}`},
		{"a placeholder that is not camelCase", `{{count}}`, `{{the count}}`},
		{"no message", `"thing": {
      "text": "This is [[subject]], which names {{count}} things.",
      "terms": { "subject": { "TypeScript": "a {{kind}}", "Swift": "an enum" } }
    }`, ``},
	} {
		if !strings.Contains(minimalMessages, refused.old) {
			test.Fatalf(
				"%s: the anchor %q is not in the minimal file, so the change would not apply",
				refused.name,
				refused.old,
			)
		}
		if _, loadMinimalMessagesError := loadMinimalMessages(
			strings.Replace(minimalMessages, refused.old, refused.new, 1),
		); loadMinimalMessagesError == nil {
			test.Errorf("%s loaded", refused.name)
		}
	}
}

// A lone house rule has no twin to differ from, so a term on one is refused, and without terms it loads.
func TestALoneRuleCarriesNoTerms(test *testing.T) {
	test.Parallel()
	lone := `{"rules": {"TypeScript": "base/consistency-no-thing"}, "messages": {"thing": {"text": "This is %s."}}}`
	if _, loadMinimalMessagesError := loadMinimalMessages(
		strings.Replace(lone, "%s", "a thing", 1),
	); loadMinimalMessagesError != nil {
		test.Fatalf("a lone rule with no terms is refused: %v", loadMinimalMessagesError)
	}
	withTerm := `{"rules": {"TypeScript": "base/consistency-no-thing"}, "messages": {"thing": {"text": "This is [[subject]].", "terms": {"subject": {"TypeScript": "a thing"}}}}}`
	if _, loadMinimalMessagesError := loadMinimalMessages(withTerm); loadMinimalMessagesError == nil {
		test.Error("a lone rule with a term loaded")
	}
}

// Go joins a file the way Swift did: a lone Go rule's file is named for its own idea, a file Go joins keeps
// the TypeScript rule's name, and Go's text resolves Go's words for each term.
func TestGoNamesALoneFileAndJoinsATwinWithItsOwnTerms(test *testing.T) {
	test.Parallel()
	lone := `{"rules": {"Go": "cohere-go/consistency-no-thing"}, "messages": {"thing": {"text": "This is a thing."}}}`
	if _, loadError := loadMinimalMessages(lone); loadError != nil {
		test.Fatalf("a lone Go rule's file, named for its idea, is refused: %v", loadError)
	}
	if _, loadError := LoadMessages(fstest.MapFS{"consistency-no-other.json": {Data: []byte(lone)}}); loadError == nil {
		test.Error("a lone Go rule's file named for another idea loaded")
	}

	triplet := strings.NewReplacer(
		`"Swift": "cohere-swift/consistency-no-thing" }`,
		`"Swift": "cohere-swift/consistency-no-thing", "Go": "cohere-go/consistency-no-thing" }`,
		`"Swift": "an enum" }`,
		`"Swift": "an enum", "Go": "a constant set" }`,
	).Replace(minimalMessages)
	if triplet == minimalMessages {
		test.Fatal("the replacement matched nothing, so the file under test is the twin, not the triplet")
	}
	catalog, loadError := loadMinimalMessages(triplet)
	if loadError != nil {
		test.Fatalf("a twin Go joined is refused: %v", loadError)
	}
	goHandle := MessageHandle{Rule: "cohere-go/consistency-no-thing", Id: "thing"}
	if got, want := catalog.Render(
		goHandle,
		map[string]string{"count": "3"},
	), "This is a constant set, which names 3 things."; got != want {
		test.Errorf("Go renders %q, want %q", got, want)
	}
	if len(catalog.MessagesFor(MessageLanguageGo)) != 1 {
		test.Errorf("MessagesFor(Go) is %v, want the one Go message", catalog.MessagesFor(MessageLanguageGo))
	}
}

// A rule named by two files is refused, wherever the second one is.
func TestARuleNamedByTwoFilesIsRefused(test *testing.T) {
	test.Parallel()
	second := strings.Replace(minimalMessages, `"base/consistency-no-thing"`, `"base/consistency-no-other"`, 1)
	_, loadMessagesError := LoadMessages(fstest.MapFS{
		"consistency-no-thing.json": {Data: []byte(minimalMessages)},
		"consistency-no-other.json": {Data: []byte(second)},
	})
	if loadMessagesError == nil {
		test.Error("cohere-swift/consistency-no-thing named by two files loaded")
	}
}

// Each language's text resolves its own terms, and Render puts in exactly the values that text uses.
func TestEachLanguageRendersItsOwnTermsAndValues(test *testing.T) {
	test.Parallel()
	catalog, loadMinimalMessagesError := loadMinimalMessages(minimalMessages)
	if loadMinimalMessagesError != nil {
		test.Fatal(loadMinimalMessagesError)
	}
	typeScript := MessageHandle{Rule: "base/consistency-no-thing", Id: "thing"}
	if got, want := catalog.Render(
		typeScript,
		map[string]string{"kind": "class", "count": "3"},
	), "This is a class, which names 3 things."; got != want {
		test.Errorf("TypeScript renders %q, want %q", got, want)
	}
	swift := MessageHandle{Rule: "cohere-swift/consistency-no-thing", Id: "thing"}
	if got, want := catalog.Render(
		swift,
		map[string]string{"count": "3"},
	), "This is an enum, which names 3 things."; got != want {
		test.Errorf("Swift renders %q, want %q", got, want)
	}

	for name, values := range map[string]map[string]string{
		"a value missing":          {"count": "3"},
		"a value the text lacks":   {"kind": "class", "count": "3", "extra": "x"},
		"a value under a misspelt": {"kind": "class", "cont": "3"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					test.Errorf("%s rendered", name)
				}
			}()
			catalog.Render(typeScript, values)
		}()
	}
}

// MessageOf refuses a message the catalog does not hold, when the rule's package initializes.
func TestMessageOfRefusesAMissingMessage(test *testing.T) {
	test.Parallel()
	defer func() {
		if recover() == nil {
			test.Error("a handle on a missing message was taken")
		}
	}()
	MessageOf("base/consistency-no-bare-throw", "noSuchMessage")
}

// The embedded catalog loaded at init, and holds the bare-throw pair for both languages.
func TestTheEmbeddedCatalogHoldsBothTwins(test *testing.T) {
	test.Parallel()
	for _, language := range []string{MessageLanguageTypeScript, MessageLanguageSwift} {
		if len(Messages.MessagesFor(language)) == 0 {
			test.Errorf("no %s messages in the embedded catalog", language)
		}
	}
}

// minimalPhrases is a lone rule's file with both kinds of phrase, every refusal below starting from it.
const minimalPhrases = `{
  "rules": { "TypeScript": "base/consistency-no-thing" },
  "phrases": {
    "shared": "Both say this.",
    "reason": { "constructor": "it is a constructor", "setter": "it is a setter for {{name}}" }
  },
  "messages": {
    "first": { "text": "First, because <<reason>>. <<shared>>" },
    "second": { "text": "Second. <<shared>>" }
  }
}`

// TestThePhraseLoaderRefusesWhatWouldReadAsSomethingElse: the file with phrases loads, and each one-place
// change that would misread does not.
func TestThePhraseLoaderRefusesWhatWouldReadAsSomethingElse(test *testing.T) {
	test.Parallel()
	if _, loadMinimalMessagesError := loadMinimalMessages(minimalPhrases); loadMinimalMessagesError != nil {
		test.Fatalf(
			"the minimal file is refused (%v), so no refusal below would mean anything",
			loadMinimalMessagesError,
		)
	}
	for _, refused := range []struct {
		name string
		old  string
		new  string
	}{
		{"a phrase no message defines", `<<reason>>`, `<<cause>>`},
		{"a string phrase only one message uses", `"Second. <<shared>>"`, `"Second."`},
		{"an object phrase no message uses", `"First, because <<reason>>. <<shared>>"`, `"First. <<shared>>"`},
		{"a phrase inside a phrase", `"Both say this."`, `"Both say <<reason>>."`},
		{"a term inside a phrase", `"Both say this."`, `"Both say [[this]]."`},
		{
			"an object with no option",
			`{ "constructor": "it is a constructor", "setter": "it is a setter for {{name}}" }`,
			`{}`,
		},
		{"an option that is not camelCase", `"setter":`, `"Setter":`},
		{"a phrase that is not camelCase", `"shared": "Both`, `"Shared": "Both`},
		{"a phrase of another shape", `"shared": "Both say this."`, `"shared": ["Both say this."]`},
		{"a malformed value in an option", `{{name}}`, `{{name`},
	} {
		if !strings.Contains(minimalPhrases, refused.old) {
			test.Fatalf(
				"%s: the anchor %q is not in the minimal file, so the change would not apply",
				refused.name,
				refused.old,
			)
		}
		if _, loadMinimalMessagesError := loadMinimalMessages(
			strings.Replace(minimalPhrases, refused.old, refused.new, 1),
		); loadMinimalMessagesError == nil {
			test.Errorf("%s loaded", refused.name)
		}
	}
}

// A string phrase is put in at load, and an object phrase is the option the rule picks, with that
// option's values, and nothing else.
func TestPhrasesRenderTheSharedTextAndThePickedOption(test *testing.T) {
	test.Parallel()
	catalog, loadMinimalMessagesError := loadMinimalMessages(minimalPhrases)
	if loadMinimalMessagesError != nil {
		test.Fatal(loadMinimalMessagesError)
	}
	first := MessageHandle{Rule: "base/consistency-no-thing", Id: "first"}
	second := MessageHandle{Rule: "base/consistency-no-thing", Id: "second"}
	setter := catalog.Option(first, "reason", "setter")
	constructor := catalog.Option(first, "reason", "constructor")
	if got, want := catalog.Render(
		first,
		map[string]string{"name": "value"},
		setter,
	), "First, because it is a setter for value. Both say this."; got != want {
		test.Errorf("the setter option renders %q, want %q", got, want)
	}
	if got, want := catalog.Render(
		first,
		nil,
		constructor,
	), "First, because it is a constructor. Both say this."; got != want {
		test.Errorf("the constructor option renders %q, want %q", got, want)
	}
	if got, want := catalog.Render(second, nil), "Second. Both say this."; got != want {
		test.Errorf("the shared phrase renders %q, want %q", got, want)
	}

	for name, render := range map[string]func(){
		"no option":                  func() { catalog.Render(first, nil) },
		"two options for one phrase": func() { catalog.Render(first, nil, constructor, constructor) },
		"another message's option":   func() { catalog.Render(second, nil, constructor) },
		"the option's value missing": func() { catalog.Render(first, nil, setter) },
		"a value the picked option lacks": func() {
			catalog.Render(
				first,
				map[string]string{"name": "value"},
				constructor,
			)
		},
		"an option the catalog does not hold": func() { catalog.Option(first, "reason", "getter") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					test.Errorf("%s rendered", name)
				}
			}()
			render()
		}()
	}
}

// A twin file can hold a message only one engine renders, named by its languages.
func TestAMessageCanBeRenderedInOneLanguageOfATwinFile(test *testing.T) {
	test.Parallel()
	withOneLanguage := strings.Replace(minimalMessages, `"messages": {`, `"messages": {
    "onlyHere": { "text": "Only TypeScript says {{this}}.", "languages": ["TypeScript"] },`, 1)
	catalog, loadMinimalMessagesError := loadMinimalMessages(withOneLanguage)
	if loadMinimalMessagesError != nil {
		test.Fatalf("a twin file with a TypeScript-only message is refused: %v", loadMinimalMessagesError)
	}
	if _, rendered := catalog.templates["cohere-swift/consistency-no-thing"]["onlyHere"]; rendered {
		test.Error("the Swift rule holds a message only TypeScript renders")
	}
	if _, rendered := catalog.templates["base/consistency-no-thing"]["onlyHere"]; !rendered {
		test.Error("the TypeScript rule does not hold its own message")
	}
	for _, refused := range []struct {
		name string
		old  string
		new  string
	}{
		{"a language the file names no rule for", `["TypeScript"]`, `["Kotlin"]`},
		{"a language twice", `["TypeScript"]`, `["TypeScript", "TypeScript"]`},
		{"no language", `["TypeScript"]`, `[]`},
		{
			"a term on a one-language message",
			`"languages": ["TypeScript"]`,
			`"languages": ["TypeScript"], "terms": {"x": {"TypeScript": "y"}}`,
		},
	} {
		if _, loadMinimalMessagesError := loadMinimalMessages(
			strings.Replace(withOneLanguage, refused.old, refused.new, 1),
		); loadMinimalMessagesError == nil {
			test.Errorf("%s loaded", refused.name)
		}
	}
	twinTerm := strings.Replace(
		minimalMessages,
		`"terms": { "subject": { "TypeScript": "a {{kind}}", "Swift": "an enum" } }`,
		`"terms": { "subject": { "TypeScript": "a {{kind}}", "Swift": "an enum" } }, "languages": ["TypeScript"]`,
		1,
	)
	if _, loadMinimalMessagesError := loadMinimalMessages(twinTerm); loadMinimalMessagesError == nil {
		test.Error("a term giving words for a language the message is not rendered in loaded")
	}
}

// optionsByLanguage is a twin file whose object phrase gives one option's text by language: `variable`
// for both engines, each in its own words, and `storeEntry` for TypeScript only.
const optionsByLanguage = `{
  "rules": { "TypeScript": "base/consistency-no-thing", "Swift": "cohere-swift/consistency-no-thing" },
  "phrases": {
    "source": {
      "everywhere": "from anywhere",
      "variable": { "TypeScript": "from {{target}}", "Swift": "from the property {{target}}" },
      "storeEntry": { "TypeScript": "from what {{read}} returned" }
    }
  },
  "messages": {
    "lostUpdate": { "text": "This write is computed <<source>>." }
  }
}`

// An option's text may be given by language, as a term's is. Each language renders its own, and an
// option with no text for a language does not reach it, so that language's rule cannot pick it.
func TestAnOptionCanGiveItsTextByLanguage(test *testing.T) {
	test.Parallel()
	catalog, loadMinimalMessagesError := loadMinimalMessages(optionsByLanguage)
	if loadMinimalMessagesError != nil {
		test.Fatalf("an option with text by language is refused: %v", loadMinimalMessagesError)
	}
	typeScript := MessageHandle{Rule: "base/consistency-no-thing", Id: "lostUpdate"}
	swift := MessageHandle{Rule: "cohere-swift/consistency-no-thing", Id: "lostUpdate"}
	for _, rendered := range []struct {
		handle MessageHandle
		option string
		values map[string]string
		want   string
	}{
		{typeScript, "everywhere", nil, "This write is computed from anywhere."},
		{swift, "everywhere", nil, "This write is computed from anywhere."},
		{typeScript, "variable", map[string]string{"target": "total"}, "This write is computed from total."},
		{swift, "variable", map[string]string{"target": "total"}, "This write is computed from the property total."},
		{
			typeScript,
			"storeEntry",
			map[string]string{"read": "cache.get(key)"},
			"This write is computed from what cache.get(key) returned.",
		},
	} {
		if got := catalog.Render(
			rendered.handle,
			rendered.values,
			catalog.Option(rendered.handle, "source", rendered.option),
		); got != rendered.want {
			test.Errorf("%s %s renders %q, want %q", rendered.handle.Rule, rendered.option, got, rendered.want)
		}
	}
	func() {
		defer func() {
			if recover() == nil {
				test.Error("the Swift rule took a TypeScript-only option")
			}
		}()
		catalog.Option(swift, "source", "storeEntry")
	}()

	for _, refused := range []struct {
		name string
		old  string
		new  string
	}{
		{
			"an option in a language the file names no rule for",
			`"Swift": "from the property {{target}}"`,
			`"Kotlin": "from the property {{target}}"`,
		},
		{"an option with text for no language", `{ "TypeScript": "from what {{read}} returned" }`, `{}`},
		{
			"an option of another shape",
			`{ "TypeScript": "from what {{read}} returned" }`,
			`["from what {{read}} returned"]`,
		},
		{"a phrase with no option in a language a message renders it in", `"everywhere": "from anywhere",
      "variable": { "TypeScript": "from {{target}}", "Swift": "from the property {{target}}" },`, ``},
		{"a term inside an option's language text", `"from the property {{target}}"`, `"from the [[kind]] {{target}}"`},
		{"a phrase inside an option's language text", `"from the property {{target}}"`, `"from <<source>>"`},
		{
			"a malformed value in an option's language text",
			`"from the property {{target}}"`,
			`"from the property {{target"`,
		},
	} {
		if !strings.Contains(optionsByLanguage, refused.old) {
			test.Fatalf(
				"%s: the anchor %q is not in the file, so the change would not apply",
				refused.name,
				refused.old,
			)
		}
		if _, loadMinimalMessagesError := loadMinimalMessages(
			strings.Replace(optionsByLanguage, refused.old, refused.new, 1),
		); loadMinimalMessagesError == nil {
			test.Errorf("%s loaded", refused.name)
		}
	}
}
