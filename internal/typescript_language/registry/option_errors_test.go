package registry

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// goTypeName is how fmt's %T prints a named type of a rule package: `core.NoUnusedVariablesOptions`,
// `*tailwind.Options`. A user who misconfigured a rule should never read one.
var goTypeName = regexp.MustCompile(
	`\b(adamic|base|boundaries|core|next|nexus|react|structure|tailwind|typescript|rule)\.[A-Z][A-Za-z0-9]*`)

// TestAMisconfiguredRuleIsNamedByItsRuleNotItsGoType hands every rule that takes options a key no rule
// declares, and reads the refusal a user would see: it names the rule as the config spells it, and no Go
// type. DecodeOptionsInto once printed the options struct with %T, so a misspelled no-unused-vars option
// read `rule no-unused-vars: decoding core.NoUnusedVarsOptions: ...`.
func TestAMisconfiguredRuleIsNamedByItsRuleNotItsGoType(test *testing.T) {
	test.Parallel()
	registry := Options()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	unknownKey := []json.RawMessage{json.RawMessage(`{"cohereNoSuchOption": true}`)}
	refused := 0
	for _, name := range names {
		_, decodeError := registry.Decode(name, unknownKey)
		if decodeError == nil {
			continue
		}
		refused++
		message := decodeError.Error()
		if !strings.HasPrefix(message, "rule "+name+" ") && !strings.HasPrefix(message, "rule "+name+":") {
			test.Errorf("%s's refusal does not open with the rule's name: %s", name, message)
		}
		if typeName := goTypeName.FindString(message); typeName != "" {
			test.Errorf("%s's refusal names the Go type %s: %s", name, typeName, message)
		}
	}
	// The check can fail only on rules that refuse the key, so it must have read many, no-unused-vars among them.
	if refused < 100 {
		test.Fatalf("only %d rules refused an unknown option key, so the check read too few refusals", refused)
	}
	if _, decodeError := registry.Decode("no-unused-vars", unknownKey); decodeError == nil {
		test.Fatal("no-unused-vars accepted an option key it does not declare")
	}
}
