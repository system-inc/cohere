package text_rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// EngineRule is one engine's entry for a family in families.json, one of three: the rules' registered names,
// written as a string for one rule or a list for several; `{"none": "<reason>"}`, where the language has no
// such idea; or `{"missing": true}`, where the verdict says the engine should have the rule and it does not
// yet, which a family-level ledger row names the task for.
//
// A list is for an engine whose language splits the family's judgment across rules that each own one shape:
// TypeScript's casing is a SCREAMING_SNAKE_CASE rule and a rule for every other name. The engine's findings
// for the family are the union of every listed rule's findings.
type EngineRule struct {
	// Rules are the rules' registered names in their engine, in the order written, or none when None or
	// Missing says why there are none.
	Rules []string
	// None is why the language has no form of the family, or empty.
	None string
	// Missing is an engine that should have the family's rule and does not yet.
	Missing bool
}

// Runs reports whether the engine has a rule for the family, so its cases there are compared.
func (entry EngineRule) Runs() bool {
	return len(entry.Rules) > 0
}

// String names the entry's rules as families.json writes them, joined.
func (entry EngineRule) String() string {
	return strings.Join(entry.Rules, ", ")
}

// UnmarshalJSON reads a rule name, a list of them, `{"none": "<reason>"}` or `{"missing": true}`, and refuses
// anything else: an empty name or reason, an empty list, a name listed twice, and a `missing` that is not
// true.
func (entry *EngineRule) UnmarshalJSON(raw []byte) error {
	var name string
	if decodeError := json.Unmarshal(raw, &name); decodeError == nil {
		if strings.TrimSpace(name) == "" {
			return errors.New("the rule name is empty")
		}
		entry.Rules = []string{name}
		return nil
	}
	var names []string
	if decodeError := json.Unmarshal(raw, &names); decodeError == nil {
		if len(names) == 0 {
			return errors.New(`a list of rules names none; write {"none": ...} or {"missing": true} instead`)
		}
		listed := map[string]bool{}
		for _, listedName := range names {
			if strings.TrimSpace(listedName) == "" {
				return errors.New("a listed rule name is empty")
			}
			if listed[listedName] {
				return fmt.Errorf("the rule %s is listed twice", listedName)
			}
			listed[listedName] = true
		}
		entry.Rules = names
		return nil
	}
	var written struct {
		None    *string `json:"none"`
		Missing *bool   `json:"missing"`
	}
	if decodeStrictlyError := decodeStrictly(raw, &written); decodeStrictlyError != nil {
		return fmt.Errorf(
			`an engine's entry is a rule name, a list of them, {"none": "<reason>"} or {"missing": true}: %w`,
			decodeStrictlyError,
		)
	}
	switch {
	case written.None != nil && written.Missing != nil:
		return errors.New(`an engine's entry is {"none": ...} or {"missing": true}, never both`)
	case written.Missing != nil:
		if !*written.Missing {
			return errors.New(`{"missing": false} says nothing; name the rule instead`)
		}
		entry.Missing = true
	case written.None == nil || strings.TrimSpace(*written.None) == "":
		return errors.New(`{"none": ...} needs the reason the language has no such rule`)
	default:
		entry.None = *written.None
	}
	return nil
}
