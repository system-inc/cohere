package configuration

import (
	"encoding/json"
	"strings"
	"testing"
)

type guardOptions struct {
	LibraryDirectory string   `json:"libraryDirectory"`
	Allowed          []string `json:"allowed"`
}

type tuningOptions struct {
	MaximumLineCount int `json:"maximumLineCount"`
}

// TestRequiredOptionsFailLoudlyWhenAbsent is the whole point of this file.
//
// boundary-no-project-import was enabled and inert for months under the gate cohere replaces: it
// declines every file when its LibraryDirectory is empty, which is correct behavior for a
// misconfigured guard and completely indistinguishable from a rule with nothing to report. A
// liveness harness reporting `fixtures=54 live=53 dead=1` was the only thing that ever noticed.
func TestRequiredOptionsFailLoudlyWhenAbsent(test *testing.T) {
	test.Parallel()
	registry := OptionsRegistry{
		"guard": {Decode: DecodeInto[guardOptions](), Required: true},
	}

	if _, decodeError := registry.Decode("guard", nil); decodeError == nil {
		test.Fatal(
			"a required option was absent and no error was raised, so the rule would decline every file in silence",
		)
	}

	decoded, decodeError := registry.Decode(
		"guard",
		[]json.RawMessage{json.RawMessage(`{"libraryDirectory":"/libraries/structure/"}`)},
	)
	if decodeError != nil {
		test.Fatalf("valid options failed to decode: %v", decodeError)
	}
	typed, isTyped := decoded.(guardOptions)
	if !isTyped {
		test.Fatalf(
			"options decoded to %T rather than the rule's own type, which fails its type assertion and makes it decline every file",
			decoded,
		)
	}
	if typed.LibraryDirectory != "/libraries/structure/" {
		test.Fatalf("the decoded value is wrong: %+v", typed)
	}
}

// TestTuningOptionsFallBackToDefaults covers the other half. A rule whose options only adjust it
// must run on its own defaults rather than fail, or every unconfigured tunable becomes a hard error.
func TestTuningOptionsFallBackToDefaults(test *testing.T) {
	test.Parallel()
	registry := OptionsRegistry{
		"tunable": {Decode: DecodeInto[tuningOptions]()},
	}

	decoded, decodeError := registry.Decode("tunable", nil)
	if decodeError != nil {
		test.Fatalf("an absent optional option was treated as fatal: %v", decodeError)
	}
	if decoded != nil {
		test.Fatalf("expected nil so the rule uses its own defaults, got %+v", decoded)
	}
}

// TestARuleWithNoDecoderGetsNil keeps the common case cheap: most rules take no options at all.
func TestARuleWithNoDecoderGetsNil(test *testing.T) {
	test.Parallel()
	decoded, decodeError := OptionsRegistry{}.Decode("plain", nil)
	if decodeError != nil {
		test.Fatalf("a rule with no decoder errored on a bare severity: %v", decodeError)
	}
	if decoded != nil {
		test.Fatalf("expected nil for a rule that takes no options, got %+v", decoded)
	}
}

// TestARuleWithNoDecoderRefusesAnOption is the other half. This used to answer nil for
// `{"ignored":true}`, and the key name was the defect stated as a fixture: an option written for a
// rule that reads none was accepted and had no effect.
func TestARuleWithNoDecoderRefusesAnOption(test *testing.T) {
	test.Parallel()
	_, decodeError := OptionsRegistry{}.Decode("plain", []json.RawMessage{json.RawMessage(`{"ignored":true}`)})
	if decodeError == nil {
		test.Fatal("an option given to a rule that takes none was accepted and would never be read")
	}
	for _, want := range []string{"plain", `element 1 {"ignored":true}`} {
		if !strings.Contains(decodeError.Error(), want) {
			test.Errorf("the refusal does not name %q, so the author cannot tell what to remove: %v", want, decodeError)
		}
	}
}

/*
 * A second element given to a rule that takes one is refused, naming the rule and the element.
 *
 * This is the guard for every rule with a single-element decoder, which is most of them. Before it,
 * `["error", {...}, {...}]` handed the decoder the first object, dropped the second, loaded clean and
 * ran. The guard lives in the config layer rather than in each decoder because a decoder never sees
 * the list it was sliced from, which is the boundary PortingARule.md section 7c is about.
 */
func TestASecondElementOnASingleElementRuleIsRefused(test *testing.T) {
	test.Parallel()
	registry := OptionsRegistry{"tunable": {Decode: DecodeInto[tuningOptions]()}}

	_, err := registry.Decode("tunable", []json.RawMessage{
		json.RawMessage(`{"maximumLineCount": 3}`),
		json.RawMessage(`{"enforceForRenamedProperties": true}`),
	})
	if err == nil {
		test.Fatal("a second option element was accepted by a rule that reads one, so it was dropped in silence")
	}
	for _, want := range []string{"tunable", `element 2 {"enforceForRenamedProperties": true}`} {
		if !strings.Contains(err.Error(), want) {
			test.Errorf("the refusal does not name %q: %v", want, err)
		}
	}

	// The control: the same rule with only its first element decodes, so the refusal above is about
	// the count and not about the first element.
	decoded, err := registry.Decode("tunable", []json.RawMessage{json.RawMessage(`{"maximumLineCount": 3}`)})
	if err != nil {
		test.Fatalf("one element on a one-element rule was refused: %v", err)
	}
	options, isTuning := decoded.(tuningOptions)
	if !isTuning {
		test.Fatalf("the single element decoded as %T, want tuningOptions", decoded)
	}
	if options.MaximumLineCount != 3 {
		test.Fatalf("the single element did not reach the decoder: %+v", decoded)
	}
}

// TestAListRuleReceivesEveryElement pins what DecodeList is handed: every element, as one JSON
// array, in the order written. And nothing at all for a bare severity, so the rule's own defaults
// apply.
func TestAListRuleReceivesEveryElement(test *testing.T) {
	test.Parallel()
	var received []string
	registry := OptionsRegistry{"listed": {DecodeList: func(raw json.RawMessage) (any, error) {
		received = append(received, string(raw))
		return string(raw), nil
	}}}

	if _, decodeError := registry.Decode("listed", []json.RawMessage{
		json.RawMessage(`"always"`),
		json.RawMessage(`{"null": "ignore"}`),
		json.RawMessage(`"third"`),
	}); decodeError != nil {
		test.Fatalf("a list rule refused its elements: %v", decodeError)
	}
	if _, decodeError := registry.Decode("listed", nil); decodeError != nil {
		test.Fatalf("a list rule with a bare severity errored: %v", decodeError)
	}

	want := []string{`["always",{"null":"ignore"},"third"]`, ``}
	if len(received) != len(want) {
		test.Fatalf("decoder called %d times, want %d", len(received), len(want))
	}
	for index := range want {
		if received[index] != want[index] {
			test.Errorf("call %d received %s, want %s", index, received[index], want[index])
		}
	}
}

// TestANullIsRefusedBeforeAnyDecoderSeesIt pins the seam: the refusal happens here, for both
// arities, so no decoder has to remember that Go reads a null as "leave the field alone". The
// decoders below accept anything, which is what proves the refusal is the config layer's.
func TestANullIsRefusedBeforeAnyDecoderSeesIt(test *testing.T) {
	test.Parallel()
	acceptAll := func(raw json.RawMessage) (any, error) { return string(raw), nil }
	registry := OptionsRegistry{"single": {Decode: acceptAll}, "listed": {DecodeList: acceptAll}}

	for _, testCase := range []struct {
		rule     string
		elements []string
		want     string
	}{
		{"single", []string{`null`}, "element 1 is null"},
		{"single", []string{`{"b": 1, "a": {"c": [0, null]}}`}, "element 1 at a.c.1 is null"},
		{"listed", []string{`"always"`, `{"null": null}`}, "element 2 at null is null"},
	} {
		elements := make([]json.RawMessage, 0, len(testCase.elements))
		for _, element := range testCase.elements {
			elements = append(elements, json.RawMessage(element))
		}
		_, decodeError := registry.Decode(testCase.rule, elements)
		if decodeError == nil {
			test.Errorf("%s %v was accepted", testCase.rule, testCase.elements)
			continue
		}
		for _, want := range []string{testCase.rule, testCase.want} {
			if !strings.Contains(decodeError.Error(), want) {
				test.Errorf(
					"%s %v: the refusal does not name %q: %v",
					testCase.rule,
					testCase.elements,
					want,
					decodeError,
				)
			}
		}
	}

	// The control: the string "null" and a key named null are values, not nulls, and pass through.
	if _, decodeError := registry.Decode(
		"listed",
		[]json.RawMessage{json.RawMessage(`"null"`), json.RawMessage(`{"null": "ignore"}`)},
	); decodeError != nil {
		test.Errorf("a string and a key spelled null were refused: %v", decodeError)
	}
}

// TestMalformedOptionsAreAnError guards against a rule silently receiving a zero struct because its
// JSON did not parse.
func TestMalformedOptionsAreAnError(test *testing.T) {
	test.Parallel()
	registry := OptionsRegistry{"guard": {Decode: DecodeInto[guardOptions](), Required: true}}

	if _, decodeError := registry.Decode(
		"guard",
		[]json.RawMessage{json.RawMessage(`{"libraryDirectory": 42}`)},
	); decodeError == nil {
		test.Fatal("options of the wrong JSON type decoded successfully")
	}
}

// TestUnconfiguredIsNotScopedOff pins the distinction the coverage line depends on.
//
// "Someone turned this rule off" and "nobody has said whether this rule should run" are different
// facts. Reporting them as one describes a brand-new rule as though it had been deliberately
// excluded, which is a lie about who decided what.
func TestUnconfiguredIsNotScopedOff(test *testing.T) {
	test.Parallel()
	configuration := &Config{
		Rules: map[string]RuleSetting{
			"configured-on":  {Severity: SeverityError},
			"configured-off": {Severity: SeverityOff},
		},
	}
	resolved := configuration.Resolve("File.ts")

	if status, _ := resolved.StatusOf("configured-on"); status != StatusEnabled {
		test.Fatalf("an enabled rule reported status %d", status)
	}
	if status, _ := resolved.StatusOf("configured-off"); status != StatusScopedOff {
		test.Fatalf("a rule the config turned off reported status %d rather than scoped-off", status)
	}
	if status, _ := resolved.StatusOf("never-mentioned"); status != StatusUnconfigured {
		test.Fatalf("a rule absent from the config reported status %d rather than unconfigured", status)
	}

	ignoring := &Config{IgnorePatterns: []string{"**"}}
	if status, _ := ignoring.Resolve("File.ts").StatusOf("anything"); status != StatusFileIgnored {
		test.Fatal("a rule on an ignored file did not report the file as the reason")
	}
}

// TestTheLiveGuardRuleGetsItsOptions is the specific rule that was inert, against the real configuration.
func TestTheLiveGuardRuleGetsItsOptions(test *testing.T) {
	test.Parallel()
	loaded, loadError := Load(liveConfigPath(test))
	if loadError != nil {
		test.Fatalf("loading the live config: %v", loadError)
	}

	elements := loaded.Resolve("libraries/structure/source/api/Fetch.ts").RawOptionsFor("boundary-no-project-import")
	if len(elements) == 0 {
		test.Fatal(
			"boundary-no-project-import received no options from the live config, which is the inert-rule defect",
		)
	}
	if len(elements) != 1 {
		test.Fatalf(
			"boundary-no-project-import takes one option element and the live config gives it %d",
			len(elements),
		)
	}

	var typed guardOptions
	if decodeError := json.Unmarshal(elements[0], &typed); decodeError != nil {
		test.Fatalf("the live options did not decode: %v", decodeError)
	}
	if typed.LibraryDirectory != "/libraries/structure/" {
		test.Fatalf("libraryDirectory did not survive the round trip: %q", typed.LibraryDirectory)
	}
}
