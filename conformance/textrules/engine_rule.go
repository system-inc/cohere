package textrules

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// EngineRule is one engine's entry for a family in families.json, one of three: the rule's registered name,
// written as a string; `{"none": "<reason>"}`, where the language has no such idea; or `{"missing": true}`,
// where the verdict says the engine should have the rule and it does not yet, which a family-level ledger row
// names the task for.
type EngineRule struct {
	// Rule is the rule's registered name in its engine, or empty when None or Missing says why there is none.
	Rule string
	// None is why the language has no form of the family, or empty.
	None string
	// Missing is an engine that should have the family's rule and does not yet.
	Missing bool
}

// Runs reports whether the engine has a rule for the family, so its cases there are compared.
func (entry EngineRule) Runs() bool {
	return entry.Rule != ""
}

// UnmarshalJSON reads a rule name, `{"none": "<reason>"}` or `{"missing": true}`, and refuses anything else,
// an empty name or reason and a `missing` that is not true included.
func (entry *EngineRule) UnmarshalJSON(raw []byte) error {
	var name string
	if decodeError := json.Unmarshal(raw, &name); decodeError == nil {
		if strings.TrimSpace(name) == "" {
			return errors.New("the rule name is empty")
		}
		entry.Rule = name
		return nil
	}
	var written struct {
		None    *string `json:"none"`
		Missing *bool   `json:"missing"`
	}
	if decodeStrictlyError := decodeStrictly(raw, &written); decodeStrictlyError != nil {
		return fmt.Errorf(
			`an engine's entry is a rule name, {"none": "<reason>"} or {"missing": true}: %w`,
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
