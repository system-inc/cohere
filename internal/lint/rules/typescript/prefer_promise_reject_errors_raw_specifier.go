package typescript

import (
	"encoding/json"

	"github.com/system-inc/cohere/internal/lint/rule"
)

// preferPromiseRejectErrorsRawSpecifier is one entry of the allowlist, in either wire form.
type preferPromiseRejectErrorsRawSpecifier struct {
	inline string

	From    string
	Name    []string
	Path    string
	Package string
}

// UnmarshalJSON accepts both wire forms: a bare string, or the specifier object.
//
// `name` is itself two shapes, a string or an array of strings. An unrecognized `from` is dropped
// rather than erroring, matching how the sibling rules treat the same field: the specifier then
// matches nothing instead of failing the run, and dropping it says nothing rather than silently
// meaning `file`, which is the zero value.
func (specifier *preferPromiseRejectErrorsRawSpecifier) UnmarshalJSON(raw []byte) error {
	var asString string
	if decodeError := json.Unmarshal(raw, &asString); decodeError == nil {
		*specifier = preferPromiseRejectErrorsRawSpecifier{inline: asString}
		return nil
	}

	var object struct {
		From    string          `json:"from"`
		Name    json.RawMessage `json:"name"`
		Path    string          `json:"path"`
		Package string          `json:"package"`
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &object); unmarshalOptionsError != nil {
		return unmarshalOptionsError
	}

	*specifier = preferPromiseRejectErrorsRawSpecifier{From: object.From, Path: object.Path, Package: object.Package}

	var names []string
	if decodeError := json.Unmarshal(object.Name, &names); decodeError == nil {
		specifier.Name = names
		return nil
	}
	var name string
	if decodeError := json.Unmarshal(object.Name, &name); decodeError != nil {
		return decodeError
	}
	specifier.Name = []string{name}
	return nil
}
