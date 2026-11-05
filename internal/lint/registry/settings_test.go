package registry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// ourTailwindSettings is the block our three Tailwind repositories write, verbatim.
const ourTailwindSettings = `{
	"entryPoint": "./app/_theme/styles/theme.css",
	"attributes": ["class", "className"],
	"callees": ["mergeClassNames", "createVariantClassNames"],
	"variables": [".*[Cc]lassName$", ".*[Cc]lassNames$"]
}`

func settingsOf(namespaces map[string]string) rule.OptionsBase {
	settings := map[string]json.RawMessage{}
	for namespace, block := range namespaces {
		settings[namespace] = json.RawMessage(block)
	}
	return rule.OptionsBase{ConfigDirectory: "/repo", Settings: settings}
}

func TestCheckSettingsAcceptsWhatTheRunReads(test *testing.T) {
	test.Parallel()
	accepted := map[string]rule.OptionsBase{
		"no settings":             {ConfigDirectory: "/repo"},
		"our repositories' block": settingsOf(map[string]string{"better-tailwindcss": ourTailwindSettings}),
		"upstream's other spelling": settingsOf(
			map[string]string{"eslint-plugin-better-tailwindcss": `{"callees": ["cn"]}`},
		),
		"a key only one rule reads": settingsOf(map[string]string{"better-tailwindcss": `{"order": "desc"}`}),
		"every location key": settingsOf(
			map[string]string{"better-tailwindcss": `{"entryPoint": "a.css", "tailwindConfig": "b.css", "cwd": "web"}`},
		),
		"an empty block":                 settingsOf(map[string]string{"better-tailwindcss": `{}`}),
		"an empty list replacing a kind": settingsOf(map[string]string{"better-tailwindcss": `{"callees": []}`}),
	}
	for name, base := range accepted {
		if checkSettingsError := CheckSettings(base); checkSettingsError != nil {
			test.Errorf("%s was refused: %v", name, checkSettingsError)
		}
	}
}

// Each refusal names what it refuses, so the fix is one edit.
func TestCheckSettingsRefusesWhatWouldBeIgnored(test *testing.T) {
	test.Parallel()
	refused := []struct {
		name     string
		base     rule.OptionsBase
		mentions []string
	}{
		{"a plugin cohere reads no settings for", settingsOf(map[string]string{"react": `{"version": "19"}`}),
			[]string{`"react"`, `"better-tailwindcss"`}},
		{"both spellings at once", settingsOf(map[string]string{
			"better-tailwindcss":               `{}`,
			"eslint-plugin-better-tailwindcss": `{}`,
		}), []string{"both", "keep one"}},
		{
			"an upstream option not ported yet",
			settingsOf(map[string]string{"better-tailwindcss": `{"messageStyle": "compact"}`}),
			[]string{`"messageStyle"`, "ignored"},
		},
		{"a key no rule reads", settingsOf(map[string]string{"better-tailwindcss": `{"calees": ["cn"]}`}),
			[]string{`"calees"`}},
		{"a value of the wrong type", settingsOf(map[string]string{"better-tailwindcss": `{"callees": "cn"}`}),
			[]string{"better-tailwindcss/", "callees"}},
		{
			"a value upstream's schema refuses",
			settingsOf(map[string]string{"better-tailwindcss": `{"order": "sideways"}`}),
			[]string{"better-tailwindcss/enforce-consistent-class-order", "order"},
		},
		{"null", settingsOf(map[string]string{"better-tailwindcss": `{"collapse": null}`}),
			[]string{"collapse"}},
	}
	for _, testCase := range refused {
		checkSettingsError := CheckSettings(testCase.base)
		if checkSettingsError == nil {
			test.Errorf("%s loaded", testCase.name)
			continue
		}
		for _, mention := range testCase.mentions {
			if !strings.Contains(checkSettingsError.Error(), mention) {
				test.Errorf("%s: the refusal does not mention %s: %v", testCase.name, mention, checkSettingsError)
			}
		}
	}
}

// The settings reach a rule through the options registry the run builds, merged under the rule's own
// element: the path a real config takes, not the decoder alone.
//
// The rule the registry binds holds its options out of sight, so each part of the merge is shown by what the
// decode refuses beside an options element: a settings key the element leaves alone stays merged and is
// refused when it is the wrong type, and an element's own key overrides settings' wrong one. The control
// decodes the same shapes well typed, so each refusal is the wrong type and nothing else.
//
// A bare severity is not probed this way: the config layer runs a rule whose options only tune it as its
// Unconfigured when its decode of nothing fails (configuration.OptionsRegistry.Decode), so a wrong settings
// value there reads as no refusal. CheckSettings refuses that value before any rule decodes, which
// TestCheckSettingsRefusesWhatWouldBeIgnored covers.
func TestSettingsReachARuleThroughTheRunsOptions(test *testing.T) {
	test.Parallel()
	const ruleName = "better-tailwindcss/enforce-consistent-class-order"
	ownOrder := []json.RawMessage{json.RawMessage(`{"order": "asc"}`)}
	optionsWith := func(block string) configuration.OptionsRegistry {
		return OptionsAt(settingsOf(map[string]string{"better-tailwindcss": block}))
	}

	control := optionsWith(`{"order": "desc", "callees": ["mergeClassNames"]}`)
	if _, decodeError := control.Decode(ruleName, nil); decodeError != nil {
		test.Fatalf("well-typed settings were refused on a bare severity: %v", decodeError)
	}
	if _, decodeError := control.Decode(ruleName, ownOrder); decodeError != nil {
		test.Fatalf("well-typed settings were refused beside an options element: %v", decodeError)
	}

	misordered := optionsWith(`{"order": 7, "callees": ["mergeClassNames"]}`)
	if _, decodeError := misordered.Decode(ruleName, ownOrder); decodeError != nil {
		test.Errorf("an options element's order did not override settings' order of 7: %v", decodeError)
	}

	miscalled := optionsWith(`{"order": "desc", "callees": 7}`)
	if _, decodeError := miscalled.Decode(ruleName, ownOrder); decodeError == nil {
		test.Error("an options element decoded beside settings' callees of 7, so settings' callees were not kept")
	}
}
